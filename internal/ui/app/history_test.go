package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
)

func tabLoc(root, r ui.Route) histLoc { return histLoc{Front: FrontTab, Root: root, Route: r} }

func always(histLoc) bool { return true }

func TestHistoryNoteAndCap(t *testing.T) {
	var h history
	a, b := tabLoc(homeR, homeR), tabLoc(ch("C1"), ch("C1"))
	h.note(a)
	h.note(a) // the same place again is no entry
	h.note(b)
	if len(h.locs) != 2 || h.cur != 1 {
		t.Fatalf("locs %d cur %d", len(h.locs), h.cur)
	}
	if i := h.target(-1, b, always); i != 0 {
		t.Fatalf("back target %d", i)
	}
	if i := h.target(+1, b, always); i != -1 {
		t.Fatalf("forward target %d", i)
	}
	// going back then somewhere new drops the forward places
	h.cur = 0
	c := tabLoc(ch("C2"), ch("C2"))
	h.note(c)
	if len(h.locs) != 2 || h.locs[1].key() != c.key() || h.cur != 1 {
		t.Fatalf("after a new place: %+v cur %d", h.locs, h.cur)
	}
	// the cap drops the oldest place first
	h.reset()
	for i := range maxHistory + 10 {
		h.note(tabLoc(ch(fmt.Sprint("C", i)), ch(fmt.Sprint("C", i))))
	}
	if len(h.locs) != maxHistory || h.cur != maxHistory-1 || h.locs[0].Root.ID != "C10" {
		t.Fatalf("cap: %d places, cur %d, oldest %s", len(h.locs), h.cur, h.locs[0].Root.ID)
	}
	// target skips places equal to now and places that are gone
	h.reset()
	for _, l := range []histLoc{a, b, c, a} {
		h.note(l)
	}
	gone := func(l histLoc) bool { return l.Root.ID != "C2" }
	if i := h.target(-1, a, gone); i != 1 {
		t.Fatalf("skip target %d, want 1 (#C1)", i)
	}
	if i := h.target(-1, b, func(histLoc) bool { return false }); i != -1 {
		t.Fatalf("all gone: %d", i)
	}
}

func TestHistoryLoc(t *testing.T) {
	n := nav(nav(newNavState(), ch("C3")), search("q"))
	if l := n.loc(); l.key() != tabLoc(ch("C3"), search("q")).key() {
		t.Fatalf("stack top: %+v", l)
	}
	a := thr("R", "C3")
	a.Anchor = "M1"
	n = nav(n, a)
	if l := n.loc(); l.Route.Key() != "thread/R/C3" || l.Route.Anchor != "" || l.Root.Key() != "channel/C3/" {
		t.Fatalf("attached thread: %+v", l)
	}
	n = nav(n, feedR("discover"))
	if l := n.loc(); l.Front != FrontFeed || l.Route.Key() != "feed//discover" {
		t.Fatalf("feed: %+v", l)
	}
	n, _ = n.toggleNotifications(layout.ModeWidget)
	if l := n.loc(); l.Front != FrontNotifications || l.Route.Key() != NotifRoute.Key() {
		t.Fatalf("notifications: %+v", l)
	}
}

