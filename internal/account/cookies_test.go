//go:build darwin

package account

import (
	"bytes"
	"testing"
)

func TestStripDomainPrefix(t *testing.T) {
	token := []byte("sk-ant-sid02-abcDEF")

	hashed := append(bytes.Repeat([]byte{0x00, 0xfc, 0xd6, 0x36}, 8), token...) // 32-byte binary prefix + token
	if got := stripDomainPrefix(hashed); !bytes.Equal(got, token) {
		t.Fatalf("prefixed value = %q, want %q", got, token)
	}

	if got := stripDomainPrefix(token); !bytes.Equal(got, token) {
		t.Fatalf("clean value was altered: %q", got)
	}
}
