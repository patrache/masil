// Package stub provides test-only stand-in screens for core tests (CA 10.2).
// Import only from _test.go files; product code and ui/e2e must not import it.
package stub

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Chat stands in for chat's channel/thread screens so core can test the framework
// (instance cache, Project, focus intent, ScrollAnchor, cursor, persistence) without importing ui/chat.
// Implements ui.Component, KeyHandler, MouseHandler, InputCapturer, Cursorer, ChannelViewer, Persistent, Titled, Helper.
//
// Keys (fixed by TestStubChat): while capturing input, enter sends (the line is
// appended locally to Pending), esc leaves the input, ctrl+c clears a non-empty
// draft (handled) or falls through on an empty one, and every other key goes to the
// Composer (handled). Otherwise only j/k (list.up/down), ctrl+u/d (page), t/enter
// (thread of the selected message; channel routes only) and i/a (focus the input)
// are handled; everything else (esc, ctrl+c included) is left to the app.
//
// Test-only hooks (additions to CA 10.2): Completion, Completed, OnUpdate, Pending,
// Resizes and Completing (ui.Completer, CR-A).
type Chat struct {
	Route    ui.Route
	Serial   uint64                     // unique per instance (identity checks across mode switches)
	List     *widgets.List[api.Message] // fixtures.Messages(channel), DensityFull/Line/Bubble/Minimal rendered minimally
	Composer *widgets.Composer          // `i` focuses, `esc` leaves, `enter` appends a pending line locally

	// Completion is set by a test to simulate an open completion popup: while
	// capturing, tab/shift+tab are then taken (appended to Completed) and it is cleared.
	Completion bool
	Completed  []string
	// OnUpdate (nil = ignored) is called at the very start of every Update.
	OnUpdate func(ctx *ui.Context, msg tea.Msg)
	// Pending holds the lines sent with enter, in order.
	Pending []string
	// Resizes records every Resize size, in order (pane geometry tests).
	Resizes []ui.Size

	base      []api.Message
	capturing bool
	size      ui.Size
	composerH int // composer height of the last Resize/View (0 = not drawn)
	me        api.UserSummary
	now       time.Time
}

// List stands in for feed/notification screens: a widgets.List of fixed rows, Persistent, Titled.
type List struct {
	Route  ui.Route
	Serial uint64
	List   *widgets.List[string]

	// OnUpdate (nil = ignored) is called at the very start of every Update.
	OnUpdate func(ctx *ui.Context, msg tea.Msg)

	size ui.Size
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
	_ ui.Completer     = (*Chat)(nil)

	_ ui.Component  = (*List)(nil)
	_ ui.KeyHandler = (*List)(nil)
	_ ui.Persistent = (*List)(nil)
	_ ui.Titled     = (*List)(nil)
)

// serial numbers every stub instance (Chat and List share it).
var serial atomic.Uint64

var installMu sync.Mutex

// Install registers RouteChannel, RouteThread -> *Chat; RouteFeed, RouteNotifications, RouteHome -> *List.
// Call from TestMain. Instead of sync.Once it registers only kinds that are missing,
// so it is safe to call again (no duplicate panic) and works after ui.ResetRegistryForTest.
func Install() {
	installMu.Lock()
	defer installMu.Unlock()
	have := ui.Registered().Routes
	add := func(kind ui.RouteKind, f ui.RouteFactory) {
		if _, ok := have[kind]; !ok {
			ui.RegisterRoute(kind, f)
		}
	}
	chat := func(ctx *ui.Context, r ui.Route) ui.Component { return NewChat(ctx, r) }
	list := func(ctx *ui.Context, r ui.Route) ui.Component { return NewList(ctx, r) }
	add(ui.RouteChannel, chat)
	add(ui.RouteThread, chat)
	add(ui.RouteFeed, list)
	add(ui.RouteNotifications, list)
	add(ui.RouteHome, list)
}

// ---- Chat ----

// NewChat builds a Chat for a RouteChannel (fixtures.Messages(ID)) or a RouteThread
// (the root message ID found in fixtures.Messages(Arg)). The newest message (or the
// route Anchor) starts selected and the view follows the bottom.
func NewChat(ctx *ui.Context, r ui.Route) *Chat {
	placeholder := "메시지 보내기"
	if r.Kind == ui.RouteThread {
		placeholder = "답글 보내기"
	}
	c := &Chat{Route: r, Serial: serial.Add(1), Composer: widgets.NewComposer(placeholder)}
	if m := ctx.Me(); m != nil {
		c.me = m.UserSummary
	}
	if ctx != nil && ctx.Now != nil {
		c.now = ctx.Now()
	}
	c.List = widgets.NewList(func(m api.Message) string { return m.ID }, c.render)
	c.base = baseMessages(r)
	c.List.SetItems(c.items())
	c.List.Follow(true)
	if ms := c.base; len(ms) > 0 {
		c.List.Select(ms[len(ms)-1].ID)
	}
	if r.Anchor != "" {
		c.List.Select(r.Anchor)
	}
	return c
}

