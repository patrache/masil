// Package instance coordinates TUI and daemon processes with tui.lock / daemon.lock (CA 3.8).
package instance

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/gofrs/flock"

	"github.com/patrache/masil/internal/config"
)

// ErrAlreadyRunning is returned by AcquireDaemon when another daemon holds daemon.lock.
var ErrAlreadyRunning = errors.New("instance: daemon already running")

// AcquireTUI takes a shared lock on tui.lock.
// Every running TUI holds it until exit; the OS drops it if the process dies.
// It blocks only while a daemon's exclusive probe (TUIRunning) momentarily holds the file.
func AcquireTUI(p config.Paths) (release func(), err error) {
	if err := config.EnsureDir(p); err != nil {
		return nil, err
	}
	fl := flock.New(p.TUILock)
	if err := fl.RLock(); err != nil {
		_ = fl.Close()
		return nil, fmt.Errorf("instance: lock %s: %w", p.TUILock, err)
	}
	return releaser(fl), nil
}

// TUIRunning probes tui.lock with an exclusive TryLock; err => caller falls back to the server signal.
// Success (released immediately) means no TUI on this device; failure means at least one TUI.
func TUIRunning(p config.Paths) (running bool, err error) {
	if _, err := os.Stat(p.TUILock); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil // no TUI has ever created the lock file
		}
		return false, fmt.Errorf("instance: stat %s: %w", p.TUILock, err)
	}
	fl := flock.New(p.TUILock)
	ok, err := fl.TryLock()
	if err != nil {
		_ = fl.Close()
		return false, fmt.Errorf("instance: probe %s: %w", p.TUILock, err)
	}
	if !ok {
		_ = fl.Close()
		return true, nil
	}
	if err := fl.Close(); err != nil {
		return false, fmt.Errorf("instance: unlock %s: %w", p.TUILock, err)
	}
	return false, nil
}

// AcquireDaemon takes an exclusive lock on daemon.lock; ErrAlreadyRunning if held.
func AcquireDaemon(p config.Paths) (release func(), err error) {
	if err := config.EnsureDir(p); err != nil {
		return nil, err
	}
	fl := flock.New(p.DaemonLock)
	ok, err := fl.TryLock()
	if err != nil {
		_ = fl.Close()
		return nil, fmt.Errorf("instance: lock %s: %w", p.DaemonLock, err)
	}
	if !ok {
		_ = fl.Close()
		return nil, ErrAlreadyRunning
	}
	return releaser(fl), nil
}

// releaser returns an idempotent func that unlocks and closes fl.
func releaser(fl *flock.Flock) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = fl.Unlock()
			_ = fl.Close()
		})
	}
}
