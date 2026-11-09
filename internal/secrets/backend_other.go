//go:build !darwin && !windows && !linux

package secrets

// defaultOS: this platform has no supported OS store; Put asks for consent.
func defaultOS() OSBackend { return nil }
