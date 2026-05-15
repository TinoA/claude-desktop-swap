//go:build darwin

package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	appName          = "Claude"
	appDataDir       = "Application Support"
	killPollInterval = 100 * time.Millisecond
	killMaxPolls     = 50 // 50 × 100ms = 5 second timeout before SIGKILL
	killForceDelay   = 300 * time.Millisecond
)

type darwinPlatform struct{}

func current() Platform { return &darwinPlatform{} }

func (d *darwinPlatform) AppDataPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", appDataDir, appName), nil
}

func (d *darwinPlatform) IsRunning() (bool, error) {
	err := exec.Command("pgrep", "-x", appName).Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (d *darwinPlatform) KillApp() error {
	running, err := d.IsRunning()
	if err != nil || !running {
		return err
	}

	exec.Command("pkill", "-TERM", "-x", appName).Run() //nolint:errcheck

	for range killMaxPolls {
		time.Sleep(killPollInterval)
		running, _ := d.IsRunning()
		if !running {
			return nil
		}
	}

	// SIGTERM wasn't enough after 5 seconds — force it
	exec.Command("pkill", "-KILL", "-x", appName).Run() //nolint:errcheck
	time.Sleep(killForceDelay)
	return nil
}

func (d *darwinPlatform) LaunchApp() error {
	return exec.Command("open", "-a", appName).Run()
}
