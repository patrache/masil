package chat

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterRoute(ui.RouteDMs, newDMList)
}

// DM list texts.
const (
	dmsTitle      = "DM"
	dmsEmptyTitle = "아직 DM 이 없어요"
	dmsEmptyHint  = "ctrl+k 로 친구를 찾아 대화를 시작해 보세요"
	dmsMine       = "나: "
	dmsDeleted    = "삭제된 메시지"
)

// dmsSaved is the Persistent state of the DM list.
type dmsSaved struct {
	Anchor   widgets.ScrollAnchor
	Selected string
}

// dmListView is RouteDMs, the DM surface root (CA 5.3.3): the visible DMs and group
// DMs by most recent activity (Store.DMOrder, a blocked peer's 1:1 DM hidden — the
// same visibleDMs the home and the sidebar use), one row each: presence dot (1:1) or
// "◇" (group), label, the last message preview when it is cached, the relative time
// and the unread badge (sideBadge). Enter / a second click opens the DM on the DM
// surface.
type dmListView struct {
	list    *widgets.List[*api.Channel]
	chs     []*api.Channel // last sync
	size    ui.Size
	focused bool // of the View being drawn (render reads it)
	restore *dmsSaved
}

var (
	_ ui.Component    = (*dmListView)(nil)
	_ ui.KeyHandler   = (*dmListView)(nil)
	_ ui.MouseHandler = (*dmListView)(nil)
	_ ui.Titled       = (*dmListView)(nil)
	_ ui.Helper       = (*dmListView)(nil)
	_ ui.Persistent   = (*dmListView)(nil)
)

// newDMList is the RouteDMs factory.
func newDMList(ctx *ui.Context, _ ui.Route) ui.Component {
	v := &dmListView{}
	v.list = widgets.NewList(func(ch *api.Channel) string { return ch.ID }, v.render)
	v.setItems(visibleDMs(ctx))
	return v
}

func (v *dmListView) setItems(chs []*api.Channel) {
	v.chs = chs
	v.list.SetItems(chs)
}

func (v *dmListView) index(id string) int {
	for i, ch := range v.chs {
		if ch.ID == id {
			return i
		}
	}
	return -1
}

// sync rebuilds the rows from the Store (the list keeps the selection by ID) and
// applies a pending LoadState.
func (v *dmListView) sync(ctx *ui.Context, focused bool) {
	v.focused = focused
	v.setItems(visibleDMs(ctx))
	if r := v.restore; r != nil && len(v.chs) > 0 {
		v.restore = nil
		v.list.Restore(r.Anchor)
		if v.index(r.Selected) >= 0 {
			v.list.Select(r.Selected)
		}
	}
}

// render draws a row: column 0 is the focus cursor "›" (blank when unfocused), then
// dmRow in width-1 cells.
func (v *dmListView) render(ctx *ui.Context, ch *api.Channel, selected bool, width int) []string {
	t := themeOf(ctx)
	if width <= 0 {
		return []string{""}
	}
	cur := t.S.Base.Render(" ")
	if selected && v.focused {
		cur = t.S.Title.Render("›")
	}
	if width-1 <= 0 {
		return []string{cur}
	}
	return []string{cur + dmRow(ctx, t, ch, width-1)}
}

// dmRow is a DM list row in exactly cw cells: "<presence> 이름" (1:1) or "◇ 라벨"
// (group), " 미리보기" (S.Dim, when there is room), then the right-aligned "3분" and
// badge. Unread (not muted) = S.Bold, muted or read-only = S.Dim. The preview goes
// first, then the time, when room runs short; the name keeps at least a little.
func dmRow(ctx *ui.Context, t *theme.Theme, ch *api.Channel, cw int) string {
	s := ctx.Store
	st := t.S.Base
	switch {
	case ch.My.Muted || ch.ReadOnly:
		st = t.S.Dim
	case ch.My.UnreadCount > 0 || ch.My.MentionCount > 0:
		st = t.S.Bold
	}
	lead, leadW := "", 0
	name := textwidth.Sanitize(s.ChannelLabel(ch.ID))
	if ch.Kind == api.ChannelGroupDM {
		name = "◇ " + name
	} else {
		lead, leadW = widgets.PresenceDot(t, dmPresence(ctx, ch))+st.Render(" "), 2
	}
	if leadW > cw {
		lead, leadW = "", 0
	}
	room := cw - leadW

	badge := widgets.Badge(t, sideBadge(ch))
	bw := textwidth.Width(ansi.Strip(badge))
	when := ""
	if ch.LastMessageAt != nil && ctx.Now != nil {
		when = humanize.Relative(ctx.Now(), *ch.LastMessageAt)
	}
	right := func() int {
		w := bw
		if when != "" {
			w += textwidth.Width(when)
			if bw > 0 {
				w++
			}
		}
		if w > 0 {
			w++ // the gap before it
		}
		return w
	}
	if room-right() < 4 {
		when = ""
	}
	if bw > 0 && room-right() < 2 {
		badge, bw = "", 0
	}
	rw := right()

	name = textwidth.Truncate(name, max(room-rw, 0), "…")
	rest := room - rw - textwidth.Width(name)
	preview := ""
	if p := dmPreview(ctx, ch); p != "" && rest >= 4 {
		preview = textwidth.Truncate(" "+p, rest, "…")
	}
	gap := rest - textwidth.Width(preview)
	out := lead + st.Render(name) + t.S.Dim.Render(preview) + st.Render(strings.Repeat(" ", max(gap, 0)))
	if rw > 0 {
		out += t.S.Base.Render(" ")
	}
	if when != "" {
		out += t.S.Dim.Render(when)
		if bw > 0 {
			out += t.S.Base.Render(" ")
		}
	}
	return out + badge
}

