package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

func init() {
	ui.RegisterPane(layout.SlotRail, newRail)
	ui.RegisterOverlay(overlaySpaceAdd, func(*ui.Context, string) ui.Overlay { return newSpaceAddMenu() })
}

// overlaySpaceAdd is the registered rail "+" menu.
const overlaySpaceAdd = "chat.space_add"

// Rail item ids: home, the DM list, one per space ("space:<id>"), and the add button.
const (
	railHome        = "home"
	railDM          = "dm"
	railAdd         = "add"
	railSpacePrefix = "space:"
)

// railGlyphW is the centered glyph area of a rail row; one marker cell follows it.
const railGlyphW = 3

// railItem is one rail entry (rebuilt from the Store on every View).
type railItem struct {
	ID      string // railHome | railDM | railAdd | railSpacePrefix+space id
	Glyph   string // sanitized, one grapheme
	Unread  bool
	Mention bool
	Current bool
	Last    bool // no blank separator line below
}

// railPane is the command-mode space rail (SlotRail): 홈, DM (the DM list), the
// spaces in SpaceOrder (icon emoji, else the name's first grapheme), +.
type railPane struct {
	list    *widgets.List[railItem]
	size    ui.Size
	focused bool     // of the View being drawn (render reads it)
	lines   []string // last View, plain (blank rows are not clickable)
}

var (
	_ ui.Component    = (*railPane)(nil)
	_ ui.KeyHandler   = (*railPane)(nil)
	_ ui.MouseHandler = (*railPane)(nil)
)

// newRail is the SlotRail pane factory.
func newRail(ctx *ui.Context) ui.Component {
	p := &railPane{}
	p.list = widgets.NewList(func(it railItem) string { return it.ID }, p.render)
	p.list.SetItems(railItems(ctx))
	return p
}

// railCurrent is the rail entry to highlight: DM while Main shows the DM list, the
// viewed channel's space (a viewed DM or group DM = DM), else the rail's selected
// space, else 홈 (chatstore: "" = home / DMs).
func railCurrent(ctx *ui.Context) string {
	if ctx == nil || ctx.Store == nil {
		return railHome
	}
	if mainShows(ctx, ui.RouteDMs) {
		return railDM
	}
	if ch := viewedChannel(ctx); ch != nil {
		if sd := spaceOf(ctx, ch); sd != nil {
			return railSpacePrefix + sd.ID
		}
		if ch.Kind == api.ChannelDM || ch.Kind == api.ChannelGroupDM {
			return railDM
		}
	}
	if c := chatOf(ctx); c != nil {
		if id := c.SelectedSpace(); id != "" && ctx.Store.Spaces[id] != nil {
			return railSpacePrefix + id
		}
	}
	return railHome
}

// mainShows reports whether SlotMain shows a route of kind k this frame.
func mainShows(ctx *ui.Context, k ui.RouteKind) bool {
	r, ok := ctx.Routes[layout.SlotMain]
	return ok && r.Kind == k
}

// railItems builds the rail entries with their unread markers. DM sums the visible
// DMs (a blocked peer's 1:1 DM never counts); a space sums its channels; 홈 has
// none (its DMs count on DM). Muted channels add no unread, their mentions still
// count (Store.TotalUnread).
func railItems(ctx *ui.Context) []railItem {
	cur := railCurrent(ctx)
	add := func(its []railItem, it railItem, chs []*api.Channel) []railItem {
		for _, ch := range chs {
			if ch.My.MentionCount > 0 {
				it.Mention = true
			}
			if !ch.My.Muted && ch.My.UnreadCount > 0 {
				it.Unread = true
			}
		}
		it.Current = it.ID == cur
		return append(its, it)
	}
	items := add(nil, railItem{ID: railHome, Glyph: "홈"}, nil)
	items = add(items, railItem{ID: railDM, Glyph: "DM"}, visibleDMs(ctx))
	if ctx != nil && ctx.Store != nil {
		s := ctx.Store
		for _, id := range s.SpaceOrder {
			sd := s.Spaces[id]
			if sd == nil {
				continue
			}
			items = add(items, railItem{ID: railSpacePrefix + id, Glyph: spaceGlyph(sd)}, s.SpaceChannels(id))
		}
	}
	items = append(items, railItem{ID: railAdd, Glyph: "+", Last: true})
	return items
}

// spaceGlyph is the space's icon emoji when set, else the first grapheme of its
// name (sanitized; "?" when both are blank).
func spaceGlyph(sd *api.SpaceDetail) string {
	if sd.IconEmoji != nil {
		if g := firstGrapheme(*sd.IconEmoji); g != "" {
			return g
		}
	}
	if g := firstGrapheme(sd.Name); g != "" {
		return g
	}
	return "?"
}

func firstGrapheme(s string) string {
	s = strings.TrimSpace(textwidth.Sanitize(s))
	if s == "" {
		return ""
	}
	g, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
	return strings.TrimSpace(g)
}

