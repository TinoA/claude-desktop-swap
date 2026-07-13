//go:build windows

package cmd

import (
	"bytes"
	"fmt"
	"os/exec"
)

func processAlive(pid int) bool {
	out, err := exec.Command("tasklist.exe", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false
	}
	return bytes.Contains(out, []byte(fmt.Sprintf("\"%d\"", pid)))
}