func TestHistoryRestore(t *testing.T) {
	base := nav(nav(nav(newNavState(), ch("C3")), search("q")), ch("C7")) // C3:[C3, search], C7 active
	t.Run("pushed route on its tab", func(t *testing.T) {
		n, fi := base.restore(tabLoc(ch("C3"), search("q")), layout.ModeWidget)
		if n.Tabs[n.Active].Stack[0].ID != "C3" || n.loc().Route.Key() != "search//q" || n.Front != FrontTab {
			t.Fatalf("got %+v", n.Tabs[n.Active])
		}
		if fi != (FocusIntent{Slot: layout.SlotMain, RouteKey: "search//q"}) {
			t.Fatalf("focus %+v", fi)
		}
	})
	t.Run("root folds the stack and closes the thread", func(t *testing.T) {
		n := nav(base, thr("R", "C3"))
		n = nav(n, ch("C7"))
		n, _ = n.restore(tabLoc(ch("C3"), ch("C3")), layout.ModeWidget)
		tab := n.Tabs[n.Active]
		if tab.Stack[0].ID != "C3" || len(tab.Stack) != 1 || tab.Thread != nil {
			t.Fatalf("got %+v", tab)
		}
	})
	t.Run("thread", func(t *testing.T) {
		n, fi := base.restore(tabLoc(ch("C3"), thr("R", "C3")), layout.ModeTiny)
		if n.loc().Route.Key() != "thread/R/C3" {
			t.Fatalf("got %+v", n.loc())
		}
		if fi.Slot != layout.SlotMain {
			t.Fatalf("tiny focus %+v", fi)
		}
		if _, fi = base.restore(tabLoc(ch("C3"), thr("R", "C3")), layout.ModeStandard); fi.Slot != layout.SlotThread {
			t.Fatalf("standard focus %+v", fi)
		}
	})
	t.Run("evicted tab comes back", func(t *testing.T) {
		n, _ := base.restore(tabLoc(ch("C9"), ch("C9")), layout.ModeWidget)
		if n.Tabs[n.Active].Stack[0].ID != "C9" || len(n.Tabs) != 4 {
			t.Fatalf("got %+v", n.Tabs)
		}
	})
	t.Run("feed and notifications", func(t *testing.T) {
		n := nav(nav(base, feedR("discover")), post("P1"))
		n, fi := n.restore(histLoc{Front: FrontFeed, Route: feedR("discover")}, layout.ModeWidget)
		if n.Front != FrontFeed || len(n.Feed) != 1 || n.Feed[0].Arg != "discover" || fi.RouteKey != "feed//discover" {
			t.Fatalf("feed root: %+v", n.Feed)
		}
		n, _ = n.restore(histLoc{Front: FrontFeed, Route: feedR("following")}, layout.ModeWidget)
		if len(n.Feed) != 1 || n.Feed[0].Arg != "following" {
			t.Fatalf("other feed root: %+v", n.Feed)
		}
		n, _ = n.restore(histLoc{Front: FrontFeed, Route: post("P1")}, layout.ModeWidget)
		if n.loc().Route.Key() != "post/P1/" {
			t.Fatalf("post: %+v", n.Feed)
		}
		n, fi = n.restore(histLoc{Front: FrontNotifications, Route: NotifRoute}, layout.ModeCommand)
		if n.Front != FrontNotifications || fi.Slot != layout.SlotNotifications {
			t.Fatalf("notifications: front %v focus %+v", n.Front, fi)
		}
	})
}

func TestPlaceExists(t *testing.T) {
	st := store.New()
	me := api.UserSummary{ID: "U0"}
	st.Me = &api.Me{UserSummary: me}
	st.Channels["C1"] = &api.Channel{ID: "C1", Kind: api.ChannelText}
	st.Channels["D1"] = &api.Channel{ID: "D1", Kind: api.ChannelDM, Members: []api.UserSummary{me, {ID: "U1"}}}
	cases := []struct {
		l    histLoc
		want bool
	}{
		{tabLoc(homeR, homeR), true},
		{tabLoc(ch("C1"), ch("C1")), true},
		{tabLoc(ch("C1"), thr("R", "C1")), true},
		{tabLoc(ch("D1"), ch("D1")), true},
		{tabLoc(ch("C9"), ch("C9")), true}, // not bootstrapped yet: unknown is not gone
		{histLoc{Front: FrontFeed, Route: post("P")}, true},
	}
	for _, c := range cases {
		if got := placeExists(st, c.l); got != c.want {
			t.Errorf("%s: %v, want %v", c.l.key(), got, c.want)
		}
	}
	st.Sync.Bootstrapped = true
	st.Blocks = map[string]time.Time{"U1": {}}
	for _, l := range []histLoc{tabLoc(ch("C9"), ch("C9")), tabLoc(ch("C9"), thr("R", "C9")), tabLoc(ch("D1"), ch("D1"))} {
		if placeExists(st, l) {
			t.Errorf("%s exists, want gone", l.key())
		}
	}
	if !placeExists(st, tabLoc(ch("C1"), ch("C1"))) {
		t.Error("#C1 must still exist")
	}
}

// The ? help lists the history keys (전역) and thread.channel (메시지) as bound.
func TestHelpRowsHistory(t *testing.T) {
	km, _ := keymap.Load(map[string][]string{"nav.history_forward": {"L"}})
	want := map[string]string{"이전 위치": "ctrl+o/alt+←", "다음 위치": "L", "스레드에서 채널로": "c"}
	for _, r := range helpRows(km) {
		if k, ok := want[r.text]; ok && r.kind == helpKey {
			if r.key != k {
				t.Errorf("%s key %q, want %q", r.text, r.key, k)
			}
			delete(want, r.text)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing help rows: %v", want)
	}
}
