package chat

import (
	"context"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterOverlay(overlayPins, newPins)
	palette.Register(palette.Command{
		ID: "pins.show", Title: "고정 메시지 보기", Keywords: []string{"pins", "pinned", "고정", "고정 메시지", "핀"}, Group: "이동",
		Available: func(ctx *ui.Context) bool { return viewedChannel(ctx) != nil },
		Run: func(ctx *ui.Context, args string) tea.Cmd {
			id := strings.TrimSpace(args)
			if id == "" {
				ch := viewedChannel(ctx)
				if ch == nil {
					return nil
				}
				id = ch.ID
			}
			return ui.OpenRegisteredOverlay(overlayPins, id)
		},
	})
}

// Pinned messages overlay texts and sizes.
const (
	pinsTitle    = "📌 고정 메시지"
	pinsEmpty    = "고정된 메시지가 없어요"
	pinsFailed   = "고정 메시지를 불러오지 못했어요"
	pinsMaxW     = 64
	pinsMaxH     = 16
	pinsMinH     = 6 // the app degrades shorter overlays to the whole body
	pinsRowLines = 2 // a pinned message is at most this many lines
)

// pinsResult is the ListPins answer of one overlay instance (ticket = the
// chatstore log position the request was built at, for SetPinsFor's replay).
type pinsResult struct {
	owner  *pinsView
	ticket chatstore.PageTicket
	pins   []api.Message
}

// pinsView is chat.pins: the pinned messages of one channel in pin order (newest
// pin first), each drawn as a line-density message row (1–2 lines). The list comes
// from chatstore when it is loaded, else from ListPins (then stored with SetPins),
// and follows message.pinned / unpinned. enter jumps to the message (the
// conversation loads around it), p unpins (owner/admin in a text channel), esc closes.
type pinsView struct {
	channelID string

	list    *widgets.List[api.Message]
	items   []api.Message
	loaded  bool   // the list is known (store or REST)
	loading bool   // ListPins in flight
	failed  string // inline error ("" = none)

	size   ui.Size
	bounds layout.Rect // last Bounds (bordered iff size is its inner size)
}

var (
	_ ui.Overlay      = (*pinsView)(nil)
	_ ui.KeyHandler   = (*pinsView)(nil)
	_ ui.MouseHandler = (*pinsView)(nil)
	_ ui.Titled       = (*pinsView)(nil)
	_ ui.Helper       = (*pinsView)(nil)
)

// newPins is the chat.pins factory: args = channel id ("" = the viewed channel);
// nil for an unknown channel.
func newPins(ctx *ui.Context, args string) ui.Overlay {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	id := strings.TrimSpace(args)
	if id == "" {
		if ch := viewedChannel(ctx); ch != nil {
			id = ch.ID
		}
	}
	if id == "" || ctx.Store.Channels[id] == nil {
		return nil
	}
	o := &pinsView{channelID: id}
	o.list = widgets.NewList(func(m api.Message) string { return m.ID }, o.render)
	return o
}

func (o *pinsView) Init(ctx *ui.Context) tea.Cmd { return o.sync(ctx) }

// sync shows the store's list when it is loaded, else fetches it (one request at a
// time; never for a channel that is gone).
func (o *pinsView) sync(ctx *ui.Context) tea.Cmd {
	c := chatOf(ctx)
	if c == nil {
		return nil
	}
	if pins, loaded := c.Pins(o.channelID); loaded {
		o.loaded, o.failed = true, ""
		o.setItems(ctx, pins)
		return nil
	}
	if o.loading || ctx.Store.Channels[o.channelID] == nil {
		return nil
	}
	if o.loaded {
		// the store dropped the list (a pin it holds no copy of): fetch it again
		o.loaded = false
	}
	if o.failed != "" {
		return nil // no retry loop: reopen to try again
	}
	o.loading = true
	ch := o.channelID
	tk := c.PinsTicket()
	return ui.Do(ctx, "chat.pins:"+ch, func(c context.Context, cl *api.Client) (pinsResult, error) {
		pins, err := cl.ListPins(c, ch)
		return pinsResult{owner: o, ticket: tk, pins: pins}, err
	})
}

// setItems shows pins (the held copy of each when there is one; a held copy
// that is deleted or no longer pinned is left out), keeping the selection on
// the same message or at the same position.
func (o *pinsView) setItems(ctx *ui.Context, pins []api.Message) {
	prev, had := o.list.Selected()
	prevIdx := -1
	if had {
		prevIdx = o.index(prev.ID)
	}
	items := make([]api.Message, 0, len(pins))
	for _, m := range pins {
		cur := m
		if c := chatOf(ctx); c != nil {
			if held, ok := c.FindMessage(m.ID); ok {
				if !held.Pinned {
					continue // the window copy is newer than the snapshot: unpinned since
				}
				cur = held
			}
		}
		if cur.Deleted {
			continue
		}
		cur.Pinned = true
		items = append(items, cur)
	}
	o.items = items
	o.list.SetItems(items)
	switch {
	case len(items) == 0:
	case had && o.index(prev.ID) >= 0:
		o.list.Select(prev.ID)
	case prevIdx >= 0:
		o.list.Select(items[min(prevIdx, len(items)-1)].ID)
	default:
		o.list.Select(items[0].ID)
	}
}

func (o *pinsView) index(id string) int {
	for i, m := range o.items {
		if m.ID == id {
			return i
		}
	}
	return -1
}

