package main

import (
	"log"
	"os"
	"os/exec"
)

func main() {
	log.Println("hello, go module!")
	cmd := exec.Command("iflow", "--yolo", "--prompt", "/init")
	log.Println(cmd.String())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}
