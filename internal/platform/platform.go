package platform

import "context"

// Platform abstracts OS-specific Claude Desktop operations.
type Platform interface {
	// AppDataPath returns the path to Claude's application data directory.
	AppDataPath() (string, error)
	// IsRunning reports whether Claude Desktop is currently running.
	IsRunning() (bool, error)
	// KillApp terminates Claude Desktop, waiting for it to fully exit.
	KillApp() error
	// LaunchApp starts Claude Desktop.
	LaunchApp() error
}

type InstallationDetector interface {
	IsInstalled() bool
}

type LoginWindowWaiter interface {
	WaitForLoginWindow(context.Context) error
}

func Installed() bool {
	if detector, ok := Current().(InstallationDetector); ok {
		return detector.IsInstalled()
	}
	return false
}

// Current returns the Platform implementation for the running OS.
func Current() Platform {
	return current()
}

// CookiesPath resolves the Chromium Cookies database below an app-data root.
func CookiesPath(appDataPath string) string { return cookiesPath(appDataPath) }
