package app

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// modeFirstSize mirrors the first entry of CA 10.2 uitest.ModeSizes per mode
// (this internal test must not import uitest: import cycle).
var modeFirstSize = map[layout.Mode][2]int{
	layout.ModeCommand:  {200, 60},
	layout.ModeStandard: {120, 40},
	layout.ModeCompact:  {120, 30},
	layout.ModeWidget:   {80, 24},
	layout.ModeTiny:     {50, 20},
	layout.ModeTooSmall: {19, 10},
}

var allModes = []layout.Mode{
	layout.ModeCommand, layout.ModeStandard, layout.ModeCompact,
	layout.ModeWidget, layout.ModeTiny, layout.ModeTooSmall,
}

func ch(id string) ui.Route      { return ui.Route{Kind: ui.RouteChannel, ID: id} }
func thr(id, c string) ui.Route  { return ui.Route{Kind: ui.RouteThread, ID: id, Arg: c} }
func post(id string) ui.Route    { return ui.Route{Kind: ui.RoutePost, ID: id} }
func feedR(arg string) ui.Route  { return ui.Route{Kind: ui.RouteFeed, Arg: arg} }
func search(q string) ui.Route   { return ui.Route{Kind: ui.RouteSearch, Arg: q} }
func profile(id string) ui.Route { return ui.Route{Kind: ui.RouteProfile, ID: id} }

var (
	homeR     = ui.Route{Kind: ui.RouteHome}
	settingsR = ui.Route{Kind: ui.RouteSettings}
	following = feedR("following")
)

func nav(n NavState, r ui.Route) NavState {
	n2, _ := n.navigate(ui.NavigateMsg{Route: r}, layout.ModeCompact)
	return n2
}

func keysOf(rs []ui.Route) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Key()
	}
	return out
}

func activeStack(n NavState) []string { return keysOf(n.Tabs[n.Active].Stack) }

func layoutFor(mode layout.Mode, n NavState) layout.Layout {
	sz := modeFirstSize[mode]
	return layout.Compute(mode, sz[0], sz[1], n.LayoutState(mode))
}

func TestNewNavState(t *testing.T) {
	n := newNavState()
	if len(n.Tabs) != 1 || n.Active != 0 || !reflect.DeepEqual(activeStack(n), []string{"home//"}) || n.Tabs[0].Thread != nil {
		t.Fatalf("tabs = %+v active %d", n.Tabs, n.Active)
	}
	if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//following"}) || n.Front != FrontTab {
		t.Fatalf("feed = %v front %v", n.Feed, n.Front)
	}
}

