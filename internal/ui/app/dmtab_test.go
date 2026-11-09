package app_test

import (
	"testing"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

var minjiDM = ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}

// bootstrapped is fixtures.Seed after a bootstrap (gone channels are really gone).
func bootstrapped(s *store.Store) { fixtures.Seed(s); s.Sync.Bootstrapped = true }

// A DM that leaves the Store during a live session (deleted, left) or whose peer I
// block folds the DM surface back to its list at once (CA 5.3.3), not only after the
// next bootstrap.
func TestGoneDMFoldsDMSurface(t *testing.T) {
	for _, tc := range []struct {
		name string
		gone func(h *uitest.Harness)
	}{
		{"deleted", func(h *uitest.Harness) {
			h.Event("channel.deleted", map[string]any{"channel_id": fixtures.DMMinji, "reason": "removed"})
		}},
		{"blocked", func(h *uitest.Harness) {
			h.Event("block.updated", map[string]any{"user_id": fixtures.UserMinji, "blocked": true})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := uitest.New(t, uitest.WithRoute(devRoute), uitest.WithSeed(bootstrapped))
			h.Resize(80, 30)
			h.Send(ui.NavigateMsg{Route: minjiDM})
			if got := h.Model.Frame().Routes[layout.SlotMain].Key(); got != minjiDM.Key() {
				t.Fatalf("DM open: Main = %q", got)
			}
			h.Press("[") // the 채팅 tab
			tc.gone(h)
			if n := h.Nav(); len(n.DM.Stack) != 1 || n.DM.Thread != nil {
				t.Fatalf("DM surface not folded: %+v", n.DM)
			}
			h.Press("]") // the DM tab: the list
			if got := h.Model.Frame().Routes[layout.SlotMain].Key(); got != "dms//" {
				t.Fatalf("DM tab: Main = %q, want the DM list", got)
			}
			h.Check()
		})
	}
	// shown when it goes: Main shows the list right away
	h := uitest.New(t, uitest.WithRoute(devRoute), uitest.WithSeed(bootstrapped))
	h.Resize(80, 30)
	h.Send(ui.NavigateMsg{Route: minjiDM})
	h.Event("channel.deleted", map[string]any{"channel_id": fixtures.DMMinji, "reason": "removed"})
	if got := h.Model.Frame().Routes[layout.SlotMain].Key(); got != "dms//" {
		t.Fatalf("shown DM gone: Main = %q, want the DM list", got)
	}
	h.Check()
}

// CA 10.3 bullet 2 through the DM tab: ] ] walks 채팅 → DM → 피드, so the chat surface
// left last is the DM surface (InDM = the last chat tab visited). esc / f from the
// feed root and the command Main show it, and the 채팅 tab keeps its place.
func TestWidgetFeedThroughDMTab(t *testing.T) {
	for _, back := range []string{"esc", "f"} {
		h := uitest.New(t, uitest.WithRoute(devRoute))
		h.Resize(80, 24)
		h.Press("]", "]")
		if n := h.Nav(); n.Front != app.FrontFeed || !n.InDM {
			t.Fatalf("] ] at widget: front %v in DM %v", n.Front, n.InDM)
		}
		h.Press(back)
		if got := h.Model.Frame().Routes[layout.SlotMain].Key(); got != "dms//" {
			t.Fatalf("%s from the feed root: Main = %q, want the DM list", back, got)
		}
		h.Press("[") // the 채팅 tab kept #개발
		if got := h.Model.Frame().Routes[layout.SlotMain].Key(); got != devRoute.Key() {
			t.Fatalf("채팅 tab: Main = %q", got)
		}
	}
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(80, 24)
	h.Press("]", "]")
	h.Resize(200, 60)
	h.Check()
	routes := h.Model.Frame().Routes
	if routes[layout.SlotFeed].Key() != "feed//following" || routes[layout.SlotMain].Key() != "dms//" {
		t.Fatalf("command routes feed %q main %q", routes[layout.SlotFeed].Key(), routes[layout.SlotMain].Key())
	}
	h.Resize(80, 24)
	if n := h.Nav(); n.Front != app.FrontFeed || !n.InDM {
		t.Fatalf("back at widget: front %v in DM %v", n.Front, n.InDM)
	}
}
