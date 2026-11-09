package app

import (
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Front selects which surface single-pane modes show in Main.
type Front int

// Fronts.
const (
	FrontTab           Front = iota // single-pane modes show the chat surface: the active tab, or the DM surface when InDM (its stack top, or its thread)
	FrontFeed                       // ... show the feed surface (standard: FeedSwapped)
	FrontNotifications              // ... show the notifications route
)

// TabState is one open place (a tab, or the DM surface).
type TabState struct {
	Stack  []ui.Route // Stack[0] = tab root (RouteHome | RouteChannel, never a DM); never RouteThread/RouteFeed/RouteNotifications. DM surface: Stack[0] = RouteDMs, Stack[1] (when open) = the DM / group DM channel
	Thread *ui.Route  // this tab's open thread (Kind=RouteThread, Arg=root channel id: the tab root, or the DM surface's open DM), nil = closed

	seen uint64 // last-activation stamp (LRU eviction of the 10th tab); larger = more recent
}

// NavState is the mode-independent logical navigation state.
//
// DMs always live in the DM surface (CA 5.3.3): a DM or group DM channel is never a
// tab root; opening one from anywhere (home, palette, notifications, links, the
// sidebar) shows it on the DM surface, so its key is held in exactly one place.
type NavState struct {
	Tabs   []TabState // open places, max 9 (compact tab bar; command/standard: sidebar-selection history; widget/tiny: the "채팅" tab)
	Active int        // index into Tabs
	DM     TabState   // DM surface (the "DM" tab, rail/sidebar DM entry): its own stack and thread; never empty
	InDM   bool       // the chat surface is DM (else Tabs[Active]): FrontTab shows it, command/standard Main shows it
	Feed   []ui.Route // feed surface stack; Feed[0].Kind == RouteFeed (default {feed, Arg:"following"}); never empty
	Front  Front      // which surface single-pane modes (and standard's FeedSwapped) show in Main
}

// dmPred reports whether a channel id is a DM or group DM (Store.IsDM); nil knows none.
type dmPred func(channelID string) bool

func (p dmPred) is(id string) bool { return p != nil && id != "" && p(id) }

// NotifRoute is the single notifications route (key "notifications//").
var NotifRoute = ui.Route{Kind: ui.RouteNotifications}

// maxTabs is the open-place limit (CA 5.3.3).
const maxTabs = 9

var (
	homeRoute        = ui.Route{Kind: ui.RouteHome}
	dmsRoute         = ui.Route{Kind: ui.RouteDMs}
	defaultFeedRoute = ui.Route{Kind: ui.RouteFeed, Arg: "following"}
)

// newNavState is the initial (and post-login-reset) state: one home tab, the DM list,
// the following feed.
func newNavState() NavState {
	return NavState{
		Tabs:  []TabState{{Stack: []ui.Route{homeRoute}, seen: 1}},
		DM:    TabState{Stack: []ui.Route{dmsRoute}},
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
// then its thread (tab order), the DM surface's stack then its thread, the feed surface
// stack, then NotifRoute.
func (n NavState) Keys() []string {
	var out []string
	for _, t := range append(append([]TabState(nil), n.Tabs...), n.DM) {
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

// activeTab returns the chat surface (the DM surface when InDM, else Tabs[Active])
// when it exists.
func (n NavState) activeTab() (TabState, bool) {
	if n.InDM {
		if len(n.DM.Stack) == 0 {
			return TabState{}, false
		}
		return n.DM, true
	}
	if n.Active < 0 || n.Active >= len(n.Tabs) || len(n.Tabs[n.Active].Stack) == 0 {
		return TabState{}, false
	}
	return n.Tabs[n.Active], true
}

// surface is the chat surface of a cloned (valid) state: &DM when InDM, else the active tab.
func (n *NavState) surface() *TabState {
	if n.InDM {
		return &n.DM
	}
	return &n.Tabs[n.Active]
}

// copyTab deep-copies t.
func copyTab(t TabState) TabState {
	nt := TabState{Stack: append([]ui.Route(nil), t.Stack...), seen: t.seen}
	if t.Thread != nil {
		th := *t.Thread
		nt.Thread = &th
	}
	return nt
}

// clone deep-copies n so the pure transitions below never alias the caller's slices.
// It also repairs a zero/invalid state (no tabs, no DM list root, empty feed, Active
// out of range).
func (n NavState) clone() NavState {
	c := NavState{Active: n.Active, Front: n.Front, InDM: n.InDM}
	for _, t := range n.Tabs {
		if len(t.Stack) == 0 {
			continue
		}
		c.Tabs = append(c.Tabs, copyTab(t))
	}
	if len(c.Tabs) == 0 {
		c.Tabs = newNavState().Tabs
	}
	if c.Active < 0 || c.Active >= len(c.Tabs) {
		c.Active = 0
	}
	c.DM = copyTab(n.DM)
	if len(c.DM.Stack) == 0 || c.DM.Stack[0].Kind != ui.RouteDMs {
		c.DM.Stack = append([]ui.Route{dmsRoute}, c.DM.Stack...)
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

// stackBase is how many routes of s back and Replace never take: the root, and on the
// DM surface the open DM above the list (popping it with back shows the list again).
func stackBase(s []ui.Route) int {
	if len(s) >= 2 && s[0].Kind == ui.RouteDMs && s[1].Kind == ui.RouteChannel {
		return 2
	}
	return 1
}

// openDM shows DM channel r on the DM surface (InDM, FrontTab): the open DM keeps its
// stack (a jump target replaces it so the app can send RouteUpdatedMsg); another DM
// replaces it above the list and closes the thread. A tab that still holds r as its
// root (restored before the Store knew the channel) closes: one key, one place.
func (n *NavState) openDM(r ui.Route) {
	n.InDM, n.Front = true, FrontTab
	d := &n.DM
	if len(d.Stack) >= 2 && d.Stack[1].Key() == r.Key() {
		if r.Anchor != "" || r.Query != nil {
			d.Stack[1] = r
		}
	} else {
		d.Stack = []ui.Route{d.Stack[0], r}
		d.Thread = nil
	}
	n.closeTab(r.Key())
}

// closeTab removes the tab whose root key is k (the active tab then is the most
// recently seen one left; no tab left = a fresh 홈 tab).
func (n *NavState) closeTab(k string) {
	i := -1
	for j, t := range n.Tabs {
		if t.Stack[0].Key() == k {
			i = j
		}
	}
	if i < 0 {
		return
	}
	n.Tabs = append(n.Tabs[:i], n.Tabs[i+1:]...)
	switch {
	case len(n.Tabs) == 0:
		n.Tabs, n.Active = newNavState().Tabs, 0
	case i < n.Active:
		n.Active--
	case i == n.Active:
		n.Active = 0
		for j, t := range n.Tabs {
			if t.seen > n.Tabs[n.Active].seen {
				n.Active = j
			}
		}
	}
}

// liftDMs moves DM tab roots onto the DM surface (tabs restored from state.json or
// opened before the Store knew the channel; CA 5.3.3 "DMs live in the DM surface"):
// each such tab closes, and the one the chat surface showed opens on the DM surface
// with its stack and thread.
func (n NavState) liftDMs(isDM dmPred) NavState {
	c := n.clone()
	for _, t := range append([]TabState(nil), c.Tabs...) {
		root := t.Stack[0]
		if root.Kind != ui.RouteChannel || !isDM.is(root.ID) {
			continue
		}
		shown := !c.InDM && c.Tabs[c.Active].Stack[0].Key() == root.Key()
		c.closeTab(root.Key())
		if shown {
			c.DM.Stack = append([]ui.Route{c.DM.Stack[0]}, t.Stack...)
			c.DM.Thread = t.Thread
			c.InDM = true
		}
	}
	return c
}

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
	dest := &n.surface().Stack
	if toFeed {
		dest = &n.Feed
	}
	for i := range n.Tabs {
		if s := &n.Tabs[i].Stack; s != dest {
			*s = without(*s)
		}
	}
	if &n.DM.Stack != dest {
		n.DM.Stack = without(n.DM.Stack)
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
	if replace && len(s) > stackBase(s) {
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

// threadSlot is where a thread opened for mode is focused: its pane in command and
// standard, Main elsewhere.
func threadSlot(mode layout.Mode) layout.SlotID {
	if mode == layout.ModeCommand || mode == layout.ModeStandard {
		return layout.SlotThread
	}
	return layout.SlotMain
}

// navigate applies a NavigateMsg (CA 5.3.3 table) and returns the new state plus the focus
// intent it sets. isDM tells which channels are DMs (they open on the DM surface; nil =
// none known). login resets to newNavState() with no intent (the app shows the login
// screen outside the shell).
func (n NavState) navigate(m ui.NavigateMsg, mode layout.Mode, isDM dmPred) (NavState, FocusIntent) {
	r := m.Route
	if r.Kind == ui.RouteLogin {
		return newNavState(), FocusIntent{}
	}
	c := n.clone()
	switch {
	case r.Kind == ui.RouteDMs:
		c.InDM, c.Front = true, FrontTab
		c.DM.Stack, c.DM.Thread = c.DM.Stack[:1], nil
		return c, FocusIntent{Slot: layout.SlotMain, RouteKey: dmsRoute.Key()}
	case r.Kind == ui.RouteChannel && isDM.is(r.ID):
		c.openDM(r)
		return c, FocusIntent{Slot: layout.SlotMain, RouteKey: r.Key()}
	case r.Kind == ui.RouteThread && isDM.is(r.Arg):
		c.openDM(ui.Route{Kind: ui.RouteChannel, ID: r.Arg})
		th := r
		c.DM.Stack, c.DM.Thread = c.DM.Stack[:2], &th
		return c, FocusIntent{Slot: threadSlot(mode), RouteKey: r.Key()}
	case isRootKind(r.Kind):
		c.activateRoot(r)
		c.Front, c.InDM = FrontTab, false
		return c, FocusIntent{Slot: layout.SlotMain, RouteKey: r.Key()}
	case r.Kind == ui.RouteThread:
		c.activateRoot(ui.Route{Kind: ui.RouteChannel, ID: r.Arg})
		t := &c.Tabs[c.Active]
		t.Stack = t.Stack[:1]
		th := r
		t.Thread = &th
		c.Front, c.InDM = FrontTab, false
		return c, FocusIntent{Slot: threadSlot(mode), RouteKey: r.Key()}
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
	t := c.surface()
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
			if c.InDM && len(t.Stack) == 1 { // back to the DM list: the DM's thread closes with it
				t.Thread = nil
			}
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
	t := *n.surface()
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

// widgetPos is the widget/tiny tab shown: 0 채팅, 1 DM, 2 피드, 3 알림.
func (n NavState) widgetPos() int {
	switch {
	case n.Front == FrontFeed:
		return 2
	case n.Front == FrontNotifications:
		return 3
	case n.InDM:
		return 1
	}
	return 0
}

// setWidgetPos shows widget/tiny tab i (widgetPos order); the chat tabs keep their place.
func (n *NavState) setWidgetPos(i int) {
	switch i {
	case 0, 1:
		n.Front, n.InDM = FrontTab, i == 1
	case 2:
		n.Front = FrontFeed
	case 3:
		n.Front = FrontNotifications
	}
}

// barPos is the compact/standard tab shown: Tabs indexes, then DM (len(Tabs)), 피드, 알림.
func (n NavState) barPos() int {
	nt := len(n.Tabs)
	switch {
	case n.Front == FrontFeed:
		return nt + 1
	case n.Front == FrontNotifications:
		return nt + 2
	case n.InDM:
		return nt
	}
	return n.Active
}

// setBarPos shows compact/standard tab i (barPos order); selecting a tab marks it seen.
func (n *NavState) setBarPos(i int) {
	nt := len(n.Tabs)
	switch {
	case i < nt:
		n.Active, n.Front, n.InDM = i, FrontTab, false
		n.touch()
	case i == nt:
		n.Front, n.InDM = FrontTab, true
	case i == nt+1:
		n.Front = FrontFeed
	default:
		n.Front = FrontNotifications
	}
}

// cycleTab is tab.next/tab.prev (delta +1/-1, wrapping). Compact and standard walk the tab
// roots then DM, 피드, 알림; widget and tiny walk the fixed 채팅/DM/피드/알림 tabs; command
// walks Tabs then DM (Front is not visible there, but becomes FrontTab: Main shows that
// place, so the switch is a history place and a mode change keeps it); TooSmall is a
// no-op. Selecting a tab marks it seen; the DM and 채팅 tabs keep their own place.
func (n NavState) cycleTab(delta int, mode layout.Mode) NavState {
	c := n.clone()
	if delta == 0 || mode == layout.ModeTooSmall {
		return c
	}
	wrap := func(i, size int) int { return ((i+delta)%size + size) % size }
	switch mode {
	case layout.ModeCommand:
		nt := len(c.Tabs)
		cur := c.Active
		if c.InDM {
			cur = nt
		}
		c.setBarPos(wrap(cur, nt+1))
		c.Front = FrontTab // Main shows that place now (the feed column stays as it was)
	case layout.ModeWidget, layout.ModeTiny:
		c.setWidgetPos(wrap(c.widgetPos(), 4))
	default:
		c.setBarPos(wrap(c.barPos(), len(c.Tabs)+3))
	}
	return c
}

// selectTab activates the tab-bar entry i (a click; the order of cycleTab and
// tabItems): compact walks the tab roots then DM, 피드, 알림, widget and tiny the fixed
// 채팅/DM/피드/알림 tabs. Out-of-range entries and other modes are no-ops.
func (n NavState) selectTab(i int, mode layout.Mode) NavState {
	c := n.clone()
	switch mode {
	case layout.ModeWidget, layout.ModeTiny:
		if i >= 0 && i < 4 {
			c.setWidgetPos(i)
		}
	case layout.ModeCompact:
		if i >= 0 && i < len(c.Tabs)+3 {
			c.setBarPos(i)
		}
	}
	return c
}
