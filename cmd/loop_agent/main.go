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
Task: Clean up the current Git repository by handling all uncommitted and modified files.

Requirements:
1. Review the current repository state and identify all uncommitted and modified files.

2. For binary files and irrelevant files:
   - Determine which files should not be tracked or committed.
   - Update ".gitignore" to ensure these binary or irrelevant files are ignored by Git.
   - Do not commit the ignored files themselves.

3. For files that should be committed:
   - Group changes by their change purpose (i.e., why the change was made).
   - Split the work into multiple atomic commits, where each commit represents a single, clear change purpose.
   - Ensure each commit is self-contained and does not mix unrelated changes.

4. Operate on the following files (to be filled at runtime):

{{files}}
Output Expectations:
- ".gitignore" is updated appropriately to ignore binary and irrelevant files.
- Necessary changes are committed in multiple atomic commits, organized by change purpose.
- No large, mixed-purpose commits are created.`

		iterationDir := GetInstance().dir + "/iter-" + strconv.Itoa(GetInstance().iteration)
		prompt = strings.ReplaceAll(prompt, "{{files}}", string(files))
		os.WriteFile(iterationDir+"/cleanup-"+strconv.Itoa(GetInstance().attemptCount)+"-prompt.txt", []byte(prompt), 0644)
		cmd.Stdin = strings.NewReader(prompt)
		execute(cmd, "IFLOW-INIT")
	}
	log.Println("[CLEANUP] Clean up finished")
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

		cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
		prompt := strings.ReplaceAll(promptTp, "{{FAIL}}", "")
		os.WriteFile(iterationDir+"/work-prompt.txt", []byte(prompt), 0644)
		cmd.Stdin = strings.NewReader(prompt)
		execute(cmd, "IFLOW-WORK")

		cleanup()

		log.Println("[Iter]", "Iter", GetInstance().iteration, "end")
	}
}
