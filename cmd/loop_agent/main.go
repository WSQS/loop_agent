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
You are an autonomous coding agent operating in a local Linux git repo.
You are invoked repeatedly by an external loop script.

AUTHORITATIVE CONTEXT
- Requirements are in @requirements.md.
- External validation command (run by the script) is:
  g++ ./sob.cpp -o sob && ./sob && ./toy
- sob builds ./toy from toy.cpp. toy.cpp is the main implementation.

AGENT PRIORITY POLICY (STRICT)
P0) Fix run/build failures first:
- If the failure snippet indicates compile/link errors, runtime crash, or non-zero exit in validation,
  focus ONLY on fixing that failure with the smallest change.
- Do not attempt new features while P0 is failing.

P1) Then verify requirements:
- If P0 appears resolved (no current failure snippet / prior run likely succeeded),
  check whether @requirements.md acceptance is fully satisfied (toy CASE outputs + exit codes).
- Do not "fake" results. LLVM must be genuinely used.

P2) If requirements are already satisfied:
- Do NOT change implementation code.
- Propose exactly ONE new incremental requirement and write it to NEXT_REQUIREMENTS.md with:
  - Requirement statement
  - Acceptance criteria
  - Minimal test(s) / how it will be validated
- Then STOP.

SCOPE & HYGIENE
- Prefer editing ONLY ./toy.cpp (and NEXT_REQUIREMENTS.md when in P2).
- Do NOT modify/commit logs or generated artifacts:
  .iflow_runs/, sob, toy, *.o, build/, *.ll

ANTI-CHEATING
- Do NOT hardcode expected CASE outputs.
- Do NOT bypass evaluation by returning constants.

ONE-CHANGESET + COMMIT (MANDATORY when changes exist)
- Make ONE focused changeset.
- If any source changes were made, create EXACTLY ONE git commit:
  Message: "iflow iter $i: <short summary>"
- If P2 path (requirements satisfied): only commit NEXT_REQUIREMENTS.md (no code changes).

FAILURE FEEDBACK (authoritative)
FAILURE SNIPPET BEGIN
{{FAIL}}
FAILURE SNIPPET END

OUTPUT FORMAT (MANDATORY; no extra text)
DECISION:
- PATH: P0 | P1 | P2
- WHY: <1-3 bullets>

CHANGELOG:
- <file>: <bullets or NONE>

COMMIT:
- COMMIT_HASH: <hash or NONE>
- FILES_COMMITTED: <list or NONE>
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

		exitCode, output := validate()

		cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
		var prompt string
		if exitCode != 0 {
			prompt = strings.ReplaceAll(promptTp, "{{FAIL}}", "[exit code:"+strconv.Itoa(exitCode)+"]"+output)
		} else {
			prompt = strings.ReplaceAll(promptTp, "{{FAIL}}", "")
		}
		os.WriteFile(iterationDir+"/work-prompt.txt", []byte(prompt), 0644)
		cmd.Stdin = strings.NewReader(prompt)
		execute(cmd, "IFLOW-WORK")

		cleanup()

		log.Println("[Iter]", "Iter", GetInstance().iteration, "end")
	}
}
