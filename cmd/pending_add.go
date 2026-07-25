package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pendingAdd struct {
	Name               string    `json:"name"`
	Previous           string    `json:"previous"`
	AppData            string    `json:"app_data"`
	Live               string    `json:"live_cookies"`
	CreatedAt          time.Time `json:"created_at"`
	LoginLogOffset     int64     `json:"login_log_offset,omitempty"`
	PersistenceRetries int       `json:"persistence_retries,omitempty"`
}

func pendingAddPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude-swap", "pending.json"), nil
}

func writePendingAdd(value pendingAdd) error {
	path, err := pendingAddPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pending-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func clearPendingAdd() error {
	path, err := pendingAddPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func loadPendingAdd() (pendingAdd, error) {
	path, err := pendingAddPath()
	if err != nil {
		return pendingAdd{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return pendingAdd{}, err
	}
	var value pendingAdd
	if err := json.Unmarshal(data, &value); err != nil {
		return pendingAdd{}, err
	}
	return value, nil
}

func writeAddDiagnostic(name, state, detail string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".claude-swap")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	_ = os.Chmod(dir, 0700)
	clean := func(value string) string {
		value = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(value)
		runes := []rune(value)
		if len(runes) > 512 {
			value = string(runes[:512])
		}
		return value
	}
	line := fmt.Sprintf("%s\t%s\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339), clean(state), clean(name), clean(detail))
	path := filepath.Join(dir, "last-add.log")
	if err := os.WriteFile(path, []byte(line), 0600); err == nil {
		_ = os.Chmod(path, 0600)
	}
}
