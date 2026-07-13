//go:build windows

package profile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalWindowsBackupRoundTrip(t *testing.T) {
	source := newTestStore(t)
	if err := source.Checkpoint("personal", syntheticAppData(t, "local-backup-secret")); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "local.csb")
	if err := source.ExportLocal(backup); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("local-backup-secret")) {
		t.Fatal("local backup leaked profile contents in cleartext")
	}
	if protection, err := DetectBackupProtection(backup); err != nil || protection != BackupProtectionWindows {
		t.Fatalf("protection = %q/%v", protection, err)
	}
	destination := newTestStore(t)
	if err := destination.ImportAuto(backup, ""); err != nil {
		t.Fatal(err)
	}
	if !destination.Exists("personal") {
		t.Fatal("local backup did not restore profile")
	}
}
