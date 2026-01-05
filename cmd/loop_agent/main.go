package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"os/exec"
	"time"
)

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
