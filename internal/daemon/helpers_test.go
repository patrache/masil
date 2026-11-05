package daemon

import (
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	_ "time/tzdata"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/notify"
)

// t0 is the fixed test clock (plan 06 Global Constraints).
var t0 = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

// fakeClock is a settable clock safe for concurrent use.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// testPaths points MASIL_CONFIG_DIR at t.TempDir(), resolves and creates it.
func testPaths(t *testing.T) config.Paths {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MASIL_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("MASIL_CACHE_DIR", filepath.Join(root, "cache"))
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	if err := config.EnsureDir(p); err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	return p
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// recv takes one value from ch, failing after 2s.
func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		var zero T
		t.Fatal("timed out waiting for a value")
		return zero
	}
}

// expectNone fails if ch yields a value within d.
func expectNone[T any](t *testing.T, ch <-chan T, d time.Duration) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected value %+v", v)
	case <-time.After(d):
	}
}

// waitFor polls cond for up to 2s.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	waitForTimeout(t, 2*time.Second, cond)
}

// waitForTimeout polls cond every 5ms until it holds or d elapses.
func waitForTimeout(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %v", d)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// expectStable fails if get() changes during d.
func expectStable[T comparable](t *testing.T, get func() []T, d time.Duration) {
	t.Helper()
	want := get()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if got := get(); !slices.Equal(got, want) {
			t.Fatalf("value changed from %v to %v", want, got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// lockedRecorder is a mutex-guarded notify.Backend that keeps every notification.
type lockedRecorder struct {
	mu    sync.Mutex
	shown []notify.OSNotification
}

func (r *lockedRecorder) Show(n notify.OSNotification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shown = append(r.shown, n)
	return nil
}

// ids returns each shown AlertID in order.
func (r *lockedRecorder) ids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.shown))
	for _, n := range r.shown {
		out = append(out, n.AlertID)
	}
	return out
}

// all returns a copy of every shown notification.
func (r *lockedRecorder) all() []notify.OSNotification {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.shown)
}

// slowSink sleeps delay per Handle and records alert IDs in call order.
type slowSink struct {
	delay time.Duration
	mu    sync.Mutex
	seen  []string
}

func (s *slowSink) Handle(a api.Alert, _ notify.Input) (notify.Decision, error) {
	time.Sleep(s.delay)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, a.ID)
	return notify.Popup, nil
}

func (s *slowSink) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.seen)
}

// runHandlerWith starts a handler on sink with clock t0 and desktop=true.
func runHandlerWith(t *testing.T, sink alertSink, probe func() (bool, error)) *handler {
	t.Helper()
	now := func() time.Time { return t0 }
	h := newHandler(sink, newPresence(probe, now), func() bool { return true }, now, time.After,
		func(int64) {}, slog.New(slog.DiscardHandler))
	go h.run(t.Context())
	return h
}
