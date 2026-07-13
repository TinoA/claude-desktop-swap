//go:build !windows

package platform

import "path/filepath"

func cookiesPath(appDataPath string) string { return filepath.Join(appDataPath, "Cookies") }
