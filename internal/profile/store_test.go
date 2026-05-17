package profile

import (
	"os"
	"path/filepath"
	"testing"
)

// idbOriginDir mimics the per-origin subdirectory Chromium creates inside
// IndexedDB/. The exact name isn't part of our contract; we just need a
// realistic shape so copyDir has something nested to walk.
const idbOriginDir = "https_claude.ai_0.indexeddb.leveldb"

func TestSaveAndRestore(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)

	if err := store.Save("test", appData); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !store.Exists("test") {
		t.Fatal("profile should exist after save")
	}

	// Simulate a session change
	mustWriteFile(t, filepath.Join(appData, cookiesFile), "new-session")

	if err := store.Restore("test", appData); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(appData, cookiesFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fake-cookies" {
		t.Errorf("Cookies = %q, want %q", got, "fake-cookies")
	}
}

func TestRestoreClearsJournal(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)
	store.Save("test", appData) //nolint:errcheck

	// Simulate a stale journal left by a previous Claude session
	journal := filepath.Join(appData, cookiesJournalFile)
	mustWriteFile(t, journal, "stale-journal")

	if err := store.Restore("test", appData); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Error("Cookies-journal should be removed after restore")
	}
}

func TestRestoreReplacesAllSessionArtifacts(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)
	if err := store.Save("test", appData); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Simulate switching profiles: every per-account artifact now belongs to a different session.
	for path, content := range map[string]string{
		filepath.Join(appData, cookiesFile):                              "other-cookies",
		filepath.Join(appData, deviceIDFile):                             "other-device",
		filepath.Join(appData, indexedDBDir, idbOriginDir, "000001.log"): "other-idb",
		filepath.Join(appData, sessionStorageDir, "000001.log"):          "other-ss",
	} {
		mustWriteFile(t, path, content)
	}

	if err := store.Restore("test", appData); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Cookies and device ID must be restored from the profile.
	for path, want := range map[string]string{
		filepath.Join(appData, cookiesFile):  "fake-cookies",
		filepath.Join(appData, deviceIDFile): "fake-device-id",
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}

	// IndexedDB and Session Storage must be wiped so Claude rebuilds them
	// from cookies — restoring stale cached auth causes token conflicts.
	for _, path := range []string{
		filepath.Join(appData, indexedDBDir),
		filepath.Join(appData, sessionStorageDir),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s should be removed after restore", path)
		}
	}
}

func TestRestoreNonexistent(t *testing.T) {
	store := newTestStore(t)
	if err := store.Restore("ghost", t.TempDir()); err == nil {
		t.Fatal("restoring nonexistent profile should error")
	}
}

func TestList(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)
	store.Save("alpha", appData) //nolint:errcheck
	store.Save("beta", appData)  //nolint:errcheck

	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 2 {
		t.Errorf("got %d profiles, want 2", len(profiles))
	}
}

func TestListEmpty(t *testing.T) {
	store := newTestStore(t)
	profiles, err := store.List()
	if err != nil {
		t.Fatalf("List on empty store: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected empty list, got %d", len(profiles))
	}
}

func TestDelete(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)
	store.Save("temp", appData) //nolint:errcheck

	if err := store.Delete("temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if store.Exists("temp") {
		t.Fatal("profile should not exist after delete")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	store := newTestStore(t)
	if err := store.Delete("ghost"); err == nil {
		t.Fatal("deleting nonexistent profile should error")
	}
}

func TestDeleteClearsCurrent(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)
	store.Save("active", appData) //nolint:errcheck
	store.SetCurrent("active")    //nolint:errcheck
	store.Delete("active")        //nolint:errcheck

	current, _ := store.Current()
	if current == "active" {
		t.Error("current should be cleared after deleting the active profile")
	}
}

func TestSetAndGetCurrent(t *testing.T) {
	store := newTestStore(t)

	if err := store.SetCurrent("work"); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}

	got, err := store.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got != "work" {
		t.Errorf("Current = %q, want %q", got, "work")
	}
}

func TestSavePreservesCreatedAt(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)
	store.Save("p", appData) //nolint:errcheck

	first, _ := store.loadMeta("p")
	store.Save("p", appData) //nolint:errcheck
	second, _ := store.loadMeta("p")

	if !first.CreatedAt.Equal(second.CreatedAt) {
		t.Error("re-saving should preserve the original CreatedAt")
	}
}

func TestWipeRemovesSessionArtifacts(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	// A non-session file should survive Wipe.
	preferences := filepath.Join(appData, "Preferences")
	mustWriteFile(t, preferences, "user prefs")

	store := newTestStore(t)
	if err := store.Wipe(appData); err != nil {
		t.Fatalf("Wipe: %v", err)
	}

	for _, p := range []string{
		filepath.Join(appData, cookiesFile),
		filepath.Join(appData, localStorageDir, leveldbDir),
		filepath.Join(appData, indexedDBDir),
		filepath.Join(appData, sessionStorageDir),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should be removed by Wipe", p)
		}
	}

	// Preferences and the machine-bound device id stay put.
	for _, p := range []string{
		preferences,
		filepath.Join(appData, deviceIDFile),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should NOT be removed by Wipe: %v", p, err)
		}
	}
}

