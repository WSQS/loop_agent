package main

import (
	"bufio"
	"log"
	"os/exec"
)

func main() {
	log.Println("hello, go module!")
	cmd := exec.Command("iflow", "-y", "-d", "--thinking", "--prompt", "/init")
	log.Println(cmd.String())
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
			log.Println("[IFLOW-STDOUT]", sc.Text())
		}
	}()

	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			log.Println("[IFLOW-STDERR]", sc.Text())
		}
	}()

	err = cmd.Wait()
	if err != nil {
		log.Fatal(err)
	}
}
