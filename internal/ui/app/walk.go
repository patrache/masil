package app

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// The tab walk (0.1.3 patch, tools-nav item 7): [ / ] (tab.prev / tab.next)
// step through one sequence of places, and the top tab bar (SlotTabs, every
// mode but TooSmall) lists exactly that sequence — the current place
// emphasised, the [ / ] targets marked — and a click on an entry goes there.
// Both the keys (walkStep), the click (walkGo) and the bar (walkItems) read
// the sequence from the same three functions — walkLen, walkPos, walkTo — so
// the bar can never show a different order than the keys walk.

// walkScope is which sequence [ / ] walk.
type walkScope int

const (
	walkNone    walkScope = iota // TooSmall: nothing
	walkBar                      // compact, standard: tab roots, DM, 피드, 알림, (도구)
	walkWidget                   // widget, tiny: 채팅, DM, 피드, 알림, (도구)
	walkCommand                  // command, chat column: tab roots, DM
	walkTools                    // the tool surface has the focus: the open tools
)

// chatWalk is the chat-side sequence of mode.
func chatWalk(mode layout.Mode) walkScope {
	switch mode {
	case layout.ModeCommand:
		return walkCommand
	case layout.ModeStandard, layout.ModeCompact:
		return walkBar
	case layout.ModeWidget, layout.ModeTiny:
		return walkWidget
	}
	return walkNone
}

// wrapWalk is pos moved by delta on a ring of size entries (size > 0).
func wrapWalk(pos, delta, size int) int { return ((pos+delta)%size + size) % size }

// placeKind is what a walk entry shows.
type placeKind int

const (
	placeRoot    placeKind = iota // the chat tab root Tabs[i]
	placeChat                     // widget/tiny 채팅: the chat tabs, as they are
	placeDM                       // the DM surface
	placeFeed                     // the feed surface
	placeNotif                    // the notifications surface
	placeToolTab                  // the 도구 tab: the tool surface in front (while a tool is open)
	placeTool                     // the open tool Tools[i]
)

// walkPlace is one entry of a walk.
type walkPlace struct {
	kind placeKind
	i    int
}

// walkPlaces is THE order of scope s — the one list the keys, the click and
// the bar read (walkLen, walkPos, walkTo, Model.walkItems).
func (n NavState) walkPlaces(s walkScope) []walkPlace {
	var out []walkPlace
	switch s {
	case walkBar, walkCommand:
		for i := range n.Tabs {
			out = append(out, walkPlace{kind: placeRoot, i: i})
		}
		out = append(out, walkPlace{kind: placeDM})
		if s == walkBar {
			out = append(out, walkPlace{kind: placeFeed}, walkPlace{kind: placeNotif})
		}
	case walkWidget:
		out = append(out, walkPlace{kind: placeChat}, walkPlace{kind: placeDM},
			walkPlace{kind: placeFeed}, walkPlace{kind: placeNotif})
	case walkTools:
		for i := range n.Tools {
			out = append(out, walkPlace{kind: placeTool, i: i})
		}
	}
	if (s == walkBar || s == walkWidget) && n.toolTabs() > 0 {
		out = append(out, walkPlace{kind: placeToolTab}) // the tool tab exists while a tool is open (A8.3)
	}
	return out
}

// walkLen is how many entries scope s has.
func (n NavState) walkLen(s walkScope) int { return len(n.walkPlaces(s)) }

// currentPlace is the place of scope s shown now.
func (n NavState) currentPlace(s walkScope) walkPlace {
	switch {
	case s == walkTools:
		return walkPlace{kind: placeTool, i: n.ToolActive}
	case s != walkCommand && n.Front == FrontTools:
		return walkPlace{kind: placeToolTab}
	case s != walkCommand && n.Front == FrontFeed:
		return walkPlace{kind: placeFeed}
	case s != walkCommand && n.Front == FrontNotifications:
		return walkPlace{kind: placeNotif}
	case n.InDM:
		return walkPlace{kind: placeDM}
	case s == walkWidget:
		return walkPlace{kind: placeChat}
	}
	return walkPlace{kind: placeRoot, i: n.Active}
}

// walkPos is the index of the current entry of scope s (0 when it is not on
// the list).
func (n NavState) walkPos(s walkScope) int {
	return max(slices.Index(n.walkPlaces(s), n.currentPlace(s)), 0)
}

