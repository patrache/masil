package share_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Dial modes of gateDial.
const (
	dialPass = iota
	dialFail
	dialBlock
)

// gateDial wraps a DialFunc: it passes, fails at once (no network), or
// blocks until released or its ctx ends (a TCP connect that hangs).
type gateDial struct {
	mu      sync.Mutex
	inner   share.DialFunc
	mode    int
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGateDial() *gateDial {
	return &gateDial{entered: make(chan struct{}, 1), release: make(chan struct{})}
}

func (g *gateDial) set(mode int) {
	g.mu.Lock()
	g.mode = mode
	g.mu.Unlock()
}

func (g *gateDial) open() { g.once.Do(func() { close(g.release) }) }

func (g *gateDial) dial(ctx context.Context, id string) (share.Conn, error) {
	g.mu.Lock()
	mode, inner := g.mode, g.inner
	g.mu.Unlock()
	switch mode {
	case dialFail:
		return nil, errors.New("no network")
	case dialBlock:
		select {
		case g.entered <- struct{}{}:
		default:
		}
		select {
		case <-g.release:
			return nil, errors.New("no network")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return inner(ctx, id)
}

// within fails the test when f does not return within a second.
func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		f()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("%s blocked while a redial hangs", what)
	}
}

func waitClosed(t *testing.T, what string, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not finish", what)
	}
}

// Review 1: a redial that hangs must not hold the host's state lock: owner
// calls return and the kill switch ends the share at once.
func TestHangingRedialKeepsHostResponsive(t *testing.T) {
	gd := newGateDial()
	r := newRigWith(t, func(c *share.HostConfig) { gd.inner, c.Dial = c.Dial, gd.dial }, 1)
	r.join(1, wire.LevelViewer, false)
	defer gd.open()
	gd.set(dialBlock)
	r.relay.Drop(wire.SeatOwner)
	r.settle()
	r.takeHost()
	r.now = r.now.Add(2 * time.Second)
	tick := make(chan struct{})
	go func() {
		r.host.Tick(r.now)
		close(tick)
	}()
	select {
	case <-gd.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("host did not redial")
	}
	within(t, "OwnerActivity", func() { r.host.OwnerActivity(r.now) })
	within(t, "Members", func() { _ = r.host.Members() })
	within(t, "SetLevel", func() { _ = r.host.SetLevel(1, wire.LevelNone) })
	r.host.Kill()
	deadline := time.Now().Add(2 * time.Second)
	var ev []share.Event
	for !hasEvent[share.EventEnded](ev) {
		if time.Now().After(deadline) {
			t.Fatal("kill did not end the share while the redial hangs")
		}
		time.Sleep(time.Millisecond)
		ev = append(ev, r.host.Drain()...)
	}
	waitClosed(t, "the hanging Tick", tick)
	if !r.auditHas(audit.EvShareEnd, dataIs("reason", share.ReasonKill)) {
		t.Fatal("share end not recorded")
	}
}

func TestHangingRedialKeepsGuestResponsive(t *testing.T) {
	gd := newGateDial()
	r := newRig(t)
	g := share.NewGuest(share.GuestConfig{ShareID: rigShare, OwnerID: rigOwner, UserID: userOf(1),
		Dial: gd.dial, Clock: fixedClock{&r.now}, ManualTick: true})
	gd.inner = r.relay.Dial(userOf(1))
	if err := g.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.guests[1] = g
	r.join(1, wire.LevelViewer, false)
	defer gd.open()
	gd.set(dialBlock)
	r.relay.Drop(1)
	r.settle()
	r.now = r.now.Add(2 * time.Second)
	tick := make(chan struct{})
	go func() {
		g.Tick(r.now)
		close(tick)
	}()
	select {
	case <-gd.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("guest did not redial")
	}
	within(t, "Level", func() { _ = g.Level() })
	within(t, "Leave", g.Leave)
	waitClosed(t, "the hanging Tick", tick)
}
