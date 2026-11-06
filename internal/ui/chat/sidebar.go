package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterPane(layout.SlotSidebar, newSidebar)
	ui.RegisterOverlay("chat.space_switch", func(ctx *ui.Context, _ string) ui.Overlay { return newSpaceSwitchMenu(ctx) })
}

// sideKind is the kind of a sidebar row.
type sideKind int

const (
	sideHeader  sideKind = iota // the space name (command: title; standard: "이름 ▾" dropdown)
	sideLabel                   // "─ 채널", "─ DM"
	sideNotice                  // one wrapped line of the no-space notice
	sideChannel                 // a text channel, DM or group DM
)

// Sidebar row ids.
const (
	sideHeaderID     = "header"
	sideChanPrefix   = "ch:"
	sideNoticePrefix = "notice:"
)

// sideNoSpace is the notice drawn instead of the channel section without a space.
const sideNoSpace = "방이 없어요 · + 로 만들기"

// sideItem is one sidebar row (rebuilt from the Store on every View; every row is
// exactly one line, so the scroll anchor maps rows to items).
type sideItem struct {
	ID       string
	Kind     sideKind
	Text     string       // header / label / notice text, sanitized
	Dropdown bool         // header: standard-mode switcher
	Ch       *api.Channel // sideChannel
}

// selectable: channel rows and the standard-mode dropdown header.
func (it sideItem) selectable() bool {
	return it.Kind == sideChannel || (it.Kind == sideHeader && it.Dropdown)
}

// sidebarPane is the channel / DM list (SlotSidebar, command and standard): the
// current space's channels then the visible DMs.
type sidebarPane struct {
	list    *widgets.List[sideItem]
	items   []sideItem // last sync
	size    ui.Size
	focused bool // of the View being drawn (render reads it)
}

var (
	_ ui.Component    = (*sidebarPane)(nil)
	_ ui.KeyHandler   = (*sidebarPane)(nil)
	_ ui.MouseHandler = (*sidebarPane)(nil)
)

// newSidebar is the SlotSidebar pane factory.
func newSidebar(ctx *ui.Context) ui.Component {
	p := &sidebarPane{}
	p.list = widgets.NewList(func(it sideItem) string { return it.ID }, p.render)
	p.setItems(sideItems(ctx, 0))
	return p
}

// sideItems builds the rows. Command: title, "─ 채널", channels, "─ DM", DMs.
// Standard: "이름 ▾" (replacing the rail and the section label), channels, "─ DM",
// DMs. Without a space the channel section is the dim notice (wrapped to cw cells
// when cw > 0). DMs are visibleDMs: a blocked peer's 1:1 DM leaves at once.
func sideItems(ctx *ui.Context, cw int) []sideItem {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	s := ctx.Store
	command := ctx.Mode == layout.ModeCommand
	sid := currentSpaceID(ctx)
	sd := s.Spaces[sid]
	var items []sideItem
	name := ""
	if sd != nil {
		name = strings.TrimSpace(textwidth.Sanitize(sd.Name))
	}
	switch {
	case !command:
		if name == "" {
			name = "방"
		}
		items = append(items, sideItem{ID: sideHeaderID, Kind: sideHeader, Text: name, Dropdown: true})
	case sd != nil:
		items = append(items, sideItem{ID: sideHeaderID, Kind: sideHeader, Text: name})
	}
	if sd != nil {
		if command {
			items = append(items, sideItem{ID: "label:channels", Kind: sideLabel, Text: "─ 채널"})
		}
		for _, ch := range s.SpaceChannels(sid) {
			items = append(items, sideItem{ID: sideChanPrefix + ch.ID, Kind: sideChannel, Ch: ch})
		}
	} else {
		lines := []string{sideNoSpace}
		if cw > 0 {
			lines = wrapWords(sideNoSpace, cw)
		}
		for i, l := range lines {
			items = append(items, sideItem{ID: sideNoticePrefix + string(rune('a'+i)), Kind: sideNotice, Text: l})
		}
	}
	items = append(items, sideItem{ID: "label:dms", Kind: sideLabel, Text: "─ DM"})
	for _, ch := range visibleDMs(ctx) {
		items = append(items, sideItem{ID: sideChanPrefix + ch.ID, Kind: sideChannel, Ch: ch})
	}
	return items
}

// wrapWords wraps s at spaces only (textwidth.Wrap also breaks between Hangul
// syllables); a word wider than w is hard-wrapped.
func wrapWords(s string, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
		case textwidth.Width(line)+1+textwidth.Width(word) <= w:
			line += " " + word
			continue
		default:
			out = append(out, line)
		}
		line = ""
		if textwidth.Width(word) > w {
			parts := textwidth.Wrap(word, w)
			out = append(out, parts[:len(parts)-1]...)
			word = parts[len(parts)-1]
		}
		line = word
	}
	if line != "" || len(out) == 0 {
		out = append(out, line)
	}
	return out
}

