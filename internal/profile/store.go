package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	storeDirName        = ".claude-swap"
	profilesDirName     = "profiles"
	currentFileName     = "current"
	cookiesFile         = "Cookies"
	cookiesJournalFile  = "Cookies-journal"
	cookiesWALFile      = "Cookies-wal"
	cookiesSHMFile      = "Cookies-shm"
	localStorageDir     = "Local Storage"
	leveldbDir          = "leveldb"
	indexedDBDir        = "IndexedDB"
	sessionStorageDir   = "Session Storage"
	deviceIDFile        = "ant-did"
	metaFile            = "meta.json"
	formatVersion       = 3
	digestFormatVersion = 2

	dirPerm  os.FileMode = 0700
	filePerm os.FileMode = 0600
)

type Meta struct {
	Name               string    `json:"name"`
	CreatedAt          time.Time `json:"created_at"`
	LastUsed           time.Time `json:"last_used,omitempty"`
	Email              string    `json:"email,omitempty"`
	Plan               string    `json:"plan,omitempty"`
	FormatVersion      int       `json:"format_version,omitempty"`
	SavedAt            time.Time `json:"saved_at,omitempty"`
	ObservedHealth     Health    `json:"observed_health,omitempty"`
	CookieDigest       string    `json:"cookie_digest,omitempty"`
	SessionDigest      string    `json:"session_digest,omitempty"`
	AccountFingerprint string    `json:"account_fingerprint,omitempty"`
}

type Store struct {
	baseDir string
	now     func() time.Time
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return newStore(filepath.Join(home, storeDirName))
}

