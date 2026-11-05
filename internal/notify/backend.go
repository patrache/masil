package notify

// Backend shows OS notifications.
type Backend interface {
	Show(n OSNotification) error
}

// NewOSBackend uses beeep.Notify with beeep.AppName = appName ("masil").
func NewOSBackend(appName string) Backend {
	// TODO(core): Task 15
	return nil
}

// Recorder is a test backend that records every notification.
type Recorder struct {
	Shown []OSNotification
}

// Show records n.
func (r *Recorder) Show(n OSNotification) error {
	// TODO(core): Task 15
	return nil
}