// dmPresence is the presence of a 1:1 DM's peer: Store.Presence, else the friend
// entry, else offline (a DM with myself: my own status).
func dmPresence(ctx *ui.Context, ch *api.Channel) api.PresenceStatus {
	s := ctx.Store
	p := dmPeer(s, ch)
	if p == nil {
		return s.Self.Status
	}
	if e, ok := s.Presence[p.ID]; ok && e.Status != "" {
		return e.Status
	}
	if f, ok := s.Friends[p.ID]; ok && f.Presence != "" {
		return f.Presence
	}
	return api.PresenceOffline
}

// sideBadge is a channel's badge: the unread count (none when muted), in the
// mention style with at least the mention count when mentioned.
func sideBadge(ch *api.Channel) ui.Badge {
	n := ch.My.UnreadCount
	if ch.My.Muted {
		n = 0
	}
	b := ui.Badge{Count: n}
	if ch.My.MentionCount > 0 {
		b.Mention = true
		b.Count = max(n, ch.My.MentionCount)
	}
	return b
}

// render draws a row: column 0 is the focus cursor "›" (blank when unfocused),
// then the content in width-1 cells.
func (p *sidebarPane) render(ctx *ui.Context, it sideItem, selected bool, width int) []string {
	t := themeOf(ctx)
	if width <= 0 {
		return []string{""}
	}
	cur := t.S.Base.Render(" ")
	if selected && p.focused && it.selectable() {
		cur = t.S.Title.Render("›")
	}
	cw := width - 1
	if cw <= 0 {
		return []string{cur}
	}
	var body string
	switch it.Kind {
	case sideHeader:
		text := it.Text
		if it.Dropdown {
			text = textwidth.Truncate(text, max(cw-2, 0), "…") + " ▾"
		}
		body = cell(t.S.Title.Render, text, cw)
	case sideLabel, sideNotice:
		body = cell(t.S.Dim.Render, it.Text, cw)
	case sideChannel:
		body = channelRow(ctx, t, it.Ch, cw)
	}
	return []string{cur + body}
}

// channelRow is a channel / DM row in exactly cw cells: "# 이름" (private "🔒이름"),
// "<presence> 이름" for a 1:1 DM, "◇ 라벨" for a group DM, then the right-aligned
// badge. Viewed = S.Selected, unread (not muted) = S.Bold, muted or a read-only DM =
// S.Dim.
func channelRow(ctx *ui.Context, t *theme.Theme, ch *api.Channel, cw int) string {
	s := ctx.Store
	st := t.S.Base
	dm := ch.Kind == api.ChannelDM || ch.Kind == api.ChannelGroupDM
	switch {
	case s.Viewing(ch.ID):
		st = t.S.Selected
	case ch.My.Muted || (dm && ch.ReadOnly):
		st = t.S.Dim
	case ch.My.UnreadCount > 0 || ch.My.MentionCount > 0:
		st = t.S.Bold
	}

	lead, leadW := "", 0
	var name string
	switch ch.Kind {
	case api.ChannelDM:
		lead, leadW = widgets.PresenceDot(t, dmPresence(ctx, ch))+st.Render(" "), 2
		name = textwidth.Sanitize(s.ChannelLabel(ch.ID))
	case api.ChannelGroupDM:
		name = "◇ " + textwidth.Sanitize(s.ChannelLabel(ch.ID))
	default:
		name = channelTitle(ctx, ch, false)
	}
	if leadW > cw {
		lead, leadW = "", 0
	}

	badge := widgets.Badge(t, sideBadge(ch))
	bw := textwidth.Width(ansi.Strip(badge))
	room := cw - leadW
	if bw > 0 && room-bw < 2 { // keep at least a little of the name
		badge, bw = "", 0
	}
	nameW := room
	if bw > 0 {
		nameW = room - bw - 1
	}
	name = textwidth.Truncate(name, max(nameW, 0), "…")
	gap := room - textwidth.Width(name) - bw
	return lead + st.Render(name+strings.Repeat(" ", max(gap, 0))) + badge
}

func (p *sidebarPane) Init(*ui.Context) tea.Cmd { return nil }

func (p *sidebarPane) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }

func (p *sidebarPane) Resize(_ *ui.Context, size ui.Size) { p.size = size }

func (p *sidebarPane) setItems(items []sideItem) {
	p.items = items
	p.list.SetItems(items)
}

