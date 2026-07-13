//go:build !windows

package profile

import (
	"errors"
	"os"
)

func protectForCurrentWindowsUser([]byte) ([]byte, error) {
	return nil, errors.New("Windows user-protected backups are only available on Windows")
}

func unprotectForCurrentWindowsUser([]byte) ([]byte, error) {
	return nil, errors.New("Windows user-protected backups are only available on Windows")
}

func replaceBackupFile(source, destination string) error {
	return os.Rename(source, destination)
}
