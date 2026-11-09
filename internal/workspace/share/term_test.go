package share_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// termRig: seat 1 at level, terminal 0 shared, the first keyframe through.
func termRig(t *testing.T, level wire.Level, seats ...uint8) (*rig, *fakeTerm) {
	t.Helper()
	r := newRig(t, seats...)
	for _, s := range seats {
		r.join(s, level, level >= wire.LevelEditor)
	}
	ft := newFakeTerm()
	if err := r.host.ShareTerminal(0, ft); err != nil {
		t.Fatal(err)
	}
	r.settle()
	return r, ft
}

func termOuts(evs []share.Event) [][]byte {
	var out [][]byte
	for _, e := range evs {
		if o, ok := e.(share.EventTermOut); ok {
			out = append(out, o.Bytes)
		}
	}
	return out
}

func TestBatching(t *testing.T) {
	r, ft := termRig(t, wire.LevelViewer, 1)
	if !hasEvent[share.EventTermKeyframe](r.takeGuest(1)) {
		t.Fatal("no first keyframe")
	}
	ft.Write([]byte("0123456789"))
	r.advance(20 * time.Millisecond)
	ft.Write([]byte("abcdefghij"))
	r.advance(29 * time.Millisecond)
	if outs := termOuts(r.takeGuest(1)); len(outs) != 0 {
		t.Fatalf("sent before 50ms: %q", outs)
	}
	r.advance(time.Millisecond)
	if outs := termOuts(r.takeGuest(1)); len(outs) != 1 || string(outs[0]) != "0123456789abcdefghij" {
		t.Fatalf("at 50ms: %q", outs)
	}

	big := bytes.Repeat([]byte("x"), 40<<10)
	ft.Write(big)
	r.settle()
	outs := termOuts(r.takeGuest(1))
	if len(outs) != 1 || len(outs[0]) != share.TermBatch {
		t.Fatalf("40 KiB at once: %d frames %v", len(outs), lens(outs))
	}
	r.advance(49 * time.Millisecond)
	if outs := termOuts(r.takeGuest(1)); len(outs) != 0 {
		t.Fatalf("remainder early: %v", lens(outs))
	}
	r.advance(time.Millisecond)
	if outs := termOuts(r.takeGuest(1)); len(outs) != 1 || len(outs[0]) != 8<<10 {
		t.Fatalf("remainder: %v", lens(outs))
	}
}

func lens(bs [][]byte) []int {
	out := make([]int, len(bs))
	for i, b := range bs {
		out[i] = len(b)
	}
	return out
}

func TestKeyframeEvery10sAndOnJoin(t *testing.T) {
	r := newRig(t, 1, 2)
	r.join(1, wire.LevelViewer, false)
	var starts []time.Time
	r.relay.Tamper(func(from, to uint8, f []byte) []byte {
		if h, _, err := wire.Parse(f); err == nil && to == 1 && h.Kind == wire.KindTermKey && h.Flags&wire.FlagGroupStart != 0 {
			starts = append(starts, r.now)
		}
		return f
	})
	ft := newFakeTerm()
	if err := r.host.ShareTerminal(0, ft); err != nil {
		t.Fatal(err)
	}
	t0 := r.now
	r.settle()
	r.advance(share.KeyframeEvery)
	r.advance(share.KeyframeEvery)
	r.join(2, wire.LevelViewer, false)
	want := []time.Time{t0, t0.Add(10 * time.Second), t0.Add(20 * time.Second), t0.Add(20 * time.Second)}
	if len(starts) != len(want) {
		t.Fatalf("keyframe group starts at %v", starts)
	}
	for i := range want {
		if !starts[i].Equal(want[i]) {
			t.Fatalf("keyframe %d at %v, want %v", i, starts[i], want[i])
		}
	}
	if n := countEvents[share.EventTermKeyframe](r.takeGuest(2)); n < 1 {
		t.Fatal("new participant got no keyframe")
	}
}

