package notify

import "github.com/gen2brain/beeep"

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

// Recorder is a test backend that records every notification.
type Recorder struct {
	Shown []OSNotification
}

// Show records n.
func (r *Recorder) Show(n OSNotification) error {
	r.Shown = append(r.Shown, n)
	return nil
}
