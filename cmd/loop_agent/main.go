package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const promptTp = `
目前按照@SPEC.md 的定义，添加了检测，并@validate.sh会因为未实现功能失败，请实现对应功能，相关日志如下：
{{FAIL}}
`

type Singleton struct {
	iteration    int
	attemptCount int
	dir          string
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
	log.Println("[CLEANUP] Repo is dirty, clean up")
	for ; ; GetInstance().attemptCount++ {
		dirty, files := is_repo_dirty()
		if !dirty {
			break
		}
		cmd := exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
		prompt := `
Role: iflow AI coding agent

Task:
Clean up the Git repository by properly handling all uncommitted and modified files listed below.

Files in Scope:

{{files}}
Scope and Constraints:
- You MAY scan the entire repository to understand context, file types, and relationships.
- You may ONLY modify ".gitignore" and commit files explicitly listed.
- Do NOT modify, commit, or delete any other files.
- The following actions are strictly prohibited:
  - Deleting any files
  - Squashing commits
  - Modifying existing commit history

Execution Requirements:

1. Repository Review
   - Review the repository state and Git status.
   - Identify which files in Files in Scope are uncommitted or modified.

2. Handling Binary or Irrelevant Files
   - Independently determine which files in Files in Scope should NOT be tracked or committed (e.g., binary files or irrelevant artifacts).
   - Update ".gitignore" to ignore these files or corresponding patterns.
   - Do NOT commit the ignored files themselves.
   - Commit the ".gitignore" change in ONE standalone commit.

3. Handling Files to Be Committed
   - For all remaining files in Files in Scope that should be committed:
     - Group changes strictly by change purpose (the underlying reason for the change).
     - Split the work into multiple atomic commits.
     - Each commit may include multiple files, but MUST represent exactly one clear and coherent change purpose.
     - Do NOT mix unrelated change purposes within a single commit.

4. Commit Message Requirements
   - Every commit message MUST follow this structure:
     "<type>[iter-{{iteration}}][atmp-{{attempt}}]: <short description>"
   - "<type>" must be semantically appropriate (e.g., fix, feat, docs, chore, refactor).
   - The description must be concise and accurately reflect the change purpose.

Output Expectations:
- ".gitignore" is updated and committed in a single, dedicated commit if required.
- All other necessary changes are committed as multiple atomic commits, organized by change purpose.
- No commit contains mixed or unrelated changes.
- No prohibited Git operations are performed.
`

		iterationDir := GetInstance().dir + "/iter-" + strconv.Itoa(GetInstance().iteration)
		prompt = strings.ReplaceAll(prompt, "{{files}}", string(files))
		prompt = strings.ReplaceAll(prompt, "{{iteration}}", strconv.Itoa(GetInstance().iteration))
		prompt = strings.ReplaceAll(prompt, "{{attempt}}", strconv.Itoa(GetInstance().attemptCount))
		os.WriteFile(iterationDir+"/cleanup-"+strconv.Itoa(GetInstance().attemptCount)+"-prompt.txt", []byte(prompt), 0644)
		cmd.Stdin = strings.NewReader(prompt)
		execute(cmd, "IFLOW-CLEANUP")
	}
	log.Println("[CLEANUP] Clean up finished")
}

