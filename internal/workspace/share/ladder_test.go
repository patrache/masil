package share_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// fakeTerm is a share.Terminal that records what is injected.
type fakeTerm struct {
	mu        sync.Mutex
	injected  [][]byte
	tap       func([]byte)
	snap      []byte
	cols      int
	rows      int
	fixed     [2]int
	bracketed bool
	title     string
}

func newFakeTerm() *fakeTerm {
	return &fakeTerm{cols: 80, rows: 24, snap: []byte("SNAP"), title: "zsh"}
}

func (f *fakeTerm) Tap(fn func([]byte)) func() {
	f.mu.Lock()
	f.tap = fn
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.tap = nil
		f.mu.Unlock()
	}
}

// Write is output from the program.
func (f *fakeTerm) Write(b []byte) {
	f.mu.Lock()
	fn := f.tap
	f.mu.Unlock()
	if fn != nil {
		fn(b)
	}
}

func (f *fakeTerm) Snapshot() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.snap...)
}

func (f *fakeTerm) setSnap(b []byte) {
	f.mu.Lock()
	f.snap = b
	f.mu.Unlock()
}

func (f *fakeTerm) Inject(p []byte) error {
	f.mu.Lock()
	f.injected = append(f.injected, append([]byte(nil), p...))
	f.mu.Unlock()
	return nil
}

func (f *fakeTerm) Size() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows
}

func (f *fakeTerm) SetFixedSize(c, r int) {
	f.mu.Lock()
	f.fixed = [2]int{c, r}
	if c > 0 {
		f.cols, f.rows = c, r
	}
	f.mu.Unlock()
}

func (f *fakeTerm) BracketedPaste() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bracketed
}

func (f *fakeTerm) Title() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.title
}

// typed is everything injected so far, joined.
func (f *fakeTerm) typed() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(bytes.Join(f.injected, nil))
}

// keyboardRig: seat 1 verified editor, terminal 0 shared, keyboard for d.
func keyboardRig(t *testing.T, edit func(*share.HostConfig), d time.Duration) (*rig, *fakeTerm) {
	t.Helper()
	r := newRigWith(t, edit, 1)
	r.join(1, wire.LevelEditor, true)
	ft := newFakeTerm()
	if err := r.host.ShareTerminal(0, ft); err != nil {
		t.Fatal(err)
	}
	if err := r.host.GrantKeyboard(1, d); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if lv := r.guests[1].Level(); lv != wire.LevelKeyboard {
		t.Fatalf("guest level = %v", lv)
	}
	return r, ft
}

func memberLevel(t *testing.T, h *share.Host, seat uint8) wire.Level {
	t.Helper()
	for _, m := range h.Members() {
		if m.Seat == seat {
			return m.Level
		}
	}
	t.Fatalf("no member %d", seat)
	return 0
}

func TestKeyboardExpiryDropsInputAtOwner(t *testing.T) {
	r, ft := keyboardRig(t, nil, 10*time.Minute)
	g := r.guests[1]
	r.advance(9 * time.Minute)
	r.host.OwnerActivity(r.now)
	if err := g.Input(0, []byte("ls\r")); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := ft.typed(); got != "ls\r" {
		t.Fatalf("typed = %q", got)
	}
	if !r.auditHas(audit.EvKeyboardInput, dataIs("b64", "bHMNCg==")) && !r.auditHas(audit.EvKeyboardInput, dataIs("b64", "bHMN")) {
		t.Fatalf("keyboard input not recorded: %#v", r.auditEntries())
	}

	// Past the end, before the engine has demoted: a frame already in
	// flight is refused at the owner.
	r.now = r.now.Add(time.Minute + time.Second)
	r.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(g, wire.KindTermInput, wire.TermPayload(0, []byte("id\r"))))
	r.relay.Pump()
	r.host.Tick(r.now)
	if got := ft.typed(); got != "ls\r" {
		t.Fatalf("typed after expiry = %q", got)
	}
	r.settle()
	if lv := memberLevel(t, r.host, 1); lv != wire.LevelEditor {
		t.Fatalf("level after expiry = %v", lv)
	}
	if !r.auditHas(audit.EvKeyboardExpire, dataIs("reason", share.DemoteExpired)) {
		t.Fatal("expiry not recorded")
	}
	if g.Level() != wire.LevelEditor {
		t.Fatalf("guest still thinks %v", g.Level())
	}
	if err := g.Input(0, []byte("x")); !errors.Is(err, share.ErrLevel) {
		t.Fatalf("guest Input after expiry = %v", err)
	}

	// +11m: a validly sealed term_input injected past the relay's level
	// filter still does not reach the terminal.
	r.advance(time.Minute)
	r.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(g, wire.KindTermInput, wire.TermPayload(0, []byte("rm -rf ~\r"))))
	r.settle()
	if got := ft.typed(); got != "ls\r" {
		t.Fatalf("typed = %q", got)
	}
	if !r.auditHas(audit.EvFrameRejected, dataIs("reason", "level")) {
		t.Fatal("forged input not recorded")
	}
}

