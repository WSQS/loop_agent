package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
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

func main() {
	dir := ".loop_agent/" + time.Now().Format("060102150405")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatal(err)
	}
	f, err := os.OpenFile(dir+"/log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, f))
	log.Println("[LOG] Log in ", dir)
	for i := 0; i < 500; i++ {
		log.Println("[Iter]", "Iter", i, "begin")
		cmd := exec.Command("iflow", "-y", "-d", "--thinking", "--prompt", "/init")
		execute(cmd, "IFLOW-INIT")

		cmd = exec.Command("iflow", "-y", "-d", "--thinking", "--prompt")
		cmd.Stdin = strings.NewReader(strings.ReplaceAll(promptTp, "{{FAIL}}", ""))
		execute(cmd, "IFLOW-WORK")
		log.Println("[Iter]", "Iter", i, "end")
	}
}
