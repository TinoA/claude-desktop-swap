package account

import "sync"

// sessionDecryptor turns a Cookies database path into a decrypted sessionKey.
// Only the concrete implementation is platform-specific (macOS keychain); the
// HTTP fetch and plan parsing are platform-agnostic.
type sessionDecryptor interface {
	SessionKey(cookiesPath string) (string, error)
}

// Fetch decrypts the sessionKey cookie at cookiesPath, calls the Claude.ai API,
// and returns the best-effort email and plan. Returns empty Info on any failure.
func Fetch(cookiesPath string) Info {
	d, err := newDecryptor()
	if err != nil {
		return Info{}
	}
	return fetchWith(d, cookiesPath)
}

// FetchMany resolves account info for several profiles at once, building the
// decryptor a single time (one keychain read) and querying the API
// concurrently. Keys are profile names; values are their Cookies paths.
func FetchMany(cookiesByName map[string]string) map[string]Info {
	result := make(map[string]Info, len(cookiesByName))
	d, err := newDecryptor()
	if err != nil {
		return result
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for name, path := range cookiesByName {
		wg.Add(1)
		go func(name, path string) {
			defer wg.Done()
			info := fetchWith(d, path)
			mu.Lock()
			result[name] = info
			mu.Unlock()
		}(name, path)
	}
	wg.Wait()
	return result
}

func fetchWith(d sessionDecryptor, cookiesPath string) Info {
	sessionKey, err := d.SessionKey(cookiesPath)
	if err != nil {
		return Info{}
	}
	info, _ := fetchFromAPI(sessionKey)
	return info
}
