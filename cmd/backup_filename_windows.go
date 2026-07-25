//go:build windows

package cmd

import "time"

func defaultBackupFilename(now time.Time, passwordProtected bool) string {
	suffix := ""
	if passwordProtected {
		suffix = "-pw"
	}
	return "windows-claude-swap-" + now.Format("2006-01-02_1504") + suffix + ".csb"
}
