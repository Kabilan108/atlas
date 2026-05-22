package output

import (
	"bytes"
	"os"
	"os/exec"
)

func PipeToCommand(path string, args []string, input []byte) error {
	cmd := exec.Command(path, args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
