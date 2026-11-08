package notify

import (
	"sync"
)

// Backend shows OS notifications.
type Backend interface {
	Show(n OSNotification) error
}

// NewOSBackend returns the desktop notifier of this OS (appName = "masil").
//
// Title and body come from other users, so no backend ever builds a script,
// a shell command or a markup document by pasting them in (pending-fixes 0):
//   - macOS: /usr/bin/osascript runs a constant AppleScript; title and body are
//     argv items after "--" (darwinCommand).
//   - Linux: org.freedesktop.Notifications.Notify over D-Bus, no exec; the
//     body is markup-escaped when the server renders body markup, and plain
//     text (summary, other bodies) has < > as look-alike quotes (dbusCall).
//   - Windows: the WinRT toast COM API with an XML document whose text nodes are
//     XML-escaped (toastXML); the PowerShell fallback is never enabled.
//   - other OSes: an error (not a release target).
//
// Each platform's show is in backend_platform.go (tested on every OS); the
// GOOS files only plug in the final OS call. Every backend also re-applies Format's sanitizing and length limits
// (prepare), so a caller that skips Format cannot reach the OS with control
// characters or unbounded text.
func NewOSBackend(appName string) Backend {
	return osBackend{appName: appName, p: newPlatform(appName)}
}

// platform is the OS-specific half of osBackend.
type platform interface {
	show(n OSNotification) error
}

type osBackend struct {
	appName string
	p       platform
}

// Show posts a desktop notification without an icon.
func (b osBackend) Show(n OSNotification) error {
	return b.p.show(prepare(n))
}

// prepare is the last line before the OS: the same sanitizing and limits as
// Format (idempotent on Format's output).
func prepare(n OSNotification) OSNotification {
	n.Title = clamp(n.Title, maxTitle)
	n.Body = clamp(n.Body, maxBody)
	return n
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