func TestNavigateTable(t *testing.T) {
	t.Run("channel new tab", func(t *testing.T) {
		n, fi := newNavState().navigate(ui.NavigateMsg{Route: ch("C3")}, layout.ModeCompact)
		if len(n.Tabs) != 2 || n.Active != 1 || !reflect.DeepEqual(activeStack(n), []string{"channel/C3/"}) || n.Front != FrontTab {
			t.Fatalf("got %+v", n)
		}
		if fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "channel/C3/"}) {
			t.Fatalf("focus = %+v", fi)
		}
	})
	t.Run("channel again keeps stack", func(t *testing.T) {
		n := nav(nav(newNavState(), ch("C3")), search("q"))
		n = nav(n, homeR)
		if n.Active != 0 {
			t.Fatalf("home not active: %d", n.Active)
		}
		n, fi := n.navigate(ui.NavigateMsg{Route: ch("C3")}, layout.ModeCommand)
		if len(n.Tabs) != 2 || n.Active != 1 || !reflect.DeepEqual(activeStack(n), []string{"channel/C3/", "search//q"}) {
			t.Fatalf("got %+v", n.Tabs)
		}
		if fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "channel/C3/"}) {
			t.Fatalf("focus = %+v", fi)
		}
	})
	t.Run("channel anchor updates root route", func(t *testing.T) {
		n := nav(newNavState(), ch("C3"))
		a := ch("C3")
		a.Anchor = "M9"
		n = nav(nav(n, homeR), a)
		if n.Tabs[1].Stack[0].Anchor != "M9" || n.Active != 1 {
			t.Fatalf("root = %+v", n.Tabs[1].Stack[0])
		}
	})
	t.Run("home front reset", func(t *testing.T) {
		n := nav(newNavState(), feedR("discover"))
		n = nav(n, homeR)
		if n.Front != FrontTab || len(n.Tabs) != 1 {
			t.Fatalf("got %+v", n)
		}
	})
	t.Run("thread folds stack", func(t *testing.T) {
		n := nav(nav(newNavState(), ch("C3")), search("q"))
		n = nav(nav(n, settingsR), feedR("discover"))
		n = nav(n, homeR)
		r := thr("R", "C3")
		n2, fi := n.navigate(ui.NavigateMsg{Route: r}, layout.ModeCommand)
		if n2.Active != 1 || !reflect.DeepEqual(activeStack(n2), []string{"channel/C3/"}) || n2.Front != FrontTab {
			t.Fatalf("got %+v", n2.Tabs)
		}
		if n2.Tabs[1].Thread == nil || !reflect.DeepEqual(*n2.Tabs[1].Thread, r) {
			t.Fatalf("thread = %v", n2.Tabs[1].Thread)
		}
		if fi != (FocusIntent{Slot: layout.SlotThread, RouteKey: "thread/R/C3"}) {
			t.Fatalf("focus = %+v", fi)
		}
		_, fi = n.navigate(ui.NavigateMsg{Route: r}, layout.ModeCompact)
		if fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "thread/R/C3"}) {
			t.Fatalf("compact focus = %+v", fi)
		}
		// the original state is not mutated (pure)
		if len(n.Tabs[1].Stack) != 3 || n.Tabs[1].Thread != nil {
			t.Fatalf("input mutated: %+v", n.Tabs[1])
		}
	})
	t.Run("thread new tab", func(t *testing.T) {
		n := nav(newNavState(), thr("R", "C7"))
		if len(n.Tabs) != 2 || n.Active != 1 || !reflect.DeepEqual(activeStack(n), []string{"channel/C7/"}) || n.Tabs[1].Thread == nil {
			t.Fatalf("got %+v", n.Tabs)
		}
	})
	t.Run("feed resets surface", func(t *testing.T) {
		n := nav(newNavState(), post("P1"))
		n, fi := n.navigate(ui.NavigateMsg{Route: feedR("discover")}, layout.ModeCommand)
		if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//discover"}) || n.Front != FrontFeed {
			t.Fatalf("got %+v", n)
		}
		if fi != (FocusIntent{Slot: layout.SlotFeed, RouteKey: "feed//discover"}) {
			t.Fatalf("focus = %+v", fi)
		}
		_, fi = n.navigate(ui.NavigateMsg{Route: feedR("discover")}, layout.ModeStandard)
		if fi.Slot != layout.SlotMain {
			t.Fatalf("standard focus = %+v", fi)
		}
	})
	t.Run("post pushes on feed", func(t *testing.T) {
		n, fi := newNavState().navigate(ui.NavigateMsg{Route: post("P")}, layout.ModeCompact)
		if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//following", "post/P/"}) || n.Front != FrontFeed {
			t.Fatalf("got %+v", n)
		}
		if fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "post/P/"}) {
			t.Fatalf("focus = %+v", fi)
		}
		_, fi = newNavState().navigate(ui.NavigateMsg{Route: post("P")}, layout.ModeCommand)
		if fi != (FocusIntent{Slot: layout.SlotFeed, RouteKey: "post/P/"}) {
			t.Fatalf("command focus = %+v", fi)
		}
		for _, k := range []ui.RouteKind{ui.RouteProfile, ui.RouteHashtag, ui.RouteTrends, ui.RouteStories, ui.RouteCompose} {
			n := nav(newNavState(), ui.Route{Kind: k, ID: "x"})
			if len(n.Feed) != 2 || n.Feed[1].Kind != k || n.Front != FrontFeed || len(n.Tabs[0].Stack) != 1 {
				t.Fatalf("%s: %+v", k, n)
			}
		}
	})
	t.Run("notifications", func(t *testing.T) {
		n, fi := newNavState().navigate(ui.NavigateMsg{Route: NotifRoute}, layout.ModeWidget)
		if n.Front != FrontNotifications || fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "notifications//"}) {
			t.Fatalf("got %+v %+v", n, fi)
		}
		_, fi = newNavState().navigate(ui.NavigateMsg{Route: NotifRoute}, layout.ModeCommand)
		if fi != (FocusIntent{Slot: layout.SlotNotifications, RouteKey: "notifications//"}) {
			t.Fatalf("command focus = %+v", fi)
		}
	})
	t.Run("other kinds push on active tab", func(t *testing.T) {
		for _, k := range []ui.RouteKind{ui.RouteSearch, ui.RouteInvite, ui.RouteSpaceSettings, ui.RouteFriends, ui.RouteSettings, ui.RouteHelp} {
			n := nav(newNavState(), feedR("discover"))
			n, fi := n.navigate(ui.NavigateMsg{Route: ui.Route{Kind: k}}, layout.ModeCommand)
			want := ui.Route{Kind: k}.Key()
			if !reflect.DeepEqual(activeStack(n), []string{"home//", want}) || n.Front != FrontTab || len(n.Feed) != 1 {
				t.Fatalf("%s: %+v", k, n)
			}
			if fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: want}) {
				t.Fatalf("%s focus = %+v", k, fi)
			}
		}
	})
	t.Run("targets", func(t *testing.T) {
		n, fi := newNavState().navigate(ui.NavigateMsg{Route: post("P"), Target: ui.TargetMain}, layout.ModeCommand)
		if !reflect.DeepEqual(activeStack(n), []string{"home//", "post/P/"}) || len(n.Feed) != 1 || n.Front != FrontTab {
			t.Fatalf("TargetMain post: %+v", n)
		}
		if fi.Slot != layout.SlotMain {
			t.Fatalf("focus = %+v", fi)
		}
		n, fi = newNavState().navigate(ui.NavigateMsg{Route: search("q"), Target: ui.TargetSide}, layout.ModeCommand)
		if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//following", "search//q"}) || len(n.Tabs[0].Stack) != 1 || n.Front != FrontFeed {
			t.Fatalf("TargetSide search: %+v", n)
		}
		if fi != (FocusIntent{Slot: layout.SlotFeed, RouteKey: "search//q"}) {
			t.Fatalf("focus = %+v", fi)
		}
		// root / lifted kinds ignore Target
		n = nav(newNavState(), ch("C1"))
		n, _ = n.navigate(ui.NavigateMsg{Route: ch("C2"), Target: ui.TargetSide}, layout.ModeCommand)
		if len(n.Tabs) != 3 || len(n.Feed) != 1 {
			t.Fatalf("channel TargetSide: %+v", n)
		}
		n, _ = n.navigate(ui.NavigateMsg{Route: feedR("discover"), Target: ui.TargetMain}, layout.ModeCommand)
		if keysOf(n.Feed)[0] != "feed//discover" || len(n.Tabs[n.Active].Stack) != 1 {
			t.Fatalf("feed TargetMain: %+v", n)
		}
	})
	t.Run("replace", func(t *testing.T) {
		n := nav(nav(newNavState(), post("P1")), post("P2"))
		n, _ = n.navigate(ui.NavigateMsg{Route: post("P3"), Replace: true}, layout.ModeCompact)
		if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//following", "post/P1/", "post/P3/"}) {
			t.Fatalf("feed = %v", keysOf(n.Feed))
		}
		// replace never replaces the root: root-only stack => push
		n, _ = newNavState().navigate(ui.NavigateMsg{Route: settingsR, Replace: true}, layout.ModeCompact)
		if !reflect.DeepEqual(activeStack(n), []string{"home//", "settings//"}) {
			t.Fatalf("stack = %v", activeStack(n))
		}
		n, _ = n.navigate(ui.NavigateMsg{Route: search("q"), Replace: true}, layout.ModeCompact)
		if !reflect.DeepEqual(activeStack(n), []string{"home//", "search//q"}) {
			t.Fatalf("stack = %v", activeStack(n))
		}
		// replace on a root kind is ignored (activates/adds the tab)
		n, _ = n.navigate(ui.NavigateMsg{Route: ch("C1"), Replace: true}, layout.ModeCompact)
		if len(n.Tabs) != 2 || !reflect.DeepEqual(keysOf(n.Tabs[0].Stack), []string{"home//", "search//q"}) {
			t.Fatalf("tabs = %+v", n.Tabs)
		}
	})
	t.Run("tenth tab evicts least recently seen inactive", func(t *testing.T) {
		n := newNavState()
		for i := 1; i <= 8; i++ {
			n = nav(n, ch(fmt.Sprintf("C%d", i)))
		}
		if len(n.Tabs) != 9 {
			t.Fatalf("tabs = %d", len(n.Tabs))
		}
		// revisit home and C1 so C2 becomes least recently seen
		n = nav(nav(n, homeR), ch("C1"))
		n = nav(n, ch("C9"))
		if len(n.Tabs) != 9 {
			t.Fatalf("tabs = %d", len(n.Tabs))
		}
		var roots []string
		for _, tb := range n.Tabs {
			roots = append(roots, tb.Stack[0].ID)
		}
		want := []string{"", "C1", "C3", "C4", "C5", "C6", "C7", "C8", "C9"}
		if !reflect.DeepEqual(roots, want) {
			t.Fatalf("roots = %v", roots)
		}
		if n.Tabs[n.Active].Stack[0].ID != "C9" {
			t.Fatalf("active = %d", n.Active)
		}
		// the active tab is never evicted even if it is the oldest
		n = nav(n, ch("C3"))
		n = nav(n, ch("C10"))
		if len(n.Tabs) != 9 || n.Tabs[n.Active].Stack[0].ID != "C10" {
			t.Fatalf("tabs = %+v active %d", n.Tabs, n.Active)
		}
	})
	t.Run("home tab is never evicted", func(t *testing.T) {
		n := newNavState()
		for i := 1; i <= 12; i++ {
			n = nav(n, ch(fmt.Sprintf("C%d", i)))
		}
		if len(n.Tabs) != 9 || n.Tabs[0].Stack[0].Kind != ui.RouteHome {
			t.Fatalf("tabs = %+v", n.Tabs)
		}
		if n.Tabs[n.Active].Stack[0].ID != "C12" || n.Tabs[1].Stack[0].ID != "C5" {
			t.Fatalf("tabs = %+v active %d", n.Tabs, n.Active)
		}
	})
	t.Run("login resets", func(t *testing.T) {
		n := nav(nav(nav(newNavState(), ch("C1")), post("P")), thr("R", "C1"))
		n2, fi := n.navigate(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteLogin, Arg: "login"}}, layout.ModeCommand)
		if !reflect.DeepEqual(n2, newNavState()) || fi != (FocusIntent{}) {
			t.Fatalf("got %+v %+v", n2, fi)
		}
	})
	t.Run("pushing key held elsewhere removes it there", func(t *testing.T) {
		n := nav(nav(newNavState(), post("P")), profile("U"))
		n = nav(n, ch("C1"))
		n, _ = n.navigate(ui.NavigateMsg{Route: post("P"), Target: ui.TargetMain}, layout.ModeCommand)
		if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//following", "profile/U/"}) {
			t.Fatalf("feed = %v", keysOf(n.Feed))
		}
		if !reflect.DeepEqual(activeStack(n), []string{"channel/C1/", "post/P/"}) {
			t.Fatalf("stack = %v", activeStack(n))
		}
		// also from other tabs' stacks
		n = nav(n, homeR)
		n, _ = n.navigate(ui.NavigateMsg{Route: post("P"), Target: ui.TargetMain}, layout.ModeCommand)
		if !reflect.DeepEqual(keysOf(n.Tabs[1].Stack), []string{"channel/C1/"}) || !reflect.DeepEqual(activeStack(n), []string{"home//", "post/P/"}) {
			t.Fatalf("tabs = %+v", n.Tabs)
		}
	})
	t.Run("pushing key already in destination pops back to it", func(t *testing.T) {
		n := nav(nav(nav(newNavState(), post("P1")), profile("U")), post("P2"))
		r := profile("U")
		r.Anchor = "X"
		n, fi := n.navigate(ui.NavigateMsg{Route: r}, layout.ModeCompact)
		if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//following", "post/P1/", "profile/U/"}) || n.Feed[2].Anchor != "X" {
			t.Fatalf("feed = %+v", n.Feed)
		}
		if fi.RouteKey != "profile/U/" {
			t.Fatalf("focus = %+v", fi)
		}
	})
}