func TestOwnerIdlePausesKeyboard(t *testing.T) {
	r, ft := keyboardRig(t, nil, 30*time.Minute)
	g := r.guests[1]
	r.host.OwnerActivity(r.now)
	r.advance(share.OwnerIdlePause)
	if !hasEvent[share.EventKeyboardPaused](r.takeHost()) {
		t.Fatal("no EventKeyboardPaused")
	}
	_ = g.Input(0, []byte("a"))
	r.settle()
	if ft.typed() != "" {
		t.Fatalf("typed while paused: %q", ft.typed())
	}
	r.host.OwnerActivity(r.now)
	if !hasEvent[share.EventKeyboardResumed](r.host.Drain()) {
		t.Fatal("no EventKeyboardResumed")
	}
	_ = g.Input(0, []byte("b"))
	r.settle()
	if ft.typed() != "b" {
		t.Fatalf("typed after resume = %q", ft.typed())
	}
	if !r.auditHas(audit.EvKeyboardPause, nil) {
		t.Fatal("pause not recorded")
	}
}

func TestPeerGoneDemotes(t *testing.T) {
	r, _ := keyboardRig(t, nil, 30*time.Minute)
	r.host.OwnerActivity(r.now)
	r.relay.Drop(1)
	delete(r.guests, 1) // stays away
	r.settle()
	r.advance(share.PeerGoneDemote)
	if lv := memberLevel(t, r.host, 1); lv != wire.LevelKeyboard {
		t.Fatalf("demoted after exactly 60s: %v", lv)
	}
	r.advance(time.Second)
	if lv := memberLevel(t, r.host, 1); lv != wire.LevelEditor {
		t.Fatalf("level after 61s away = %v", lv)
	}
	if e, ok := findEvent[share.EventKeyboardEnded](r.takeHost()); !ok || e.Reason != share.DemoteGone {
		t.Fatalf("EventKeyboardEnded = %#v %v", e, ok)
	}
}

// TestHostResumeWithinGraceKeepsExpiry is Review Focus 1: the owner's
// socket is away 90 s; on return the participant carries on without a new
// key exchange and the keyboard end time is unchanged.
func TestHostResumeWithinGraceKeepsExpiry(t *testing.T) {
	var mu sync.Mutex
	offline := false
	var dial share.DialFunc
	r, ft := keyboardRig(t, func(c *share.HostConfig) {
		dial = c.Dial
		c.Dial = func(ctx context.Context, id string) (share.Conn, error) {
			mu.Lock()
			off := offline
			mu.Unlock()
			if off {
				return nil, errors.New("no network")
			}
			return dial(ctx, id)
		}
	}, 30*time.Minute)
	g := r.guests[1]
	until := r.host.Members()[0].KeyboardUntil
	r.takeGuest(1)

	mu.Lock()
	offline = true
	mu.Unlock()
	r.relay.Drop(wire.SeatOwner)
	for i := 0; i < 9; i++ {
		r.advance(10 * time.Second)
	}
	if !hasEvent[share.EventReconnecting](r.takeHost()) {
		t.Fatal("host did not notice the drop")
	}
	mu.Lock()
	offline = false
	mu.Unlock()
	for i := 0; i < 31 && !r.relay.Connected(wire.SeatOwner); i++ {
		r.advance(time.Second)
	}
	if !r.relay.Connected(wire.SeatOwner) {
		t.Fatal("host did not redial")
	}
	r.settle()
	ev := r.takeGuest(1)
	if !hasEvent[share.EventResumed](ev) || hasEvent[share.EventSASReady](ev) {
		t.Fatalf("guest events = %#v", ev)
	}
	m := r.host.Members()[0]
	if !m.KeyboardUntil.Equal(until) || m.Level != wire.LevelKeyboard {
		t.Fatalf("after resume: %v until %v, want keyboard until %v", m.Level, m.KeyboardUntil, until)
	}
	if ro, ok := findEvent[share.EventRoster](ev); !ok || !ro.Members[0].KeyboardUntil.Equal(until) {
		t.Fatalf("guest roster = %#v %v", ro, ok)
	}
	r.host.OwnerActivity(r.now)
	if err := g.Input(0, []byte("pwd\r")); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if ft.typed() != "pwd\r" {
		t.Fatalf("typed = %q", ft.typed())
	}
}

