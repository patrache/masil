package share_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// stallConn wraps a Conn; once stalled, Send blocks until released (a
// server that never answers).
type stallConn struct {
	share.Conn
	stalled *atomic.Bool
	release chan struct{}
}

func (c stallConn) Send(f []byte) error {
	if c.stalled.Load() {
		<-c.release
		return errors.New("stalled")
	}
	return c.Conn.Send(f)
}

func TestKillStopsInputWithinOneSecond(t *testing.T) {
	t.Run("server never answers", func(t *testing.T) {
		var stalled atomic.Bool
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		r, ft := keyboardRig(t, func(c *share.HostConfig) {
			dial := c.Dial
			c.Dial = func(ctx context.Context, id string) (share.Conn, error) {
				cn, err := dial(ctx, id)
				if err != nil {
					return nil, err
				}
				return stallConn{Conn: cn, stalled: &stalled, release: release}, nil
			}
		}, 30*time.Minute)
		r.host.OwnerActivity(r.now)
		stalled.Store(true)
		start := time.Now()
		r.host.Kill()
		if d := time.Since(start); d > 50*time.Millisecond {
			t.Fatalf("Kill took %v", d)
		}
		if !r.host.Killed() {
			t.Fatal("Killed() false")
		}
		if err := r.guests[1].Input(0, []byte("rm -rf ~\r")); err != nil {
			t.Fatal(err)
		}
		r.relay.Pump()
		r.host.Tick(r.now)
		r.relay.Pump()
		r.host.Tick(r.now)
		if ft.typed() != "" {
			t.Fatalf("typed after Kill: %q", ft.typed())
		}
		if err := r.host.GrantKeyboard(1, share.DefaultKeyboard); !errors.Is(err, share.ErrEnded) {
			t.Fatalf("GrantKeyboard after Kill = %v", err)
		}
	})

	t.Run("participants see the end", func(t *testing.T) {
		ended := make(chan struct{}, 1)
		r, ft := keyboardRig(t, func(c *share.HostConfig) {
			c.EndShare = func(context.Context) error { ended <- struct{}{}; return nil }
		}, 30*time.Minute)
		r.host.OwnerActivity(r.now)
		start := time.Now()
		r.host.Kill()
		for !r.relay.Ended() {
			if time.Since(start) > time.Second {
				t.Fatal("server not told within 1s")
			}
			time.Sleep(time.Millisecond)
		}
		r.relay.Pump()
		r.guests[1].Tick(r.now)
		if e, ok := findEvent[share.EventEnded](r.guests[1].Drain()); !ok || e.Reason != share.ReasonEnded {
			t.Fatalf("guest end = %#v %v", e, ok)
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("participant saw the end after %v", d)
		}
		select {
		case <-ended:
		case <-time.After(2 * time.Second):
			t.Fatal("REST end not called")
		}
		_ = r.guests[1].Input(0, []byte("x"))
		r.settle()
		if ft.typed() != "" {
			t.Fatalf("typed after Kill: %q", ft.typed())
		}
		deadline := time.Now().Add(time.Second)
		for !r.auditHas(audit.EvShareEnd, dataIs("reason", share.ReasonKill)) {
			if time.Now().After(deadline) {
				t.Fatal("kill not recorded")
			}
			time.Sleep(time.Millisecond)
		}
		if e, ok := findEvent[share.EventEnded](append(r.takeHost(), r.host.Drain()...)); !ok || e.Reason != share.ReasonKill {
			t.Fatalf("host end = %#v %v", e, ok)
		}
	})
}

func TestEndWaitsForREST(t *testing.T) {
	called := false
	r := newRigWith(t, func(c *share.HostConfig) {
		c.EndShare = func(context.Context) error { called = true; return nil }
	}, 1)
	r.join(1, wire.LevelViewer, false)
	if err := r.host.End(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called || !r.relay.Ended() {
		t.Fatalf("End: REST called %v, server ended %v", called, r.relay.Ended())
	}
	r.relay.Pump()
	r.guests[1].Tick(r.now)
	if e, ok := findEvent[share.EventEnded](r.guests[1].Drain()); !ok || e.Reason != share.ReasonEnded {
		t.Fatalf("guest end = %#v %v", e, ok)
	}
	if err := r.host.End(context.Background()); !errors.Is(err, share.ErrEnded) {
		t.Fatalf("second End = %v", err)
	}
}

func TestKickRekeysAndKickedCannotRead(t *testing.T) {
	var mu sync.Mutex
	var removed []string
	r := newRigWith(t, func(c *share.HostConfig) {
		c.RemoveParticipant = func(_ context.Context, u string) error {
			mu.Lock()
			removed = append(removed, u)
			mu.Unlock()
			return nil
		}
	}, 1, 2)
	r.join(1, wire.LevelEditor, true)
	r.join(2, wire.LevelEditor, true)
	if err := r.host.GrantKeyboard(1, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	r.settle()
	old2 := share.GuestSessionKey(r.guests[2])
	if err := r.host.Kick(2); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if e, ok := findEvent[share.EventEnded](r.takeGuest(2)); !ok || e.Reason != share.ReasonKicked {
		t.Fatalf("kicked guest end = %#v %v", e, ok)
	}
	if _, ok := r.relay.Levels()[2]; ok {
		t.Fatal("server still has seat 2")
	}
	if lv := memberLevel(t, r.host, 1); lv != wire.LevelEditor || r.guests[1].Level() != wire.LevelEditor {
		t.Fatalf("keyboard holder after rekey: host %v guest %v", lv, r.guests[1].Level())
	}

	var captured []byte
	r.relay.Tamper(func(from, to uint8, f []byte) []byte {
		if h, _, err := wire.Parse(f); err == nil && from == wire.SeatOwner && to == 1 && h.Kind == wire.KindTermOut {
			captured = append([]byte(nil), f...)
		}
		return f
	})
	if err := share.HostBroadcast(r.host, wire.KindTermOut, wire.TermPayload(0, []byte("secret"))); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if captured == nil {
		t.Fatal("no broadcast reached seat 1")
	}
	new1 := share.GuestSessionKey(r.guests[1])
	if new1.Epoch != old2.Epoch+1 {
		t.Fatalf("epoch %d -> %d", old2.Epoch, new1.Epoch)
	}
	if _, pt, err := wscrypto.NewOpener(new1.Key, wscrypto.DirBroadcast).Open(captured, wscrypto.SignatureLen); err != nil || string(pt[1:]) != "secret" {
		t.Fatalf("remaining member cannot open: %v", err)
	}
	if _, _, err := wscrypto.NewOpener(old2.Key, wscrypto.DirBroadcast).Open(captured, wscrypto.SignatureLen); !errors.Is(err, wscrypto.ErrAuth) {
		t.Fatalf("kicked key opens the new epoch: %v", err)
	}
	if hasEvent[share.EventTamperWarning](r.takeGuest(1)) {
		t.Fatal("remaining member rejects frames after rekey")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(removed)
		mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("REST removal not called")
		}
		time.Sleep(time.Millisecond)
	}
	if removed[0] != "U_2" || !r.auditHas(audit.EvKick, nil) {
		t.Fatalf("removed %v", removed)
	}
}
