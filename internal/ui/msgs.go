package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui/layout"
)

// EventMsg wraps a realtime event from the bridge.
type EventMsg struct{ Event realtime.Event }

// RealtimeStateMsg reports a realtime state change.
type RealtimeStateMsg struct{ State realtime.State }

// ModeChangedMsg is broadcast when the layout mode changes.
type ModeChangedMsg struct{ From, To layout.Mode }

// ThemeChangedMsg is broadcast after a theme switch (invalidate cached renders).
type ThemeChangedMsg struct{ Theme *theme.Theme }

// SetThemeMsg asks the app to switch theme.
type SetThemeMsg struct{ Name theme.Name }

// FocusChangedMsg is broadcast when focus moves.
type FocusChangedMsg struct {
	Slot     layout.SlotID
	RouteKey string
}

// ToastMsg is shown 4s (ui.After) as a bordered box at the body's top-right
// (status line in tiny); rect reported in app.FrameInfo.Toast.
type ToastMsg struct {
	Text  string
	Kind  ToastKind
	Route *Route
}

// ToastKind styles a toast.
type ToastKind int

// Toast kinds.
const (
	ToastInfo ToastKind = iota
	ToastSuccess
	ToastError
	ToastAlert
)

// ErrorMsg becomes a ToastError with api.Describe(err).
type ErrorMsg struct {
	Err     error
	Context string
}

// OpenOverlayMsg: Overlay != nil => push it; else build RegisterOverlay(ID) with Args (unknown ID => ignored + debug log).
type OpenOverlayMsg struct {
	Overlay  Overlay
	ID, Args string
}

// CloseOverlayMsg closes the top overlay.
type CloseOverlayMsg struct{}

// RunCommandMsg runs a palette command by id. The app consumes it (not broadcast):
// palette.Lookup(ID) → unknown => ignored; Available false => toast; else Run(ctx, Args) (CR-B).
type RunCommandMsg struct {
	ID   string
	Args string
}

// LoggedOutMsg reports that the session ended.
type LoggedOutMsg struct{ Reason string }

// ResyncStartedMsg reports a resync start.
type ResyncStartedMsg struct{}

// ResyncDoneMsg reports a resync end.
type ResyncDoneMsg struct{ Err error }

// Toast emits a ToastMsg.
func Toast(text string, kind ToastKind) tea.Cmd {
	return func() tea.Msg { return ToastMsg{Text: text, Kind: kind} }
}

// RunCommand emits a RunCommandMsg.
func RunCommand(id, args string) tea.Cmd {
	return func() tea.Msg { return RunCommandMsg{ID: id, Args: args} }
}

// OpenOverlay emits OpenOverlayMsg{Overlay: o}.
func OpenOverlay(o Overlay) tea.Cmd {
	return func() tea.Msg { return OpenOverlayMsg{Overlay: o} }
}

// OpenRegisteredOverlay emits OpenOverlayMsg{ID: id, Args: args}.
func OpenRegisteredOverlay(id, args string) tea.Cmd {
	return func() tea.Msg { return OpenOverlayMsg{ID: id, Args: args} }
}
