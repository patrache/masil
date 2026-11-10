package share_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// countDial counts the dials that go through d.
func countDial(d share.DialFunc, n *atomic.Int32) share.DialFunc {
	return func(ctx context.Context, id string) (share.Conn, error) {
		n.Add(1)
		return d(ctx, id)
	}
}

// guestCounted adds the guest of seat with a dial counter.
func guestCounted(r *rig, seat uint8, n *atomic.Int32) {
	r.t.Helper()
	g := share.NewGuest(share.GuestConfig{ShareID: rigShare, OwnerID: rigOwner, UserID: userOf(seat),
		Dial: countDial(r.relay.Dial(userOf(seat)), n), Clock: fixedClock{&r.now}, ManualTick: true})
	if err := g.Start(context.Background()); err != nil {
		r.t.Fatal(err)
	}
	r.guests[seat] = g
}

// Security (r) follow-up: 4001 and 4003 are terminal (workspace.md 8, "재접속
// 금지"), for a guest and for the owner, with their own end reasons.
func TestRevokeCloseIsTerminal(t *testing.T) {
	for _, tc := range []struct {
		code   share.CloseCode
		reason string
	}{{share.CloseUnauthorized, share.ReasonUnauthorized}, {share.CloseSuspended, share.ReasonSuspended}} {
		t.Run("guest "+tc.reason, func(t *testing.T) {
			var dials atomic.Int32
			r := newRig(t)
			guestCounted(r, 1, &dials)
			r.join(1, wire.LevelViewer, false)
			r.takeGuest(1)
			before := dials.Load()
			r.relay.Revoke(userOf(1), tc.code)
			r.settle()
			ev := r.takeGuest(1)
			if e, ok := findEvent[share.EventEnded](ev); !ok || e.Reason != tc.reason {
				t.Fatalf("guest events = %#v", ev)
			}
			if hasEvent[share.EventReconnecting](ev) {
				t.Fatal("guest shows reconnecting after a terminal close")
			}
			r.advance(time.Minute)
			if got := dials.Load(); got != before {
				t.Fatalf("guest redialed %d times after %d", got-before, tc.code)
			}
		})
		t.Run("host "+tc.reason, func(t *testing.T) {
			var dials atomic.Int32
			r := newRigWith(t, func(c *share.HostConfig) { c.Dial = countDial(c.Dial, &dials) })
			r.takeHost()
			before := dials.Load()
			r.relay.Revoke(rigOwner, tc.code)
			r.settle()
			ev := r.takeHost()
			if e, ok := findEvent[share.EventEnded](ev); !ok || e.Reason != tc.reason {
				t.Fatalf("host events = %#v", ev)
			}
			if hasEvent[share.EventReconnecting](ev) {
				t.Fatal("host shows reconnecting after a terminal close")
			}
			r.advance(time.Minute)
			if got := dials.Load(); got != before {
				t.Fatalf("host redialed %d times after %d", got-before, tc.code)
			}
		})
	}
}

// A redial refused 401 (session revoked while the socket was down) or 403
// account_suspended ends the share like the 404 does, instead of retrying.
func TestRedialAuthRefusalIsTerminal(t *testing.T) {
	for _, tc := range []struct {
		code   share.CloseCode
		reason string
	}{{share.CloseUnauthorized, share.ReasonUnauthorized}, {share.CloseSuspended, share.ReasonSuspended}} {
		t.Run(tc.reason, func(t *testing.T) {
			var dials atomic.Int32
			r := newRig(t)
			guestCounted(r, 1, &dials)
			r.join(1, wire.LevelViewer, false)
			r.relay.Drop(1)
			r.settle()
			r.relay.Revoke(userOf(1), tc.code)
			r.takeGuest(1)
			r.advance(2 * time.Second)
			ev := r.takeGuest(1)
			if e, ok := findEvent[share.EventEnded](ev); !ok || e.Reason != tc.reason {
				t.Fatalf("guest events = %#v", ev)
			}
			n := dials.Load()
			r.advance(time.Minute)
			if got := dials.Load(); got != n {
				t.Fatalf("guest kept redialing after a %d refusal", tc.code)
			}
		})
	}
}
