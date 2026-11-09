package workspace

import (
	"strconv"
	"testing"
	"time"

	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// guestScene is the B6.7 picture from the participant's side: sam edits
// src/server/hub.ts with minji's cursor in it, minji's claude below.
func guestScene(t *testing.T, h *uitest.Harness, snap func(name string)) *guestRig {
	t.Helper()
	t.Cleanup(SetSASForTest("482 913"))
	r := newGuestRig(t, h, nil)
	snap("guest_sas")
	r.admit(wire.LevelEditor)
	r.term.output([]byte("\x1b[2J\x1b[1;1H> 테스트를 돌려 줘\r\n● Running npm test …\r\n"))
	h.Advance(share.KeyframeEvery)
	r.settle()
	r.s.guestOpen(h.Context(), hubTS)
	r.settle()
	r.host.Presence(hubTS, 40, 40, 40)
	r.settle()
	return r
}

func TestGoldenGuest(t *testing.T) {
	setup(t)
	sizes := append(append([]uitest.Size(nil), uitest.Required...), uitest.Tiny)
	uitest.ForEachSize(t, sizes, func(t *testing.T, h *uitest.Harness) {
		r := guestScene(t, h, func(name string) { h.Golden(name) })
		h.Golden("guest_editing")
		r.host.Kill()
		waitEnded(t, r)
		h.Golden("guest_ended")
	})
	t.Run("terminal_pan", func(t *testing.T) {
		h := uitest.New(t)
		h.Resize(80, 24)
		r := guestScene(t, h, func(string) {})
		r.s.setFocus(h.Context(), PaneTerm)
		r.term.output([]byte("\x1b[5;100Hfar right"))
		r.settle()
		h.Golden("guest_terminal_pan")
	})
	for _, sz := range []uitest.Size{{W: 50, H: 20}, uitest.Tiny} {
		t.Run("tiny_"+strconv.Itoa(sz.W)+"x"+strconv.Itoa(sz.H), func(t *testing.T) {
			h := uitest.New(t)
			h.Resize(sz.W, sz.H)
			guestScene(t, h, func(string) {})
			h.Golden("guest_tiny")
		})
	}
}

// waitEnded waits for the kill switch to reach the server (it runs in the
// background) and pumps the end to the guest screen.
func waitEnded(t *testing.T, r *guestRig) {
	t.Helper()
	start := time.Now()
	for !r.relay.Ended() {
		if time.Since(start) > time.Second {
			t.Fatal("server not told within 1s")
		}
		time.Sleep(time.Millisecond)
	}
	r.settle()
}
