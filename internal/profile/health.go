package profile

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Health string

const (
	HealthUsable  Health = "usable"
	HealthExpired Health = "expired"
	HealthMissing Health = "missing"
	HealthUnknown Health = "unknown"
)

type Inspection struct {
	Health Health
	Reason string
}

type cookieEvidence struct {
	sessionDigest      string
	accountFingerprint string
}

const chromiumEpochOffsetMicros int64 = 11644473600000000

const inspectionTimeout = 500 * time.Millisecond

// sqlitePath converts an OS path into the form net/url.URL expects for a
// file: scheme, so a Windows drive letter (e.g. C:\Users\...) isn't parsed
// as a URL host.
func sqlitePath(path string) string {
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return slashed
}

func InspectCookies(path string, now time.Time) Inspection {
	return inspectCookiesWithTimeout(path, now, inspectionTimeout)
}

func inspectCookiesWithTimeout(path string, now time.Time, timeout time.Duration) Inspection {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return Inspection{Health: HealthMissing, Reason: "Cookies database is missing"}
	} else if err != nil {
		return Inspection{Health: HealthUnknown, Reason: "Cookies database cannot be inspected"}
	}

	dsn := &url.URL{Scheme: "file", Path: sqlitePath(path)}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", max(timeout.Milliseconds(), 1)))
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return Inspection{Health: HealthUnknown, Reason: "Cookies database cannot be opened"}
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var check string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		return Inspection{Health: HealthUnknown, Reason: "Cookies database failed integrity inspection"}
	}

	var expires int64
	err = db.QueryRowContext(ctx, `SELECT expires_utc FROM cookies WHERE host_key IN ('.claude.ai', 'claude.ai') AND name = 'sessionKey' LIMIT 1`).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Inspection{Health: HealthMissing, Reason: "Claude session cookie is missing"}
	}
	if err != nil {
		return Inspection{Health: HealthUnknown, Reason: "Cookies schema is unsupported"}
	}
	if expires > 0 && expires-chromiumEpochOffsetMicros <= now.UnixMicro() {
		return Inspection{Health: HealthExpired, Reason: "Claude session has expired; reauthentication is required"}
	}
	return Inspection{Health: HealthUsable, Reason: "Claude session evidence is locally usable"}
}

func CheckpointCookies(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	var busy, logFrames, checkpointed int
	if err := db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return fmt.Errorf("cookies WAL remains busy")
	}
	return nil
}

func cookieDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// SessionDigest hashes only the encrypted Claude session cookie. Unlike a
// whole-file digest, this remains stable when Chromium refreshes unrelated
// cookies or rewrites SQLite metadata.
func SessionDigest(path string) (string, error) {
	evidence, err := readCookieEvidence(path)
	if err != nil {
		return "", err
	}
	if evidence.sessionDigest == "" {
		return "", errors.New("session cookie has no value")
	}
	return evidence.sessionDigest, nil
}

func readCookieEvidence(path string) (cookieEvidence, error) {
	dsn := &url.URL{Scheme: "file", Path: sqlitePath(path)}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", inspectionTimeout.Milliseconds()))
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return cookieEvidence{}, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), inspectionTimeout)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT name, encrypted_value, value
		FROM cookies
		WHERE host_key IN ('.claude.ai', 'claude.ai')
			AND name IN ('sessionKey', 'lastActiveOrg', 'routingHint')
		ORDER BY CASE host_key WHEN '.claude.ai' THEN 0 ELSE 1 END`)
	if err != nil {
		return cookieEvidence{}, err
	}
	defer rows.Close()
	values := make(map[string][]byte, 3)
	for rows.Next() {
		var name, plain string
		var encrypted []byte
		if err := rows.Scan(&name, &encrypted, &plain); err != nil {
			return cookieEvidence{}, err
		}
		if _, exists := values[name]; exists {
			continue
		}
		if len(encrypted) == 0 {
			encrypted = []byte(plain)
		}
		if len(encrypted) > 0 {
			values[name] = encrypted
		}
	}
	if err := rows.Err(); err != nil {
		return cookieEvidence{}, err
	}
	evidence := cookieEvidence{sessionDigest: digestValue(values["sessionKey"])}
	org := values["lastActiveOrg"]
	routing := values["routingHint"]
	if len(org) > 0 && len(routing) > 0 {
		h := sha256.New()
		_, _ = h.Write([]byte("lastActiveOrg\x00"))
		_, _ = h.Write(org)
		_, _ = h.Write([]byte("\x00routingHint\x00"))
		_, _ = h.Write(routing)
		evidence.accountFingerprint = fmt.Sprintf("%x", h.Sum(nil))
	}
	return evidence, nil
}

func digestValue(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	digest := sha256.Sum256(value)
	return fmt.Sprintf("%x", digest[:])
}
