// Package ui is the TUI framework contract: components, context, routes,
// messages, the registration registry and timers (CA 5.3).
package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Size is a pane's inner size.
type Size struct{ W, H int }

// Component is a pointer-receiver, mutate-in-place screen piece.
type Component interface {
	Init(ctx *Context) tea.Cmd
	// Update receives every non-input message (broadcast) plus input routed to it (5.3.4).
	Update(ctx *Context, msg tea.Msg) tea.Cmd
	// Resize is called before View whenever the pane's inner size changes (and on mode change).
	Resize(ctx *Context, size Size)
	// View returns EXACTLY size.H lines, each EXACTLY size.W cells wide (use textwidth.Fit).
	View(ctx *Context, focused bool) string
}

// Optional capabilities (type-asserted by the app):

// KeyHandler receives keys when focused.
type KeyHandler interface {
	// HandleKey: the focused component gets keys first (after overlays and hard globals). handled=false lets soft globals run.
	HandleKey(ctx *Context, msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd)
}

// MouseHandler receives mouse events.
type MouseHandler interface {
	HandleMouse(ctx *Context, msg tea.MouseMsg, local Point) tea.Cmd // local = coords inside the pane's inner rect
}

// InputCapturer is true while a text field is active: printable keys never trigger soft globals.
type InputCapturer interface{ CapturesInput() bool }

// Cursorer returns the real cursor relative to the inner rect (IME!).
type Cursorer interface{ Cursor() *tea.Cursor }

// Titled provides the pane border title / tab label.
type Titled interface {
	Title(ctx *Context) (title string, badge Badge)
}

// Helper provides status-bar hints (first 4 shown).
type Helper interface {
	Help(ctx *Context) []key.Binding
}

// ThreadHider is true while the component's selection cannot open a thread
// (D7: a system message): the status line then drops the mode's thread hint.
type ThreadHider interface{ HidesThread(ctx *Context) bool }

// Watcher lists visible posts ("post:<id>"), sns-fun.
type Watcher interface{ WatchTopics() []string }

// ChannelViewer lists visible channels, for Store.SetViewing / notify ViewingTarget.
type ChannelViewer interface{ ViewingChannels() []string }

// Persistent is optional component state for state.json.
type Persistent interface {
	SaveState() any
	LoadState(any)
}

// Completer is true while an input-owned completion popup is open (CR-A):
// before hard-global tab/shift+tab, a focused component with
// CapturesInput() && Completing() gets the key through HandleKey first.
type Completer interface{ Completing() bool }

// Point is a cell coordinate.
type Point struct{ X, Y int }

// Badge decorates a title or tab. Mention => BadgeMention style; Dot => "●".
type Badge struct {
	Count   int
	Mention bool
	Dot     bool
}

// DragHandler is a MouseHandler that follows left-button drags (text
// selection). After a left press it got through HandleMouse, the app sends it
// every MouseMotionMsg and the MouseReleaseMsg of that gesture through
// HandleDrag, wherever the pointer is: local is relative to its inner rect and
// may lie outside it (above, below, on the border). ClearDrag drops the drag
// and any selection: the app calls it when a later press lands outside the
// pane or the gesture is cut short (an overlay opened, the layout changed).
type DragHandler interface {
	MouseHandler
	HandleDrag(ctx *Context, msg tea.MouseMsg, local Point) tea.Cmd
	ClearDrag()
}
