package app

import (
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Front selects which surface single-pane modes show in Main.
type Front int

// Fronts.
const (
	FrontTab           Front = iota // single-pane modes show the active tab (its stack top, or its thread)
	FrontFeed                       // ... show the feed surface (standard: FeedSwapped)
	FrontNotifications              // ... show the notifications route
)

// TabState is one open place.
type TabState struct {
	Stack  []ui.Route // Stack[0] = tab root (RouteHome | RouteChannel); never RouteThread/RouteFeed/RouteNotifications
	Thread *ui.Route  // this tab's open thread (Kind=RouteThread, Arg=root channel id), nil = closed

	seen uint64 // last-activation stamp (LRU eviction of the 10th tab); larger = more recent
}

// NavState is the mode-independent logical navigation state.
type NavState struct {
	Tabs   []TabState // open places, max 9 (compact tab bar; command/standard: sidebar-selection history; widget/tiny: the "채팅" tab)
	Active int        // index into Tabs
	Feed   []ui.Route // feed surface stack; Feed[0].Kind == RouteFeed (default {feed, Arg:"following"}); never empty
	Front  Front      // which surface single-pane modes (and standard's FeedSwapped) show in Main
}

// NotifRoute is the single notifications route (key "notifications//").
var NotifRoute = ui.Route{Kind: ui.RouteNotifications}

// maxTabs is the open-place limit (CA 5.3.3).
const maxTabs = 9

var (
	homeRoute        = ui.Route{Kind: ui.RouteHome}
	defaultFeedRoute = ui.Route{Kind: ui.RouteFeed, Arg: "following"}
)

// newNavState is the initial (and post-login-reset) state: one home tab, the following feed.
func newNavState() NavState {
	return NavState{
		Tabs:  []TabState{{Stack: []ui.Route{homeRoute}, seen: 1}},
		Feed:  []ui.Route{defaultFeedRoute},
		Front: FrontTab,
	}
}

// LayoutState derives layout.State for mode (CA 5.3.3).
func (n NavState) LayoutState(mode layout.Mode) layout.State {
	t, ok := n.activeTab()
	thr := ok && t.Thread != nil
	return layout.State{
		ThreadOpen:  thr && (mode == layout.ModeCommand || n.Front == FrontTab),
		FeedSwapped: mode == layout.ModeStandard && n.Front == FrontFeed,
	}
}

// Keys returns every route key held anywhere (instance-cache pinning): each tab's stack
// then its thread (tab order), the feed surface stack, then NotifRoute.
func (n NavState) Keys() []string {
	var out []string
	for _, t := range n.Tabs {
		for _, r := range t.Stack {
			out = append(out, r.Key())
		}
		if t.Thread != nil {
			out = append(out, t.Thread.Key())
		}
	}
	for _, r := range n.Feed {
		out = append(out, r.Key())
	}
	return append(out, NotifRoute.Key())
}

// activeTab returns Tabs[Active] when it exists.
func (n NavState) activeTab() (TabState, bool) {
	if n.Active < 0 || n.Active >= len(n.Tabs) || len(n.Tabs[n.Active].Stack) == 0 {
		return TabState{}, false
	}
	return n.Tabs[n.Active], true
}

// clone deep-copies n so the pure transitions below never alias the caller's slices.
// It also repairs a zero/invalid state (no tabs, empty feed, Active out of range).
func (n NavState) clone() NavState {
	c := NavState{Active: n.Active, Front: n.Front}
	for _, t := range n.Tabs {
		if len(t.Stack) == 0 {
			continue
		}
		nt := TabState{Stack: append([]ui.Route(nil), t.Stack...), seen: t.seen}
		if t.Thread != nil {
			th := *t.Thread
			nt.Thread = &th
		}
		c.Tabs = append(c.Tabs, nt)
	}
	if len(c.Tabs) == 0 {
		c.Tabs = newNavState().Tabs
	}
	if c.Active < 0 || c.Active >= len(c.Tabs) {
		c.Active = 0
	}
	c.Feed = append([]ui.Route(nil), n.Feed...)
	if len(c.Feed) == 0 || c.Feed[0].Kind != ui.RouteFeed {
		c.Feed = append([]ui.Route{defaultFeedRoute}, c.Feed...)
	}
	return c
}

// touch marks the active tab as the most recently seen.
func (n *NavState) touch() {
	var hi uint64
	for _, t := range n.Tabs {
		hi = max(hi, t.seen)
	}
	n.Tabs[n.Active].seen = hi + 1
}

// activateRoot activates the tab whose root key is root.Key(), adding it (and evicting the
// least-recently-seen inactive tab other than 홈 past maxTabs) when missing. The stack is kept as is; a
// root carrying a jump target (Anchor/Query) replaces the stored root so the app can send
// RouteUpdatedMsg to the cached instance.
func (n *NavState) activateRoot(root ui.Route) {
	k := root.Key()
	for i, t := range n.Tabs {
		if t.Stack[0].Key() == k {
			n.Active = i
			if root.Anchor != "" || root.Query != nil {
				n.Tabs[i].Stack[0] = root
			}
			n.touch()
			return
		}
	}
	n.Tabs = append(n.Tabs, TabState{Stack: []ui.Route{root}})
	n.Active = len(n.Tabs) - 1
	n.touch()
	if len(n.Tabs) <= maxTabs {
		return
	}
	victim := -1
	for i, t := range n.Tabs {
		// the 홈 tab is never evicted: compact has no rail or sidebar to bring it back
		if i != n.Active && t.Stack[0].Kind != ui.RouteHome && (victim < 0 || t.seen < n.Tabs[victim].seen) {
			victim = i
		}
	}
	n.Tabs = append(n.Tabs[:victim], n.Tabs[victim+1:]...)
	if victim < n.Active {
		n.Active--
	}
}

// isRootKind reports tab-root kinds.
func isRootKind(k ui.RouteKind) bool { return k == ui.RouteHome || k == ui.RouteChannel }

// isFeedContent reports the sns content kinds that default to the feed surface stack.
func isFeedContent(k ui.RouteKind) bool {
	switch k {
	case ui.RoutePost, ui.RouteProfile, ui.RouteHashtag, ui.RouteTrends, ui.RouteStories, ui.RouteCompose:
		return true
	}
	return false
}

// push places r on the feed surface stack (toFeed) or the active tab's stack, keeping the
// one-key-one-place invariant: r's key is removed from every other stack (roots untouched),
// and if the destination already holds it the stack pops back to it. Replace swaps the top
// (never the root).
func (n *NavState) push(r ui.Route, toFeed, replace bool) {
	k := r.Key()
	without := func(s []ui.Route) []ui.Route {
		out := s[:1]
		for _, x := range s[1:] {
			if x.Key() != k {
				out = append(out, x)
			}
		}
		return out
	}
	dest := &n.Tabs[n.Active].Stack
	if toFeed {
		dest = &n.Feed
	}
	for i := range n.Tabs {
		if s := &n.Tabs[i].Stack; s != dest {
			*s = without(*s)
		}
	}
	if &n.Feed != dest {
		n.Feed = without(n.Feed)
	}
	s := *dest
	for j := 1; j < len(s); j++ {
		if s[j].Key() == k {
			s[j] = r
			*dest = s[:j+1]
			return
		}
	}
	if replace && len(s) >= 2 {
		s[len(s)-1] = r
		return
	}
	*dest = append(s, r)
}

// mainSlotFor is the slot a surface's route lands in for mode: its own pane in command,
// Main elsewhere.
func mainSlotFor(mode layout.Mode, own layout.SlotID) layout.SlotID {
	if mode == layout.ModeCommand {
		return own
	}
	return layout.SlotMain
}

// navigate applies a NavigateMsg (CA 5.3.3 table) and returns the new state plus the focus
// intent it sets. login resets to newNavState() with no intent (the app shows the login
// screen outside the shell).
func (n NavState) navigate(m ui.NavigateMsg, mode layout.Mode) (NavState, FocusIntent) {
	r := m.Route
	if r.Kind == ui.RouteLogin {
		return newNavState(), FocusIntent{}
	}
	c := n.clone()
	switch {
	case isRootKind(r.Kind):
		c.activateRoot(r)
		c.Front = FrontTab
		return c, FocusIntent{Slot: layout.SlotMain, RouteKey: r.Key()}
	case r.Kind == ui.RouteThread:
		c.activateRoot(ui.Route{Kind: ui.RouteChannel, ID: r.Arg})
		t := &c.Tabs[c.Active]
		t.Stack = t.Stack[:1]
		th := r
		t.Thread = &th
		c.Front = FrontTab
		slot := layout.SlotMain
		if mode == layout.ModeCommand || mode == layout.ModeStandard {
			slot = layout.SlotThread
		}
		return c, FocusIntent{Slot: slot, RouteKey: r.Key()}
	case r.Kind == ui.RouteFeed:
		c.Feed = []ui.Route{r}
		c.Front = FrontFeed
		return c, FocusIntent{Slot: mainSlotFor(mode, layout.SlotFeed), RouteKey: r.Key()}
	case r.Kind == ui.RouteNotifications:
		c.Front = FrontNotifications
		return c, FocusIntent{Slot: mainSlotFor(mode, layout.SlotNotifications), RouteKey: NotifRoute.Key()}
	}
	toFeed := isFeedContent(r.Kind)
	switch m.Target {
	case ui.TargetMain:
		toFeed = false
	case ui.TargetSide:
		toFeed = true
	}
	c.push(r, toFeed, m.Replace)
	if toFeed {
		c.Front = FrontFeed
		return c, FocusIntent{Slot: mainSlotFor(mode, layout.SlotFeed), RouteKey: r.Key()}
	}
	c.Front = FrontTab
	return c, FocusIntent{Slot: layout.SlotMain, RouteKey: r.Key()}
}

// back undoes one step of the surface shown in the focused slot (CA 5.3.3 "뒤로"). l is the
// current layout (Compute(mode, …, n.LayoutState(mode))); what Main shows is read from
// Project(l, n), so l is authoritative for mode. Slots without a surface are no-ops.
func (n NavState) back(focus layout.SlotID, mode layout.Mode, l layout.Layout) NavState {
	c := n.clone()
	t := &c.Tabs[c.Active]
	popFeed := func() bool {
		if len(c.Feed) >= 2 {
			c.Feed = c.Feed[:len(c.Feed)-1]
			return true
		}
		return false
	}
	switch focus {
	case layout.SlotThread:
		t.Thread = nil
	case layout.SlotFeed:
		popFeed()
	case layout.SlotMain:
		shown, ok := Project(l, n)[layout.SlotMain]
		if !ok {
			return c
		}
		switch k := shown.Key(); {
		case t.Thread != nil && k == t.Thread.Key():
			t.Thread = nil
		case k == c.Feed[len(c.Feed)-1].Key():
			if !popFeed() {
				c.Front = FrontTab
			}
		case k == NotifRoute.Key():
			c.Front = FrontTab
		case len(t.Stack) >= 2:
			t.Stack = t.Stack[:len(t.Stack)-1]
		}
	}
	return c
}

// shownMain is the route Main shows for mode without a layout: Front, then (single-pane
// modes) the attached thread, then the active stack top. Standard keeps the thread in its
// own pane, so its tab surface is the stack top.
func (n NavState) shownMain(mode layout.Mode) ui.Route {
	switch n.Front {
	case FrontFeed:
		return n.Feed[len(n.Feed)-1]
	case FrontNotifications:
		return NotifRoute
	}
	t := n.Tabs[n.Active]
	top := t.Stack[len(t.Stack)-1]
	if mode != layout.ModeStandard && attached(t) {
		return *t.Thread
	}
	return top
}

// attached reports whether t's thread belongs to the channel on top of its stack.
func attached(t TabState) bool {
	if t.Thread == nil || len(t.Stack) == 0 {
		return false
	}
	top := t.Stack[len(t.Stack)-1]
	return top.Kind == ui.RouteChannel && top.ID == t.Thread.Arg
}

// toggleFeed is `f` (feed.toggle): command focuses the feed pane (state unchanged); other
// modes toggle Front between FrontTab and FrontFeed (from FrontNotifications => FrontFeed)
// and point the focus intent at what Main now shows (so a later mode change follows it).
func (n NavState) toggleFeed(mode layout.Mode) (NavState, *FocusIntent) {
	c := n.clone()
	if mode == layout.ModeCommand {
		return n, &FocusIntent{Slot: layout.SlotFeed, RouteKey: c.Feed[len(c.Feed)-1].Key()}
	}
	if c.Front == FrontFeed {
		c.Front = FrontTab
	} else {
		c.Front = FrontFeed
	}
	return c, &FocusIntent{Slot: layout.SlotMain, RouteKey: c.shownMain(mode).Key()}
}

// toggleNotifications is ctrl+n (notifications.open): command focuses the notifications pane
// (state unchanged); other modes toggle Front between FrontNotifications and FrontTab.
func (n NavState) toggleNotifications(mode layout.Mode) (NavState, *FocusIntent) {
	if mode == layout.ModeCommand {
		return n, &FocusIntent{Slot: layout.SlotNotifications, RouteKey: NotifRoute.Key()}
	}
	c := n.clone()
	if c.Front == FrontNotifications {
		c.Front = FrontTab
	} else {
		c.Front = FrontNotifications
	}
	return c, &FocusIntent{Slot: layout.SlotMain, RouteKey: c.shownMain(mode).Key()}
}

// cycleTab is tab.next/tab.prev (delta +1/-1, wrapping). Compact and standard walk the tab
// roots then 피드 then 알림; widget and tiny walk the fixed 채팅/피드/알림 tabs; command walks
// Tabs only (Front is not visible there); TooSmall is a no-op. Selecting a tab marks it seen.
func (n NavState) cycleTab(delta int, mode layout.Mode) NavState {
	c := n.clone()
	if delta == 0 || mode == layout.ModeTooSmall {
		return c
	}
	wrap := func(i, size int) int { return ((i+delta)%size + size) % size }
	switch mode {
	case layout.ModeCommand:
		c.Active = wrap(c.Active, len(c.Tabs))
		c.touch()
	case layout.ModeWidget, layout.ModeTiny:
		c.Front = Front(wrap(int(c.Front), 3))
	default:
		nt := len(c.Tabs)
		cur := c.Active
		switch c.Front {
		case FrontFeed:
			cur = nt
		case FrontNotifications:
			cur = nt + 1
		}
		switch next := wrap(cur, nt+2); {
		case next < nt:
			c.Active, c.Front = next, FrontTab
			c.touch()
		case next == nt:
			c.Front = FrontFeed
		default:
			c.Front = FrontNotifications
		}
	}
	return c
}

// selectTab activates the tab-bar entry i (a click; the order of cycleTab and
// tabItems): compact walks the tab roots then 피드 then 알림, widget and tiny the fixed
// 채팅/피드/알림 tabs. Out-of-range entries and other modes are no-ops.
func (n NavState) selectTab(i int, mode layout.Mode) NavState {
	c := n.clone()
	switch mode {
	case layout.ModeWidget, layout.ModeTiny:
		if i >= 0 && i < 3 {
			c.Front = Front(i)
		}
	case layout.ModeCompact:
		nt := len(c.Tabs)
		switch {
		case i < 0 || i > nt+1:
		case i < nt:
			c.Active, c.Front = i, FrontTab
			c.touch()
		case i == nt:
			c.Front = FrontFeed
		default:
			c.Front = FrontNotifications
		}
	}
	return c
}
