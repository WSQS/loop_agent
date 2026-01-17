package main

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/WSQS/loop_agent/assets/prompts"
)

const promptTp = `
目前按照@SPEC.md 的定义，添加了检测，并@{{validate_script}}会因为未实现功能失败，请实现对应功能，相关日志如下：
{{FAIL}}
`

type Singleton struct {
	iteration      int
	attemptCount   int
	dir            string
	validateScript string
	baseOutput     io.Writer
}

var (
	instance *Singleton
	once     sync.Once
)

func GetInstance() *Singleton {
	once.Do(func() {
		instance = &Singleton{}
	})
	return instance
}

func trace(tag string) func() {
	start := time.Now()
	log.Println("["+tag+"]", "begin")
	return func() {
		log.Println("["+tag+"]", "end", "seconds:", time.Since(start).Seconds())
	}
}

func execute(cmd *exec.Cmd, tag string) {
	defer trace(tag)()
	iterationDir := GetInstance().dir + "/iter-" + strconv.Itoa(GetInstance().iteration)
	f, err := os.OpenFile(filepath.Join(iterationDir, tag+".txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	log.SetOutput(io.MultiWriter(GetInstance().baseOutput, f))
	defer log.SetOutput(GetInstance().baseOutput)
	command := cmd.String()
	log.Println("[EXEC] command:", command)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Fatal(err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			log.Println("["+tag+"-STDOUT]", sc.Text())
		}
	}()

	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			log.Println("["+tag+"-STDERR]", sc.Text())
		}
	}()

	err = cmd.Wait()
	if err != nil {
		log.Fatalln("[EXEC]", command, "error:", err)
	}
}

func is_repo_dirty() (bool, []byte) {
	cmd := exec.Command("git", "status", "--porcelain=v1")
	command := cmd.String()
	output, err := cmd.Output()
	if err != nil {
		log.Fatalln("[EXEC]", command, "error:", err)
	}
	is_dirty := len(output) > 0
	return is_dirty, output
}

func cleanup() {
	if dirty_flag, _ := is_repo_dirty(); !dirty_flag {
		return
	}
	tag := "ITER-" + strconv.Itoa(GetInstance().iteration) + "-" + "CLEANUP" + "-" + strconv.Itoa(GetInstance().attemptCount)
	log.Println("[" + tag + "] Repo is dirty, clean up")
	for ; ; GetInstance().attemptCount++ {
		dirty, files := is_repo_dirty()
		if !dirty {
			break
		}
		cmd := exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
		cleanupByte, err := prompts.FS.ReadFile("cleanup_prompt.txt")
		if err != nil {
			log.Fatal(err)
		}
		prompt := string(cleanupByte)

		iterationDir := GetInstance().dir + "/iter-" + strconv.Itoa(GetInstance().iteration)
		prompt = strings.ReplaceAll(prompt, "{{files}}", string(files))
		prompt = strings.ReplaceAll(prompt, "{{iteration}}", strconv.Itoa(GetInstance().iteration))
		prompt = strings.ReplaceAll(prompt, "{{attempt}}", strconv.Itoa(GetInstance().attemptCount))
		os.WriteFile(iterationDir+"/cleanup-"+strconv.Itoa(GetInstance().attemptCount)+"-prompt.txt", []byte(prompt), 0644)
		cmd.Stdin = strings.NewReader(prompt)
		execute(cmd, tag)
	}
}

func validate() (int, string) {
	defer trace("VALIDATE")()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, GetInstance().validateScript)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			return exitCode, output
		}
		return -1, err.Error() + output
	}
	return 0, output
}