func baseMessages(r ui.Route) []api.Message {
	if r.Kind != ui.RouteThread {
		return fixtures.Messages(r.ID)
	}
	for _, m := range fixtures.Messages(r.Arg) {
		if m.ID == r.ID {
			return []api.Message{m}
		}
	}
	return nil
}

// channelID is the channel the instance belongs to.
func (c *Chat) channelID() string {
	if c.Route.Kind == ui.RouteThread {
		return c.Route.Arg
	}
	return c.Route.ID
}

// items is the canned history plus the locally sent lines.
func (c *Chat) items() []api.Message {
	out := append([]api.Message(nil), c.base...)
	for i, line := range c.Pending {
		out = append(out, api.Message{
			ID:        fmt.Sprintf("~pending-%04d", i+1),
			ChannelID: c.channelID(),
			Author:    c.me,
			Kind:      "text",
			Body:      line,
			CreatedAt: c.now,
		})
	}
	return out
}

// render draws one message minimally per density (body sanitized, then truncated).
func (c *Chat) render(ctx *ui.Context, m api.Message, selected bool, width int) []string {
	name := textwidth.Sanitize(m.Author.DisplayName)
	body := textwidth.Sanitize(m.Body)
	loc := time.Local
	if ctx != nil && ctx.Loc != nil {
		loc = ctx.Loc
	}
	clock := m.CreatedAt.In(loc).Format("15:04")
	mine := ctx != nil && ctx.Store != nil && ctx.Store.IsMe(m.Author.ID)
	cut := func(s string) string { return textwidth.Truncate(s, width, "…") }

	var lines []string
	density := ui.DensityFull
	if ctx != nil {
		density = ctx.Density()
	}
	switch density {
	case ui.DensityFull:
		lines = []string{cut(name + " " + clock), cut(body)}
	case ui.DensityLine:
		lines = []string{cut(clock + " " + name + " " + body)}
	case ui.DensityBubble:
		if mine {
			lines = []string{textwidth.PadLeft(cut(body), width)}
		} else {
			lines = []string{cut(name + " " + body)}
		}
	default:
		lines = []string{cut(name + ": " + body)}
	}
	t := theme.Get(theme.Default)
	if ctx != nil && ctx.Theme != nil {
		t = ctx.Theme
	}
	for i, l := range lines {
		if selected {
			lines[i] = t.S.Selected.Render(textwidth.PadRight(l, width))
		} else {
			lines[i] = t.S.Base.Render(l)
		}
	}
	return lines
}

// maxRows is the composer's visible text rows for the density.
func (c *Chat) maxRows(ctx *ui.Context) int {
	switch ctx.Density() {
	case ui.DensityFull:
		return 3
	case ui.DensityMinimal:
		return 1
	}
	return 2
}

// composerHeight is the composer's drawn height, 0 when it does not fit below at
// least one list row.
func (c *Chat) composerHeight(ctx *ui.Context) int {
	if c.size.W < 3 {
		return 0
	}
	h := c.Composer.Height(c.size.W, c.maxRows(ctx))
	if c.size.H < h+1 {
		return 0
	}
	return h
}

// Init implements ui.Component.
func (c *Chat) Init(ctx *ui.Context) tea.Cmd {
	return nil
}

// Update implements ui.Component.
func (c *Chat) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if c.OnUpdate != nil {
		c.OnUpdate(ctx, msg)
	}
	switch m := msg.(type) {
	case tea.PasteMsg:
		if c.capturing {
			return c.Composer.Update(ctx, m)
		}
	case ui.RouteUpdatedMsg:
		c.Route = m.Route
		if m.Route.Anchor != "" {
			c.List.Select(m.Route.Anchor)
		}
	}
	return nil
}

// Resize implements ui.Component.
func (c *Chat) Resize(ctx *ui.Context, size ui.Size) {
	c.size = size
	c.Resizes = append(c.Resizes, size)
	if c.composerH = c.composerHeight(ctx); c.composerH > 0 {
		c.Composer.View(ctx, size.W, c.maxRows(ctx)) // keep the composer's cursor geometry current
	}
}

