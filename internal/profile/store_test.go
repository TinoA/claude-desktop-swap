package profile

import (
	"os"
	"path/filepath"
	"testing"
)

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
	if err := os.WriteFile(filepath.Join(appData, "Cookies"), []byte("new-session"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := store.Restore("test", appData); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(appData, "Cookies"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fake-cookies" {
		t.Errorf("Cookies = %q, want %q", got, "fake-cookies")
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

func newTestStore(t *testing.T) *Store {
	t.Helper()
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	return &Store{baseDir: base}
}

func setupFakeAppData(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "Cookies"), []byte("fake-cookies"), 0600); err != nil {
		t.Fatal(err)
	}
	lsDir := filepath.Join(dir, "Local Storage", "leveldb")
	if err := os.MkdirAll(lsDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lsDir, "CURRENT"), []byte("MANIFEST-000001\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lsDir, "000001.ldb"), []byte("fake-ldb-data"), 0600); err != nil {
		t.Fatal(err)
	}
}
