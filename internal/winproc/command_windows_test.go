//go:build windows

package winproc

import (
	"context"
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCommandsDoNotCreateConsoleWindows(t *testing.T) {
	commands := []*exec.Cmd{
		Command("icacls.exe"),
		CommandContext(context.Background(), "taskkill.exe"),
	}
	for _, cmd := range commands {
		if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
			t.Fatal("hidden command does not hide its window")
		}
		if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
			t.Fatal("hidden command can create a console window")
		}
	}
}