// View implements ui.Component.
func (c *Chat) View(ctx *ui.Context, focused bool) string {
	w, h := c.size.W, c.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	ch := c.composerHeight(ctx)
	c.composerH = ch
	out := c.List.View(ctx, ui.Size{W: w, H: h - ch})
	if ch > 0 {
		out += "\n" + c.Composer.View(ctx, w, c.maxRows(ctx))
	}
	return out
}

// HandleKey implements ui.KeyHandler.
func (c *Chat) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd) {
	km := ctx.Keys
	if c.capturing {
		s := msg.String()
		switch {
		case c.Completion && (s == "tab" || s == "shift+tab"):
			c.Completed = append(c.Completed, s)
			c.Completion = false
			return true, nil
		case s == "ctrl+c":
			had := c.Composer.Value() != ""
			return had, c.Composer.Update(ctx, msg)
		case km.Matches(msg, keymap.ComposeSend):
			if v := c.Composer.Value(); v != "" {
				c.Pending = append(c.Pending, v)
				c.List.SetItems(c.items())
				c.List.Select(c.items()[len(c.items())-1].ID)
			}
			c.Composer.Reset()
			return true, nil
		case km.Matches(msg, keymap.ComposeLeave):
			c.capturing = false
			c.Composer.Blur()
			return true, nil
		}
		return true, c.Composer.Update(ctx, msg)
	}
	switch {
	case km.Matches(msg, keymap.ListUp), km.Matches(msg, keymap.ListDown),
		km.Matches(msg, keymap.ListPageUp), km.Matches(msg, keymap.ListPageDown):
		return c.List.HandleKey(ctx, msg)
	case c.Route.Kind == ui.RouteChannel && km.Matches(msg, keymap.MsgThread):
		sel, ok := c.List.Selected()
		if !ok || !isCanned(sel.ID) {
			return true, nil
		}
		return true, ui.Navigate(ui.Route{Kind: ui.RouteThread, ID: sel.ID, Arg: c.channelID()})
	case km.Matches(msg, keymap.ComposeFocus):
		return true, c.focus()
	}
	return false, nil
}

func isCanned(id string) bool { return len(id) > 0 && id[0] != '~' }

func (c *Chat) focus() tea.Cmd {
	c.capturing = true
	return c.Composer.Focus()
}

// HandleMouse implements ui.MouseHandler: the list area goes to the List, a click on
// the composer focuses it.
func (c *Chat) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	listH := c.size.H - c.composerHeight(ctx)
	if local.Y < listH {
		return c.List.HandleMouse(ctx, msg, local)
	}
	if _, click := msg.(tea.MouseClickMsg); click && !c.capturing {
		return c.focus()
	}
	return nil
}

// CapturesInput implements ui.InputCapturer.
func (c *Chat) CapturesInput() bool {
	return c.capturing
}

// Completing implements ui.Completer (CR-A): true while capturing with Completion set.
func (c *Chat) Completing() bool {
	return c.capturing && c.Completion
}

// Cursor implements ui.Cursorer (relative to the inner rect; nil unless capturing
// with the composer drawn).
func (c *Chat) Cursor() *tea.Cursor {
	if !c.capturing || c.size.W < 3 {
		return nil
	}
	cur := c.Composer.Cursor()
	if cur == nil {
		return nil
	}
	// Cursor has no Context (density), so it uses the geometry of the last Resize/View.
	h := c.composerH
	if h == 0 {
		return nil
	}
	cur.Position.Y += c.size.H - h
	cur.Position.X = min(max(cur.Position.X, 0), c.size.W-1)
	cur.Position.Y = min(max(cur.Position.Y, 0), c.size.H-1)
	return cur
}

// ViewingChannels implements ui.ChannelViewer: the channel for RouteChannel, nil for
// RouteThread (like chat's threadView — a lone thread does not mark its channel seen).
func (c *Chat) ViewingChannels() []string {
	if c.Route.Kind != ui.RouteChannel {
		return nil
	}
	return []string{c.Route.ID}
}

// chatState is the Chat's Persistent state.
type chatState struct {
	Draft    string
	Selected string
	Anchor   widgets.ScrollAnchor
	Pending  []string
}

// SaveState implements ui.Persistent.
func (c *Chat) SaveState() any {
	st := chatState{Draft: c.Composer.Value(), Anchor: c.List.Anchor(), Pending: append([]string(nil), c.Pending...)}
	if m, ok := c.List.Selected(); ok {
		st.Selected = m.ID
	}
	return st
}