// dmPreview is the one-line preview of a DM's newest cached top-level message ("" when
// none is cached; a window moved back from the live end, hasMoreAfter, has none): "나: " for mine, "이름: " in a group DM, the body's first line (a
// deleted message says so). Sanitized.
func dmPreview(ctx *ui.Context, ch *api.Channel) string {
	c := chatOf(ctx)
	if c == nil {
		return ""
	}
	var m api.Message
	ok := false
	if ch.LastMessageID != nil {
		m, ok = c.FindMessage(*ch.LastMessageID)
	}
	if !ok && c.HasWindow(ch.ID) && !c.Window(ch.ID).HasMoreAfter() { // only a window that reaches the live end
		items := c.Window(ch.ID).Items()
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].ThreadRootID == nil {
				m, ok = items[i], true
				break
			}
		}
	}
	if !ok {
		return ""
	}
	body := m.Body
	if m.Deleted {
		body = dmsDeleted
	}
	body, _, _ = strings.Cut(strings.TrimSpace(body), "\n")
	body = strings.TrimSpace(textwidth.Sanitize(body))
	if body == "" {
		return ""
	}
	s := ctx.Store
	switch {
	case s.IsMe(m.Author.ID):
		body = dmsMine + body
	case ch.Kind == api.ChannelGroupDM:
		u := m.Author
		if cu := s.User(u.ID); cu != nil {
			u = *cu
		}
		if n := strings.TrimSpace(textwidth.Sanitize(displayName(u))); n != "" {
			body = n + ": " + body
		}
	}
	return body
}

func (v *dmListView) Init(*ui.Context) tea.Cmd { return nil }

func (v *dmListView) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }

func (v *dmListView) Resize(_ *ui.Context, size ui.Size) { v.size = size }

func (v *dmListView) View(ctx *ui.Context, focused bool) string {
	w, h := v.size.W, v.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	v.sync(ctx, focused)
	if len(v.chs) == 0 {
		if title, hint, ok := notLoaded(ctx); ok {
			icon := ""
			if hint != "" {
				icon = "⚠"
			}
			return widgets.Empty(themeOf(ctx), v.size, icon, title, hint)
		}
		return widgets.Empty(themeOf(ctx), v.size, "💬", dmsEmptyTitle, dmsEmptyHint)
	}
	lines := strings.Split(v.list.View(ctx, v.size), "\n")
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

// Title is "DM", badged with the DM unread (Store.DMUnread).
func (v *dmListView) Title(ctx *ui.Context) (string, ui.Badge) {
	return dmsTitle, dmUnreadBadge(ctx)
}

// dmUnreadBadge is the DM tab / rail / sidebar entry badge: the unread of the visible
// DMs and group DMs, in the mention style when any mentions me.
func dmUnreadBadge(ctx *ui.Context) ui.Badge {
	if ctx == nil || ctx.Store == nil {
		return ui.Badge{}
	}
	u, m := ctx.Store.DMUnread()
	return ui.Badge{Count: u, Mention: m > 0}
}

func (v *dmListView) Help(ctx *ui.Context) []key.Binding {
	return ctx.Keys.Help(keymap.ListOpen, keymap.ListDown, keymap.ListUp)
}

func (v *dmListView) SaveState() any {
	st := dmsSaved{Anchor: v.list.Anchor()}
	if ch, ok := v.list.Selected(); ok {
		st.Selected = ch.ID
	}
	return st
}

func (v *dmListView) LoadState(s any) {
	switch x := s.(type) {
	case dmsSaved:
		v.restore = &x
	case *dmsSaved:
		if x != nil {
			st := *x
			v.restore = &st
		}
	}
}

func (v *dmListView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	v.focused = true
	if len(v.chs) == 0 {
		return false, nil
	}
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		ch, ok := v.list.Selected()
		if !ok {
			return false, nil
		}
		return true, openDM(ch)
	}
	return v.list.HandleKey(ctx, msg)
}

// itemAt is the row index drawn at local row y of the last View (every row is one
// line), or -1.
func (v *dmListView) itemAt(y int) int {
	if y < 0 || y >= v.size.H {
		return -1
	}
	top := v.index(v.list.Anchor().ItemID)
	if top < 0 {
		top = 0
	}
	if i := top + y; i < len(v.chs) {
		return i
	}
	return -1
}

// HandleMouse: the wheel scrolls; a left click selects a row and a click on the
// selected row opens it (CA 5.5).
func (v *dmListView) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if len(v.chs) == 0 {
		return nil
	}
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return v.list.HandleMouse(ctx, msg, local)
	}
	if click.Button != tea.MouseLeft {
		return nil
	}
	i := v.itemAt(local.Y)
	if i < 0 {
		return nil
	}
	ch := v.chs[i]
	if cur, ok := v.list.Selected(); ok && cur.ID == ch.ID {
		return openDM(ch)
	}
	v.list.Select(ch.ID)
	return nil
}

// openDM opens a DM (the app places it on the DM surface).
func openDM(ch *api.Channel) tea.Cmd {
	return ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: ch.ID})
}

// goDMs opens the DM list (rail and sidebar DM entries).
func goDMs() tea.Cmd {
	return ui.Navigate(ui.Route{Kind: ui.RouteDMs})
}
