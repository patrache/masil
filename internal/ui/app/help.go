package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Help (? overlay and RouteHelp): the keymap per group as a "key  description" table
// plus two notices, scrolled with a widgets.List (plan 03 Task 28).

func init() {
	ui.RegisterRoute(ui.RouteHelp, func(*ui.Context, ui.Route) ui.Component { return newHelpView(false) })
}

// Help overlay outer size cap (centered in the body).
const helpMaxW, helpMaxH = 76, 28

// helpTitle is the help title (overlay border, pane title).
const helpTitle = "도움말"

// helpNotices are shown above the key table.
var helpNotices = []string{
	"이 클라이언트에서 입력한 글의 일부 이모지 수식자(피부색·결합 이모지 등)는 화면 폭을 맞추려고 정규화돼요.",
	"모호 폭 문자를 2칸으로 그리는 터미널 설정은 지원하지 않아요.",
}

// helpDragKey / helpDragText describe the mouse drag in the message list
// (msgPane drag selection; the terminal's own selection needs a modifier
// while masil reports the mouse).
const (
	helpDragKey  = "드래그"
	helpDragText = "끈 줄 복사 · 글자 단위 선택은 ⌥/fn/Shift+드래그"
)

// helpGroups are the keymap groups (전역·창·목록·입력·메시지·글) in display order.
var helpGroups = []struct {
	name    string
	actions []keymap.Action
}{
	{"전역", []keymap.Action{keymap.PaletteOpen, keymap.SearchGlobal, keymap.NotificationsOpen, keymap.HelpToggle,
		keymap.FeedToggle, keymap.TabPrev, keymap.TabNext, keymap.NavBack, keymap.NavHistoryBack, keymap.NavHistoryForward,
		keymap.AppQuit}},
	{"창", []keymap.Action{keymap.FocusNext, keymap.FocusPrev}},
	{"목록", []keymap.Action{keymap.ListUp, keymap.ListDown, keymap.ListTop, keymap.ListBottom,
		keymap.ListPageUp, keymap.ListPageDown, keymap.ListOpen}},
	{"입력", []keymap.Action{keymap.ComposeFocus, keymap.ComposeSend, keymap.ComposeNewline,
		keymap.ComposeComplete, keymap.ComposeEditLast, keymap.ComposeLeave}},
	{"메시지", []keymap.Action{keymap.MsgThread, keymap.MsgReact, keymap.MsgEdit, keymap.MsgDelete,
		keymap.MsgPin, keymap.MsgCopy, keymap.MsgActions, keymap.ChannelSearch, keymap.ThreadChannel}},
	{"글", []keymap.Action{keymap.PostLike, keymap.PostRepost, keymap.PostQuote, keymap.PostReply,
		keymap.PostOpenMedia, keymap.PostAuthor, keymap.PostCompose}},
}

// helpTabOrder follows the tab.next description in the help: the widget order (the
// compact bar puts the open tabs before DM; command walks the tabs then DM).
const helpTabOrder = " (채팅·DM·피드·알림)"

// helpRowKind is what a help row draws.
type helpRowKind int

const (
	helpBlank helpRowKind = iota
	helpNotice
	helpHeader
	helpKey
)

// helpRow is one item of the help list.
type helpRow struct {
	id        string
	kind      helpRowKind
	key, text string
	keyW      int // key column width
}

// helpKeyColMax caps the aligned key column; longer key labels flow past it.
const helpKeyColMax = 14

