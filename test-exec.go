package main

import (
	"log"
	"os/exec"
	"path/filepath"
)

func main() {
	shimPath, _ := filepath.Abs("bin/forge-shim-amd64")
	cmd := exec.Command(shimPath)
	cmd.Dir = "/tmp/forge_ws/testproj/work"
	err := cmd.Start()
	log.Println(err)
}
