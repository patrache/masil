package workspace

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/workspace/share"
)

// SetPaneSupportedForTest makes embedded panes (un)available, as on Windows,
// and returns the restore func.
func SetPaneSupportedForTest(on bool) (restore func()) {
	old := paneSupported
	paneSupported = func() bool { return on }
	return func() { paneSupported = old }
}

// SetOwnerActivityForTest replaces the owner-activity report.
func SetOwnerActivityForTest(f func(*share.Host, time.Time)) (restore func()) {
	old := ownerActivity
	ownerActivity = f
	return func() { ownerActivity = old }
}

// SetSASForTest shows every check number as sas (goldens).
func SetSASForTest(sas string) (restore func()) {
	old := formatSAS
	formatSAS = func(string) string { return sas }
	return func() { formatSAS = old }
}

// SetTerminalForTest adapts terminals for sharing with f (fake panes have
// no termhost.Pane).
func SetTerminalForTest(f func(instanceID string) share.Terminal) (restore func()) {
	old := shareTerminal
	shareTerminal = func(t *term) share.Terminal { return f(t.id) }
	return func() { shareTerminal = old }
}

// CountBellsForTest counts the bells the owner screens ring.
func CountBellsForTest(n *int) (restore func()) {
	old := ringBell
	ringBell = func() tea.Cmd { *n++; return nil }
	return func() { ringBell = old }
}