// helpRows builds the rows from the current keymap (so [keys] overrides show).
func helpRows(km *keymap.Keymap) []helpRow {
	var rows []helpRow
	n := 0
	add := func(r helpRow) {
		n++
		r.id = fmt.Sprint(n)
		rows = append(rows, r)
	}
	for _, t := range helpNotices {
		add(helpRow{kind: helpNotice, text: t})
	}
	prefix := km.Binding(keymap.WindowPrefix).Help().Key
	for _, g := range helpGroups {
		add(helpRow{kind: helpBlank})
		add(helpRow{kind: helpHeader, text: g.name})
		for _, a := range g.actions {
			b := km.Binding(a).Help()
			desc := b.Desc
			switch a {
			case keymap.AppQuit:
				desc += " (두 번)"
			case keymap.TabNext: // the order; per-mode orders are in README / CA 5.4
				desc += helpTabOrder
			}
			add(helpRow{kind: helpKey, key: b.Key, text: desc})
		}
		if g.name == "창" {
			for _, r := range [][2]string{
				{prefix + " h/j/k/l", "그 방향의 창으로"},
				{prefix + " w", "다음 창"},
				{prefix + " 1~5", "레이아웃 고정 (지휘소·표준·컴팩트·위젯·초소형)"},
				{prefix + " 0", "레이아웃 자동"},
			} {
				add(helpRow{kind: helpKey, key: r[0], text: r[1]})
			}
		}
		if g.name == "메시지" {
			add(helpRow{kind: helpKey, key: helpDragKey, text: helpDragText})
		}
	}
	kw := 0
	for _, r := range rows {
		if w := textwidth.Width(textwidth.Sanitize(r.key)); r.kind == helpKey && w <= helpKeyColMax {
			kw = max(kw, w)
		}
	}
	for i := range rows {
		rows[i].keyW = kw
	}
	return rows
}

// renderHelpRow draws one row at width w (notices wrap).
func renderHelpRow(t *theme.Theme, r helpRow, w int) []string {
	switch r.kind {
	case helpNotice:
		var out []string
		for _, l := range textwidth.Wrap(textwidth.Sanitize(r.text), max(1, w)) {
			out = append(out, t.S.Dim.Render(l))
		}
		return out
	case helpHeader:
		return []string{t.S.Title.Render(textwidth.Truncate(r.text, w, "…"))}
	case helpKey:
		k := textwidth.Sanitize(r.key)
		if kw := min(r.keyW, max(1, w/2)); textwidth.Width(k) < kw {
			k = textwidth.PadRight(k, kw)
		}
		line := t.S.Bold.Render("  "+k) + t.S.Base.Render("  "+textwidth.Sanitize(r.text))
		return []string{textwidth.Truncate(line, w, "…")}
	}
	return []string{""}
}

// helpView is the help component, used as the ? overlay and as the RouteHelp route.
type helpView struct {
	overlay  bool
	list     *widgets.List[helpRow]
	rows     []helpRow
	keys     *keymap.Keymap // keymap the rows were built from
	size     ui.Size        // inner size
	pendingG bool           // first "g" of "g g"
}

var (
	_ ui.Overlay      = (*helpView)(nil)
	_ ui.KeyHandler   = (*helpView)(nil)
	_ ui.MouseHandler = (*helpView)(nil)
	_ ui.Titled       = (*helpView)(nil)
	_ ui.Helper       = (*helpView)(nil)
)

func newHelpView(overlay bool) *helpView {
	h := &helpView{overlay: overlay}
	h.list = widgets.NewList(func(r helpRow) string { return r.id }, func(ctx *ui.Context, r helpRow, _ bool, w int) []string {
		return renderHelpRow(ctx.Theme, r, w)
	})
	return h
}

// ensureRows (re)builds the rows when the keymap changed.
func (h *helpView) ensureRows(ctx *ui.Context) {
	if h.rows != nil && h.keys == ctx.Keys {
		return
	}
	h.keys = ctx.Keys
	h.rows = helpRows(ctx.Keys)
	h.list.SetItems(h.rows)
	if len(h.rows) > 0 {
		h.list.Restore(widgets.ScrollAnchor{ItemID: h.rows[0].id})
	}
}

