//go:build windows

package cmd

import "testing"

func TestNativeBackupFileFilterUsesDoubleNULTerminator(t *testing.T) {
	filter := nativeBackupFileFilter()
	if len(filter) < 2 || filter[len(filter)-1] != 0 || filter[len(filter)-2] != 0 {
		t.Fatal("native backup filter is not double-NUL terminated")
	}
}
