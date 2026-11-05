// Package stub provides test-only stand-in screens for core tests (CA 10.2).
// Import only from _test.go files; product code and ui/e2e must not import it.
package stub

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Chat stands in for chat's channel/thread screens so core can test the framework
// (instance cache, Project, focus intent, ScrollAnchor, cursor, persistence) without importing ui/chat.
// Implements ui.Component, KeyHandler, MouseHandler, InputCapturer, Cursorer, ChannelViewer, Persistent, Titled, Helper.
type Chat struct {
	Route    ui.Route
	Serial   uint64                     // unique per instance (identity checks across mode switches)
	List     *widgets.List[api.Message] // fixtures.Messages(channel), DensityFull/Line/Bubble/Minimal rendered minimally
	Composer *widgets.Composer          // `i` focuses, `esc` leaves, `enter` appends a pending line locally
}

// List stands in for feed/notification screens: a widgets.List of fixed rows, Persistent, Titled.
type List struct {
	Route  ui.Route
	Serial uint64
	List   *widgets.List[string]
}

var (
	_ ui.Component     = (*Chat)(nil)
	_ ui.KeyHandler    = (*Chat)(nil)
	_ ui.MouseHandler  = (*Chat)(nil)
	_ ui.InputCapturer = (*Chat)(nil)
	_ ui.Cursorer      = (*Chat)(nil)
	_ ui.ChannelViewer = (*Chat)(nil)
	_ ui.Persistent    = (*Chat)(nil)
	_ ui.Titled        = (*Chat)(nil)
	_ ui.Helper        = (*Chat)(nil)

	_ ui.Component  = (*List)(nil)
	_ ui.KeyHandler = (*List)(nil)
	_ ui.Persistent = (*List)(nil)
	_ ui.Titled     = (*List)(nil)
)

// Install registers RouteChannel, RouteThread -> *Chat; RouteFeed, RouteNotifications, RouteHome -> *List.
// Call from TestMain.
func Install() {
	// TODO(core): Task 23
}

// Init implements ui.Component.
func (c *Chat) Init(ctx *ui.Context) tea.Cmd {
	// TODO(core): Task 23
	return nil
}

// Update implements ui.Component.
func (c *Chat) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	// TODO(core): Task 23
	return nil
}

// Resize implements ui.Component.
func (c *Chat) Resize(ctx *ui.Context, size ui.Size) {
	// TODO(core): Task 23
}

// View implements ui.Component.
func (c *Chat) View(ctx *ui.Context, focused bool) string {
	// TODO(core): Task 23
	return ""
}

// HandleKey implements ui.KeyHandler.
func (c *Chat) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd) {
	// TODO(core): Task 23
	return false, nil
}

// HandleMouse implements ui.MouseHandler.
func (c *Chat) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	// TODO(core): Task 23
	return nil
}

// CapturesInput implements ui.InputCapturer.
func (c *Chat) CapturesInput() bool {
	// TODO(core): Task 23
	return false
}

// Cursor implements ui.Cursorer.
func (c *Chat) Cursor() *tea.Cursor {
	// TODO(core): Task 23
	return nil
}

// ViewingChannels implements ui.ChannelViewer.
func (c *Chat) ViewingChannels() []string {
	// TODO(core): Task 23
	return nil
}

// SaveState implements ui.Persistent.
func (c *Chat) SaveState() any {
	// TODO(core): Task 23
	return nil
}

// LoadState implements ui.Persistent.
func (c *Chat) LoadState(any) {
	// TODO(core): Task 23
}

// Title implements ui.Titled.
func (c *Chat) Title(ctx *ui.Context) (title string, badge ui.Badge) {
	// TODO(core): Task 23
	return "", ui.Badge{}
}

// Help implements ui.Helper.
func (c *Chat) Help(ctx *ui.Context) []key.Binding {
	// TODO(core): Task 23
	return nil
}

// Init implements ui.Component.
func (l *List) Init(ctx *ui.Context) tea.Cmd {
	// TODO(core): Task 23
	return nil
}

// Update implements ui.Component.
func (l *List) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	// TODO(core): Task 23
	return nil
}

// Resize implements ui.Component.
func (l *List) Resize(ctx *ui.Context, size ui.Size) {
	// TODO(core): Task 23
}

// View implements ui.Component.
func (l *List) View(ctx *ui.Context, focused bool) string {
	// TODO(core): Task 23
	return ""
}

// HandleKey implements ui.KeyHandler.
func (l *List) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd) {
	// TODO(core): Task 23
	return false, nil
}

// SaveState implements ui.Persistent.
func (l *List) SaveState() any {
	// TODO(core): Task 23
	return nil
}

// LoadState implements ui.Persistent.
func (l *List) LoadState(any) {
	// TODO(core): Task 23
}

// Title implements ui.Titled.
func (l *List) Title(ctx *ui.Context) (title string, badge ui.Badge) {
	// TODO(core): Task 23
	return "", ui.Badge{}
}
