//go:build !windows

package config

// isTransientRenameErr is always false off Windows: POSIX rename(2) replaces
// an open target atomically, so a failure there is not worth retrying.
func isTransientRenameErr(error) bool { return false }
