//go:build darwin

package account

// Fetch decrypts the sessionKey cookie at cookiesPath, calls the Claude.ai API,
// and returns the best-effort email and plan. Returns empty Info on any failure.
func Fetch(cookiesPath string) Info {
	cm, err := NewCookieManager()
	if err != nil {
		return Info{}
	}
	sessionKey, err := cm.SessionKey(cookiesPath)
	if err != nil {
		return Info{}
	}
	info, _ := fetchFromAPI(sessionKey)
	return info
}
