package notify

import (
	"sync"

	"github.com/gen2brain/beeep"
)

// Backend shows OS notifications.
type Backend interface {
	Show(n OSNotification) error
}

// NewOSBackend uses beeep.Notify with beeep.AppName = appName ("masil").
func NewOSBackend(appName string) Backend {
	beeep.AppName = appName
	return osBackend{}
}

type osBackend struct{}

// Show posts a desktop notification without an icon.
func (osBackend) Show(n OSNotification) error {
	return beeep.Notify(n.Title, n.Body, "")
}

// Recorder is a test backend that records every notification. Show may be
// called from several goroutines (TUI tea.Cmds); read Shown once they are done.
type Recorder struct {
	mu    sync.Mutex
	Shown []OSNotification
}

// Show records n.
func (r *Recorder) Show(n OSNotification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Shown = append(r.Shown, n)
	return nil
}
