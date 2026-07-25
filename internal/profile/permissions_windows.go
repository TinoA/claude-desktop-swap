//go:build windows

package profile

import (
	"fmt"
	"os"

	"github.com/FranCalveyra/claude-desktop-swap/internal/winproc"
)

func securePath(path string) error {
	user := os.Getenv("USERDOMAIN") + "\\" + os.Getenv("USERNAME")
	if user == "\\" {
		return fmt.Errorf("windows account identity is unavailable for ACL protection")
	}
	// Keep the current environment's required inherited access entry, then
	// remove the broad principals that could otherwise read profile snapshots.
	if out, err := winproc.Command("icacls.exe", path, "/inheritance:d", "/grant:r", user+":(OI)(CI)F", "/remove:g", "*S-1-1-0", "*S-1-5-32-545", "*S-1-5-11", "/T", "/C").CombinedOutput(); err != nil {
		return fmt.Errorf("secure ACL for %s: %w: %s", path, err, string(out))
	}
	return nil
}