func (h *helpView) Init(ctx *ui.Context) tea.Cmd { h.ensureRows(ctx); return nil }
func (h *helpView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if _, ok := msg.(ui.ThemeChangedMsg); ok {
		h.list.SetItems(h.rows)
	}
	return nil
}
func (h *helpView) Resize(ctx *ui.Context, size ui.Size) { h.size = size; h.ensureRows(ctx) }
func (h *helpView) Modal() bool                          { return true }
func (h *helpView) Title(*ui.Context) (string, ui.Badge) { return helpTitle, ui.Badge{} }

func (h *helpView) Bounds(body layout.Rect) layout.Rect {
	w, ht := min(helpMaxW, body.W), min(helpMaxH, body.H)
	return layout.Rect{X: body.X + (body.W-w)/2, Y: body.Y + (body.H-ht)/2, W: w, H: ht}
}

func (h *helpView) Help(*ui.Context) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "스크롤")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기")),
	}
}

// contentSize is the list area (one blank column each side when wide enough).
func (h *helpView) contentSize() (ui.Size, int) {
	p := 0
	if h.size.W >= 12 {
		p = 1
	}
	return ui.Size{W: max(0, h.size.W-2*p), H: h.size.H}, p
}

// topIndex is the row index the list currently shows first.
func (h *helpView) topIndex() int {
	id := h.list.Anchor().ItemID
	for i, r := range h.rows {
		if r.id == id {
			return i
		}
	}
	return 0
}

// scrollTo shows row i first (clamped so the view never ends in blank rows).
func (h *helpView) scrollTo(ctx *ui.Context, i int) {
	if len(h.rows) == 0 {
		return
	}
	i = min(max(i, 0), len(h.rows)-1)
	h.list.Restore(widgets.ScrollAnchor{ItemID: h.rows[i].id})
	if cs, _ := h.contentSize(); cs.W > 0 && cs.H > 0 {
		h.list.View(ctx, cs) // normalizes the anchor (clamps at the bottom)
	}
}

func (h *helpView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	h.ensureRows(ctx)
	km := ctx.Keys
	if msg.String() == "g" {
		if h.pendingG {
			h.pendingG = false
			h.scrollTo(ctx, 0)
		} else {
			h.pendingG = true
		}
		return true, nil
	}
	h.pendingG = false
	cs, _ := h.contentSize()
	half := max(1, cs.H/2)
	top := h.topIndex()
	switch {
	case h.overlay && km.Matches(msg, keymap.HelpToggle):
		return true, func() tea.Msg { return ui.CloseOverlayMsg{} }
	case km.Matches(msg, keymap.ListDown):
		h.scrollTo(ctx, top+1)
	case km.Matches(msg, keymap.ListUp):
		h.scrollTo(ctx, top-1)
	case km.Matches(msg, keymap.ListPageDown):
		h.scrollTo(ctx, top+half)
	case km.Matches(msg, keymap.ListPageUp):
		h.scrollTo(ctx, top-half)
	case km.Matches(msg, keymap.ListTop):
		h.scrollTo(ctx, 0)
	case km.Matches(msg, keymap.ListBottom):
		h.scrollTo(ctx, len(h.rows)-1)
	default:
		return false, nil
	}
	return true, nil
}

func (h *helpView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		_, p := h.contentSize()
		return h.list.HandleMouse(ctx, msg, ui.Point{X: local.X - p, Y: local.Y})
	}
	return nil
}

func (h *helpView) View(ctx *ui.Context, _ bool) string {
	if h.size.W <= 0 || h.size.H <= 0 {
		return ""
	}
	h.ensureRows(ctx)
	cs, p := h.contentSize()
	body := h.list.View(ctx, cs)
	if p == 0 {
		return body
	}
	side := ctx.Theme.S.Base.Render(strings.Repeat(" ", p))
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = side + l + side
	}
	return strings.Join(lines, "\n")
}

// openHelp opens the help overlay (help.toggle) unless it is already on top.
func (m *Model) openHelp() tea.Cmd {
	if hv, ok := m.topOverlay().(*helpView); ok && hv.overlay {
		return nil
	}
	cmd, _ := m.pushOverlay(newHelpView(true))
	return cmd
}
