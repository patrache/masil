package gittool_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/lazygit"
	"github.com/patrache/masil/internal/ui/gittool"
	"github.com/patrache/masil/internal/ui/uitest"
)

// versionRunner answers "<lgBin> --version" with out, after release is
// closed (nil: at once), and counts the calls.
type versionRunner struct {
	out     string
	release chan struct{}
	mu      sync.Mutex
	n       int
}

func (r *versionRunner) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	if r.release != nil {
		<-r.release
	}
	return []byte(r.out), nil
}

func (r *versionRunner) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func versionEnv(r *versionRunner) lazygit.Env {
	env, _ := fakeEnv(true, "darwin")
	env.Run = r
	return env
}

// The hub draws without waiting for "lazygit --version": the lookup runs off
// the UI goroutine and the row shows its answer once it is in.
func TestHubDrawDoesNotWaitForLazygit(t *testing.T) {
	r := &versionRunner{out: "commit=abc, version=0.66.3, os=darwin\n", release: make(chan struct{})}
	setup(t, versionEnv(r))
	h := uitest.New(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Press("ctrl+t")
		_ = h.Screen()
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(r.release)
		<-done
		t.Fatal("the hub draw waited for lazygit --version")
	}
	close(r.release)
	gittool.WaitProbeForTest(t)
	if row := hubRow(t, h); !strings.Contains(row, "설치됨 v0.66.3") {
		t.Fatalf("hub row = %q", row)
	}
}

// A version lazygit does not report readably is asked once per binary, not
// again on every refresh.
func TestHubUnknownVersionAskedOnce(t *testing.T) {
	r := &versionRunner{out: "commit=abc, version=unversioned, os=darwin\n"}
	setup(t, versionEnv(r))
	h := uitest.New(t)
	h.Press("ctrl+t")
	for range 3 {
		_ = h.Screen()
		gittool.WaitProbeForTest(t)
		h.Advance(31 * time.Second)
	}
	if row := hubRow(t, h); !strings.Contains(row, "4 🌿 lazygit") || strings.Contains(row, "설치 필요") || strings.Contains(row, " v") {
		t.Fatalf("hub row = %q", row)
	}
	if n := r.calls(); n != 1 {
		t.Fatalf("lazygit --version ran %d times, want once", n)
	}
}
