package profile

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEncryptedBackupRoundTripRestoresProfilesAndTracking(t *testing.T) {
	source := newTestStore(t)
	appData := syntheticAppData(t, "backup-secret")
	if err := source.Checkpoint("personal", appData); err != nil {
		t.Fatal(err)
	}
	if err := source.SetCurrent("personal"); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "accounts.csb")
	if err := os.WriteFile(backup, []byte("old backup"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := source.Export(backup, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("backup-secret")) || bytes.Contains(data, []byte("sessionKey")) {
		t.Fatal("backup leaked profile contents in cleartext")
	}
	if protection, err := DetectBackupProtection(backup); err != nil || protection != BackupProtectionPassword {
		t.Fatalf("protection = %q/%v", protection, err)
	}

	destination, err := newStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := destination.ImportAuto(backup, "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	if !destination.Exists("personal") {
		t.Fatal("imported profile is missing")
	}
	if current, err := destination.Current(); err != nil || current != "personal" {
		t.Fatalf("current = %q/%v, want personal", current, err)
	}
	if got := destination.Inspect("personal").Health; got != HealthUsable {
		t.Fatalf("imported health = %s, want usable", got)
	}
	local := filepath.Join(destination.profileDir("personal"), localStorageDir, leveldbDir, "CURRENT")
	if got, err := os.ReadFile(local); err != nil || string(got) != "backup-secret" {
		t.Fatalf("imported Local Storage = %q/%v", got, err)
	}
	for _, path := range []string{filepath.Join(indexedDBDir, "data"), filepath.Join(sessionStorageDir, "data")} {
		if got, err := os.ReadFile(filepath.Join(destination.profileDir("personal"), path)); err != nil || string(got) != "backup-secret" {
			t.Fatalf("imported %s = %q/%v", path, got, err)
		}
	}
}

func TestEncryptedBackupRejectsWrongPasswordWithoutReplacingProfiles(t *testing.T) {
	source := newTestStore(t)
	if err := source.Checkpoint("source", syntheticAppData(t, "source")); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "accounts.csb")
	if err := source.Export(backup, "secret"); err != nil {
		t.Fatal(err)
	}
	destination := newTestStore(t)
	if err := destination.Checkpoint("keep", syntheticAppData(t, "keep")); err != nil {
		t.Fatal(err)
	}
	if err := destination.Import(backup, "wrong"); err == nil {
		t.Fatal("wrong password should fail")
	}
	if !destination.Exists("keep") || destination.Exists("source") {
		t.Fatal("wrong-password import changed existing profiles")
	}
}

func TestExportRejectsLegacyProfile(t *testing.T) {
	store := newTestStore(t)
	if err := store.Checkpoint("legacy", syntheticAppData(t, "legacy")); err != nil {
		t.Fatal(err)
	}
	meta, err := store.loadMeta("legacy")
	if err != nil {
		t.Fatal(err)
	}
	meta.FormatVersion = 2
	if err := store.saveMeta("legacy", meta); err != nil {
		t.Fatal(err)
	}

	err = store.Export(filepath.Join(t.TempDir(), "legacy.csb"), "secret")
	if err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("Export error = %v, want incomplete legacy profile", err)
	}
}

func TestReadBackupFileRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.csb")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxBackupFileSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readBackupFile(path); err == nil {
		t.Fatal("oversized backup was accepted")
	}
}

func TestImportRejectsBackupWithTooManyEntries(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for index := 0; index <= maxBackupEntries; index++ {
		entry, err := writer.Create("profiles/p" + strconv.Itoa(index) + "/meta.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	store := newTestStore(t)
	if err := store.installBackupArchive(archive.Bytes()); err == nil {
		t.Fatal("backup with too many entries was accepted")
	}
}