func main() {
	timestamp := time.Now().Format("060102150405")
	GetInstance().dir = ".loop_agent/" + timestamp
	if err := os.MkdirAll(GetInstance().dir, 0755); err != nil {
		log.Fatal(err)
	}
	f, err := os.OpenFile(GetInstance().dir+"/log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	GetInstance().baseOutput = io.MultiWriter(os.Stdout, f)
	log.SetOutput(GetInstance().baseOutput)
	log.Println("[LOG] Log in ", GetInstance().dir)
	if runtime.GOOS == "windows" {
		GetInstance().validateScript = ".\\validate.bat"
	} else {
		GetInstance().validateScript = "./validate.sh"
	}
	log.Println("[OS]", "Running on:", runtime.GOOS, "using:", GetInstance().validateScript)
	cmd := exec.Command("git", "status")
	execute(cmd, "GIT-STATUS")
	cmd = exec.Command("git", "checkout", "-b", "ai/gen/loop-"+timestamp)
	execute(cmd, "GIT-CHECKOUT")
	for GetInstance().iteration = 1; GetInstance().iteration < 500; GetInstance().iteration++ {
		func() {
			iterTag := "ITER-" + strconv.Itoa(GetInstance().iteration)
			defer trace(iterTag)()

			GetInstance().attemptCount = 1

			iterationDir := GetInstance().dir + "/iter-" + strconv.Itoa(GetInstance().iteration)
			if err := os.MkdirAll(iterationDir, 0755); err != nil {
				log.Fatal(err)
			}

			cleanup()

			cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt", "/init")
			execute(cmd, iterTag+"-IFLOW-INIT")

			cleanup()

			var task string

			for i := 1; ; i++ {
				files, err := os.ReadDir("./tasks")
				if err != nil {
					log.Fatalln("[FILE]", err.Error())
				}
				var tasks []string
				for _, f := range files {
					if f.IsDir() {
						continue
					}
					name := "./tasks/" + f.Name()
					tasks = append(tasks, name)
				}

				if len(tasks) == 0 {
					createTastPrompt := `基于项目现状在tasks目录中创建一个新任务，定义一个适当的新特性`
					cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt", createTastPrompt)
					execute(cmd, iterTag+"-IFLOW-TASK-CREATE")
					continue
				}

				task = tasks[0]

				taskByte, err := os.ReadFile(task)

				if err != nil {
					log.Fatalln("[FILE]", task, err.Error())
				}

				taskStr := string(taskByte)

				taskFilterStr := "" +
					"你是“需求文档维护器”。下面是需求文档@" + task + "。你必须结合项目当前内容判断需求是否“完全过时”。\n" +
					"\n" +
					"【过时(OUTDATED)定义】\n" +
					"- 当且仅当：需求所描述的功能/行为已经在项目中“完全实现”。\n" +
					"- 注意：即使仅缺少测试用例，也仍然算“完全实现”，应判定为过时。\n" +
					"\n" +
					"【允许的标记集合（硬约束）】\n" +
					"- 你只允许在文档中新增以下标记之一：`[OUTDATED]`。\n" +
					"- 严禁新增或输出任何其他状态标记，尤其严禁出现：`[COMPLETED]`、`COMPLETED`、`DONE`、`FINISHED`。（只要出现任意一个都算违反要求）\n" +
					"\n" +
					"【决策规则（硬约束）】\n" +
					"1) 若你能在项目中找到充分证据表明“需求全部要点都已实现”（允许缺测试）：\n" +
					"   - 仅在文档中增加标记`[OUTDATED]`（建议加在第一行或标题行末尾），其余内容不要做结构性改写。\n" +
					"2) 否则（包括你无法确定是否完全实现）：\n" +
					"   - 不得添加`[OUTDATED]`。\n" +
					"   - 你必须更新需求文档内容，使其与项目当前实际一致。\n" +
					"\n" +
					"【输出与修改范围（硬约束）】\n" +
					"- 你只允许修改这个文档本身，不得修改项目其他文件。\n" +
					"- 你的输出必须是“修改后的文档全文”，不要附加解释、不要输出分析过程、不要输出额外段落。\n" +
					"\n" +
					"【需求文档内容】\n" + taskStr

				os.WriteFile(iterationDir+"/task-filter-prompt-"+strconv.Itoa(i)+".txt", []byte(taskFilterStr), 0644)
				cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
				cmd.Stdin = strings.NewReader(taskFilterStr)
				execute(cmd, iterTag+"-IFLOW-TASK-FILTER")

				taskByte, err = os.ReadFile(task)

				if err != nil {
					log.Fatalln("[FILE]", task, err.Error())
				}

				taskStr = string(taskByte)
				if strings.Contains(taskStr, "[OUTDATED]") {
					log.Println("[FILE]", task, "is outdated")
					err := os.Rename(task, filepath.Join(iterationDir, filepath.Base(task)))
					if err != nil {
						log.Fatal(err)
					}
				} else {
					log.Println("[FILE]", task, "is Uptodate")
					break
				}
			}

			cleanup()

			taskByte, err := os.ReadFile(task)

			if err != nil {
				log.Fatalln("[FILE]", task, err.Error())
			}

			taskStr := string(taskByte)

			specTp := `下面是我的需求。请在项目根目录生成一份 "SPEC.md"，并严格遵守以下要求：

【必须包含的模块（使用这些标题）】
1) 不可修改条款
2) 可验证验收标准
3) 后续任务

【关键约束（非常重要）】
- 你只允许在本次 "SPEC.md" 中定义“原子化的第一步工作”（Atomic Step 1）：
  - 该步骤必须足够小，能够在一次迭代内实现并通过 @{{validate_script}} 验证。
  - 不要把所有功能一次性塞进第一步。
  - 在定义任务时要考虑当前的实现，不要将已经实现的内容定义为任务。
- 其余未包含在第一步中的工作，必须拆分为 2~8 条“后续任务”，写入 "后续任务" 模块：
  - 每条后续任务必须是独立可实现、可验证的小步。
  - 每条后续任务必须包含：任务标题 + 简要描述 + 可验证验收标准（至少 1 条）+ 最小测试计划（@{{validate_script}} 如何先失败再通过）。
- "可验证验收标准" 只针对“第一步工作”，不能覆盖后续任务。
- 不要实现代码，不要修改 @{{validate_script}}；只生成/更新 "SPEC.md"。

【输出要求】
- 若 "SPEC.md" 已存在：仅在其缺失上述模块或未满足“原子化第一步 + 后续任务”要求时补全；否则不要重写。
- 生成后停止，不要输出额外说明。

下面是需求正文：
`
			specTaskStr := strings.ReplaceAll(specTp, "{{validate_script}}", GetInstance().validateScript)
			taskStr = specTaskStr + taskStr
			os.WriteFile(iterationDir+"/spec-prompt.txt", []byte(taskStr), 0644)

			for {
				_, err := os.Stat("SPEC.md")
				if !os.IsNotExist(err) {
					log.Println("[FILE]", "SPEC.md exist")
					break
				}
				cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
				cmd.Stdin = strings.NewReader(taskStr)
				execute(cmd, iterTag+"-IFLOW-SPEC")
			}

			specByte, err := os.ReadFile("./SPEC.md")

			if err != nil {
				log.Fatalln("[FILE]", "SPEC.md", err.Error())
			}

			cleanup()

			specStr := string(specByte)
			specStr = "下面是我的规范，请基于`不可修改条款`和`可验证验收标准`改动代码测试验证部分和测试脚本 @{{validate_script}}\n确保脚本因为未实现功能失败\n除了测试验证代码和@{{validate_script}}禁止修改其他内容\n忽略`后续任务`部分内容，不要将其添加到测试中\n" + specStr
			specStr = strings.ReplaceAll(specStr, "{{validate_script}}", GetInstance().validateScript)
			os.WriteFile(iterationDir+"/red-prompt.txt", []byte(specStr), 0644)
			for {
				cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
				cmd.Stdin = strings.NewReader(specStr)
				execute(cmd, iterTag+"-IFLOW-RED")
				exitCode, _ := validate()
				if exitCode != 0 {
					log.Println("[RED]", "Validate Failed")
					break
				}
				log.Println("[RED]", "Validate Success")
			}

			cleanup()

			for i := 1; ; i++ {
				exitCode, output := validate()
				if exitCode == 0 {
					log.Println("[GREEN]", "Validate Success")
					break
				}
				log.Println("[GREEN]", "Validate Failed")
				greenPrompt := strings.ReplaceAll(promptTp, "{{FAIL}}", "[exit code:"+strconv.Itoa(exitCode)+"]"+output)
				greenPrompt = strings.ReplaceAll(greenPrompt, "{{validate_script}}", GetInstance().validateScript)
				os.WriteFile(iterationDir+"/green-"+strconv.Itoa(i)+"-prompt.txt", []byte(greenPrompt), 0644)
				cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
				cmd.Stdin = strings.NewReader(greenPrompt)
				execute(cmd, iterTag+"-IFLOW-GREEN-"+strconv.Itoa(i))
			}

			cleanup()

			taskStr = string(taskByte)
			evolveTp := `下面是当前需求与上下文。请参考最近几次提交的实现情况，并在 "./tasks/" 文件夹下创建下一步任务（任务队列），满足以下规则：

【目标（两条路径，择一或组合，但数量受控）】
A) 优先路径：如果 @SPEC.md 中存在 "后续任务" 模块，落地为新的任务文件写入 "./tasks/"，过滤"tasks"中已有的任务。
B) 可选路径：如果 "后续任务" 为空、过大、过时，或无法反映当前实现状态，你可以额外定义 1 个“新的需求”（新功能/新能力/质量提升方向），并写入 "./tasks/"。

【严格要求】
1) 本次最多创建 1～2 个“新的需求”任务文件（避免任务爆炸）,对"后续任务"数量不做限制。
2) 每个新任务必须是“原子化的小步”，能够在一次迭代内完成并通过 @{{validate_script}} 验证。
3) 每个任务文件必须包含以下结构（使用这些标题）：
   - 标题
   - 背景/动机（为什么需要做）
   - 可验证验收标准（至少 2 条，必须可自动检查）
   - 最小测试计划（@{{validate_script}} 如何先失败再通过）
4) 命名规范：任务文件名必须使用递增数字前缀，例如：
   - "002_<short_slug>.md"
   - "003_<short_slug>.md"
5) 不要修改实现代码，不要修改 @{{validate_script}}；只创建任务文件
6) 新任务文件不要和已有的"tasks"中的任务重复

【完成条件】
- "./tasks/" 下出现新的任务文件（1～10 个）。
- 新任务与当前实现状态一致，不重复已完成内容，且可被下一轮直接执行。

下面是需求正文（供参考）：
`
			evolveStr := strings.ReplaceAll(evolveTp, "{{validate_script}}", GetInstance().validateScript)
			taskStr = evolveStr + taskStr
			cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
			os.WriteFile(iterationDir+"/evolve-prompt.txt", []byte(taskStr), 0644)
			cmd.Stdin = strings.NewReader(taskStr)
			execute(cmd, iterTag+"-IFLOW-EVOLVE")

			os.Rename(task, filepath.Join(iterationDir, filepath.Base(task)))
			os.Rename("./SPEC.md", iterationDir+"/SPEC.md")

			cleanup()
		}()
	}
}