func TestProjectTable(t *testing.T) {
	type want map[layout.SlotID]string
	base := nav(nav(nav(newNavState(), ch("C3")), search("q")), post("P"))
	// base: tabs [home, C3+search], active 1, feed [following, P], Front=FrontFeed
	withThread := nav(base, thr("R", "C3")) // folds C3 stack, Front=FrontTab, attached
	detached := nav(withThread, search("q2"))
	detached.Front = FrontTab // top = search q2, thread open but not attached
	cases := []struct {
		name string
		n    NavState
		want map[layout.Mode]want
	}{
		{"front feed no thread", base, map[layout.Mode]want{
			layout.ModeCommand:  {layout.SlotMain: "search//q", layout.SlotFeed: "post/P/", layout.SlotNotifications: "notifications//"},
			layout.ModeStandard: {layout.SlotMain: "post/P/"},
			layout.ModeCompact:  {layout.SlotMain: "post/P/"},
			layout.ModeWidget:   {layout.SlotMain: "post/P/"},
			layout.ModeTiny:     {layout.SlotMain: "post/P/"},
			layout.ModeTooSmall: {},
		}},
		{"front tab attached thread", withThread, map[layout.Mode]want{
			layout.ModeCommand:  {layout.SlotMain: "channel/C3/", layout.SlotThread: "thread/R/C3", layout.SlotFeed: "post/P/", layout.SlotNotifications: "notifications//"},
			layout.ModeStandard: {layout.SlotMain: "channel/C3/", layout.SlotThread: "thread/R/C3"},
			layout.ModeCompact:  {layout.SlotMain: "thread/R/C3"},
			layout.ModeWidget:   {layout.SlotMain: "thread/R/C3"},
			layout.ModeTiny:     {layout.SlotMain: "thread/R/C3"},
			layout.ModeTooSmall: {},
		}},
		{"front tab detached thread", detached, map[layout.Mode]want{
			layout.ModeCommand:  {layout.SlotMain: "search//q2", layout.SlotThread: "thread/R/C3", layout.SlotFeed: "post/P/", layout.SlotNotifications: "notifications//"},
			layout.ModeStandard: {layout.SlotMain: "search//q2", layout.SlotThread: "thread/R/C3"},
			layout.ModeCompact:  {layout.SlotMain: "search//q2"},
			layout.ModeWidget:   {layout.SlotMain: "search//q2"},
			layout.ModeTiny:     {layout.SlotMain: "search//q2"},
			layout.ModeTooSmall: {},
		}},
		{"front notifications with thread", nav(withThread, NotifRoute), map[layout.Mode]want{
			layout.ModeCommand:  {layout.SlotMain: "channel/C3/", layout.SlotThread: "thread/R/C3", layout.SlotFeed: "post/P/", layout.SlotNotifications: "notifications//"},
			layout.ModeStandard: {layout.SlotMain: "notifications//"},
			layout.ModeCompact:  {layout.SlotMain: "notifications//"},
			layout.ModeWidget:   {layout.SlotMain: "notifications//"},
			layout.ModeTiny:     {layout.SlotMain: "notifications//"},
			layout.ModeTooSmall: {},
		}},
		{"front feed with thread", nav(withThread, post("P")), map[layout.Mode]want{
			layout.ModeCommand:  {layout.SlotMain: "channel/C3/", layout.SlotThread: "thread/R/C3", layout.SlotFeed: "post/P/", layout.SlotNotifications: "notifications//"},
			layout.ModeStandard: {layout.SlotMain: "post/P/"},
			layout.ModeCompact:  {layout.SlotMain: "post/P/"},
			layout.ModeWidget:   {layout.SlotMain: "post/P/"},
			layout.ModeTiny:     {layout.SlotMain: "post/P/"},
			layout.ModeTooSmall: {},
		}},
		{"fresh", newNavState(), map[layout.Mode]want{
			layout.ModeCommand:  {layout.SlotMain: "home//", layout.SlotFeed: "feed//following", layout.SlotNotifications: "notifications//"},
			layout.ModeStandard: {layout.SlotMain: "home//"},
			layout.ModeCompact:  {layout.SlotMain: "home//"},
			layout.ModeWidget:   {layout.SlotMain: "home//"},
			layout.ModeTiny:     {layout.SlotMain: "home//"},
			layout.ModeTooSmall: {},
		}},
	}
	for _, tc := range cases {
		for _, mode := range allModes {
			t.Run(tc.name+"/"+mode.ID(), func(t *testing.T) {
				got := Project(layoutFor(mode, tc.n), tc.n)
				if got == nil {
					t.Fatal("Project returned nil map")
				}
				gotKeys := want{}
				for s, r := range got {
					gotKeys[s] = r.Key()
				}
				if !reflect.DeepEqual(gotKeys, tc.want[mode]) {
					t.Fatalf("got %v want %v", gotKeys, tc.want[mode])
				}
			})
		}
	}
	t.Run("standard short thread pane falls back to main", func(t *testing.T) {
		// 120x16 standard: B=15, top=max(10,9)=10, bottom 5 < 6 => no thread pane
		l := layout.Compute(layout.ModeStandard, 120, 16, withThread.LayoutState(layout.ModeStandard))
		if _, ok := l.Pane(layout.SlotThread); ok {
			t.Fatal("unexpected thread pane")
		}
		got := Project(l, withThread)
		if got[layout.SlotMain].Key() != "thread/R/C3" || len(got) != 1 {
			t.Fatalf("got %v", got)
		}
		// not attached => top stays in main, thread not shown
		got = Project(layout.Compute(layout.ModeStandard, 120, 16, detached.LayoutState(layout.ModeStandard)), detached)
		if got[layout.SlotMain].Key() != "search//q2" || len(got) != 1 {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("project carries anchor", func(t *testing.T) {
		a := ch("C3")
		a.Anchor = "M1"
		n := nav(newNavState(), a)
		if got := Project(layoutFor(layout.ModeCompact, n), n); got[layout.SlotMain].Anchor != "M1" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestLayoutState(t *testing.T) {
	plain := nav(newNavState(), ch("C1"))
	open := nav(plain, thr("R", "C1"))
	for _, tc := range []struct {
		name  string
		n     NavState
		front Front
		mode  layout.Mode
		want  layout.State
	}{
		{"no thread command", plain, FrontTab, layout.ModeCommand, layout.State{}},
		{"thread command tab", open, FrontTab, layout.ModeCommand, layout.State{ThreadOpen: true}},
		{"thread command feed", open, FrontFeed, layout.ModeCommand, layout.State{ThreadOpen: true}},
		{"thread command notif", open, FrontNotifications, layout.ModeCommand, layout.State{ThreadOpen: true}},
		{"thread standard tab", open, FrontTab, layout.ModeStandard, layout.State{ThreadOpen: true}},
		{"thread standard feed", open, FrontFeed, layout.ModeStandard, layout.State{FeedSwapped: true}},
		{"thread standard notif", open, FrontNotifications, layout.ModeStandard, layout.State{}},
		{"no thread standard feed", plain, FrontFeed, layout.ModeStandard, layout.State{FeedSwapped: true}},
		{"thread compact tab", open, FrontTab, layout.ModeCompact, layout.State{ThreadOpen: true}},
		{"thread compact feed", open, FrontFeed, layout.ModeCompact, layout.State{}},
		{"thread widget feed", open, FrontFeed, layout.ModeWidget, layout.State{}},
		{"thread tiny tab", open, FrontTab, layout.ModeTiny, layout.State{ThreadOpen: true}},
	} {
		n := tc.n
		n.Front = tc.front
		if got := n.LayoutState(tc.mode); got != tc.want {
			t.Errorf("%s: got %+v want %+v", tc.name, got, tc.want)
		}
	}
	if got := (NavState{}).LayoutState(layout.ModeCommand); got != (layout.State{}) {
		t.Errorf("zero NavState: %+v", got)
	}
}

func TestBackRules(t *testing.T) {
	back := func(n NavState, focus layout.SlotID, mode layout.Mode) NavState {
		return n.back(focus, mode, layoutFor(mode, n))
	}
	open := nav(nav(newNavState(), ch("C1")), thr("R", "C1"))
	t.Run("thread slot closes thread", func(t *testing.T) {
		n := back(open, layout.SlotThread, layout.ModeCommand)
		if n.Tabs[n.Active].Thread != nil || !reflect.DeepEqual(activeStack(n), []string{"channel/C1/"}) {
			t.Fatalf("got %+v", n.Tabs[n.Active])
		}
		if open.Tabs[1].Thread == nil {
			t.Fatal("input mutated")
		}
	})
	t.Run("feed slot pops feed", func(t *testing.T) {
		n := nav(nav(open, post("P1")), post("P2"))
		n = back(n, layout.SlotFeed, layout.ModeCommand)
		if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//following", "post/P1/"}) {
			t.Fatalf("feed = %v", keysOf(n.Feed))
		}
		n = back(back(n, layout.SlotFeed, layout.ModeCommand), layout.SlotFeed, layout.ModeCommand)
		if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//following"}) {
			t.Fatalf("feed root popped: %v", keysOf(n.Feed))
		}
	})
	t.Run("notifications slot no-op", func(t *testing.T) {
		n := nav(open, post("P1"))
		if got := back(n, layout.SlotNotifications, layout.ModeCommand); !reflect.DeepEqual(got, n) {
			t.Fatalf("changed: %+v", got)
		}
	})
	t.Run("pane factory slots no-op", func(t *testing.T) {
		for _, s := range []layout.SlotID{layout.SlotRail, layout.SlotSidebar, layout.SlotMembers} {
			if got := back(open, s, layout.ModeCommand); !reflect.DeepEqual(got, open) {
				t.Fatalf("%s changed: %+v", s, got)
			}
		}
	})
	t.Run("main attached thread", func(t *testing.T) {
		n := back(open, layout.SlotMain, layout.ModeCompact)
		if n.Tabs[n.Active].Thread != nil || len(n.Tabs[n.Active].Stack) != 1 {
			t.Fatalf("got %+v", n.Tabs[n.Active])
		}
		// standard with a thread pane: main shows the channel (root) => no-op
		if got := back(open, layout.SlotMain, layout.ModeStandard); !reflect.DeepEqual(got, open) {
			t.Fatalf("standard changed: %+v", got)
		}
	})
	t.Run("main feed surface", func(t *testing.T) {
		n := nav(open, post("P1"))
		n = back(n, layout.SlotMain, layout.ModeWidget)
		if !reflect.DeepEqual(keysOf(n.Feed), []string{"feed//following"}) || n.Front != FrontFeed {
			t.Fatalf("got %+v", n)
		}
		n = back(n, layout.SlotMain, layout.ModeWidget)
		if n.Front != FrontTab || len(n.Feed) != 1 {
			t.Fatalf("got %+v", n)
		}
		// standard FeedSwapped likewise
		n = back(nav(open, feedR("discover")), layout.SlotMain, layout.ModeStandard)
		if n.Front != FrontTab {
			t.Fatalf("standard: %+v", n)
		}
	})
	t.Run("main notifications", func(t *testing.T) {
		n := back(nav(open, NotifRoute), layout.SlotMain, layout.ModeTiny)
		if n.Front != FrontTab || n.Tabs[n.Active].Thread == nil {
			t.Fatalf("got %+v", n)
		}
	})
	t.Run("main top pops but never root", func(t *testing.T) {
		n := nav(nav(newNavState(), search("q")), settingsR)
		n = back(n, layout.SlotMain, layout.ModeCommand)
		if !reflect.DeepEqual(activeStack(n), []string{"home//", "search//q"}) {
			t.Fatalf("stack = %v", activeStack(n))
		}
		n = back(back(n, layout.SlotMain, layout.ModeCommand), layout.SlotMain, layout.ModeCommand)
		if !reflect.DeepEqual(activeStack(n), []string{"home//"}) {
			t.Fatalf("root popped: %v", activeStack(n))
		}
		// command main ignores Front: pops tab stack even while FrontFeed
		n = nav(nav(newNavState(), search("q")), post("P"))
		n = back(n, layout.SlotMain, layout.ModeCommand)
		if !reflect.DeepEqual(activeStack(n), []string{"home//"}) || len(n.Feed) != 2 {
			t.Fatalf("got %+v", n)
		}
	})
	t.Run("too small no-op", func(t *testing.T) {
		n := nav(newNavState(), search("q"))
		if got := back(n, layout.SlotMain, layout.ModeTooSmall); !reflect.DeepEqual(got, n) {
			t.Fatalf("changed: %+v", got)
		}
	})
}

func TestToggleFeedAndNotifications(t *testing.T) {
	n := nav(newNavState(), ch("C1"))
	t.Run("command focuses panes", func(t *testing.T) {
		got, fi := n.toggleFeed(layout.ModeCommand)
		if !reflect.DeepEqual(got, n) || fi == nil || *fi != (FocusIntent{Slot: layout.SlotFeed, RouteKey: "feed//following"}) {
			t.Fatalf("feed: %+v %v", got, fi)
		}
		got, fi = n.toggleNotifications(layout.ModeCommand)
		if !reflect.DeepEqual(got, n) || fi == nil || *fi != (FocusIntent{Slot: layout.SlotNotifications, RouteKey: "notifications//"}) {
			t.Fatalf("notifications: %+v %v", got, fi)
		}
	})
	for _, mode := range []layout.Mode{layout.ModeStandard, layout.ModeCompact, layout.ModeWidget, layout.ModeTiny} {
		t.Run(mode.ID(), func(t *testing.T) {
			f, fi := n.toggleFeed(mode)
			if f.Front != FrontFeed || fi == nil || *fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "feed//following"}) {
				t.Fatalf("feed on: %+v %v", f, fi)
			}
			b, fi := f.toggleFeed(mode)
			if b.Front != FrontTab || fi == nil || *fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "channel/C1/"}) {
				t.Fatalf("feed off: %+v %v", b, fi)
			}
			nn, fi := n.toggleNotifications(mode)
			if nn.Front != FrontNotifications || fi == nil || *fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "notifications//"}) {
				t.Fatalf("notif on: %+v %v", nn, fi)
			}
			if b, _ := nn.toggleNotifications(mode); b.Front != FrontTab {
				t.Fatalf("notif off: %+v", b)
			}
			if x, _ := nn.toggleFeed(mode); x.Front != FrontFeed {
				t.Fatalf("notif -> feed: %+v", x)
			}
			if x, _ := f.toggleNotifications(mode); x.Front != FrontNotifications {
				t.Fatalf("feed -> notif: %+v", x)
			}
		})
	}
	t.Run("single pane back to attached thread", func(t *testing.T) {
		o := nav(nav(n, thr("R", "C1")), feedR("following"))
		_, fi := o.toggleFeed(layout.ModeCompact)
		if fi == nil || fi.RouteKey != "thread/R/C1" {
			t.Fatalf("focus = %v", fi)
		}
		_, fi = o.toggleFeed(layout.ModeStandard)
		if fi == nil || fi.RouteKey != "channel/C1/" {
			t.Fatalf("standard focus = %v", fi)
		}
	})
}

