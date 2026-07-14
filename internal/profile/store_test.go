package profile

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCheckpointCreatesSecureV3ProfileWithAccountState(t *testing.T) {
	appData := syntheticAppData(t, "live")
	store := newTestStore(t)
	if err := store.Checkpoint("work", appData); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	entries, err := os.ReadDir(store.profileDir("work"))
	if err != nil {
		t.Fatal(err)
	}
	if got := entryNames(entries); len(got) != 5 || got[0] != cookiesFile || got[1] != indexedDBDir || got[2] != localStorageDir || got[3] != sessionStorageDir || got[4] != metaFile {
		t.Fatalf("profile artifacts = %v, want complete account state", got)
	}
	assertMode(t, store.profileDir("work"), dirPerm)
	assertMode(t, filepath.Join(store.profileDir("work"), cookiesFile), filePerm)
	assertMode(t, filepath.Join(store.profileDir("work"), metaFile), filePerm)
	snapshot := filepath.Join(store.profileDir("work"), localStorageDir, leveldbDir, "CURRENT")
	assertMode(t, filepath.Join(store.profileDir("work"), localStorageDir, leveldbDir), dirPerm)
	assertMode(t, snapshot, filePerm)
	assertMode(t, filepath.Join(store.profileDir("work"), indexedDBDir, "data"), filePerm)
	assertMode(t, filepath.Join(store.profileDir("work"), sessionStorageDir, "data"), filePerm)
	meta, err := store.loadMeta("work")
	if err != nil {
		t.Fatal(err)
	}
	if meta.FormatVersion != 3 || meta.ObservedHealth != HealthUsable || meta.CookieDigest == "" || meta.SavedAt.IsZero() {
		t.Fatalf("incomplete v3 metadata: %+v", meta)
	}
}

func TestCheckpointRefusesUnusableLiveStateWithoutOverwritingProfile(t *testing.T) {
	store := newTestStore(t)
	healthy := syntheticAppData(t, "healthy")
	if err := store.Checkpoint("work", healthy); err != nil {
		t.Fatal(err)
	}
	before, _ := cookieDigest(filepath.Join(store.profileDir("work"), cookiesFile))

	unusable := t.TempDir()
	createCookiesDB(t, filepath.Join(unusable, cookiesFile), ".claude.ai", "other", 0)
	if err := store.Checkpoint("work", unusable); err == nil {
		t.Fatal("Checkpoint should reject missing session evidence")
	}
	after, _ := cookieDigest(filepath.Join(store.profileDir("work"), cookiesFile))
	if after != before {
		t.Fatal("unusable live state overwrote the saved usable profile")
	}
}

func TestRestoreRefusesUnsafePermissionsAndIntegrityMismatchBeforeMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *Store)
	}{
		{"unsafe permissions", func(t *testing.T, s *Store) {
			if runtime.GOOS == "windows" {
				t.Skip("Windows has no POSIX mode bits to make unsafe")
			}
			if err := os.Chmod(filepath.Join(s.profileDir("work"), cookiesFile), 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"digest mismatch", func(t *testing.T, s *Store) {
			path := filepath.Join(s.profileDir("work"), cookiesFile)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			createCookiesDB(t, path, ".claude.ai", "sessionKey", 0)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			appData := syntheticAppData(t, "saved")
			if err := store.Checkpoint("work", appData); err != nil {
				t.Fatal(err)
			}
			live := syntheticAppData(t, "live-before")
			before, _ := cookieDigest(filepath.Join(live, cookiesFile))
			tt.mutate(t, store)
			if err := store.Restore("work", live); err == nil {
				t.Fatal("Restore should refuse invalid profile")
			}
			after, _ := cookieDigest(filepath.Join(live, cookiesFile))
			if after != before {
				t.Fatal("live Cookies changed before validation completed")
			}
		})
	}
}

func TestV2CookieIntegrityRemainsEnforced(t *testing.T) {
	store := newTestStore(t)
	appData := syntheticAppData(t, "saved")
	if err := store.Checkpoint("work", appData); err != nil {
		t.Fatal(err)
	}
	meta, err := store.loadMeta("work")
	if err != nil {
		t.Fatal(err)
	}
	meta.FormatVersion = 2
	if err := store.saveMeta("work", meta); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.profileDir("work"), cookiesFile)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	createCookiesDBWithMarker(t, path, "tampered")

	if got := store.Inspect("work"); got.Health != HealthUnknown {
		t.Fatalf("v2 integrity health = %s, want unknown", got.Health)
	}
}

