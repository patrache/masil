//go:build !windows

package termhost

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// recTap records every Output call; block (if set) is waited on first.
type recTap struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	calls int
	block chan struct{}
}

func (r *recTap) Output(b []byte) {
	if r.block != nil {
		<-r.block
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf.Write(b)
	r.calls++
}

func (r *recTap) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func (r *recTap) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Len()
}

func TestTapGetsOutputAndSnapshot(t *testing.T) {
	size := Size{W: 30, H: 5}
	p := startSh(t, "printf one; read x; printf two; sleep 30", size, PaneOptions{})
	waitFor(t, 3*time.Second, "one", screenHas(p, "one"))
	if p.Shared() {
		t.Fatal("Shared() before any tap")
	}
	tap := &recTap{}
	snap, detach := p.AttachTap(tap)
	if !p.Shared() {
		t.Fatal("Shared() false with a tap")
	}
	r := NewReplay(size, 0)
	r.Write(snap)
	if !strings.Contains(plain(r.Render()), "one") {
		t.Fatalf("snapshot replay = %q, want one", plain(r.Render()))
	}
	p.SendKey(enter())
	waitFor(t, 3*time.Second, "two through the tap", func() bool { return strings.Contains(tap.String(), "two") })
	if strings.Contains(tap.String(), "one") {
		t.Fatal("tap got bytes from before it was attached (they belong to the snapshot)")
	}
	detach()
	detach() // idempotent
	if p.Shared() {
		t.Fatal("Shared() after detach")
	}
}

