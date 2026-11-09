package workspace

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// A participant's replay screen never takes an owner size past the limit
// (a tampered owner client could otherwise make it allocate gigabytes).
func TestReplaySizeCapped(t *testing.T) {
	rt := newReplayTerm(1)
	rt.setStatus(wire.TermStatus{Term: 1, Cols: 100000, Rows: 100000})
	if sz := rt.size(); sz.W > share.MaxTermCols || sz.H > share.MaxTermRows {
		t.Fatalf("replay size %dx%d past the cap", sz.W, sz.H)
	}
	rt.keyframe([]byte("hi"))
	rt.setStatus(wire.TermStatus{Term: 1, Cols: 200000, Rows: 3})
	if sz := rt.size(); sz.W > share.MaxTermCols {
		t.Fatalf("resize to %dx%d past the cap", sz.W, sz.H)
	}
}

// With a shell overlay (the palette, help) over the sharing workspace,
// ctrl+\ x still cuts the share at once and the x never reaches the
// overlay; once it closes, the surface's own ctrl+\ keys work again.
func TestKillWithOverlayOverSharingWorkspace(t *testing.T) {
	for _, open := range []string{"ctrl+k", "?"} {
		t.Run(open, func(t *testing.T) {
			fake := setup(t)
			h := uitest.New(t)
			r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})
			r.shareFriends("minji")
			r.join(1, "편집", true)
			if open == "?" {
				h.Press("ctrl+\\", "1") // the tree: ? is not text there
			}
			h.Screen()
			h.Press(open)
			h.Screen() // a frame with the overlay drawn
			if n := len(h.Model.Frame().Overlays); n == 0 {
				t.Fatalf("%s opened no overlay:\n%s", open, h.Screen())
			}
			host := r.s.own.host
			h.Press("ctrl+\\", "x")
			h.Screen()
			if !host.Killed() {
				t.Fatalf("ctrl+\\ x under the %s overlay did not kill the share:\n%s", open, h.Screen())
			}
			if open == "ctrl+k" && strings.Contains(h.Screen(), "› x") {
				t.Fatalf("x was typed into the palette:\n%s", h.Screen())
			}
		})
	}
}

// After an overlay closes, the surface reads ctrl+\ again (ctrl+\ 4).
func TestOverlayClosedSurfacePrefixBack(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})
	r.shareFriends("minji")
	r.join(1, "편집", true)
	h.Screen()
	h.Press("ctrl+k")
	h.Screen()
	h.Press("esc")
	h.Screen()
	h.Press("ctrl+\\", "4")
	if r.s.focus != PanePeople {
		t.Fatalf("ctrl+\\ 4 after the palette closed did not reach the surface (focus %d)", r.s.focus)
	}
	if r.s.own.host.Killed() {
		t.Fatal("share killed")
	}
}

// The participant's "share ended" notice does not swallow ctrl+\: with a
// share of mine live elsewhere, ctrl+\ x there cuts it.
func TestKillFromGuestEndedNotice(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelViewer)
	mine := r.host // stands in for a share of mine in another instance
	guard.add(mine)
	t.Cleanup(func() { guard.drop(mine) })
	r.s.guest.over(func(ctx *ui.Context) string { return ctx.T.Workspace.Ended() })
	r.s.dropRemote()
	h.Screen()
	h.Press("ctrl+\\", "x")
	if !mine.Killed() {
		t.Fatal("ctrl+\\ x on the ended notice did not cut my live share")
	}
}

// Any key on the ended notice goes to the tool hub (Task 23).
func TestGuestEndedKeyGoesToHub(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelViewer)
	r.s.guest.over(func(ctx *ui.Context) string { return ctx.T.Workspace.Ended() })
	r.s.dropRemote()
	h.Screen()
	h.Press("a")
	if h.Instance(joinRoute(rigShare).Key()) != nil {
		t.Fatal("the notice stayed open")
	}
	if got, want := h.Model.Focus().RouteKey, (ui.Route{Kind: ui.RouteTools}).Key(); got != want {
		t.Fatalf("after the notice: focus %q, want the hub %q", got, want)
	}
}