func TestWipeIdempotent(t *testing.T) {
	appData := t.TempDir()
	store := newTestStore(t)

	if err := store.Wipe(appData); err != nil {
		t.Errorf("Wipe on empty dir: %v", err)
	}
	if err := store.Wipe(appData); err != nil {
		t.Errorf("second Wipe: %v", err)
	}
}

func TestHasActiveSession(t *testing.T) {
	appData := t.TempDir()

	if HasActiveSession(appData) {
		t.Error("empty dir should not report an active session")
	}

	cookies := filepath.Join(appData, cookiesFile)
	mustWriteFile(t, cookies, "")
	if HasActiveSession(appData) {
		t.Error("empty Cookies file should not report an active session")
	}

	mustWriteFile(t, cookies, "real-session")
	if !HasActiveSession(appData) {
		t.Error("non-empty Cookies file should report an active session")
	}
}

func TestSaveCapturesAllArtifacts(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)
	if err := store.Save("test", appData); err != nil {
		t.Fatalf("Save: %v", err)
	}

	profileDir := store.profileDir("test")
	for _, p := range []string{
		filepath.Join(profileDir, cookiesFile),
		filepath.Join(profileDir, leveldbDir),
		filepath.Join(profileDir, indexedDBDir),
		filepath.Join(profileDir, sessionStorageDir),
		filepath.Join(profileDir, deviceIDFile),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should exist in profile dir: %v", p, err)
		}
	}
}

func TestSaveSkipsMissingOptionalArtifacts(t *testing.T) {
	appData := t.TempDir()
	// Minimal app data — only the required artifacts.
	mustWriteFile(t, filepath.Join(appData, cookiesFile), "c")
	mustWriteFile(t, filepath.Join(appData, localStorageDir, leveldbDir, "CURRENT"), "")

	store := newTestStore(t)
	if err := store.Save("test", appData); err != nil {
		t.Fatalf("Save with missing optional artifacts: %v", err)
	}

	profileDir := store.profileDir("test")
	for _, p := range []string{
		filepath.Join(profileDir, indexedDBDir),
		filepath.Join(profileDir, sessionStorageDir),
		filepath.Join(profileDir, deviceIDFile),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should not exist when source was missing", p)
		}
	}
}

func TestRestoreSetsCurrent(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)
	store.Save("p1", appData) //nolint:errcheck
	store.Save("p2", appData) //nolint:errcheck

	if err := store.Restore("p1", appData); err != nil {
		t.Fatalf("Restore p1: %v", err)
	}
	if got, _ := store.Current(); got != "p1" {
		t.Errorf("after Restore p1, Current = %q, want %q", got, "p1")
	}

	if err := store.Restore("p2", appData); err != nil {
		t.Fatalf("Restore p2: %v", err)
	}
	if got, _ := store.Current(); got != "p2" {
		t.Errorf("after Restore p2, Current = %q, want %q", got, "p2")
	}
}

func TestRestoreUpdatesLastUsed(t *testing.T) {
	appData := t.TempDir()
	setupFakeAppData(t, appData)

	store := newTestStore(t)
	store.Save("p", appData) //nolint:errcheck

	before, _ := store.loadMeta("p")
	if !before.LastUsed.IsZero() {
		t.Fatal("LastUsed should be zero before any Restore")
	}

	if err := store.Restore("p", appData); err != nil {
		t.Fatal(err)
	}

	after, _ := store.loadMeta("p")
	if after.LastUsed.IsZero() {
		t.Error("LastUsed should be set after Restore")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, profilesDirName), dirPerm); err != nil {
		t.Fatal(err)
	}
	return &Store{baseDir: base}
}

func setupFakeAppData(t *testing.T, dir string) {
	t.Helper()
	lsDir := filepath.Join(dir, localStorageDir, leveldbDir)
	idbDir := filepath.Join(dir, indexedDBDir, idbOriginDir)
	ssDir := filepath.Join(dir, sessionStorageDir)

	for path, content := range map[string]string{
		filepath.Join(dir, cookiesFile):     "fake-cookies",
		filepath.Join(lsDir, "CURRENT"):     "MANIFEST-000001\n",
		filepath.Join(lsDir, "000001.ldb"):  "fake-ldb-data",
		filepath.Join(idbDir, "000001.log"): "fake-idb",
		filepath.Join(ssDir, "000001.log"):  "fake-ss",
		filepath.Join(dir, deviceIDFile):    "fake-device-id",
	} {
		mustWriteFile(t, path, content)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), filePerm); err != nil {
		t.Fatal(err)
	}
}
