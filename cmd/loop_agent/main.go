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
		log.Println("[Iter]", "Iter", GetInstance().iteration, "begin")

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
		taskStr = "下面是我的需求，请在项目根目录生成一份`SPEC.md`\n 必须包含`不可修改条款`和`可验证验收标准`\n" + taskStr
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
		specStr = "下面是我的规范，请基于`不可修改条款`和`可验证验收标准`改动代码测试验证部分和测试脚本 @validate.sh\n 确保脚本因为未实现功能失败\n" + specStr
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

		exitCode, output := validate()
		cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
		var prompt string
		if exitCode != 0 {
			prompt = strings.ReplaceAll(promptTp, "{{FAIL}}", "[exit code:"+strconv.Itoa(exitCode)+"]"+output)
		} else {
			prompt = strings.ReplaceAll(promptTp, "{{FAIL}}", "")
		}
		os.WriteFile(iterationDir+"/green-prompt.txt", []byte(prompt), 0644)
		for {
			exitCode, _ := validate()
			if exitCode == 0 {
				log.Println("[GREEN]", "Validate Success")
				break
			}
			log.Println("[GREEN]", "Validate Failed")
			cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
			cmd.Stdin = strings.NewReader(prompt)
			execute(cmd, "IFLOW-GREEN")
		}

		cleanup()

		taskStr = string(taskByte)
		taskStr = "下面是我的需求，请参考最近几次提交分析实现情况，并在tasks文件夹下创建进一步完善的需求或者是在此基础上实现新的需求\n" + taskStr
		cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
		cmd.Stdin = strings.NewReader(taskStr)
		execute(cmd, "IFLOW-EVOLVE")

		os.Rename(task, iterationDir+"/task.md")
		os.Remove("./SPEC.md")

		cleanup()

		log.Println("[Iter]", "Iter", GetInstance().iteration, "end")
	}
}
