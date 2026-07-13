package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type operationLock struct {
	file *os.File
	path string
}

func acquireOperationLock(name string) (*operationLock, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".claude-swap")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name+".lock")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		owner := "another process"
		if data, readErr := os.ReadFile(path); readErr == nil {
			owner = strings.TrimSpace(string(data))
			if pid, parseErr := strconv.Atoi(owner); parseErr == nil && !processAlive(pid) {
				_ = os.Remove(path)
				return acquireOperationLock(name)
			}
		}
		return nil, fmt.Errorf("another Claude Desktop Swap operation is active (%s)", owner)
	}
	if err != nil {
		return nil, err
	}
	if _, err := file.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return &operationLock{file: file, path: path}, nil
}

func (l *operationLock) Release() {
	if l == nil {
		return
	}
	_ = l.file.Close()
	_ = os.Remove(l.path)
}