func TestCycleTab(t *testing.T) {
	n := nav(nav(newNavState(), ch("C1")), ch("C2"))
	n = nav(n, homeR) // tabs [home, C1, C2], active 0
	pos := func(n NavState) string {
		switch n.Front {
		case FrontFeed:
			return "피드"
		case FrontNotifications:
			return "알림"
		}
		return fmt.Sprintf("tab%d", n.Active)
	}
	t.Run("compact", func(t *testing.T) {
		var got []string
		c := n
		for range 6 {
			c = c.cycleTab(1, layout.ModeCompact)
			got = append(got, pos(c))
		}
		want := []string{"tab1", "tab2", "피드", "알림", "tab0", "tab1"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("forward %v", got)
		}
		got = nil
		c = n
		for range 3 {
			c = c.cycleTab(-1, layout.ModeCompact)
			got = append(got, pos(c))
		}
		if want := []string{"알림", "피드", "tab2"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("backward %v", got)
		}
		// selecting a tab marks it seen (LRU)
		c = n.cycleTab(1, layout.ModeCompact)
		if c.Tabs[1].seen <= c.Tabs[0].seen || c.Tabs[1].seen <= c.Tabs[2].seen {
			t.Fatalf("seen not bumped: %d %d %d", c.Tabs[0].seen, c.Tabs[1].seen, c.Tabs[2].seen)
		}
	})
	for _, mode := range []layout.Mode{layout.ModeWidget, layout.ModeTiny} {
		t.Run(mode.ID(), func(t *testing.T) {
			c := nav(n, ch("C2")) // active tab 2
			var got []string
			for range 4 {
				c = c.cycleTab(1, mode)
				got = append(got, pos(c))
			}
			if want := []string{"피드", "알림", "tab2", "피드"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v", got)
			}
			if c := n.cycleTab(-1, mode); pos(c) != "알림" {
				t.Fatalf("backward %s", pos(c))
			}
		})
	}
	t.Run("command cycles tabs only", func(t *testing.T) {
		c := n
		c.Front = FrontFeed
		before := c.loc()
		// Main now shows that tab: Front follows, so the switch is a history place
		// and a later mode change shows it (CA 5.3.3 "기록")
		c = c.cycleTab(1, layout.ModeCommand)
		if c.Active != 1 || c.Front != FrontTab {
			t.Fatalf("got active %d front %d", c.Active, c.Front)
		}
		if c.loc().key() == before.key() {
			t.Fatal("a command-mode tab switch must change the place")
		}
		if c = c.cycleTab(-2, layout.ModeCommand); c.Active != 2 {
			t.Fatalf("got active %d", c.Active)
		}
	})
	t.Run("standard like compact", func(t *testing.T) {
		c := n.cycleTab(-1, layout.ModeStandard)
		if pos(c) != "알림" {
			t.Fatalf("got %s", pos(c))
		}
	})
	t.Run("pure", func(t *testing.T) {
		before := n
		_ = n.cycleTab(1, layout.ModeCompact)
		if !reflect.DeepEqual(before, n) || n.Active != 0 {
			t.Fatal("input mutated")
		}
	})
}