// walkTo shows entry i of scope s (out of range: unchanged). A tab root is
// marked seen; the DM and 채팅 entries keep their own place. Command: Front
// becomes FrontTab (Main shows that place, so the switch is a history place
// and a mode change keeps it), the tool column beside it staying when it was
// in front and not zoomed.
func (n NavState) walkTo(s walkScope, i int) NavState {
	c := n.clone()
	places := c.walkPlaces(s)
	if i < 0 || i >= len(places) {
		return c
	}
	tools := c.Front == FrontTools
	switch p := places[i]; p.kind {
	case placeRoot:
		c.Active, c.Front, c.InDM = p.i, FrontTab, false
		c.touch()
	case placeChat, placeDM:
		c.Front, c.InDM = FrontTab, p.kind == placeDM
	case placeFeed:
		c.Front = FrontFeed
	case placeNotif:
		c.Front = FrontNotifications
	case placeToolTab:
		c.Front = FrontTools
	case placeTool:
		c, _ = c.showToolAt(p.i, layout.ModeStandard)
	}
	if s == walkCommand && tools && !c.zoomed() {
		c.Front = FrontTools
	}
	return c
}

// walkScope is the sequence [ / ] walk now: the open tools while the focus is
// on the tool surface (onToolSurface), else the mode's chat-side sequence.
func (m *Model) walkScope() walkScope {
	if m.onToolSurface() {
		return walkTools
	}
	return chatWalk(m.lay.Mode)
}

// walkStep is tab.prev / tab.next (delta -1 / +1).
func (m *Model) walkStep(delta int) tea.Cmd {
	s := m.walkScope()
	size := m.nav.walkLen(s)
	if size == 0 || delta == 0 {
		return nil
	}
	return m.walkGo(s, wrapWalk(m.nav.walkPos(s), delta, size), false)
}

// walkGo shows entry i of scope s: a tool through applyTools; a chat-side
// place through the relayout, the focus following Main when it was there (a
// key) or always (a click on the bar).
func (m *Model) walkGo(s walkScope, i int, click bool) tea.Cmd {
	if i < 0 || i >= m.nav.walkLen(s) {
		return nil
	}
	if s == walkTools {
		return m.applyTools(m.nav.showToolAt(i, m.lay.Mode))
	}
	wasMain := m.focus.Slot == layout.SlotMain
	prev := m.toolSnap()
	m.nav = m.nav.walkTo(s, i)
	cmd := m.relayout()
	if wasMain || click {
		m.followMain()
	}
	return tea.Batch(cmd, m.toolSwitched(prev))
}

// walkItems is the top tab bar's entries — one per walkPlaces entry of
// walkScope, in that order — and the current one.
func (m *Model) walkItems() ([]widgets.Tab, int) {
	s := m.walkScope()
	fixed := m.fixedTabs()
	var items []widgets.Tab
	for _, p := range m.nav.walkPlaces(s) {
		var t widgets.Tab
		switch p.kind {
		case placeRoot:
			t = m.rootTab(m.nav.Tabs[p.i].Stack[0])
		case placeChat:
			t = fixed.chat
		case placeDM:
			t = fixed.dm
		case placeFeed:
			t = fixed.feed
		case placeNotif:
			t = fixed.notif
		case placeToolTab:
			t = fixed.tools
		case placeTool:
			t = m.toolTab(m.nav.Tools[p.i])
		}
		items = append(items, t)
	}
	return items, m.nav.walkPos(s)
}

// toolTab is a tool-surface entry's tab: its title and Titled badge, the
// tool's icon (the hub: its short label) as the narrowest label.
func (m *Model) toolTab(r ui.Route) widgets.Tab {
	t := widgets.Tab{Label: m.toolEntryTitle(r), Short: m.ctx.T.Tools.TabLabelShort()}
	if s, ok := ui.ToolByID(r.Arg); ok && r.Kind == ui.RouteTool && s.Icon != "" {
		t.Short = s.Icon
	}
	if in := m.cache.lookup(r.Key()); in != nil {
		if tt, ok := in.comp.(ui.Titled); ok {
			_, t.Badge = tt.Title(m.ctx)
		}
	}
	return t
}
