// Package instance coordinates TUI and daemon processes with tui.lock / daemon.lock (CA 3.8).
package instance

import (
	"errors"

	"github.com/patrache/masil/internal/config"
)

// ErrAlreadyRunning is returned by AcquireDaemon when another daemon holds daemon.lock.
var ErrAlreadyRunning = errors.New("instance: daemon already running")

// AcquireTUI takes a shared lock on tui.lock.
func AcquireTUI(p config.Paths) (release func(), err error) {
	// TODO(core): Task 5
	return nil, nil
}

// TUIRunning probes tui.lock with an exclusive TryLock; err => caller falls back to the server signal.
func TUIRunning(p config.Paths) (running bool, err error) {
	// TODO(core): Task 5
	return false, nil
}

// AcquireDaemon takes an exclusive lock on daemon.lock; ErrAlreadyRunning if held.
func AcquireDaemon(p config.Paths) (release func(), err error) {
	// TODO(core): Task 5
	return nil, nil
}
