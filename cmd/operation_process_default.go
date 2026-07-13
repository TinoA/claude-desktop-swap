//go:build !windows

package cmd

import (
	"errors"
	"os"
	"syscall"
)

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || !errors.Is(err, os.ErrProcessDone)
}
