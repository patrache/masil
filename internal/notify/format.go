package notify

import "github.com/patrache/masil/internal/api"

// OSNotification is one desktop notification.
type OSNotification struct {
	Title, Body string
	AlertID     string
}

// Format builds the notification; title/body sanitized: control chars stripped, title <= 80, body <= 200 code points.
func Format(a api.Alert) OSNotification {
	// TODO(core): Task 15
	return OSNotification{}
}
