package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Navigation history (CA 5.3.3 "기록"): the places Main showed, oldest first, walked
// by nav.history_back / nav.history_forward. It lives beside NavState (never in it, so
// a mode change still leaves NavState alone) and is not saved in state.json.

// maxHistory caps the recorded places (the oldest is dropped first).
const maxHistory = 50

// History texts.
const (
	historyStartText = "더 뒤로 갈 곳이 없어요"
	historyEndText   = "더 앞으로 갈 곳이 없어요"
)

// histLoc is one recorded place: the surface Main showed and the route on it.
type histLoc struct {
	Front Front
	Root  ui.Route // FrontTab: the tab root (Anchor/Query cleared); zero otherwise
	Route ui.Route // the route shown (Anchor cleared): the attached thread, the stack top, the feed top, NotifRoute
}

// key identifies a place (Anchor/Query never count).
func (l histLoc) key() string {
	return string(rune('0'+l.Front)) + "|" + l.Root.Key() + "|" + l.Route.Key()
}

// loc is the place n shows, mode-independently: FrontTab => the active tab's
// attached thread, else its stack top; FrontFeed => the feed top; FrontNotifications.
func (n NavState) loc() histLoc {
	plain := func(r ui.Route) ui.Route { r.Anchor = ""; return r }
	c := n.clone()
	switch c.Front {
	case FrontFeed:
		return histLoc{Front: FrontFeed, Route: plain(c.Feed[len(c.Feed)-1])}
	case FrontNotifications:
		return histLoc{Front: FrontNotifications, Route: NotifRoute}
	}
	t := c.Tabs[c.Active]
	root := t.Stack[0]
	root.Anchor, root.Query = "", nil
	r := t.Stack[len(t.Stack)-1]
	if attached(t) {
		r = *t.Thread
	}
	return histLoc{Front: FrontTab, Root: root, Route: plain(r)}
}

// history is the recorded places and the index of the current one.
type history struct {
	locs []histLoc
	cur  int
}

// note records l as the current place unless it already is: the places after the
// current one (left by going back) are dropped, then l is appended (cap maxHistory).
func (h *history) note(l histLoc) {
	if len(h.locs) > 0 && h.locs[h.cur].key() == l.key() {
		h.locs[h.cur] = l
		return
	}
	if len(h.locs) > 0 {
		h.locs = h.locs[:h.cur+1]
	}
	h.locs = append(h.locs, l)
	if over := len(h.locs) - maxHistory; over > 0 {
		h.locs = append([]histLoc(nil), h.locs[over:]...)
	}
	h.cur = len(h.locs) - 1
}

// reset forgets every place (login, logout).
func (h *history) reset() { h.locs, h.cur = nil, 0 }

// target is the index the next walk in direction delta (-1 back, +1 forward) lands
// on: the nearest place that still exists and differs from now; -1 when none.
func (h *history) target(delta int, now histLoc, exists func(histLoc) bool) int {
	for i := h.cur + delta; i >= 0 && i < len(h.locs); i += delta {
		if l := h.locs[i]; l.key() != now.key() && exists(l) {
			return i
		}
	}
	return -1
}

// placeExists reports whether the conversations l needs are still there: a channel
// root or thread whose channel left the Store after bootstrap (deleted, the space
// left) or a DM with someone I blocked is gone. Other places always exist.
func placeExists(st *store.Store, l histLoc) bool {
	if l.Front != FrontTab {
		return true
	}
	known := func(id string) bool {
		ch := st.Channels[id]
		if ch == nil {
			return !st.Sync.Bootstrapped
		}
		if ch.Kind == api.ChannelDM {
			for _, m := range ch.Members {
				if !st.IsMe(m.ID) && st.IsBlocked(m.ID) {
					return false
				}
			}
		}
		return true
	}
	if l.Root.Kind == ui.RouteChannel && !known(l.Root.ID) {
		return false
	}
	return l.Route.Kind != ui.RouteThread || known(l.Route.Arg)
}

