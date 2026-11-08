package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/patrache/masil/internal/instance"
)

func TestPresenceDecide(t *testing.T) {
	errLock := errors.New("probe failed")
	tru, fal := true, false
	cases := []struct {
		name     string
		local    bool
		localErr error
		server   *bool         // final server signal
		flipAgo  time.Duration // >0: server went true->false this long before t0
		want     bool
		wantWait time.Duration
	}{
		{"lock_held_server_unknown", true, nil, nil, 0, true, 0},
		{"lock_held_server_false", true, nil, &fal, 0, true, 0},
		{"lock_free_beats_stale_server_true", false, nil, &tru, 0, false, 0},
		{"lock_free_no_grace_needed", false, nil, &fal, time.Second, false, 0},
		{"lock_error_server_unknown", false, errLock, nil, 0, false, 0},
		{"lock_error_server_true", false, errLock, &tru, 0, true, 0},
		{"lock_error_in_grace", false, errLock, &fal, time.Second, false, 2 * time.Second},
		{"lock_error_after_grace", false, errLock, &fal, 3 * time.Second, false, 0},
		{"lock_error_hello_false_no_grace", false, errLock, &fal, 0, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clk := &fakeClock{now: t0}
			p := newPresence(func() (bool, error) { return c.local, c.localErr }, clk.Now)
			if c.flipAgo > 0 {
				clk.Set(t0.Add(-c.flipAgo - time.Second))
				p.observe(&tru)
				clk.Set(t0.Add(-c.flipAgo))
				p.observe(&fal)
			} else if c.server != nil {
				p.observe(c.server)
			}
			clk.Set(t0)
			got, wait := p.decide()
			if got != c.want || wait != c.wantWait {
				t.Fatalf("decide() = %v, %v; want %v, %v", got, wait, c.want, c.wantWait)
			}
		})
	}
}

func TestPresenceRealTUILock(t *testing.T) {
	p := testPaths(t)
	pr := newPresence(func() (bool, error) { return instance.TUIRunning(p) }, time.Now)
	if got, _ := pr.decide(); got {
		t.Fatal("no TUI yet, decide() = true")
	}
	release, err := instance.AcquireTUI(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := pr.decide(); !got {
		t.Fatal("TUI lock held, decide() = false")
	}
	release()
	if got, _ := pr.decide(); got {
		t.Fatal("TUI lock released, decide() = true")
	}
}