func TestHealthyV1RestoresAndMigratesOnlyOnCheckpoint(t *testing.T) {
	store := newTestStore(t)
	v1 := store.profileDir("legacy")
	if err := os.MkdirAll(filepath.Join(v1, "leveldb"), dirPerm); err != nil {
		t.Fatal(err)
	}
	createCookiesDB(t, filepath.Join(v1, cookiesFile), ".claude.ai", "sessionKey", 0)
	mustWriteFile(t, filepath.Join(v1, "leveldb", "legacy"), "keep-until-commit")
	mustWriteMeta(t, filepath.Join(v1, metaFile), Meta{Name: "legacy", CreatedAt: time.Now()})
	live := syntheticAppData(t, "other")

	if err := store.Restore("legacy", live); err != nil {
		t.Fatalf("restore v1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(v1, "leveldb", "legacy")); err != nil {
		t.Fatal("restore eagerly migrated v1")
	}
	if err := store.Checkpoint("legacy", live); err != nil {
		t.Fatalf("checkpoint migrated v1: %v", err)
	}
	if _, err := os.Stat(filepath.Join(v1, "leveldb")); !os.IsNotExist(err) {
		t.Fatal("legacy payload remains after v2 commit")
	}
	meta, _ := store.loadMeta("legacy")
	if meta.FormatVersion != 3 {
		t.Fatalf("format version = %d", meta.FormatVersion)
	}
}

func TestUnusableV1IsNotRepaired(t *testing.T) {
	store := newTestStore(t)
	v1 := store.profileDir("expired")
	if err := os.MkdirAll(v1, dirPerm); err != nil {
		t.Fatal(err)
	}
	createCookiesDB(t, filepath.Join(v1, cookiesFile), ".claude.ai", "sessionKey", chromiumTime(store.now().Add(-time.Hour)))
	live := syntheticAppData(t, "live")
	if err := store.Restore("expired", live); err == nil {
		t.Fatal("expired v1 should require reauthentication")
	}
	meta, _ := store.loadMeta("expired")
	if meta.FormatVersion == 2 {
		t.Fatal("expired v1 was synthesized as v2")
	}
}

func TestStoreRecoversBackupAndRemovesOrphanStage(t *testing.T) {
	base := t.TempDir()
	profiles := filepath.Join(base, profilesDirName)
	backup := filepath.Join(profiles, ".work.backup")
	stage := filepath.Join(profiles, ".work.stage-dead")
	if err := os.MkdirAll(backup, dirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stage, dirPerm); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(backup, metaFile), `{}`)
	store, err := newStore(base)
	if err != nil {
		t.Fatal(err)
	}
	if !store.Exists("work") {
		t.Fatal("backup was not recovered")
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Fatal("orphan stage was not removed")
	}
}

