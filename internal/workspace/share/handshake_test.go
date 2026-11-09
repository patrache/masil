package share_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/share/sharetest"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

const (
	rigShare = "W1"
	rigOwner = "U_O"
)

type fixedClock struct{ t *time.Time }

func (c fixedClock) Now() time.Time { return *c.t }

// rig is one share over sharetest.Relay with a host and guests, all on
// ManualTick and one shared engine time.
type rig struct {
	t      *testing.T
	relay  *sharetest.Relay
	host   *share.Host
	guests map[uint8]*share.Guest
	now    time.Time
	audit  string
	hostEv []share.Event
	gEv    map[uint8][]share.Event
	cfg    share.HostConfig
}

func userOf(seat uint8) string { return "U_" + string(rune('0'+seat)) }

func newRig(t *testing.T, seats ...uint8) *rig {
	return newRigWith(t, nil, seats...)
}

// newRigWith builds the rig; edit may change the host config before start.
func newRigWith(t *testing.T, edit func(*share.HostConfig), seats ...uint8) *rig {
	t.Helper()
	r := &rig{t: t, now: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC), guests: map[uint8]*share.Guest{}, gEv: map[uint8][]share.Event{}}
	members := map[uint8]string{}
	for s := uint8(1); s <= 3; s++ {
		members[s] = userOf(s)
	}
	r.relay = sharetest.NewRelay(rigShare, rigOwner, members)
	r.relay.SetClock(func() time.Time { return r.now })
	dir := t.TempDir()
	log, err := audit.Open(dir, rigShare)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	r.audit = filepath.Join(dir, rigShare+".jsonl")
	r.cfg = share.HostConfig{ShareID: rigShare, OwnerID: rigOwner, Title: "t", Dial: r.relay.Dial(rigOwner),
		Audit: log, Clock: fixedClock{&r.now}, ManualTick: true}
	if edit != nil {
		edit(&r.cfg)
	}
	r.host = share.NewHost(r.cfg)
	if err := r.host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, s := range seats {
		r.addGuest(s)
	}
	r.settle()
	return r
}

func (r *rig) addGuest(seat uint8) *share.Guest {
	r.t.Helper()
	g := share.NewGuest(share.GuestConfig{ShareID: rigShare, OwnerID: rigOwner, UserID: userOf(seat),
		Dial: r.relay.Dial(userOf(seat)), Clock: fixedClock{&r.now}, ManualTick: true})
	if err := g.Start(context.Background()); err != nil {
		r.t.Fatal(err)
	}
	r.guests[seat] = g
	return g
}

// settle pumps and ticks everyone at r.now until nothing moves.
func (r *rig) settle() {
	r.t.Helper()
	for i := 0; i < 200; i++ {
		n := r.relay.Pump()
		r.tickAll()
		if n == 0 && r.relay.Pump() == 0 {
			r.tickAll()
			if r.relay.Pump() == 0 {
				return
			}
		}
	}
	r.t.Fatal("rig did not settle")
}

func (r *rig) tickAll() {
	r.host.Tick(r.now)
	r.hostEv = append(r.hostEv, r.host.Drain()...)
	for s, g := range r.guests {
		g.Tick(r.now)
		r.gEv[s] = append(r.gEv[s], g.Drain()...)
	}
}

// advance moves the engine time and settles.
func (r *rig) advance(d time.Duration) {
	r.now = r.now.Add(d)
	r.settle()
}

// join runs the whole join of seat at level.
func (r *rig) join(seat uint8, level wire.Level, verified bool) {
	r.t.Helper()
	r.settle()
	if err := r.host.Approve(seat, level, verified); err != nil {
		r.t.Fatalf("Approve(%d): %v", seat, err)
	}
	if verified {
		if err := r.guests[seat].Confirm(true); err != nil {
			r.t.Fatalf("Confirm(%d): %v", seat, err)
		}
	}
	r.settle()
	if !hasEvent[share.EventGranted](r.gEv[seat]) {
		r.t.Fatalf("seat %d not granted: %#v", seat, r.gEv[seat])
	}
}

// takeHost / takeGuest return and clear the gathered events.
func (r *rig) takeHost() []share.Event {
	ev := r.hostEv
	r.hostEv = nil
	return ev
}

func (r *rig) takeGuest(seat uint8) []share.Event {
	ev := r.gEv[seat]
	r.gEv[seat] = nil
	return ev
}

