package share_test

import (
	"context"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Review 2/7: the guest's hs_resume is lost while the owner is away; when
// the owner comes back the guest asks again and can act.
func TestGuestResumesWhenOwnerReturnsLater(t *testing.T) {
	gd := newGateDial()
	r := newRigWith(t, func(c *share.HostConfig) { gd.inner, c.Dial = c.Dial, gd.dial }, 1)
	r.join(1, wire.LevelPrompter, false)
	gd.set(dialFail)
	r.relay.Drop(wire.SeatOwner)
	r.relay.Drop(1)
	r.settle()
	r.advance(time.Second) // the guest is back first; its hs_resume goes nowhere
	if r.relay.Connected(wire.SeatOwner) || !r.relay.Connected(1) {
		t.Fatal("setup: expected only the guest back")
	}
	gd.set(dialPass)
	r.advance(2 * time.Second) // the owner is back
	r.advance(time.Second)
	if !r.relay.Connected(wire.SeatOwner) {
		t.Fatal("owner did not come back")
	}
	if _, err := r.guests[1].Propose("after"); err != nil {
		t.Fatalf("Propose after the owner came back: %v", err)
	}
	if !hasEvent[share.EventResumed](r.takeGuest(1)) {
		t.Fatal("no EventResumed")
	}
}

// The guest's hs_resume is also resent on a timer (a missed notice).
func TestGuestResendsResumeOnTimer(t *testing.T) {
	r := newRig(t, 1)
	r.join(1, wire.LevelPrompter, false)
	r.relay.Drop(1)
	r.settle()
	// Drop the first hs_resume on the way.
	dropped := 0
	r.relay.Tamper(func(from, to uint8, f []byte) []byte {
		if h, _, err := wire.Parse(f); err == nil && from == 1 && h.Kind == wire.KindHsResume && dropped == 0 {
			dropped++
			return nil
		}
		return f
	})
	r.advance(time.Second)
	if dropped != 1 {
		t.Fatal("setup: hs_resume not dropped")
	}
	r.advance(10 * time.Second)
	if _, err := r.guests[1].Propose("later"); err != nil {
		t.Fatalf("Propose after a lost hs_resume: %v", err)
	}
}

// An owner that cannot open the hs_resume (a new Host on the same share)
// answers hs_decline{resume} and the guest starts a new key exchange.
func TestUnknownResumeStartsOver(t *testing.T) {
	r := newRig(t, 1)
	r.join(1, wire.LevelViewer, false)
	h2 := share.NewHost(r.cfg)
	if err := h2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.host = h2
	r.settle()
	r.takeGuest(1)
	r.relay.Drop(1)
	r.settle()
	r.advance(time.Second)
	ev := r.takeGuest(1)
	if hasEvent[share.EventEnded](ev) || hasEvent[share.EventDeclined](ev) || !hasEvent[share.EventSASReady](ev) {
		t.Fatalf("guest events = %#v", ev)
	}
	if p := h2.Pending(); len(p) != 1 || p[0].Seat != 1 {
		t.Fatalf("Pending = %#v", p)
	}
}