func TestRestoreReplacesCompleteAccountStateAndPreservesGlobalState(t *testing.T) {
	store := newTestStore(t)
	saved := syntheticAppData(t, "saved")
	db := openSQLite(t, filepath.Join(saved, cookiesFile))
	mustExec(t, db, `INSERT INTO cookies(host_key, name, expires_utc, value, encrypted_value) VALUES ('.claude.ai', 'cf_clearance', 0, '', x'03')`)
	mustExec(t, db, `INSERT INTO cookies(host_key, name, expires_utc, value, encrypted_value) VALUES ('.claude.ai', '__cf_bm', 0, '', x'04')`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Checkpoint("work", saved); err != nil {
		t.Fatal(err)
	}
	live := syntheticAppData(t, "live")
	for _, name := range []string{cookiesJournalFile, cookiesWALFile, cookiesSHMFile} {
		mustWriteFile(t, filepath.Join(live, name), "stale")
	}
	global := map[string]string{"config.json": "global", "WebStorage/state": "web", "partitions/p": "partition", deviceIDFile: "machine"}
	for path, content := range global {
		mustWriteFile(t, filepath.Join(live, path), content)
	}

	if err := store.Restore("work", live); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restored := filepath.Join(live, localStorageDir, leveldbDir, "CURRENT")
	if got, err := os.ReadFile(restored); err != nil || string(got) != "saved" {
		t.Fatalf("Local Storage not restored from snapshot: %q %v", got, err)
	}
	for _, path := range []string{filepath.Join(indexedDBDir, "data"), filepath.Join(sessionStorageDir, "data")} {
		if got, err := os.ReadFile(filepath.Join(live, path)); err != nil || string(got) != "saved" {
			t.Fatalf("%s not restored from snapshot: %q %v", path, got, err)
		}
	}
	db = openSQLite(t, filepath.Join(live, cookiesFile))
	defer db.Close()
	var securityCookies int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cookies WHERE name IN ('cf_clearance', '__cf_bm')`).Scan(&securityCookies); err != nil {
		t.Fatal(err)
	}
	if securityCookies != 2 {
		t.Fatalf("restored security cookies = %d, want 2", securityCookies)
	}
	for _, name := range []string{cookiesJournalFile, cookiesWALFile, cookiesSHMFile} {
		if _, err := os.Stat(filepath.Join(live, name)); !os.IsNotExist(err) {
			t.Fatalf("sidecar remains: %s", name)
		}
	}
	for path, want := range global {
		got, err := os.ReadFile(filepath.Join(live, path))
		if err != nil || string(got) != want {
			t.Fatalf("global %s changed: %q %v", path, got, err)
		}
	}
}

func TestRestoreWithoutSnapshotLeavesAccountStateCleared(t *testing.T) {
	store := newTestStore(t)
	saved := t.TempDir()
	createCookiesDBWithMarker(t, filepath.Join(saved, cookiesFile), "saved")
	if err := store.Checkpoint("work", saved); err != nil {
		t.Fatal(err)
	}
	live := syntheticAppData(t, "live")
	if err := store.Restore("work", live); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, path := range []string{filepath.Join(localStorageDir, leveldbDir), indexedDBDir, sessionStorageDir} {
		if _, err := os.Stat(filepath.Join(live, path)); !os.IsNotExist(err) {
			t.Fatalf("live %s should stay cleared when the profile has no snapshot", path)
		}
	}
}

func TestRestoreTrackingFailureRollsBackCookies(t *testing.T) {
	store := newTestStore(t)
	saved := syntheticAppData(t, "saved")
	if err := store.Checkpoint("work", saved); err != nil {
		t.Fatal(err)
	}
	live := syntheticAppData(t, "live")
	before, _ := cookieDigest(filepath.Join(live, cookiesFile))
	localPath := filepath.Join(live, localStorageDir, leveldbDir, "CURRENT")
	localBefore, _ := os.ReadFile(localPath)
	indexedPath := filepath.Join(live, indexedDBDir, "data")
	indexedBefore, _ := os.ReadFile(indexedPath)
	sessionPath := filepath.Join(live, sessionStorageDir, "data")
	sessionBefore, _ := os.ReadFile(sessionPath)
	if err := os.Mkdir(filepath.Join(store.baseDir, currentFileName), dirPerm); err != nil {
		t.Fatal(err)
	}

	if err := store.Restore("work", live); err == nil {
		t.Fatal("Restore should fail when tracking cannot commit")
	}
	after, _ := cookieDigest(filepath.Join(live, cookiesFile))
	if after != before {
		t.Fatal("live Cookies were not rolled back")
	}
	localAfter, _ := os.ReadFile(localPath)
	if string(localAfter) != string(localBefore) {
		t.Fatal("live Local Storage was not rolled back")
	}
	indexedAfter, _ := os.ReadFile(indexedPath)
	if string(indexedAfter) != string(indexedBefore) {
		t.Fatal("live IndexedDB was not rolled back")
	}
	sessionAfter, _ := os.ReadFile(sessionPath)
	if string(sessionAfter) != string(sessionBefore) {
		t.Fatal("live Session Storage was not rolled back")
	}
}

func TestRestoreFailureRemovesNewAccountStateWhenLiveHadNone(t *testing.T) {
	store := newTestStore(t)
	saved := syntheticAppData(t, "saved")
	if err := store.Checkpoint("work", saved); err != nil {
		t.Fatal(err)
	}
	live := t.TempDir()
	createCookiesDBWithMarker(t, filepath.Join(live, cookiesFile), "live")
	before, _ := cookieDigest(filepath.Join(live, cookiesFile))
	if err := os.Mkdir(filepath.Join(store.baseDir, currentFileName), dirPerm); err != nil {
		t.Fatal(err)
	}

	if err := store.Restore("work", live); err == nil {
		t.Fatal("Restore should fail when tracking cannot commit")
	}
	after, _ := cookieDigest(filepath.Join(live, cookiesFile))
	if after != before {
		t.Fatal("live Cookies were not rolled back")
	}
	for _, path := range []string{filepath.Join(localStorageDir, leveldbDir), indexedDBDir, sessionStorageDir} {
		if _, err := os.Stat(filepath.Join(live, path)); !os.IsNotExist(err) {
			t.Fatalf("new account state remains after rollback: %s", path)
		}
	}
}

func TestMatchLiveDoesNotTrustStaleCurrent(t *testing.T) {
	store := newTestStore(t)
	a := syntheticAppData(t, "a")
	b := syntheticAppData(t, "b")
	if err := store.Checkpoint("a", a); err != nil {
		t.Fatal(err)
	}
	if err := store.SetCurrent("a"); err != nil {
		t.Fatal(err)
	}
	if name, health := store.MatchLive(b); name != "" || health != HealthUsable {
		t.Fatalf("match = %q/%s, want unknown usable", name, health)
	}
	if name, health := store.MatchLive(a); name != "a" || health != HealthUsable {
		t.Fatalf("match = %q/%s, want a/usable", name, health)
	}
}

func TestMatchLiveRecognizesRenewedSessionByAccountFingerprint(t *testing.T) {
	store := newTestStore(t)
	saved := syntheticAppData(t, "old-session")
	setAccountIdentity(t, filepath.Join(saved, cookiesFile), "org-a", "route-a")
	if err := store.Checkpoint("personal", saved); err != nil {
		t.Fatal(err)
	}
	meta, err := store.loadMeta("personal")
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.AccountFingerprint) != sha256.Size*2 || strings.Contains(meta.AccountFingerprint, "org-a") || strings.Contains(meta.AccountFingerprint, "route-a") {
		t.Fatalf("unsafe account fingerprint metadata: %q", meta.AccountFingerprint)
	}
	live := syntheticAppData(t, "renewed-session")
	setAccountIdentity(t, filepath.Join(live, cookiesFile), "org-a", "route-a")

	if name, health := store.MatchLive(live); name != "personal" || health != HealthUsable {
		t.Fatalf("match = %q/%s, want personal/usable", name, health)
	}
	differentAccount := syntheticAppData(t, "different-session")
	setAccountIdentity(t, filepath.Join(differentAccount, cookiesFile), "org-a", "route-b")
	if name, health := store.MatchLive(differentAccount); name != "" || health != HealthUsable {
		t.Fatalf("match = %q/%s, want different usable account", name, health)
	}
}

func TestMatchLiveRejectsAmbiguousAccountFingerprint(t *testing.T) {
	store := newTestStore(t)
	for _, name := range []string{"first", "second"} {
		appData := syntheticAppData(t, name+"-session")
		setAccountIdentity(t, filepath.Join(appData, cookiesFile), "shared-org", "shared-route")
		if err := store.Checkpoint(name, appData); err != nil {
			t.Fatal(err)
		}
	}
	live := syntheticAppData(t, "renewed-session")
	setAccountIdentity(t, filepath.Join(live, cookiesFile), "shared-org", "shared-route")

	if name, health := store.MatchLive(live); name != "" || health != HealthUsable {
		t.Fatalf("match = %q/%s, want ambiguous usable session", name, health)
	}
}

func TestWipePreservesMachineAndGlobalFiles(t *testing.T) {
	store := newTestStore(t)
	appData := syntheticAppData(t, "live")
	for path, content := range map[string]string{deviceIDFile: "device", "config.json": "config"} {
		mustWriteFile(t, filepath.Join(appData, path), content)
	}
	if err := store.Wipe(appData); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{deviceIDFile, "config.json"} {
		if _, err := os.Stat(filepath.Join(appData, path)); err != nil {
			t.Fatalf("%s was removed", path)
		}
	}
}

func TestProfileCookiesPathResolvesUnderProfile(t *testing.T) {
	store := newTestStore(t)
	got := store.ProfileCookiesPath("work")
	want := filepath.Join(store.profileDir("work"), cookiesFile)
	if got != want {
		t.Fatalf("ProfileCookiesPath = %q, want %q", got, want)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := newStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC) }
	return store
}

func syntheticAppData(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	createCookiesDBWithMarker(t, filepath.Join(dir, cookiesFile), marker)
	for _, path := range []string{filepath.Join(localStorageDir, leveldbDir, "CURRENT"), filepath.Join(indexedDBDir, "data"), filepath.Join(sessionStorageDir, "data")} {
		mustWriteFile(t, filepath.Join(dir, path), marker)
	}
	return dir
}

func createCookiesDBWithMarker(t *testing.T, path, marker string) {
	t.Helper()
	db := openSQLite(t, path)
	mustExec(t, db, `CREATE TABLE cookies (host_key TEXT, name TEXT, expires_utc INTEGER, value TEXT, encrypted_value BLOB)`)
	mustExec(t, db, `CREATE TABLE fixture_marker (marker TEXT)`)
	mustExec(t, db, `INSERT INTO cookies(host_key, name, expires_utc, value, encrypted_value) VALUES ('.claude.ai', 'sessionKey', 0, ?, ?)`, marker, []byte(marker))
	mustExec(t, db, `INSERT INTO fixture_marker VALUES (?)`, marker)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func setAccountIdentity(t *testing.T, path, org, routing string) {
	t.Helper()
	db := openSQLite(t, path)
	defer db.Close()
	for name, value := range map[string]string{"lastActiveOrg": org, "routingHint": routing} {
		mustExec(t, db, `INSERT INTO cookies(host_key, name, expires_utc, value, encrypted_value) VALUES ('.claude.ai', ?, 0, '', ?)`, name, []byte(value))
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

func mustWriteMeta(t *testing.T, path string, meta Meta) {
	t.Helper()
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, path, string(data))
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}