func hasEvent[T share.Event](evs []share.Event) bool {
	_, ok := findEvent[T](evs)
	return ok
}

func findEvent[T share.Event](evs []share.Event) (T, bool) {
	for _, e := range evs {
		if v, ok := e.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

func countEvents[T share.Event](evs []share.Event) int {
	n := 0
	for _, e := range evs {
		if _, ok := e.(T); ok {
			n++
		}
	}
	return n
}

// auditEntries reads the host's record file.
func (r *rig) auditEntries() []audit.Entry {
	r.t.Helper()
	f, err := os.Open(r.audit)
	if err != nil {
		r.t.Fatal(err)
	}
	defer f.Close()
	var out []audit.Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e audit.Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			r.t.Fatalf("bad audit line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}

func (r *rig) auditHas(ev string, match func(audit.Entry) bool) bool {
	for _, e := range r.auditEntries() {
		if e.Ev == ev && (match == nil || match(e)) {
			return true
		}
	}
	return false
}

func dataIs(key string, want any) func(audit.Entry) bool {
	return func(e audit.Entry) bool { return reflect.DeepEqual(e.Data[key], want) }
}

// rewrite returns frame with its plaintext JSON body changed by fn.
func rewrite(frame []byte, fn func(map[string]any)) []byte {
	h, body, err := wire.Parse(frame)
	if err != nil {
		return frame
	}
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return frame
	}
	fn(m)
	nb, _ := json.Marshal(m)
	h.Len = uint32(len(nb))
	hb := h.Marshal()
	return append(hb[:], nb...)
}

func TestJoinVerified(t *testing.T) {
	r := newRig(t, 1)
	p := r.host.Pending()
	if len(p) != 1 || p[0].Seat != 1 || p[0].UserID != "U_1" {
		t.Fatalf("Pending = %#v", p)
	}
	if !hasEvent[share.EventJoinRequest](r.takeHost()) {
		t.Fatal("no EventJoinRequest")
	}
	g := r.guests[1]
	if p[0].SAS == "" || p[0].SAS != g.SAS() {
		t.Fatalf("SAS host %q guest %q", p[0].SAS, g.SAS())
	}
	if !hasEvent[share.EventSASReady](r.takeGuest(1)) {
		t.Fatal("guest has no EventSASReady")
	}
	if err := r.host.Approve(1, wire.LevelEditor, true); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if hasEvent[share.EventGranted](r.gEv[1]) {
		t.Fatal("granted before the participant confirmed")
	}
	if err := g.Confirm(true); err != nil {
		t.Fatal(err)
	}
	r.settle()
	gr, ok := findEvent[share.EventGranted](r.takeGuest(1))
	if !ok || gr.Level != wire.LevelEditor || !gr.Verified {
		t.Fatalf("grant = %#v %v", gr, ok)
	}
	if len(r.host.Pending()) != 0 {
		t.Fatal("request still pending")
	}
	if !hasEvent[share.EventPeerJoined](r.takeHost()) {
		t.Fatal("no EventPeerJoined")
	}
	if lv := r.relay.Levels(); lv[1] != wire.LevelEditor || !r.relay.Verified()[1] {
		t.Fatalf("server levels %v verified %v", lv, r.relay.Verified())
	}
	if !r.auditHas(audit.EvSAS, dataIs("result", "match")) || !r.auditHas(audit.EvJoinAllow, nil) || !r.auditHas(audit.EvJoinRequest, nil) {
		t.Fatalf("audit = %#v", r.auditEntries())
	}
}

func TestJoinUnverifiedOnlyLowLevels(t *testing.T) {
	r := newRig(t, 1)
	if err := r.host.Approve(1, wire.LevelEditor, false); err != share.ErrNeedsVerify {
		t.Fatalf("Approve editor unverified = %v", err)
	}
	if err := r.host.Approve(1, wire.LevelKeyboard, true); err != share.ErrUseGrant {
		t.Fatalf("Approve keyboard = %v", err)
	}
	if err := r.host.Approve(1, wire.LevelViewer, false); err != nil {
		t.Fatal(err)
	}
	r.settle()
	gr, ok := findEvent[share.EventGranted](r.takeGuest(1))
	if !ok || gr.Level != wire.LevelViewer || gr.Verified {
		t.Fatalf("grant = %#v %v", gr, ok)
	}
	if !r.auditHas(audit.EvSAS, dataIs("result", "skipped")) {
		t.Fatal("audit has no sas=skipped")
	}
}

func TestMITMSASDiffers(t *testing.T) {
	r := newRig(t)
	atkA, _ := wscrypto.NewKeyPair() // faces the owner
	atkB, _ := wscrypto.NewKeyPair() // faces the participant
	r.relay.Tamper(func(from, to uint8, f []byte) []byte {
		h, _, err := wire.Parse(f)
		if err != nil {
			return f
		}
		switch h.Kind {
		case wire.KindHsCommit:
			return rewrite(f, func(m map[string]any) { m["commit"] = wscrypto.Commit(rigShare, atkA.Public()) })
		case wire.KindHsOwnerPub:
			return rewrite(f, func(m map[string]any) { m["pub"] = atkB.Public() })
		case wire.KindHsReveal:
			return rewrite(f, func(m map[string]any) { m["pub"] = atkA.Public() })
		}
		return f
	})
	r.addGuest(1)
	r.settle()
	p := r.host.Pending()
	if len(p) != 1 {
		t.Fatalf("Pending = %#v", p)
	}
	if p[0].SAS == r.guests[1].SAS() || r.guests[1].SAS() == "" {
		t.Fatalf("SAS equal under MITM: %q", p[0].SAS)
	}
	// The participant presses "different": the owner is warned.
	if err := r.guests[1].Confirm(false); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if !hasEvent[share.EventDeclined](r.takeGuest(1)) {
		t.Fatal("guest not declined")
	}
}

func TestCommitMismatchDeclines(t *testing.T) {
	r := newRig(t)
	other, _ := wscrypto.NewKeyPair()
	r.relay.Tamper(func(from, to uint8, f []byte) []byte {
		if h, _, err := wire.Parse(f); err == nil && h.Kind == wire.KindHsReveal {
			return rewrite(f, func(m map[string]any) { m["pub"] = other.Public() })
		}
		return f
	})
	r.addGuest(1)
	r.settle()
	if len(r.host.Pending()) != 0 {
		t.Fatal("request despite a bad commitment")
	}
	d, ok := findEvent[share.EventDeclined](r.takeGuest(1))
	if !ok || d.Reason != share.ReasonCommit {
		t.Fatalf("guest decline = %#v %v", d, ok)
	}
	if !hasEvent[share.EventMITMWarning](r.takeHost()) {
		t.Fatal("owner not warned")
	}
	if !r.auditHas(audit.EvSAS, dataIs("result", "mismatch")) {
		t.Fatal("audit has no sas=mismatch")
	}
}

func TestJoinRequestExpires(t *testing.T) {
	r := newRig(t, 1)
	r.advance(share.JoinTimeout - time.Second)
	if len(r.host.Pending()) != 1 {
		t.Fatal("expired early")
	}
	r.advance(2 * time.Second)
	if len(r.host.Pending()) != 0 {
		t.Fatal("still pending after 5 minutes")
	}
	d, ok := findEvent[share.EventDeclined](r.takeGuest(1))
	if !ok || d.Reason != share.ReasonExpired {
		t.Fatalf("guest decline = %#v %v", d, ok)
	}
	if !hasEvent[share.EventJoinExpired](r.takeHost()) || !r.auditHas(audit.EvJoinExpired, nil) {
		t.Fatal("no expiry event / record")
	}
	if err := r.host.Approve(1, wire.LevelViewer, false); err != share.ErrNoRequest {
		t.Fatalf("Approve after expiry = %v", err)
	}
}

func TestTamperedFramesDroppedAndWarned(t *testing.T) {
	r := newRig(t, 1)
	r.join(1, wire.LevelViewer, false)
	r.takeGuest(1)
	r.relay.Tamper(func(from, to uint8, f []byte) []byte {
		h, _, err := wire.Parse(f)
		if err == nil && from == wire.SeatOwner && h.Kind == wire.KindNotice {
			f[wire.HeaderLen] ^= 0x01
		}
		return f
	})
	for i := 0; i < 5; i++ {
		if err := share.HostBroadcast(r.host, wire.KindNotice, []byte(`{"code":"x"}`)); err != nil {
			t.Fatal(err)
		}
		r.advance(time.Second)
	}
	ev := r.takeGuest(1)
	if n := countEvents[share.EventTamperWarning](ev); n != 1 {
		t.Fatalf("tamper warnings = %d (%#v)", n, ev)
	}
	// Untampered frames still open afterwards.
	r.relay.Tamper(nil)
	if err := share.HostBroadcast(r.host, wire.KindNotice, []byte(`{"code":"x"}`)); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if hasEvent[share.EventTamperWarning](r.takeGuest(1)) {
		t.Fatal("warned again for a good frame")
	}
}

func TestHostRecordsForgedPeerFrames(t *testing.T) {
	r := newRig(t, 1)
	r.join(1, wire.LevelViewer, false)
	// A frame that claims seat 1 but is not sealed with its key.
	for i := 0; i < 5; i++ {
		h := wire.Header{Kind: wire.KindDocOpen, From: 1, To: 0, Seq: uint64(100 + i), Len: 32}
		hb := h.Marshal()
		r.relay.Inject(wire.SeatOwner, append(hb[:], make([]byte, 32)...))
	}
	r.settle()
	if !hasEvent[share.EventTamperWarning](r.takeHost()) {
		t.Fatal("owner not warned")
	}
	if !r.auditHas(audit.EvFrameRejected, dataIs("reason", "auth")) || !r.auditHas(audit.EvTamperWarning, nil) {
		t.Fatal("rejections not recorded")
	}
}

func TestGuestResumeWithoutSAS(t *testing.T) {
	r := newRig(t, 1)
	r.join(1, wire.LevelViewer, false)
	r.takeHost()
	r.takeGuest(1)
	r.relay.Drop(1)
	r.settle()
	if !hasEvent[share.EventReconnecting](r.takeGuest(1)) {
		t.Fatal("no EventReconnecting")
	}
	r.advance(time.Second)
	ev := r.takeGuest(1)
	if !hasEvent[share.EventResumed](ev) || hasEvent[share.EventSASReady](ev) || hasEvent[share.EventGranted](ev) {
		t.Fatalf("guest events after redial = %#v", ev)
	}
	if len(r.host.Pending()) != 0 || hasEvent[share.EventJoinRequest](r.takeHost()) {
		t.Fatal("resume made a new join request")
	}
	// The resumed guest still reads broadcasts.
	if err := share.HostBroadcast(r.host, wire.KindNotice, []byte(`{"code":"x"}`)); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if hasEvent[share.EventTamperWarning](r.takeGuest(1)) {
		t.Fatal("resumed guest rejects broadcasts")
	}
}

func TestShareEndedRemotely(t *testing.T) {
	r := newRig(t, 1)
	r.join(1, wire.LevelViewer, false)
	r.relay.End()
	r.settle()
	if e, ok := findEvent[share.EventEnded](r.takeHost()); !ok || e.Reason != share.ReasonEnded {
		t.Fatalf("host end = %#v %v", e, ok)
	}
	if e, ok := findEvent[share.EventEnded](r.takeGuest(1)); !ok || e.Reason != share.ReasonEnded {
		t.Fatalf("guest end = %#v %v", e, ok)
	}
	if !r.auditHas(audit.EvShareEnd, nil) || !r.auditHas(audit.EvShareStart, nil) {
		t.Fatal("share start/end not recorded")
	}
}

// TestWallClockLoops runs host and guest on their own 10 ms loops (no
// ManualTick) while the test only pumps the relay.
func TestWallClockLoops(t *testing.T) {
	relay := sharetest.NewRelay(rigShare, rigOwner, map[uint8]string{1: "U_1"})
	host := share.NewHost(share.HostConfig{ShareID: rigShare, OwnerID: rigOwner, Dial: relay.Dial(rigOwner)})
	guest := share.NewGuest(share.GuestConfig{ShareID: rigShare, OwnerID: rigOwner, UserID: "U_1", Dial: relay.Dial("U_1")})
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := guest.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	until := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			relay.Pump()
			time.Sleep(2 * time.Millisecond)
		}
	}
	until("join request", func() bool { return len(host.Pending()) == 1 })
	if err := host.Approve(1, wire.LevelPrompter, false); err != nil {
		t.Fatal(err)
	}
	var gev []share.Event
	until("grant", func() bool { gev = append(gev, guest.Drain()...); return hasEvent[share.EventGranted](gev) })
	relay.End()
	until("end", func() bool { gev = append(gev, guest.Drain()...); return hasEvent[share.EventEnded](gev) })
	var hev []share.Event
	until("host end", func() bool { hev = append(hev, host.Drain()...); return hasEvent[share.EventEnded](hev) })
}
