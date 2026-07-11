//go:build darwin

package account

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const (
	keychainAccount  = "Claude Key"
	keychainService  = "Claude Safe Storage"
	pbkdf2Salt       = "saltysalt"
	pbkdf2Iterations = 1003
	pbkdf2KeyLen     = 16
	chromiumPrefix   = "v10"
)

// CookieManager decrypts Chromium session cookies using the macOS keychain key.
type CookieManager struct {
	key []byte
}

// NewCookieManager reads the AES key from the macOS keychain and returns a
// ready CookieManager. Returns an error if the keychain entry is missing.
func NewCookieManager() (*CookieManager, error) {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-a", keychainAccount, "-s", keychainService, "-w",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("keychain: %w", err)
	}
	password := strings.TrimSpace(string(out))
	key := pbkdf2.Key([]byte(password), []byte(pbkdf2Salt), pbkdf2Iterations, pbkdf2KeyLen, sha1.New)
	return &CookieManager{key: key}, nil
}

// SessionKey reads and decrypts the sessionKey cookie from a Chromium Cookies
// SQLite file. Uses the system sqlite3 CLI to avoid adding a CGO dependency.
func (cm *CookieManager) SessionKey(cookiesPath string) (string, error) {
	out, err := exec.Command(
		"sqlite3", cookiesPath,
		`SELECT hex(encrypted_value) FROM cookies `+
			`WHERE host_key LIKE '%.claude.ai' AND name='sessionKey' LIMIT 1`,
	).Output()
	if err != nil {
		return "", fmt.Errorf("sqlite3: %w", err)
	}
	hexVal := strings.TrimSpace(string(out))
	if hexVal == "" {
		return "", fmt.Errorf("sessionKey cookie not found in %s", cookiesPath)
	}
	blob, err := hex.DecodeString(hexVal)
	if err != nil {
		return "", fmt.Errorf("hex decode: %w", err)
	}
	return cm.decrypt(blob)
}

// decrypt decrypts a Chromium AES-128-CBC cookie blob.
// Chromium prepends a "v10" prefix; the IV is 16 space bytes.
func (cm *CookieManager) decrypt(blob []byte) (string, error) {
	if len(blob) < len(chromiumPrefix) || string(blob[:len(chromiumPrefix)]) != chromiumPrefix {
		return string(blob), nil // stored unencrypted
	}
	ct := blob[len(chromiumPrefix):]
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length: %d", len(ct))
	}
	block, err := aes.NewCipher(cm.key)
	if err != nil {
		return "", err
	}
	iv := bytes.Repeat([]byte{' '}, aes.BlockSize)
	pt := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(pt, ct)

	// PKCS7 unpad
	pad := int(pt[len(pt)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(pt) {
		return "", fmt.Errorf("invalid PKCS7 padding: %d", pad)
	}
	return string(stripDomainPrefix(pt[:len(pt)-pad])), nil
}

const domainHashPrefixLen = 32

// stripDomainPrefix drops the 32-byte SHA256 domain hash that Chrome v130+ on
// macOS prepends to the plaintext. Cookie values are printable ASCII, so a
// non-printable decryption means the prefix is present.
func stripDomainPrefix(value []byte) []byte {
	if len(value) > domainHashPrefixLen && !isPrintableASCII(value) {
		return value[domainHashPrefixLen:]
	}
	return value
}

func isPrintableASCII(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}
