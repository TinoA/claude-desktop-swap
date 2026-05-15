package platform

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

// Current returns the Platform implementation for the running OS.
func Current() Platform {
	return current()
}
