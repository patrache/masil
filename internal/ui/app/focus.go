package app

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// resolveFocus picks this frame's focus slot from the remembered intent (CA 5.3.5,
// first match): (1) the slot showing the intent's route key, (2) the intent's slot
// when the layout has it, (3) Main. The intent itself is never changed here, so a
// route/pane that comes back is focused again. RouteKey is the resolved slot's route
// key ("" for pane-factory slots).
func resolveFocus(intent FocusIntent, l layout.Layout, routes map[layout.SlotID]ui.Route) FocusIntent {
	keyOf := func(s layout.SlotID) string {
		if r, ok := routes[s]; ok {
			return r.Key()
		}
		return ""
	}
	if intent.RouteKey != "" {
		for _, s := range focusOrder {
			if r, ok := routes[s]; ok && r.Key() == intent.RouteKey {
				return FocusIntent{Slot: s, RouteKey: intent.RouteKey}
			}
		}
	}
	if intent.Slot != "" {
		if _, ok := l.Pane(intent.Slot); ok {
			return FocusIntent{Slot: intent.Slot, RouteKey: keyOf(intent.Slot)}
		}
	}
	return FocusIntent{Slot: layout.SlotMain, RouteKey: keyOf(layout.SlotMain)}
}

// focusOrder is the focus ring order (CA 5.3.5).
var focusOrder = []layout.SlotID{
	layout.SlotRail, layout.SlotSidebar, layout.SlotMain, layout.SlotThread,
	layout.SlotTool, // v0.1.3: command's tool surface (in place of the feed and right columns)
	layout.SlotFeed, layout.SlotMembers, layout.SlotNotifications,
}

// focusRing is the focus ring of l (CA 5.3.5): its bordered panes in focusOrder.
func focusRing(l layout.Layout) []layout.SlotID {
	var ring []layout.SlotID
	for _, s := range focusOrder {
		if p, ok := l.Pane(s); ok && p.Bordered {
			ring = append(ring, s)
		}
	}
	return ring
}

// isFocusable reports the slots focus can rest on (route and pane-factory slots).
func isFocusable(s layout.SlotID) bool {
	return slices.Contains(focusOrder, s)
}

// setFocus makes slot the focus intent (user focus move: tab, click, ctrl+w): the
// intent remembers the route shown there ("" for pane-factory slots).
func (m *Model) setFocus(slot layout.SlotID) {
	key := ""
	if r, ok := m.routes[slot]; ok {
		key = r.Key()
	}
	m.intent = FocusIntent{Slot: slot, RouteKey: key}
	m.focus = resolveFocus(m.intent, m.lay, m.routes)
}

// followMain points the intent at the route Main shows now (after a tab/Front switch
// moved what Main shows while it had the focus), so a later mode change follows it.
func (m *Model) followMain() {
	if r, ok := m.routes[layout.SlotMain]; ok {
		m.intent = FocusIntent{Slot: layout.SlotMain, RouteKey: r.Key()}
		m.focus = resolveFocus(m.intent, m.lay, m.routes)
	}
}

// cycleFocus is focus.next/focus.prev (delta +1/-1): the next ring pane, wrapping. A
// ring of at most one pane (compact, widget, tiny) walks the top tab bar instead,
// exactly as ] / [ do (walkStep: the open tools on the tool surface).
func (m *Model) cycleFocus(delta int) tea.Cmd {
	if len(focusRing(m.lay)) <= 1 {
		return m.walkStep(delta)
	}
	m.nextPane(delta)
	return nil
}

// nextPane moves the focus to the next (delta +1) or previous ring pane; no-op on a
// ring of at most one pane.
func (m *Model) nextPane(delta int) {
	ring := focusRing(m.lay)
	if len(ring) <= 1 {
		return
	}
	i := slices.Index(ring, m.focus.Slot)
	switch {
	case i < 0 && delta > 0:
		i = len(ring) - 1 // => ring[0]
	case i < 0:
		i = 0 // => last
	}
	n := len(ring)
	m.setFocus(ring[((i+delta)%n+n)%n])
}

// moveFocus is ctrl+w h/j/k/l: the ring pane whose center lies in direction (dx, dy)
// from the focused pane's center, nearest by center distance (ties: ring order).
func (m *Model) moveFocus(dx, dy int) {
	cur, ok := m.lay.Pane(m.focus.Slot)
	if !ok {
		return
	}
	center := func(r layout.Rect) (int, int) { return 2*r.X + r.W, 2*r.Y + r.H } // doubled: integer centers
	cx, cy := center(cur.Rect)
	best, bestD := layout.SlotID(""), 0
	for _, s := range focusRing(m.lay) {
		if s == cur.Slot {
			continue
		}
		p, _ := m.lay.Pane(s)
		px, py := center(p.Rect)
		ddx, ddy := px-cx, py-cy
		if (dx > 0 && ddx <= 0) || (dx < 0 && ddx >= 0) || (dy > 0 && ddy <= 0) || (dy < 0 && ddy >= 0) {
			continue
		}
		if d := ddx*ddx + ddy*ddy; best == "" || d < bestD {
			best, bestD = s, d
		}
	}
	if best != "" {
		m.setFocus(best)
	}
}

// syncFocus resolves this frame's focus slot (CA 5.3.5) at the end of every Update and
// broadcasts ui.FocusChangedMsg when it changed since the last broadcast.
func (m *Model) syncFocus() tea.Cmd {
	if !m.laidOut {
		return nil
	}
	m.focus = resolveFocus(m.intent, m.lay, m.routes)
	if m.focus == m.sentFocus {
		return nil
	}
	m.sentFocus = m.focus
	return m.broadcast(ui.FocusChangedMsg{Slot: m.focus.Slot, RouteKey: m.focus.RouteKey})
}
