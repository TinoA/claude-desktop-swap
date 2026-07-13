//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCookiesPathPrefersNetwork(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Network"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Network", "Cookies"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := cookiesPath(root); got != filepath.Join(root, "Network", "Cookies") {
		t.Fatalf("cookiesPath = %q", got)
	}
}

func TestCookiesPathFallsBackToRoot(t *testing.T) {
	root := t.TempDir()
	if got := cookiesPath(root); got != filepath.Join(root, "Cookies") {
		t.Fatalf("cookiesPath = %q", got)
	}
}

func TestDesktopProcessPIDsMatchExecutablePathOnly(t *testing.T) {
	output := "101|C:\\Program Files\\WindowsApps\\Claude_1.0_x64__pzs8sxrjxfjjc\\app\\Claude.exe\n202|C:\\Users\\user\\AppData\\Roaming\\Claude\\claude-code\\claude.exe\n"
	got := desktopProcessPIDs(output, `C:\Program Files\WindowsApps\Claude_1.0_x64__pzs8sxrjxfjjc\app\Claude.exe`, false)
	if len(got) != 1 || got[0] != 101 {
		t.Fatalf("desktopProcessPIDs = %v", got)
	}
}

func TestDesktopProcessPIDsAcceptMissingMSIXPath(t *testing.T) {
	output := "101|\n202|C:\\Users\\user\\AppData\\Roaming\\Claude\\claude-code\\claude.exe\n"
	got := desktopProcessPIDs(output, `C:\Program Files\WindowsApps\Claude_1.0_x64__pzs8sxrjxfjjc\app\Claude.exe`, true)
	if len(got) != 1 || got[0] != 101 {
		t.Fatalf("desktopProcessPIDs = %v", got)
	}
}

func TestDesktopProcessRootPIDsExcludeChildren(t *testing.T) {
	output := "101|50|C:\\Program Files\\WindowsApps\\Claude_1.0_x64__pzs8sxrjxfjjc\\app\\Claude.exe\n202|101|C:\\Program Files\\WindowsApps\\Claude_1.0_x64__pzs8sxrjxfjjc\\app\\Claude.exe\n303|77|C:\\Users\\user\\AppData\\Roaming\\Claude\\claude-code\\claude.exe\n"
	got := desktopProcessRootPIDs(output, `C:\Program Files\WindowsApps\Claude_1.0_x64__pzs8sxrjxfjjc\app\Claude.exe`, false)
	if len(got) != 1 || got[0] != 101 {
		t.Fatalf("desktopProcessRootPIDs = %v", got)
	}
}

func TestDesktopProcessRootPIDsAcceptMissingMSIXPaths(t *testing.T) {
	output := "101|50|\n202|101|\n303|77|C:\\Users\\user\\AppData\\Roaming\\Claude\\claude-code\\claude.exe\n"
	got := desktopProcessRootPIDs(output, `C:\Program Files\WindowsApps\Claude_1.0_x64__pzs8sxrjxfjjc\app\Claude.exe`, true)
	if len(got) != 1 || got[0] != 101 {
		t.Fatalf("desktopProcessRootPIDs = %v", got)
	}
}

func TestSquirrelOwnsVersionedDesktopProcessesOnly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "AnthropicClaude")
	platform := &windowsPlatform{
		executable:  filepath.Join(root, "claude.exe"),
		processRoot: root,
		kind:        installSquirrel,
	}
	if !platform.ownsProcess(filepath.Join(root, "app-1.20186.1", "claude.exe")) {
		t.Fatal("versioned Squirrel process was not recognized")
	}
	if !platform.ownsProcess(filepath.Join(root, "claude.exe")) {
		t.Fatal("Squirrel launcher was not recognized")
	}
	if platform.ownsProcess(filepath.Join(root, "resources", "claude.exe")) {
		t.Fatal("non-app Squirrel executable was recognized")
	}
	if platform.ownsProcess(filepath.Join(t.TempDir(), "claude.exe")) {
		t.Fatal("unrelated Claude executable was recognized")
	}
}

func TestPathWithinRejectsSiblingDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "AnthropicClaude")
	if !pathWithin(filepath.Join(root, "app-1", "claude.exe"), root) {
		t.Fatal("child path was rejected")
	}
	if pathWithin(root+"-old", root) {
		t.Fatal("sibling path was accepted")
	}
}
