package rpc

import (
	"bytes"
	"os"
	"os/exec"
)

func runCmdImpl(dir string, args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	return out.String(), cmd.Run()
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}