// restore shows l again (a history walk): FrontNotifications; FrontFeed with its route
// on the feed stack (a feed root resets the stack to it); FrontTab activates the tab of
// l.Root (re-added when it was closed or evicted) and shows l.Route there — a thread
// as its attached thread (stack folded to the root), the root itself with no thread,
// any other route pushed on that tab (one key, one place). The focus intent is the
// slot that shows it.
func (n NavState) restore(l histLoc, mode layout.Mode) (NavState, FocusIntent) {
	c := n.clone()
	r := l.Route
	switch l.Front {
	case FrontNotifications:
		c.Front = FrontNotifications
		return c, FocusIntent{Slot: mainSlotFor(mode, layout.SlotNotifications), RouteKey: NotifRoute.Key()}
	case FrontFeed:
		if r.Kind == ui.RouteFeed {
			if c.Feed[0].Key() == r.Key() {
				c.Feed = c.Feed[:1]
			} else {
				c.Feed = []ui.Route{r}
			}
		} else {
			c.push(r, true, false)
		}
		c.Front = FrontFeed
		return c, FocusIntent{Slot: mainSlotFor(mode, layout.SlotFeed), RouteKey: r.Key()}
	}
	c.activateRoot(l.Root)
	c.Front = FrontTab
	t := &c.Tabs[c.Active]
	switch {
	case r.Kind == ui.RouteThread:
		t.Stack = t.Stack[:1]
		th := r
		t.Thread = &th
		slot := layout.SlotMain
		if mode == layout.ModeCommand || mode == layout.ModeStandard {
			slot = layout.SlotThread
		}
		return c, FocusIntent{Slot: slot, RouteKey: r.Key()}
	case r.Key() == l.Root.Key():
		t.Stack = t.Stack[:1]
		t.Thread = nil
	default:
		c.push(r, false, false)
	}
	return c, FocusIntent{Slot: layout.SlotMain, RouteKey: r.Key()}
}

// noteHistory records the place shown after an Update (CA 5.3.3 "기록"); logged out
// there is no history.
func (m *Model) noteHistory() {
	if m.login != nil || m.creds == nil {
		m.hist.reset()
		return
	}
	m.hist.note(m.nav.loc())
}

// historyTarget is m.hist.target for the current state and Store.
func (m *Model) historyTarget(delta int) int {
	return m.hist.target(delta, m.nav.loc(), func(l histLoc) bool { return placeExists(m.ctx.Store, l) })
}

// walkHistory is nav.history_back (delta -1) / nav.history_forward (+1): the nearest
// existing place in that direction is shown again; at the end a toast says so.
func (m *Model) walkHistory(delta int) tea.Cmd {
	m.noteHistory() // a change not yet recorded is the current place
	i := m.historyTarget(delta)
	if i < 0 {
		text := historyStartText
		if delta > 0 {
			text = historyEndText
		}
		return m.toasts.push(m.ctx, ui.ToastMsg{Text: text, Kind: ui.ToastInfo})
	}
	m.hist.cur = i
	n, intent := m.nav.restore(m.hist.locs[i], m.lay.Mode)
	m.nav, m.intent = n, intent
	return m.relayout()
}

// openParent is thread.channel on a focused thread: its channel opens on the root
// message (stack folded to the channel). Where the thread is shown in Main (no thread
// pane) it is closed; a thread pane stays open.
func (m *Model) openParent() tea.Cmd {
	r, ok := m.routes[m.focus.Slot]
	if !ok || r.Kind != ui.RouteThread {
		return nil
	}
	target := ui.Route{Kind: ui.RouteChannel, ID: r.Arg, Anchor: r.ID}
	n, intent := m.nav.navigate(ui.NavigateMsg{Route: target}, m.lay.Mode)
	t := &n.Tabs[n.Active]
	t.Stack = t.Stack[:1]
	if _, pane := m.lay.Pane(layout.SlotThread); !pane {
		t.Thread = nil
	}
	m.nav, m.intent = n, intent
	m.cache.rejump = target.Key()
	return m.relayout()
}
