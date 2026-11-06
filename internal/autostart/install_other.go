//go:build !darwin && !linux && !windows

package autostart

import "github.com/patrache/masil/internal/config"

// Install is not supported on this OS.
func Install(exe string, p config.Paths) error { return ErrUnsupported }

// Uninstall is not supported on this OS.
func Uninstall() error { return ErrUnsupported }

// Query is not supported on this OS.
func Query() (Status, error) { return Status{}, ErrUnsupported }
