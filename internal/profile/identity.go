package profile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxIdentityFileBytes  int64 = 16 << 20
	maxIdentityTotalBytes int64 = 64 << 20
)

var (
	emailPattern = regexp.MustCompile(`(?i)[a-z0-9._%+\-]{1,64}@[a-z0-9.\-]{1,253}\.[a-z]{2,63}`)
	uuidPattern  = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
)

type localIdentity struct {
	uuidHashes  []string
	emailHashes []string
}

func localIdentityAt(appDataPath string) localIdentity {
	uuids := make(map[string]struct{})
	emails := make(map[string]struct{})
	var total int64
	root := filepath.Join(appDataPath, indexedDBDir)
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || total >= maxIdentityTotalBytes {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxIdentityFileBytes || total+info.Size() > maxIdentityTotalBytes {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		total += int64(len(data))
		lower := bytes.ToLower(data)
		if !bytes.Contains(lower, []byte("email_address")) && !bytes.Contains(lower, []byte("account_profile")) {
			return nil
		}
		for _, value := range uuidPattern.FindAll(lower, -1) {
			uuids[identityHash(value)] = struct{}{}
		}
		for _, value := range emailPattern.FindAll(lower, -1) {
			emails[identityHash(value)] = struct{}{}
		}
		return nil
	})
	return localIdentity{uuidHashes: sortedIdentityHashes(uuids), emailHashes: sortedIdentityHashes(emails)}
}

func identityHash(value []byte) string {
	digest := sha256.Sum256([]byte(strings.ToLower(string(value))))
	return hex.EncodeToString(digest[:])
}

func sortedIdentityHashes(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func identityMatchScore(live, saved localIdentity) int {
	uuidMatches := identityOverlapCount(live.uuidHashes, saved.uuidHashes)
	emailMatches := identityOverlapCount(live.emailHashes, saved.emailHashes)
	if uuidMatches < 2 && emailMatches == 0 {
		return 0
	}
	return uuidMatches*10 + emailMatches
}

func identityOverlapCount(left, right []string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	values := make(map[string]struct{}, len(left))
	for _, value := range left {
		values[value] = struct{}{}
	}
	matches := 0
	for _, value := range right {
		if _, ok := values[value]; ok {
			matches++
		}
	}
	return matches
}
