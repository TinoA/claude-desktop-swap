//go:build !darwin

package account

// Fetch is a no-op on non-Darwin platforms.
func Fetch(_ string) Info { return Info{} }