// LoadState implements ui.Persistent (anything but a saved Chat state is ignored).
func (c *Chat) LoadState(v any) {
	st, ok := v.(chatState)
	if !ok {
		return
	}
	c.Pending = append([]string(nil), st.Pending...)
	c.List.SetItems(c.items())
	c.Composer.SetValue(st.Draft)
	if st.Selected != "" {
		c.List.Select(st.Selected)
	}
	c.List.Restore(st.Anchor)
}

// Title implements ui.Titled: the channel label (thread: "스레드"), badge = unread.
func (c *Chat) Title(ctx *ui.Context) (title string, badge ui.Badge) {
	if c.Route.Kind == ui.RouteThread {
		return "스레드", ui.Badge{}
	}
	if ctx == nil || ctx.Store == nil {
		return "채널", ui.Badge{}
	}
	title = ctx.Store.ChannelLabel(c.Route.ID)
	if title == "" {
		title = "채널"
	}
	if ch := ctx.Store.Channels[c.Route.ID]; ch != nil {
		badge = ui.Badge{Count: ch.My.UnreadCount, Mention: ch.My.MentionCount > 0}
	}
	return title, badge
}

// Help implements ui.Helper.
func (c *Chat) Help(ctx *ui.Context) []key.Binding {
	switch {
	case c.capturing:
		return ctx.Keys.Help(keymap.ComposeSend, keymap.ComposeNewline, keymap.ComposeLeave)
	case c.Route.Kind == ui.RouteThread:
		return ctx.Keys.Help(keymap.ComposeFocus, keymap.ListDown, keymap.ListUp, keymap.NavBack)
	}
	return ctx.Keys.Help(keymap.MsgThread, keymap.ComposeFocus, keymap.ListDown, keymap.ListUp)
}

// ---- List ----

// listRows is the fixed content of every List: "row 1" .. "row 30".
func listRows() []string {
	rows := make([]string, 30)
	for i := range rows {
		rows[i] = fmt.Sprintf("row %d", i+1)
	}
	return rows
}

// NewList builds a List for RouteFeed, RouteNotifications or RouteHome.
func NewList(ctx *ui.Context, r ui.Route) *List {
	l := &List{Route: r, Serial: serial.Add(1)}
	l.List = widgets.NewList(func(s string) string { return s }, func(ctx *ui.Context, s string, selected bool, width int) []string {
		line := textwidth.Truncate(s, width, "…")
		if selected {
			return []string{ctx.Theme.S.Selected.Render(textwidth.PadRight(line, width))}
		}
		return []string{ctx.Theme.S.Base.Render(line)}
	})
	l.List.SetItems(listRows())
	return l
}

// Init implements ui.Component.
func (l *List) Init(ctx *ui.Context) tea.Cmd {
	return nil
}

// Update implements ui.Component.
func (l *List) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if l.OnUpdate != nil {
		l.OnUpdate(ctx, msg)
	}
	return nil
}

// Resize implements ui.Component.
func (l *List) Resize(ctx *ui.Context, size ui.Size) {
	l.size = size
}

// View implements ui.Component.
func (l *List) View(ctx *ui.Context, focused bool) string {
	return l.List.View(ctx, l.size)
}

// HandleKey implements ui.KeyHandler: only list.up/down/pageup/pagedown.
func (l *List) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (handled bool, cmd tea.Cmd) {
	km := ctx.Keys
	if km.Matches(msg, keymap.ListUp) || km.Matches(msg, keymap.ListDown) ||
		km.Matches(msg, keymap.ListPageUp) || km.Matches(msg, keymap.ListPageDown) {
		return l.List.HandleKey(ctx, msg)
	}
	return false, nil
}

// listState is the List's Persistent state.
type listState struct {
	Selected string
	Anchor   widgets.ScrollAnchor
}

// SaveState implements ui.Persistent.
func (l *List) SaveState() any {
	st := listState{Anchor: l.List.Anchor()}
	if s, ok := l.List.Selected(); ok {
		st.Selected = s
	}
	return st
}

// LoadState implements ui.Persistent (anything but a saved List state is ignored).
func (l *List) LoadState(v any) {
	st, ok := v.(listState)
	if !ok {
		return
	}
	if st.Selected != "" {
		l.List.Select(st.Selected)
	}
	l.List.Restore(st.Anchor)
}

// Title implements ui.Titled: "피드" / "알림" (badge = unread notifications) / "홈".
func (l *List) Title(ctx *ui.Context) (title string, badge ui.Badge) {
	switch l.Route.Kind {
	case ui.RouteFeed:
		return "피드", ui.Badge{}
	case ui.RouteNotifications:
		if ctx != nil && ctx.Store != nil {
			badge.Count = ctx.Store.NotifUnread
		}
		return "알림", badge
	}
	return "홈", ui.Badge{}
}