// TestTermBurstFallsBackToKeyframe is Review Focus 3: 5 MiB of output in a
// second. The owner sends at most 2 MiB that second, drops the rest and
// redraws with a keyframe; the participant's screen ends on that keyframe.
func TestTermBurstFallsBackToKeyframe(t *testing.T) {
	r, ft := termRig(t, wire.LevelViewer, 1)
	r.takeGuest(1)
	t0 := r.now
	inFirst, keyframes := 0, 0
	keySize := 0
	r.relay.Tamper(func(from, to uint8, f []byte) []byte {
		h, _, err := wire.Parse(f)
		if err != nil || to != 1 || (h.Kind != wire.KindTermOut && h.Kind != wire.KindTermKey) {
			return f
		}
		if r.now.Before(t0.Add(time.Second)) {
			inFirst += len(f)
			if h.Kind == wire.KindTermKey {
				keyframes++
				keySize = len(f)
			}
		}
		return f
	})
	ft.setSnap([]byte("FINAL-SCREEN")) // the screen as the flood leaves it
	chunk := bytes.Repeat([]byte("y"), (5<<20)/100)
	for i := 0; i < 100; i++ {
		ft.Write(chunk)
		r.advance(10 * time.Millisecond)
	}
	r.advance(time.Second)
	if keyframes > 1 || inFirst > share.OwnerOutputPerSecond+keySize {
		t.Fatalf("first second: %d bytes, %d keyframes", inFirst, keyframes)
	}
	if inFirst < share.OwnerOutputPerSecond/2 {
		t.Fatalf("first second sent only %d bytes", inFirst)
	}
	var last share.Event
	for _, e := range r.takeGuest(1) {
		switch e.(type) {
		case share.EventTermOut, share.EventTermKeyframe:
			last = e
		}
	}
	k, ok := last.(share.EventTermKeyframe)
	if !ok || string(k.Bytes) != "FINAL-SCREEN" {
		t.Fatalf("last terminal event = %#v", last)
	}
	if r.relay.Ended() || !r.relay.Connected(wire.SeatOwner) {
		t.Fatal("hub closed the owner for flooding")
	}
}

func TestTermStatusAndAttention(t *testing.T) {
	r, _ := termRig(t, wire.LevelViewer, 1)
	r.takeGuest(1)
	if err := r.host.SetTermSize(0, 120, 40); err != nil {
		t.Fatal(err)
	}
	if err := r.host.SetTermSize(0, 90, 20); !errors.Is(err, share.ErrBadSize) {
		t.Fatalf("odd size = %v", err)
	}
	if err := r.host.SetTermSize(9, 0, 0); !errors.Is(err, share.ErrNoTerminal) {
		t.Fatalf("no terminal = %v", err)
	}
	r.host.SetAttention(0, true)
	r.host.SetUnsaved(2)
	r.settle()
	var last wire.TermStatus
	for _, e := range r.takeGuest(1) {
		if s, ok := e.(share.EventTermStatus); ok {
			last = s.TermStatus
		}
	}
	if last.Cols != 120 || last.Rows != 40 || !last.Attention || last.Unsaved != 2 || last.Title != "zsh" {
		t.Fatalf("status = %#v", last)
	}
	if r.relay.Attention() != 1 {
		t.Fatalf("ctl_attention sent %d times", r.relay.Attention())
	}
}

// A tampered owner client cannot make a participant build a huge screen: a
// term_status size out of range reaches the participant clamped (Review
// Focus 5).
func TestTermStatusSizeClamped(t *testing.T) {
	r, _ := termRig(t, wire.LevelViewer, 1)
	r.takeGuest(1)
	for _, p := range []string{`{"term":0,"cols":100000,"rows":100000}`, `{"term":0,"cols":-5,"rows":-1}`} {
		if err := share.HostBroadcast(r.host, wire.KindTermStatus, []byte(p)); err != nil {
			t.Fatal(err)
		}
		r.settle()
		var got *wire.TermStatus
		for _, e := range r.takeGuest(1) {
			if s, ok := e.(share.EventTermStatus); ok {
				got = &s.TermStatus
			}
		}
		if got == nil {
			t.Fatalf("%s: no status event", p)
		}
		if got.Cols < 0 || got.Rows < 0 || got.Cols > share.MaxTermCols || got.Rows > share.MaxTermRows {
			t.Fatalf("%s: status size %dx%d not clamped", p, got.Cols, got.Rows)
		}
	}
}
