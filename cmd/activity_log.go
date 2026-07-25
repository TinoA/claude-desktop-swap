package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	activityLogMaxBytes  int64 = 1 << 20
	activityLogFileCount       = 3
)

var (
	activityEmailPattern     = regexp.MustCompile(`(?i)[a-z0-9._%+\-]{1,64}@[a-z0-9.\-]{1,253}\.[a-z]{2,63}`)
	activityUUIDPattern      = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
	activitySensitivePattern = regexp.MustCompile(`(?i)\b(sessionkey|access_token|refresh_token|token|password|cookie)\b(\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
)

type activityLogger struct {
	mu          sync.Mutex
	path        string
	homePattern *regexp.Regexp
	maxBytes    int64
	fileCount   int
	now         func() time.Time
}

func newActivityLogger() (*activityLogger, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return newActivityLoggerWithOptions(
		filepath.Join(home, ".claude-swap", "logs", "tray.log"),
		home,
		activityLogMaxBytes,
		activityLogFileCount,
		time.Now,
	)
}

func newActivityLoggerWithOptions(path, home string, maxBytes int64, fileCount int, now func() time.Time) (*activityLogger, error) {
	if maxBytes <= 0 || fileCount < 1 {
		return nil, errors.New("invalid activity log limits")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	logger := &activityLogger{
		path:      path,
		maxBytes:  maxBytes,
		fileCount: fileCount,
		now:       now,
	}
	if strings.TrimSpace(home) != "" {
		logger.homePattern = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(filepath.Clean(home)))
	}
	return logger, nil
}

func (l *activityLogger) Directory() string {
	return filepath.Dir(l.path)
}

func (l *activityLogger) Write(message string) error {
	if l == nil {
		return nil
	}
	line := fmt.Sprintf("%s %s\n", l.now().Format(time.RFC3339), l.sanitize(message))
	l.mu.Lock()
	defer l.mu.Unlock()
	if info, err := os.Stat(l.path); err == nil && info.Size() > 0 && info.Size()+int64(len(line)) > l.maxBytes {
		if err := l.rotate(); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	_, err = file.WriteString(line)
	return err
}

func (l *activityLogger) rotate() error {
	if l.fileCount == 1 {
		if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	for index := l.fileCount - 1; index >= 1; index-- {
		destination := fmt.Sprintf("%s.%d", l.path, index)
		source := l.path
		if index > 1 {
			source = fmt.Sprintf("%s.%d", l.path, index-1)
		}
		if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(source, destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (l *activityLogger) sanitize(message string) string {
	message = strings.Map(func(value rune) rune {
		if value < 32 {
			return ' '
		}
		return value
	}, message)
	message = strings.Join(strings.Fields(message), " ")
	if l.homePattern != nil {
		message = l.homePattern.ReplaceAllString(message, "%USERPROFILE%")
	}
	message = activityEmailPattern.ReplaceAllString(message, "[email]")
	message = activityUUIDPattern.ReplaceAllString(message, "[uuid]")
	message = activitySensitivePattern.ReplaceAllString(message, "${1}${2}[redacted]")
	runes := []rune(message)
	if len(runes) > 1000 {
		message = string(runes[:1000])
	}
	return message
}
