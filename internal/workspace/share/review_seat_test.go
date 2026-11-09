package share_test

import (
	"context"
	"testing"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Review 10: a seat freed by a REST removal and given to a new invitee
// shows and records the new user.
func TestReusedSeatShowsNewUser(t *testing.T) {
	r := newRig(t, 3)
	r.join(3, wire.LevelViewer, false)
	r.relay.Remove(userOf(3))
	r.settle()
	r.relay.Invite(3, "U_9")
	g := share.NewGuest(share.GuestConfig{ShareID: rigShare, OwnerID: rigOwner, UserID: "U_9",
		Dial: r.relay.Dial("U_9"), Clock: fixedClock{&r.now}, ManualTick: true})
	must(t, g.Start(context.Background()))
	r.guests[3] = g
	r.gEv[3] = nil
	r.settle()
	if p := r.host.Pending(); len(p) != 1 || p[0].UserID != "U_9" {
		t.Fatalf("Pending = %#v", p)
	}
	r.join(3, wire.LevelViewer, false)
	ms := r.host.Members()
	if len(ms) != 1 || ms[0].UserID != "U_9" {
		t.Fatalf("Members = %#v", ms)
	}
	if !r.auditHas(audit.EvJoinAllow, func(e audit.Entry) bool { return e.User == "U_9" }) {
		t.Fatal("join_allow not recorded for the new user")
	}
	ev, _ := findEvent[share.EventRoster](r.takeGuest(3))
	for _, m := range ev.Members {
		if m.Seat == 3 && m.UserID != "U_9" {
			t.Fatalf("roster shows %s on seat 3", m.UserID)
		}
	}
}