func TestSetLevelRules(t *testing.T) {
	r := newRig(t, 1, 2)
	r.join(1, wire.LevelViewer, false)
	r.join(2, wire.LevelEditor, true)
	if err := r.host.SetLevel(1, wire.LevelEditor); !errors.Is(err, share.ErrNeedsVerify) {
		t.Fatalf("unverified -> editor = %v", err)
	}
	if err := r.host.SetLevel(1, wire.LevelPrompter); err != nil {
		t.Fatalf("unverified -> prompter = %v", err)
	}
	if err := r.host.SetLevel(2, wire.LevelKeyboard); !errors.Is(err, share.ErrUseGrant) {
		t.Fatalf("SetLevel keyboard = %v", err)
	}
	if err := r.host.GrantKeyboard(2, 3*time.Hour); !errors.Is(err, share.ErrBadDuration) {
		t.Fatalf("GrantKeyboard 3h = %v", err)
	}
	if err := r.host.GrantKeyboard(1, share.DefaultKeyboard); !errors.Is(err, share.ErrNeedsVerify) {
		t.Fatalf("GrantKeyboard unverified = %v", err)
	}
	if err := r.host.ExtendKeyboard(2, 10*time.Minute); !errors.Is(err, share.ErrNotKeyboard) {
		t.Fatalf("Extend without a grant = %v", err)
	}
	if err := r.host.GrantKeyboard(2, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	r.advance(5 * time.Minute)
	if err := r.host.ExtendKeyboard(2, share.MaxKeyboard); err != nil {
		t.Fatal(err)
	}
	if got := r.host.Members()[1].KeyboardUntil; !got.Equal(r.now.Add(share.MaxKeyboard)) {
		t.Fatalf("extended to %v", got)
	}
	if err := r.host.SetLevel(2, wire.LevelViewer); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if lv := r.relay.Levels(); lv[1] != wire.LevelPrompter || lv[2] != wire.LevelViewer {
		t.Fatalf("server levels = %v", lv)
	}
	if r.guests[2].Level() != wire.LevelViewer || !r.host.Members()[1].KeyboardUntil.IsZero() {
		t.Fatal("lowering did not reach the guest / clear the keyboard end")
	}
	if !r.auditHas(audit.EvLevel, dataIs("to", "viewer")) || !r.auditHas(audit.EvKeyboardGrant, nil) {
		t.Fatal("level changes not recorded")
	}
}

func TestGuestLowersSelf(t *testing.T) {
	r := newRig(t, 1)
	r.join(1, wire.LevelEditor, true)
	g := r.guests[1]
	if err := g.LowerSelf(wire.LevelKeyboard); !errors.Is(err, share.ErrLevel) {
		t.Fatalf("raising self = %v", err)
	}
	if err := g.LowerSelf(wire.LevelViewer); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if lv := memberLevel(t, r.host, 1); lv != wire.LevelViewer {
		t.Fatalf("host sees %v", lv)
	}
	if err := g.LowerSelf(wire.LevelNone); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if !hasEvent[share.EventPeerLeft](r.takeHost()) {
		t.Fatal("no EventPeerLeft")
	}
	if _, ok := r.relay.Levels()[1]; ok {
		t.Fatal("server still has a level for seat 1")
	}
}
