package share_test

import (
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Review 8: hs_commit sent while the owner is away is sent again.
func TestCommitRetriedWhileOwnerAway(t *testing.T) {
	gd := newGateDial()
	r := newRigWith(t, func(c *share.HostConfig) { gd.inner, c.Dial = c.Dial, gd.dial })
	gd.set(dialFail)
	r.relay.Drop(wire.SeatOwner)
	r.settle()
	r.addGuest(1)
	r.settle() // hs_commit goes nowhere
	gd.set(dialPass)
	r.advance(time.Second)
	if !r.relay.Connected(wire.SeatOwner) {
		t.Fatal("owner did not come back")
	}
	for i := 0; i < 5 && len(r.host.Pending()) == 0; i++ {
		r.advance(time.Second)
	}
	if p := r.host.Pending(); len(p) != 1 || r.guests[1].SAS() == "" || p[0].SAS != r.guests[1].SAS() {
		t.Fatalf("Pending = %#v, guest SAS %q", p, r.guests[1].SAS())
	}
	r.join(1, wire.LevelViewer, false)
}