func (o *pinsView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ui.ResultMsg[pinsResult]:
		if m.Value.owner != o {
			return nil
		}
		o.loading = false
		if m.Err != nil {
			o.failed = pinsFailed + ": " + api.Describe(m.Err)
			return nil
		}
		o.failed = ""
		if c := chatOf(ctx); c != nil {
			c.SetPinsFor(o.channelID, m.Value.ticket, m.Value.pins)
		}
		return o.sync(ctx)
	case ui.ResultMsg[msgAction]:
		cmd := landAction(ctx, m) // once-guarded: an unpin from here lands even with no conversation open
		return tea.Batch(cmd, o.sync(ctx))
	case ui.EventMsg, chatChangedMsg:
		return o.sync(ctx)
	}
	return nil
}

func (o *pinsView) Resize(_ *ui.Context, size ui.Size) { o.size = size }

func (o *pinsView) Bounds(body layout.Rect) layout.Rect {
	rows := max(1, len(o.items)) // the list scrolls when a row takes its second line
	o.bounds = centered(body, pinsMaxW, min(pinsMaxH, max(pinsMinH, rows+2)))
	return o.bounds
}

func (o *pinsView) Modal() bool { return true }

func (o *pinsView) title() string {
	if !o.loaded {
		return pinsTitle
	}
	return pinsTitle + " " + strconv.Itoa(len(o.items))
}

func (o *pinsView) Title(*ui.Context) (string, ui.Badge) { return o.title(), ui.Badge{} }

func (o *pinsView) Help(*ui.Context) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "이동")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "고정 해제")),
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "이동")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기")),
	}
}

func (o *pinsView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	km := ctx.Keys
	switch {
	case km.Matches(msg, keymap.NavBack):
		return false, nil // the app closes it
	case km.Matches(msg, keymap.ListOpen):
		m, ok := o.list.Selected()
		if !ok {
			return true, nil
		}
		return true, tea.Batch(closeOverlay, ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: o.channelID, Anchor: m.ID}))
	case km.Matches(msg, keymap.MsgPin):
		m, ok := o.list.Selected()
		if !ok {
			return true, nil
		}
		if !canPin(ctx, ctx.Store.Channels[o.channelID]) {
			return true, ui.Toast(noPinPerm, ui.ToastInfo)
		}
		if m.ChannelID == "" {
			m.ChannelID = o.channelID
		}
		return true, pinCmd(ctx, m, false)
	}
	return o.list.HandleKey(ctx, msg)
}

func (o *pinsView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	local.Y -= o.headRows()
	if local.Y < 0 {
		return nil
	}
	local.X -= overlayPad(o.size.W)
	return o.list.HandleMouse(ctx, msg, local)
}

// bordered: the app drew a border (the title is on it); otherwise the overlay
// was degraded to the whole body and the title goes inside.
func (o *pinsView) bordered() bool {
	return o.bounds.W >= 2 && o.bounds.H >= 2 && o.size.W == o.bounds.W-2 && o.size.H == o.bounds.H-2
}

// headRows is the title line (and a spacer) drawn inside when not bordered.
func (o *pinsView) headRows() int {
	if o.bordered() || o.size.H <= 1 {
		return 0
	}
	if o.size.H > 2 {
		return 2
	}
	return 1
}

// render draws one pinned message: "› " marker then its line-density row, at
// most pinsRowLines lines, continuation lines indented under the marker.
func (o *pinsView) render(ctx *ui.Context, m api.Message, selected bool, width int) []string {
	t := themeOf(ctx)
	marker, st := "  ", t.S.Base
	if selected {
		marker, st = "› ", t.S.Cursorline // the same band renderRow paints a selected row with
	}
	mw := textwidth.Width(marker)
	if width <= mw {
		return []string{fitPad(t, st.Render(marker), width)}
	}
	r := row{Kind: rowMessage, ID: m.ID, Msg: &m}
	lines := renderRow(ctx, r, msgStyle{Density: ui.DensityLine, Width: width - mw, Selected: selected})
	if len(lines) == 0 {
		lines = []string{""}
	}
	if len(lines) > pinsRowLines {
		lines = lines[:pinsRowLines]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		lead := st.Render(marker)
		if i > 0 {
			lead = st.Render(strings.Repeat(" ", mw))
		}
		out[i] = fitPad(t, lead+l, width)
	}
	return out
}

func (o *pinsView) View(ctx *ui.Context, _ bool) string {
	w, h := o.size.W, o.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	t := themeOf(ctx)
	cw := w - 2*overlayPad(w)
	var head []string
	if n := o.headRows(); n > 0 {
		head = append(head, cell(t.S.Title.Render, o.title(), cw))
		if n > 1 {
			head = append(head, "")
		}
	}
	bodyH := h - len(head)
	var body []string
	switch {
	case o.failed != "" && len(o.items) == 0:
		body = []string{cell(t.S.Error.Render, textwidth.Sanitize(o.failed), cw)}
	case !o.loaded:
		body = []string{cell(t.S.Dim.Render, loadingText, cw)}
	case len(o.items) == 0:
		body = []string{cell(t.S.Dim.Render, pinsEmpty, cw)}
	default:
		body = strings.Split(o.list.View(ctx, ui.Size{W: cw, H: bodyH}), "\n")
	}
	return renderOverlayRows(t, append(head, body...), o.size)
}
