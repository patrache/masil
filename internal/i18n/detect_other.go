//go:build !darwin && !windows

package i18n

// preferredUILanguages has no OS source here; the POSIX variables are all.
func preferredUILanguages() []string { return nil }