func newStore(base string) (*Store, error) {
	profiles := filepath.Join(base, profilesDirName)
	if err := os.MkdirAll(profiles, dirPerm); err != nil {
		return nil, err
	}
	if err := os.Chmod(base, dirPerm); err != nil {
		return nil, err
	}
	if err := os.Chmod(profiles, dirPerm); err != nil {
		return nil, err
	}
	if err := securePath(base); err != nil {
		return nil, err
	}
	if err := securePath(profiles); err != nil {
		return nil, err
	}
	s := &Store{baseDir: base, now: time.Now}
	if err := s.recoverProfiles(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Exists(name string) bool {
	info, err := os.Stat(s.profileDir(name))
	return err == nil && info.IsDir()
}

func (s *Store) Save(name, appDataPath string) error {
	return s.Checkpoint(name, appDataPath)
}

func (s *Store) Checkpoint(name, appDataPath string) error {
	return s.CheckpointAt(name, appDataPath, filepath.Join(appDataPath, cookiesFile))
}

func (s *Store) CheckpointAt(name, appDataPath, live string) error {
	if !validProfileName(name) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	if inspection := InspectCookies(live, s.now()); inspection.Health != HealthUsable {
		return fmt.Errorf("refuse checkpoint of %s session: %s", inspection.Health, inspection.Reason)
	}
	if err := CheckpointCookies(live); err != nil {
		return fmt.Errorf("checkpoint Cookies WAL: %w", err)
	}
	if inspection := InspectCookies(live, s.now()); inspection.Health != HealthUsable {
		return fmt.Errorf("refuse checkpoint of %s session: %s", inspection.Health, inspection.Reason)
	}

	stage, err := os.MkdirTemp(s.profilesPath(), "."+name+".stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, dirPerm); err != nil {
		return err
	}
	stagedCookies := filepath.Join(stage, cookiesFile)
	if err := copyFile(live, stagedCookies); err != nil {
		return fmt.Errorf("stage cookies: %w", err)
	}
	for _, directory := range []struct {
		rel   string
		label string
	}{
		{filepath.Join(localStorageDir, leveldbDir), "Local Storage"},
		{indexedDBDir, "IndexedDB"},
		{sessionStorageDir, "Session Storage"},
	} {
		liveDirectory := filepath.Join(appDataPath, directory.rel)
		if info, err := os.Stat(liveDirectory); err == nil && info.IsDir() {
			if err := copyDir(liveDirectory, filepath.Join(stage, directory.rel)); err != nil {
				return fmt.Errorf("stage %s: %w", directory.label, err)
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	digest, err := cookieDigest(stagedCookies)
	if err != nil {
		return err
	}
	evidence, _ := readCookieEvidence(stagedCookies)
	meta := Meta{Name: name, CreatedAt: s.now(), FormatVersion: formatVersion, SavedAt: s.now(), ObservedHealth: HealthUsable, CookieDigest: digest, SessionDigest: evidence.sessionDigest, AccountFingerprint: evidence.accountFingerprint}
	if existing, err := s.loadMeta(name); err == nil {
		meta.CreatedAt = existing.CreatedAt
		meta.LastUsed = existing.LastUsed
		meta.Email = existing.Email
		meta.Plan = existing.Plan
		if meta.SessionDigest == "" {
			meta.SessionDigest = existing.SessionDigest
		}
	}
	if err := writeJSONAtomic(filepath.Join(stage, metaFile), meta); err != nil {
		return err
	}
	if inspection := InspectCookies(stagedCookies, s.now()); inspection.Health != HealthUsable {
		return fmt.Errorf("staged profile is %s: %s", inspection.Health, inspection.Reason)
	}
	if runtime.GOOS != "windows" {
		if err := syncTree(stage); err != nil {
			return err
		}
	}
	if err := securePath(stage); err != nil {
		return err
	}
	return s.commitProfile(name, stage)
}

func (s *Store) Inspect(name string) Inspection {
	if !s.Exists(name) {
		return Inspection{Health: HealthMissing, Reason: "profile is missing"}
	}
	if err := validateSecureTree(s.profileDir(name)); err != nil {
		return Inspection{Health: HealthUnknown, Reason: err.Error()}
	}
	cookies := filepath.Join(s.profileDir(name), cookiesFile)
	inspection := InspectCookies(cookies, s.now())
	if inspection.Health != HealthUsable {
		return inspection
	}
	meta, err := s.loadMeta(name)
	if err == nil && meta.FormatVersion >= digestFormatVersion && meta.CookieDigest != "" {
		digest, err := cookieDigest(cookies)
		if err != nil || digest != meta.CookieDigest {
			return Inspection{Health: HealthUnknown, Reason: "profile integrity digest does not match"}
		}
	}
	return inspection
}

func (s *Store) Restore(name, appDataPath string) error {
	return s.RestoreAt(name, appDataPath, filepath.Join(appDataPath, cookiesFile))
}

func (s *Store) RestoreAt(name, appDataPath, live string) error {
	if !validProfileName(name) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	inspection := s.Inspect(name)
	if inspection.Health != HealthUsable {
		return fmt.Errorf("profile %q is %s: %s", name, inspection.Health, inspection.Reason)
	}
	if err := os.MkdirAll(appDataPath, dirPerm); err != nil {
		return err
	}

	stage, err := os.MkdirTemp(appDataPath, ".claude-restore-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	stageCookies := filepath.Join(stage, cookiesFile)
	if err := copyFile(filepath.Join(s.profileDir(name), cookiesFile), stageCookies); err != nil {
		return fmt.Errorf("stage live cookies: %w", err)
	}
	if got := InspectCookies(stageCookies, s.now()); got.Health != HealthUsable {
		return fmt.Errorf("staged live cookies are %s: %s", got.Health, got.Reason)
	}
	stateDirectories := []struct {
		rel      string
		label    string
		rollback string
	}{
		{filepath.Join(localStorageDir, leveldbDir), "Local Storage", "leveldb"},
		{indexedDBDir, "IndexedDB", "indexeddb"},
		{sessionStorageDir, "Session Storage", "sessionstorage"},
	}
	for _, directory := range stateDirectories {
		snapshotDirectory := filepath.Join(s.profileDir(name), directory.rel)
		if info, err := os.Stat(snapshotDirectory); err == nil && info.IsDir() {
			if err := copyDir(snapshotDirectory, filepath.Join(stage, directory.rel)); err != nil {
				return fmt.Errorf("stage %s: %w", directory.label, err)
			}
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	rollbackRoot, err := os.MkdirTemp(appDataPath, ".claude-restore-rollback-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(rollbackRoot)
	backupCookies := filepath.Join(rollbackRoot, "cookies")
	backupData := filepath.Join(rollbackRoot, "data")
	if err := os.MkdirAll(backupCookies, dirPerm); err != nil {
		return err
	}
	if err := os.MkdirAll(backupData, dirPerm); err != nil {
		return err
	}

	type movedPath struct {
		live    string
		backup  string
		existed bool
	}
	var moved []movedPath
	moveToRollback := func(livePath, backupPath string) error {
		if _, err := os.Stat(livePath); errors.Is(err, os.ErrNotExist) {
			moved = append(moved, movedPath{live: livePath, backup: backupPath})
			return nil
		} else if err != nil {
			return err
		}
		if err := os.Rename(livePath, backupPath); err != nil {
			return err
		}
		moved = append(moved, movedPath{live: livePath, backup: backupPath, existed: true})
		return nil
	}
	rollback := func() {
		for index := len(moved) - 1; index >= 0; index-- {
			entry := moved[index]
			_ = os.RemoveAll(entry.live)
			if !entry.existed {
				continue
			}
			_ = os.MkdirAll(filepath.Dir(entry.live), dirPerm)
			_ = os.Rename(entry.backup, entry.live)
		}
	}

	for _, sidecar := range []string{cookiesJournalFile, cookiesWALFile, cookiesSHMFile} {
		if err := moveToRollback(filepath.Join(filepath.Dir(live), sidecar), filepath.Join(backupCookies, sidecar)); err != nil {
			rollback()
			return fmt.Errorf("retain Cookies sidecar: %w", err)
		}
	}
	if err := moveToRollback(live, filepath.Join(backupCookies, cookiesFile)); err != nil {
		rollback()
		return fmt.Errorf("retain live cookies: %w", err)
	}
	for _, directory := range stateDirectories {
		if err := moveToRollback(filepath.Join(appDataPath, directory.rel), filepath.Join(backupData, directory.rollback)); err != nil {
			rollback()
			return fmt.Errorf("retain live %s: %w", directory.label, err)
		}
	}

	if err := os.Rename(stageCookies, live); err != nil {
		rollback()
		return fmt.Errorf("commit live cookies: %w", err)
	}
	if err := os.Chmod(live, filePerm); err != nil {
		rollback()
		return err
	}
	for _, directory := range stateDirectories {
		stageDirectory := filepath.Join(stage, directory.rel)
		liveDirectory := filepath.Join(appDataPath, directory.rel)
		if _, err := os.Stat(stageDirectory); err == nil {
			if err := os.MkdirAll(filepath.Dir(liveDirectory), dirPerm); err != nil {
				rollback()
				return err
			}
			if err := os.Rename(stageDirectory, liveDirectory); err != nil {
				rollback()
				return fmt.Errorf("commit %s: %w", directory.label, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			rollback()
			return err
		}
	}
	previousMeta, metaErr := s.loadMeta(name)
	previousCurrent, currentErr := s.Current()
	if err := s.setLastUsed(name); err != nil {
		rollback()
		return err
	}
	if err := s.SetCurrent(name); err != nil {
		if metaErr == nil {
			_ = s.saveMeta(name, previousMeta)
		}
		if currentErr == nil {
			_ = s.SetCurrent(previousCurrent)
		} else {
			_ = os.RemoveAll(filepath.Join(s.baseDir, currentFileName))
		}
		rollback()
		return err
	}
	return nil
}

func (s *Store) Wipe(appDataPath string) error {
	return s.WipeAt(appDataPath, filepath.Join(appDataPath, cookiesFile))
}

func (s *Store) WipeAt(appDataPath, live string) error {
	for _, name := range []string{cookiesFile, cookiesJournalFile, cookiesWALFile, cookiesSHMFile} {
		if err := os.Remove(filepath.Join(filepath.Dir(live), name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("wipe %s: %w", name, err)
		}
	}
	return clearVolatile(appDataPath)
}

func HasActiveSession(appDataPath string) bool {
	return InspectCookies(filepath.Join(appDataPath, cookiesFile), time.Now()).Health == HealthUsable
}

func HasActiveSessionAt(cookiesPath string) bool {
	return InspectCookies(cookiesPath, time.Now()).Health == HealthUsable
}

func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(s.profilesPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	profiles := make([]Meta, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		meta, err := s.loadMeta(entry.Name())
		if err != nil {
			meta = Meta{Name: entry.Name()}
		}
		meta.ObservedHealth = s.Inspect(entry.Name()).Health
		profiles = append(profiles, meta)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

func (s *Store) IncompleteProfiles() ([]string, error) {
	profiles, err := s.List()
	if err != nil {
		return nil, err
	}
	var incomplete []string
	for _, meta := range profiles {
		if meta.FormatVersion < formatVersion {
			incomplete = append(incomplete, meta.Name)
		}
	}
	return incomplete, nil
}

func (s *Store) MatchLive(appDataPath string) (string, Health) {
	return s.MatchLiveAt(filepath.Join(appDataPath, cookiesFile))
}

func (s *Store) MatchLiveAt(live string) (string, Health) {
	inspection := InspectCookies(live, s.now())
	if inspection.Health != HealthUsable {
		return "", inspection.Health
	}
	digest, err := cookieDigest(live)
	if err != nil {
		return "", HealthUnknown
	}
	liveEvidence, _ := readCookieEvidence(live)
	profiles, err := s.List()
	if err != nil {
		return "", HealthUnknown
	}
	identityMatches := make([]string, 0, 1)
	for _, meta := range profiles {
		profileSessionDigest := meta.SessionDigest
		profileFingerprint := meta.AccountFingerprint
		if profileSessionDigest == "" || profileFingerprint == "" {
			evidence, _ := readCookieEvidence(filepath.Join(s.profileDir(meta.Name), cookiesFile))
			if profileSessionDigest == "" {
				profileSessionDigest = evidence.sessionDigest
			}
			if profileFingerprint == "" {
				profileFingerprint = evidence.accountFingerprint
			}
		}
		if liveEvidence.sessionDigest != "" && profileSessionDigest == liveEvidence.sessionDigest && s.Inspect(meta.Name).Health == HealthUsable {
			return meta.Name, HealthUsable
		}
		profileDigest := meta.CookieDigest
		if profileDigest == "" {
			profileDigest, _ = cookieDigest(filepath.Join(s.profileDir(meta.Name), cookiesFile))
		}
		if profileDigest == digest && s.Inspect(meta.Name).Health == HealthUsable {
			return meta.Name, HealthUsable
		}
		if liveEvidence.accountFingerprint != "" && profileFingerprint == liveEvidence.accountFingerprint && s.Inspect(meta.Name).Health == HealthUsable {
			identityMatches = append(identityMatches, meta.Name)
		}
	}
	if len(identityMatches) == 1 {
		return identityMatches[0], HealthUsable
	}
	return "", HealthUsable
}

func (s *Store) UpdateAccountInfo(name, email, plan string) error {
	meta, err := s.loadMeta(name)
	if err != nil {
		return err
	}
	if email != "" {
		meta.Email = email
	}
	if plan != "" {
		meta.Plan = plan
	}
	return s.saveMeta(name, meta)
}

func (s *Store) Delete(name string) error {
	if !validProfileName(name) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	if !s.Exists(name) {
		return fmt.Errorf("profile %q not found", name)
	}
	current, _ := s.Current()
	if current == name {
		_ = os.Remove(filepath.Join(s.baseDir, currentFileName))
	}
	return os.RemoveAll(s.profileDir(name))
}

func (s *Store) Current() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.baseDir, currentFileName))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Store) SetCurrent(name string) error {
	return writeFileAtomic(filepath.Join(s.baseDir, currentFileName), []byte(name))
}

func (s *Store) setLastUsed(name string) error {
	meta, err := s.loadMeta(name)
	if err != nil {
		return err
	}
	meta.LastUsed = s.now()
	return s.saveMeta(name, meta)
}

func (s *Store) commitProfile(name, stage string) error {
	final := s.profileDir(name)
	backup := filepath.Join(s.profilesPath(), "."+name+".backup")
	_ = os.RemoveAll(backup)
	hadBackup := false
	if _, err := os.Stat(final); err == nil {
		if err := os.Rename(final, backup); err != nil {
			return err
		}
		hadBackup = true
	}
	if err := os.Rename(stage, final); err != nil {
		_ = os.Rename(backup, final)
		return err
	}
	if err := syncDir(s.profilesPath()); err != nil {
		if hadBackup {
			_ = os.RemoveAll(final)
			_ = os.Rename(backup, final)
		}
		return err
	}
	return os.RemoveAll(backup)
}

func (s *Store) recoverProfiles() error {
	entries, err := os.ReadDir(s.profilesPath())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".backup") && strings.HasPrefix(name, ".") {
			profileName := strings.TrimSuffix(strings.TrimPrefix(name, "."), ".backup")
			final := s.profileDir(profileName)
			if _, err := os.Stat(final); errors.Is(err, os.ErrNotExist) {
				if err := os.Rename(filepath.Join(s.profilesPath(), name), final); err != nil {
					return err
				}
			} else {
				_ = os.RemoveAll(filepath.Join(s.profilesPath(), name))
			}
		}
		if strings.Contains(name, ".stage-") && strings.HasPrefix(name, ".") {
			_ = os.RemoveAll(filepath.Join(s.profilesPath(), name))
		}
	}
	return nil
}

func (s *Store) ProfileCookiesPath(name string) string {
	return filepath.Join(s.profileDir(name), cookiesFile)
}

func (s *Store) profileDir(name string) string { return filepath.Join(s.profilesPath(), name) }
func (s *Store) profilesPath() string          { return filepath.Join(s.baseDir, profilesDirName) }

func (s *Store) loadMeta(name string) (Meta, error) {
	data, err := os.ReadFile(filepath.Join(s.profileDir(name), metaFile))
	if err != nil {
		return Meta{}, err
	}
	var meta Meta
	return meta, json.Unmarshal(data, &meta)
}

func (s *Store) saveMeta(name string, meta Meta) error {
	return writeJSONAtomic(filepath.Join(s.profileDir(name), metaFile), meta)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(filePerm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, filePerm)
	if err != nil {
		return err
	}
	if err := out.Chmod(filePerm); err != nil {
		out.Close()
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, dirPerm); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		from := filepath.Join(src, entry.Name())
		to := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(from, to); err != nil {
				return err
			}
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		if err := copyFile(from, to); err != nil {
			return err
		}
	}
	return nil
}

func clearVolatile(appDataPath string) error {
	for _, path := range []string{
		filepath.Join(appDataPath, localStorageDir, leveldbDir),
		filepath.Join(appDataPath, indexedDBDir),
		filepath.Join(appDataPath, sessionStorageDir),
	} {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("clear %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

// validateSecureTree rejects group/other-readable files. Windows has no
// POSIX mode bits — os.FileMode there is synthesized from file attributes
// and duplicates owner bits into group/other, so the check is meaningless;
// Windows enforces access via ACLs instead, which this does not inspect.
func validateSecureTree(root string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&0077 != 0 {
			return fmt.Errorf("unsafe permissions on %s", filepath.Base(path))
		}
		return nil
	})
}

func syncTree(root string) error {
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		// Opened read-write because Windows' FlushFileBuffers (what
		// File.Sync calls) requires a write-capable handle; a read-only
		// os.Open succeeds but Sync() then fails with "Access is denied".
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		return f.Sync()
	}); err != nil {
		return err
	}
	return syncDir(root)
}

// syncDir fsyncs a directory's metadata so a prior rename/create is durable.
// Windows has no equivalent: a directory handle opened read-only cannot be
// flushed (FlushFileBuffers returns "Access is denied"), and NTFS journals
// directory changes itself, so the fsync is unnecessary there.
func syncDir(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
