package profile

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	storeDirName    = ".claude-swap"
	profilesDirName = "profiles"
	currentFileName = "current"
	cookiesFile     = "Cookies"
	localStorageDir = "Local Storage"
	leveldbDir      = "leveldb"
	metaFile        = "meta.json"

	dirPerm  os.FileMode = 0700
	filePerm os.FileMode = 0600
)

type Meta struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	LastUsed  time.Time `json:"last_used,omitempty"`
}

type Store struct {
	baseDir string
}

func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(home, storeDirName)
	if err := os.MkdirAll(filepath.Join(base, profilesDirName), dirPerm); err != nil {
		return nil, err
	}
	return &Store{baseDir: base}, nil
}

func (s *Store) Exists(name string) bool {
	_, err := os.Stat(s.profileDir(name))
	return err == nil
}

// Save snapshots the current Claude session as a named profile.
// If the profile already exists it is overwritten.
func (s *Store) Save(name, appDataPath string) error {
	dir := s.profileDir(name)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}

	if err := copyFile(
		filepath.Join(appDataPath, cookiesFile),
		filepath.Join(dir, cookiesFile),
	); err != nil {
		return fmt.Errorf("copy cookies: %w", err)
	}

	if err := copyDir(
		filepath.Join(appDataPath, localStorageDir, leveldbDir),
		filepath.Join(dir, leveldbDir),
	); err != nil {
		return fmt.Errorf("copy local storage: %w", err)
	}

	meta := Meta{Name: name, CreatedAt: time.Now()}
	if existing, err := s.loadMeta(name); err == nil {
		meta.CreatedAt = existing.CreatedAt
	}
	return s.saveMeta(name, meta)
}

// Restore replaces the current Claude session with the named profile.
func (s *Store) Restore(name, appDataPath string) error {
	dir := s.profileDir(name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("profile %q not found", name)
	}

	if err := copyFile(
		filepath.Join(dir, cookiesFile),
		filepath.Join(appDataPath, cookiesFile),
	); err != nil {
		return fmt.Errorf("restore cookies: %w", err)
	}

	lsPath := filepath.Join(appDataPath, localStorageDir, leveldbDir)
	if err := os.RemoveAll(lsPath); err != nil {
		return fmt.Errorf("clear local storage: %w", err)
	}
	if err := copyDir(filepath.Join(dir, leveldbDir), lsPath); err != nil {
		return fmt.Errorf("restore local storage: %w", err)
	}

	meta, _ := s.loadMeta(name)
	meta.Name = name
	meta.LastUsed = time.Now()
	_ = s.saveMeta(name, meta)

	return s.SetCurrent(name)
}

func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(filepath.Join(s.baseDir, profilesDirName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var profiles []Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := s.loadMeta(e.Name())
		if err != nil {
			continue
		}
		profiles = append(profiles, meta)
	}
	return profiles, nil
}

func (s *Store) Delete(name string) error {
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
	return os.WriteFile(filepath.Join(s.baseDir, currentFileName), []byte(name), filePerm)
}

func (s *Store) profileDir(name string) string {
	return filepath.Join(s.baseDir, profilesDirName, name)
}

func (s *Store) loadMeta(name string) (Meta, error) {
	data, err := os.ReadFile(filepath.Join(s.profileDir(name), metaFile))
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	return m, json.Unmarshal(data, &m)
}

func (s *Store) saveMeta(name string, m Meta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.profileDir(name), metaFile), data, filePerm)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, dirPerm)
		}
		return copyFile(path, target)
	})
}