func validate() (int, string) {
	log.Println("[VALIDATE]", "Start")
	defer log.Println("[VALIDATE]", "End")
	cmd := exec.Command("./validate.sh")
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
	log.SetOutput(io.MultiWriter(os.Stdout, f))
	log.Println("[LOG] Log in ", GetInstance().dir)
	cmd := exec.Command("git", "status")
	execute(cmd, "GIT-STATUS")
	cmd = exec.Command("git", "checkout", "-b", "ai/gen/loop-"+timestamp)
	execute(cmd, "GIT-STATUS")
	for GetInstance().iteration = 1; GetInstance().iteration < 500; GetInstance().iteration++ {
		func() {
			defer trace("ITER")()

			GetInstance().attemptCount = 1

			iterationDir := GetInstance().dir + "/iter-" + strconv.Itoa(GetInstance().iteration)
			if err := os.MkdirAll(iterationDir, 0755); err != nil {
				log.Fatal(err)
			}

			cleanup()

			cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt", "/init")
			execute(cmd, "IFLOW-INIT")

			cleanup()

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
				log.Fatalln("[FILE]", "No Task")
			}

			task := tasks[0]

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
  - 该步骤必须足够小，能够在一次迭代内实现并通过 "./validate.sh" 验证。
  - 不要把所有功能一次性塞进第一步。
  - 在定义任务时要考虑当前的实现，不要将已经实现的内容定义为任务。
- 其余未包含在第一步中的工作，必须拆分为 2~8 条“后续任务”，写入 "后续任务" 模块：
  - 每条后续任务必须是独立可实现、可验证的小步。
  - 每条后续任务必须包含：任务标题 + 简要描述 + 可验证验收标准（至少 1 条）+ 最小测试计划（validate.sh 如何先失败再通过）。
- "可验证验收标准" 只针对“第一步工作”，不能覆盖后续任务。
- 不要实现代码，不要修改 validate.sh；只生成/更新 "SPEC.md"。

【输出要求】
- 若 "SPEC.md" 已存在：仅在其缺失上述模块或未满足“原子化第一步 + 后续任务”要求时补全；否则不要重写。
- 生成后停止，不要输出额外说明。

下面是需求正文：
`
			taskStr = specTp + taskStr
			os.WriteFile(iterationDir+"/spec-prompt.txt", []byte(taskStr), 0644)

			for {
				_, err := os.Stat("SPEC.md")
				if !os.IsNotExist(err) {
					log.Println("[FILE]", "SPEC.md exist")
					break
				}
				cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
				cmd.Stdin = strings.NewReader(taskStr)
				execute(cmd, "IFLOW-SPEC")
			}

			specByte, err := os.ReadFile("./SPEC.md")

			if err != nil {
				log.Fatalln("[FILE]", "SPEC.md", err.Error())
			}

			cleanup()

			specStr := string(specByte)
			specStr = "下面是我的规范，请基于`不可修改条款`和`可验证验收标准`改动代码测试验证部分和测试脚本 @validate.sh\n确保脚本因为未实现功能失败\n除了测试验证代码和@validate.sh禁止修改其他内容\n忽略`后续任务`部分内容，不要将其添加到测试中\n" + specStr
			os.WriteFile(iterationDir+"/red-prompt.txt", []byte(specStr), 0644)
			for {
				cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
				cmd.Stdin = strings.NewReader(specStr)
				execute(cmd, "IFLOW-RED")
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
				os.WriteFile(iterationDir+"/green-"+strconv.Itoa(i)+"-prompt.txt", []byte(greenPrompt), 0644)
				cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
				cmd.Stdin = strings.NewReader(greenPrompt)
				execute(cmd, "IFLOW-GREEN")
			}

			cleanup()

			taskStr = string(taskByte)
			evolveTp := `下面是当前需求与上下文。请参考最近几次提交的实现情况，并在 "./tasks/" 文件夹下创建下一步任务（任务队列），满足以下规则：

【目标（两条路径，择一或组合，但数量受控）】
A) 优先路径：如果 @SPEC.md 中存在 "后续任务" 模块，落地为新的任务文件写入 "./tasks/"，过滤"tasks"中已有的任务。
B) 可选路径：如果 "后续任务" 为空、过大、过时，或无法反映当前实现状态，你可以额外定义 1 个“新的需求”（新功能/新能力/质量提升方向），并写入 "./tasks/"。

【严格要求】
1) 本次最多创建 1～2 个“新的需求”任务文件（避免任务爆炸）,对"后续任务"数量不做限制。
2) 每个新任务必须是“原子化的小步”，能够在一次迭代内完成并通过 "./validate.sh" 验证。
3) 每个任务文件必须包含以下结构（使用这些标题）：
   - 标题
   - 背景/动机（为什么需要做）
   - 可验证验收标准（至少 2 条，必须可自动检查）
   - 最小测试计划（validate.sh 如何先失败再通过）
4) 命名规范：任务文件名必须使用递增数字前缀，例如：
   - "002_<short_slug>.md"
   - "003_<short_slug>.md"
5) 不要修改实现代码，不要修改 validate.sh；只创建任务文件
6) 新任务文件不要和已有的"tasks"中的任务重复

【完成条件】
- "./tasks/" 下出现新的任务文件（1～10 个）。
- 新任务与当前实现状态一致，不重复已完成内容，且可被下一轮直接执行。

下面是需求正文（供参考）：
`
			taskStr = evolveTp + taskStr
			cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
			os.WriteFile(iterationDir+"/evolve-prompt.txt", []byte(taskStr), 0644)
			cmd.Stdin = strings.NewReader(taskStr)
			execute(cmd, "IFLOW-EVOLVE")

			os.Rename(task, iterationDir+"/task.md")
			os.Remove("./SPEC.md")

			cleanup()
		}()
	}
}