func (p *sidebarPane) index(id string) int {
	for i, it := range p.items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

func (p *sidebarPane) selectedIndex() int {
	if it, ok := p.list.Selected(); ok {
		return p.index(it.ID)
	}
	return -1
}

// snap moves the cursor off a non-selectable row: on in the direction it moved
// (from prev), else back the other way, else to prev.
func (p *sidebarPane) snap(prev int) {
	cur := p.selectedIndex()
	if cur < 0 || p.items[cur].selectable() {
		return
	}
	dir := 1
	if prev >= 0 && cur < prev {
		dir = -1
	}
	for _, d := range []int{dir, -dir} {
		for i := cur + d; i >= 0 && i < len(p.items); i += d {
			if p.items[i].selectable() {
				p.list.Select(p.items[i].ID)
				return
			}
		}
	}
	if prev >= 0 && prev < len(p.items) {
		p.list.Select(p.items[prev].ID)
	}
}

// sync rebuilds the rows from the Store; unfocused, the cursor follows the viewed
// channel (so focusing the sidebar starts there, and it stays in view).
func (p *sidebarPane) sync(ctx *ui.Context, focused bool) {
	p.focused = focused
	p.setItems(sideItems(ctx, p.size.W-1))
	if !focused {
		if ch := viewedChannel(ctx); ch != nil && p.index(sideChanPrefix+ch.ID) >= 0 {
			p.list.Select(sideChanPrefix + ch.ID)
		}
	}
	p.snap(-1)
}

func (p *sidebarPane) View(ctx *ui.Context, focused bool) string {
	w, h := p.size.W, p.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	p.sync(ctx, focused)
	lines := strings.Split(p.list.View(ctx, p.size), "\n")
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

func (p *sidebarPane) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	p.focused = true
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		it, ok := p.list.Selected()
		if !ok || !it.selectable() {
			return false, nil
		}
		return true, p.open(ctx, it)
	}
	prev := p.selectedIndex()
	handled, cmd := p.list.HandleKey(ctx, msg)
	if handled {
		p.snap(prev)
	}
	return handled, cmd
}

// itemAt is the row index drawn at local row y of the last View (every row is one
// line, so it is the scroll anchor's item + y), or -1.
func (p *sidebarPane) itemAt(y int) int {
	if y < 0 || y >= p.size.H {
		return -1
	}
	a := p.list.Anchor()
	top := p.index(a.ItemID)
	if top < 0 {
		top = 0
	}
	if i := top + y; i < len(p.items) {
		return i
	}
	return -1
}

// HandleMouse: the wheel scrolls; a left click selects a row and a click on the
// selected row opens it (CA 5.5); the dropdown header opens the space switcher at
// once; labels and the notice only focus the pane.
func (p *sidebarPane) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok {
		return p.list.HandleMouse(ctx, msg, local)
	}
	if click.Button != tea.MouseLeft {
		return nil
	}
	i := p.itemAt(local.Y)
	if i < 0 || !p.items[i].selectable() {
		return nil
	}
	it := p.items[i]
	if it.Kind == sideHeader {
		p.list.Select(it.ID)
		return p.open(ctx, it)
	}
	if cur, ok := p.list.Selected(); ok && cur.ID == it.ID {
		return p.open(ctx, it)
	}
	p.list.Select(it.ID)
	return nil
}

// open runs a row: the dropdown opens the space switcher, a channel row opens that
// channel (a space channel also becomes the selected space).
func (p *sidebarPane) open(ctx *ui.Context, it sideItem) tea.Cmd {
	switch it.Kind {
	case sideHeader:
		return ui.OpenOverlay(newSpaceSwitchMenu(ctx))
	case sideChannel:
		if it.Ch == nil {
			return nil
		}
		if it.Ch.SpaceID != nil {
			if c := chatOf(ctx); c != nil {
				c.SetSelectedSpace(*it.Ch.SpaceID)
			}
		}
		return ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: it.Ch.ID})
	}
	return nil
}

// newSpaceSwitchMenu is the standard-mode space switcher (chat.space_switch): 홈,
// the spaces in SpaceOrder, 방 만들기, 초대 코드로 참여.
func newSpaceSwitchMenu(ctx *ui.Context) ui.Overlay {
	items := []menuItem{{Label: "홈", Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) { return goHome(ctx), nil }}}
	if ctx != nil && ctx.Store != nil {
		for _, id := range ctx.Store.SpaceOrder {
			sd := ctx.Store.Spaces[id]
			if sd == nil {
				continue
			}
			name := strings.TrimSpace(textwidth.Sanitize(sd.Name))
			if name == "" {
				name = "?"
			}
			items = append(items, menuItem{Label: name, Run: func(ctx *ui.Context) (tea.Cmd, ui.Overlay) { return goSpace(ctx, id), nil }})
		}
	}
	items = append(items,
		menuItem{Label: "방 만들기", Run: openRegistered("chat.space_create", "")},
		menuItem{Label: "초대 코드로 참여", Run: openRegistered("chat.space_join", "")},
	)
	return newMenu("방 전환", items)
}