func TestTapSlowConsumerDoesNotBlock(t *testing.T) {
	p := startSh(t, "i=0; while :; do echo tick$i; i=$((i+1)); sleep 0.02; done", Size{W: 30, H: 5}, PaneOptions{})
	waitFor(t, 3*time.Second, "tick", screenHas(p, "tick"))
	tap := &recTap{block: make(chan struct{})}
	_, detach := p.AttachTap(tap)
	defer detach()
	time.AfterFunc(time.Second, func() { close(tap.block) })
	before := plain(p.Render())
	deadline := time.Now().Add(500 * time.Millisecond)
	changed := false
	for time.Now().Before(deadline) {
		start := time.Now()
		now := plain(p.Render())
		if d := time.Since(start); d > 100*time.Millisecond {
			t.Fatalf("Render() took %v behind a blocked tap", d)
		}
		if now != before {
			changed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !changed {
		t.Fatal("pane stopped drawing while a tap was blocked")
	}
	if !p.Shared() {
		t.Fatal("a slow (not overflowing) tap was detached")
	}
	waitFor(t, 3*time.Second, "tap catches up", func() bool { return strings.Contains(tap.String(), "tick") })
}

func TestTapBufferOverflowDetaches(t *testing.T) {
	p := startSh(t, `read x; head -c 1500000 /dev/zero | tr '\0' 'x'; echo END; sleep 30`, Size{W: 80, H: 5}, PaneOptions{})
	slow := &recTap{block: make(chan struct{})}
	fast := &recTap{}
	_, detachSlow := p.AttachTap(slow)
	_, detachFast := p.AttachTap(fast)
	defer close(slow.block)
	p.SendKey(enter())
	waitFor(t, 20*time.Second, "END", screenHas(p, "END"))
	waitFor(t, 5*time.Second, "fast tap got everything", func() bool { return strings.Contains(fast.String(), "END") })
	if n := strings.Count(fast.String(), "x"); n < 1500000 {
		t.Fatalf("fast tap got %d x, want 1500000", n)
	}
	if !p.Shared() {
		t.Fatal("Shared() false while the fast tap is attached")
	}
	p.mu.Lock()
	n := len(p.taps)
	p.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d taps attached, want 1 (the overflowing one detached)", n)
	}
	detachSlow() // already detached: no-op
	detachFast()
	if p.Shared() {
		t.Fatal("Shared() after the last tap left")
	}
}

func TestTapOverflowLastTapUnshares(t *testing.T) {
	p := startSh(t, `read x; head -c 1500000 /dev/zero | tr '\0' 'y'; echo END; sleep 30`, Size{W: 80, H: 5}, PaneOptions{})
	slow := &recTap{block: make(chan struct{})}
	defer close(slow.block)
	p.AttachTap(slow)
	p.SendKey(enter())
	waitFor(t, 20*time.Second, "END", screenHas(p, "END"))
	if p.Shared() {
		t.Fatal("Shared() true after the only tap overflowed")
	}
}

func TestInjectReachesChild(t *testing.T) {
	lw := &lockedWriter{w: &bytes.Buffer{}}
	logger := slog.New(slog.NewTextHandler(lw, &slog.HandlerOptions{Level: slog.LevelDebug}))
	p, err := StartPane(Request{Program: "cat"}, Size{W: 30, H: 5}, PaneOptions{Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	p.Inject("peer", []byte("hi-INJECTED\n"))
	waitFor(t, 3*time.Second, "injected text", screenHas(p, "hi-INJECTED"))
	_ = p.Close()
	waitExited(t, p, 5*time.Second)
	if out := lw.String(); strings.Contains(out, "INJECTED") || !strings.Contains(out, "cat") {
		t.Fatalf("log must name the program but not the injected bytes:\n%s", out)
	}
}

func TestTapEndsWithPane(t *testing.T) {
	p, err := StartPane(Request{Program: "sh", Args: []string{"-c", "read x; echo bye"}}, Size{W: 20, H: 3}, PaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tap := &recTap{}
	_, detach := p.AttachTap(tap)
	p.SendKey(enter())
	waitExited(t, p, 5*time.Second)
	if p.Shared() {
		t.Fatal("Shared() after the pane exited")
	}
	detach()
	p.Inject("peer", []byte("late")) // dropped, never blocks
	snap, d2 := p.AttachTap(&recTap{})
	d2()
	if len(snap) == 0 {
		t.Fatal("no snapshot from an exited pane")
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	scripts := map[string]string{
		"main": `printf '\033[1;31mred\033[0m plain \033[44m blue \033[0m한글'; printf '\033[3;5Hxy'; read x; sleep 30`,
		"alt":  `printf 'below'; printf '\033[?1049h\033[2;3H\033[4mUL\033[0m 한\033[?2004h\033[?1h\033[?25l'; printf '\033[5;10H'; read x; sleep 30`,
	}
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			size := Size{W: 24, H: 6}
			p := startSh(t, script, size, PaneOptions{})
			marker := "xy"
			if name == "alt" {
				marker = "UL"
			}
			waitFor(t, 3*time.Second, marker, screenHas(p, marker))
			time.Sleep(50 * time.Millisecond)
			snap, detach := p.AttachTap(&recTap{})
			defer detach()
			r := NewReplay(size, 0)
			r.Write(snap)
			if got, want := r.Render(), p.Render(); got != want {
				t.Fatalf("replay render differs\n got %q\nwant %q", got, want)
			}
			px, py, pv := p.Cursor()
			rx, ry, rv := r.Cursor()
			if px != rx || py != ry || pv != rv {
				t.Fatalf("cursor replay = %d,%d,%v, pane = %d,%d,%v", rx, ry, rv, px, py, pv)
			}
			if name == "alt" {
				if !strings.Contains(string(snap), "\x1b[?2004h") || !strings.Contains(string(snap), "\x1b[?1h") {
					t.Fatalf("snapshot lacks input modes: %q", snap)
				}
			}
		})
	}
}

// TestTapReplayFollowsSource: snapshot + every tap byte after it rebuilds the
// pane's screen even when the later output depends on state set before the
// tap (pen, scroll region, autowrap off).
func TestTapReplayFollowsSource(t *testing.T) {
	size := Size{W: 20, H: 6}
	script := `printf '1\n2\n3\n4\n5\033[2;4r\033[4;1H\033[?7l\033[31m'; read x; printf 'RED\n\nwrap-off-0123456789abcdef'; printf '\033[6;1Hdone'; sleep 30`
	p := startSh(t, script, size, PaneOptions{})
	waitFor(t, 3*time.Second, "5", screenHas(p, "5"))
	time.Sleep(50 * time.Millisecond)
	tap := &recTap{}
	snap, detach := p.AttachTap(tap)
	defer detach()
	p.SendKey(enter())
	waitFor(t, 3*time.Second, "done through the tap", func() bool { return strings.Contains(tap.String(), "done") })
	waitFor(t, 3*time.Second, "done on the pane", screenHas(p, "done"))
	r := NewReplay(size, 0)
	r.Write(snap)
	r.Write([]byte(tap.String()))
	if got, want := r.Render(), p.Render(); got != want {
		t.Fatalf("replay differs\n got %q\nwant %q", got, want)
	}
}