// render draws an item: the glyph centered in 3 cells (S.Selected when it is the
// current entry, S.Cursorline for the focused cursor), one marker cell (● S.Error
// mention / • S.Bold unread / blank), and a blank separator row unless last.
func (p *railPane) render(ctx *ui.Context, it railItem, selected bool, width int) []string {
	t := themeOf(ctx)
	st := t.S.Base
	switch {
	case it.Current:
		st = t.S.Selected
	case selected && p.focused:
		st = t.S.Cursorline
	}
	gw, mw := width, 0 // railGlyphW + 1 at the CA width 4; narrower drops the marker last
	if width >= 2 {
		gw, mw = width-1, 1
	}
	glyph := textwidth.Truncate(it.Glyph, gw, "")
	pad := gw - textwidth.Width(glyph)
	left := (pad + 1) / 2 // the odd cell goes left: wide glyphs line up with "+" in column 1
	glyph = strings.Repeat(" ", left) + glyph + strings.Repeat(" ", pad-left)
	line := st.Render(glyph)
	if mw > 0 {
		switch {
		case it.Mention:
			line += t.S.Error.Render("●")
		case it.Unread:
			line += t.S.Bold.Render("•")
		default:
			line += t.S.Base.Render(" ")
		}
	}
	lines := []string{line}
	if !it.Last {
		lines = append(lines, t.S.Base.Render(strings.Repeat(" ", max(width, 0))))
	}
	return lines
}

func (p *railPane) Init(*ui.Context) tea.Cmd { return nil }

func (p *railPane) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }

func (p *railPane) Resize(_ *ui.Context, size ui.Size) { p.size = size }

// sync rebuilds the items from the Store; unfocused, the cursor follows the
// current entry (so focusing the rail starts there, and it stays in view).
func (p *railPane) sync(ctx *ui.Context, focused bool) {
	p.focused = focused
	items := railItems(ctx)
	p.list.SetItems(items)
	if !focused {
		for _, it := range items {
			if it.Current {
				p.list.Select(it.ID)
				break
			}
		}
	}
}

func (p *railPane) View(ctx *ui.Context, focused bool) string {
	w, h := p.size.W, p.size.H
	if w <= 0 || h <= 0 {
		p.lines = nil
		return ""
	}
	p.sync(ctx, focused)
	out := p.list.View(ctx, p.size)
	lines := strings.Split(out, "\n")
	p.lines = p.lines[:0]
	for _, l := range lines {
		p.lines = append(p.lines, ansi.Strip(l))
	}
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

func (p *railPane) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	p.focused = true
	if ctx.Keys.Matches(msg, keymap.ListOpen) {
		it, ok := p.list.Selected()
		if !ok {
			return false, nil
		}
		return true, p.run(ctx, it)
	}
	p.list.SetItems(railItems(ctx))
	return p.list.HandleKey(ctx, msg)
}

// HandleMouse: the wheel scrolls; a left click on an item's glyph row runs it
// (CA 5.5: rail clicks move), blank rows only focus the pane.
func (p *railPane) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if click, ok := msg.(tea.MouseClickMsg); ok {
		if click.Button != tea.MouseLeft || local.Y < 0 || local.Y >= len(p.lines) || strings.TrimSpace(p.lines[local.Y]) == "" {
			return nil
		}
	}
	cmd := p.list.HandleMouse(ctx, msg, local)
	if _, ok := msg.(tea.MouseClickMsg); !ok {
		return cmd
	}
	it, ok := p.list.Selected()
	if !ok {
		return cmd
	}
	return tea.Batch(cmd, p.run(ctx, it))
}

// run activates an entry: 홈 → RouteHome, DM → RouteDMs (the DM list in Main), a
// space → select it + its default channel, + → the space add menu.
func (p *railPane) run(ctx *ui.Context, it railItem) tea.Cmd {
	switch {
	case it.ID == railHome:
		return goHome(ctx)
	case it.ID == railDM:
		return goDMs()
	case it.ID == railAdd:
		return ui.OpenOverlay(newSpaceAddMenu())
	case strings.HasPrefix(it.ID, railSpacePrefix):
		return goSpace(ctx, strings.TrimPrefix(it.ID, railSpacePrefix))
	}
	return nil
}

// goHome clears the space selection and opens RouteHome (rail 홈, space switcher 홈).
func goHome(ctx *ui.Context) tea.Cmd {
	if c := chatOf(ctx); c != nil {
		c.SetSelectedSpace("")
	}
	return ui.Navigate(ui.Route{Kind: ui.RouteHome})
}

// goSpace selects space id and opens its default channel (else its first channel;
// nil when the space is unknown or has none).
func goSpace(ctx *ui.Context, id string) tea.Cmd {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	sd := ctx.Store.Spaces[id]
	if sd == nil {
		return nil
	}
	if c := chatOf(ctx); c != nil {
		c.SetSelectedSpace(id)
	}
	target := sd.DefaultChannelID
	if target == "" || ctx.Store.Channels[target] == nil {
		target = ""
		if chs := ctx.Store.SpaceChannels(id); len(chs) > 0 {
			target = chs[0].ID
		}
	}
	if target == "" {
		return nil
	}
	return ui.Navigate(ui.Route{Kind: ui.RouteChannel, ID: target})
}

// newSpaceAddMenu is the rail "+" menu (chat.space_add):
// 방 만들기 → chat.space_create, 초대 코드로 참여 → chat.space_join.
func newSpaceAddMenu() ui.Overlay {
	return newMenu("방 추가", []menuItem{
		{Label: "방 만들기", Run: openRegistered(overlaySpaceCreate, "")},
		{Label: "초대 코드로 참여", Run: openRegistered(overlaySpaceJoin, "")},
	})
}

// openRegistered is a menu action opening the overlay registered under id: the
// menu morphs into it in place (closing then opening from one tea.Batch could
// arrive in either order); unregistered, the menu closes and the app's
// OpenRegisteredOverlay handling applies (unknown id = ignored).
func openRegistered(id, args string) func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
	return func(ctx *ui.Context) (tea.Cmd, ui.Overlay) {
		if f, ok := ui.Registered().Overlays[id]; ok {
			if o := f(ctx, args); o != nil {
				return nil, o
			}
		}
		return ui.OpenRegisteredOverlay(id, args), nil
	}
}