func TestKeys(t *testing.T) {
	n := nav(nav(nav(newNavState(), ch("C1")), thr("R", "C1")), post("P"))
	n = nav(n, search("q"))
	got := n.Keys()
	want := []string{"home//", "channel/C1/", "search//q", "thread/R/C1", "feed//following", "post/P/", "notifications//"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestInvariantRandomWalk(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	ids := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L"}
	kinds := []ui.RouteKind{
		ui.RouteHome, ui.RouteChannel, ui.RouteThread, ui.RouteSearch, ui.RouteInvite, ui.RouteSpaceSettings,
		ui.RouteFriends, ui.RouteFeed, ui.RoutePost, ui.RouteProfile, ui.RouteHashtag, ui.RouteTrends,
		ui.RouteNotifications, ui.RouteStories, ui.RouteCompose, ui.RouteSettings, ui.RouteHelp, ui.RouteLogin,
	}
	slots := []layout.SlotID{
		layout.SlotMain, layout.SlotThread, layout.SlotFeed, layout.SlotNotifications,
		layout.SlotSidebar, layout.SlotRail, layout.SlotMembers,
	}
	n := newNavState()
	for step := range 1000 {
		mode := allModes[rng.IntN(len(allModes))]
		var desc string
		switch op := rng.IntN(10); {
		case op < 6:
			k := kinds[rng.IntN(len(kinds))]
			if k == ui.RouteLogin && rng.IntN(4) != 0 {
				k = ui.RoutePost // keep logins rare so stacks grow
			}
			r := ui.Route{Kind: k, ID: ids[rng.IntN(len(ids))]}
			switch k {
			case ui.RouteThread:
				r.Arg = ids[rng.IntN(len(ids))]
			case ui.RouteFeed:
				r.ID, r.Arg = "", []string{"following", "discover"}[rng.IntN(2)]
			case ui.RouteHome, ui.RouteNotifications:
				r.ID = ""
			}
			if rng.IntN(3) == 0 {
				r.Anchor = ids[rng.IntN(len(ids))]
			}
			m := ui.NavigateMsg{Route: r, Target: ui.Target(rng.IntN(3)), Replace: rng.IntN(4) == 0}
			desc = fmt.Sprintf("navigate %+v", m)
			n, _ = n.navigate(m, mode)
		case op < 8:
			s := slots[rng.IntN(len(slots))]
			desc = fmt.Sprintf("back %s", s)
			n = n.back(s, mode, layoutFor(mode, n))
		case op < 9:
			if rng.IntN(2) == 0 {
				desc = "toggleFeed"
				n, _ = n.toggleFeed(mode)
			} else {
				desc = "toggleNotifications"
				n, _ = n.toggleNotifications(mode)
			}
		default:
			d := rng.IntN(5) - 2
			desc = fmt.Sprintf("cycleTab %d", d)
			n = n.cycleTab(d, mode)
		}
		where := fmt.Sprintf("step %d (%s in %s)", step, desc, mode.ID())
		checkInvariants(t, where, n)
		if t.Failed() {
			t.Fatalf("state: %+v", n)
		}
	}
}

func checkInvariants(t *testing.T, where string, n NavState) {
	t.Helper()
	if len(n.Tabs) == 0 || len(n.Tabs) > 9 || n.Active < 0 || n.Active >= len(n.Tabs) {
		t.Errorf("%s: tabs %d active %d", where, len(n.Tabs), n.Active)
		return
	}
	if len(n.Feed) == 0 || n.Feed[0].Kind != ui.RouteFeed {
		t.Errorf("%s: feed %v", where, n.Feed)
		return
	}
	lifted := map[ui.RouteKind]bool{ui.RouteThread: true, ui.RouteFeed: true, ui.RouteNotifications: true, ui.RouteLogin: true}
	roots := map[ui.RouteKind]bool{ui.RouteHome: true, ui.RouteChannel: true}
	for i, tb := range n.Tabs {
		if len(tb.Stack) == 0 || !roots[tb.Stack[0].Kind] {
			t.Errorf("%s: tab %d root %v", where, i, tb.Stack)
			return
		}
		for _, r := range tb.Stack[1:] {
			if lifted[r.Kind] || roots[r.Kind] {
				t.Errorf("%s: tab %d holds %s", where, i, r.Key())
			}
		}
		if tb.Thread != nil && (tb.Thread.Kind != ui.RouteThread || tb.Thread.Arg != tb.Stack[0].ID || tb.Stack[0].Kind != ui.RouteChannel) {
			t.Errorf("%s: tab %d thread %v root %v", where, i, tb.Thread, tb.Stack[0])
		}
	}
	for _, r := range n.Feed[1:] {
		if lifted[r.Kind] || roots[r.Kind] {
			t.Errorf("%s: feed holds %s", where, r.Key())
		}
	}
	seen := map[string]bool{}
	for _, k := range n.Keys() {
		if seen[k] {
			t.Errorf("%s: duplicate key %s in Keys()", where, k)
		}
		seen[k] = true
	}
	for _, mode := range allModes {
		l := layoutFor(mode, n)
		pr := Project(l, n)
		got := map[string]layout.SlotID{}
		for s, r := range pr {
			if _, ok := l.Pane(s); !ok {
				t.Errorf("%s/%s: projected slot %s missing from layout", where, mode.ID(), s)
			}
			if prev, dup := got[r.Key()]; dup {
				t.Errorf("%s/%s: key %s in %s and %s", where, mode.ID(), r.Key(), prev, s)
			}
			got[r.Key()] = s
			if !seen[r.Key()] {
				t.Errorf("%s/%s: projected key %s not in Keys()", where, mode.ID(), r.Key())
			}
		}
		if mode != layout.ModeTooSmall {
			if _, ok := pr[layout.SlotMain]; !ok {
				t.Errorf("%s/%s: no main route", where, mode.ID())
			}
		}
	}
}
