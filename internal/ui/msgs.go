package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
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

// SetLocaleMsg asks the app to switch the shown language (P5): Choice is
// "auto" or a locale code ("ko", "en", "zh-Hans", "x-pseudo"). The app
// resolves it like [ui] language (closed languages fall back, unknown values
// mean auto; MASIL_LANG still wins), swaps ctx.T and ctx.Lang and broadcasts LocaleChangedMsg. It is
// consumed, never broadcast. Saving the choice is the caller's.
type SetLocaleMsg struct{ Choice string }

// LocaleChangedMsg is broadcast after the shown language changed (From != To):
// ctx.T and ctx.Lang already hold To. Components drop text they cached in the
// old language (row caches, titles); state such as drafts and scroll is kept.
type LocaleChangedMsg struct{ From, To i18n.Locale }

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

// ErrorMsg becomes a ToastError with errtext.Describe(ctx.T, err).
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

// FriendRequestsChangedMsg reports that my sent friend requests changed by an
// action outside the friends screen (the profile menu's 친구 요청 / 친구 요청
// 취소): friend.request_received goes to the receiver only, so the friends
// screen fetches its 보낸 요청 again on it.
type FriendRequestsChangedMsg struct{}

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
