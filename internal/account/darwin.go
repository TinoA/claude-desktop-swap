//go:build darwin

package account

// newDecryptor builds a keychain-backed decryptor for macOS.
func newDecryptor() (sessionDecryptor, error) { return NewCookieManager() }
