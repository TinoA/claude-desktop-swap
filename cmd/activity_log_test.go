//go:build windows

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestActivityLoggerRedactsPrivateValues(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "logs", "tray.log")
	now := func() time.Time { return time.Date(2026, 7, 23, 10, 30, 0, 0, time.UTC) }
	logger, err := newActivityLoggerWithOptions(path, home, 4096, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	message := "Active account: person@example.test 11111111-1111-4111-8111-111111111111 at " + home + ` sessionKey=secret token: hidden`
	if err := logger.Write(message); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, private := range []string{"person@example.test", "11111111-1111-4111-8111-111111111111", home, "secret", "hidden"} {
		if strings.Contains(text, private) {
			t.Fatalf("private value was logged: %q", private)
		}
	}
	for _, replacement := range []string{"[email]", "[uuid]", "%USERPROFILE%", "sessionKey=[redacted]", "token: [redacted]"} {
		if !strings.Contains(text, replacement) {
			t.Fatalf("redaction %q is missing from %q", replacement, text)
		}
	}
}

func TestActivityLoggerKeepsOnlyConfiguredFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "logs", "tray.log")
	now := func() time.Time { return time.Date(2026, 7, 23, 10, 30, 0, 0, time.UTC) }
	logger, err := newActivityLoggerWithOptions(path, root, 120, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 8 {
		if err := logger.Write(strings.Repeat(string(rune('a'+index)), 70)); err != nil {
			t.Fatal(err)
		}
	}
	for _, candidate := range []string{path, path + ".1", path + ".2"} {
		if _, err := os.Stat(candidate); err != nil {
			t.Fatalf("expected rotated log %s: %v", candidate, err)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("unexpected fourth log file: %v", err)
	}
}

func TestActivityLoggerSingleFileStillRotates(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tray.log")
	now := func() time.Time { return time.Date(2026, 7, 23, 10, 30, 0, 0, time.UTC) }
	logger, err := newActivityLoggerWithOptions(path, root, 100, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := logger.Write(strings.Repeat("x", 60)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf("single-file logger created a backup: %v", err)
	}
}

func TestActivityLoggerSerializesConcurrentWrites(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tray.log")
	logger, err := newActivityLoggerWithOptions(path, root, 1<<20, 3, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	for index := range 20 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			if err := logger.Write("concurrent entry"); err != nil {
				t.Errorf("writer %d: %v", index, err)
			}
		}()
	}
	writers.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(data), "\n"); lines != 20 {
		t.Fatalf("logged lines = %d, want 20", lines)
	}
}
