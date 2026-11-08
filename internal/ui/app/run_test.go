package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/instance"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/widgets"
)

// runProbeKind is a test-only route the run lifecycle opens in Main. Query["do"]:
// "quit" => Init returns tea.Quit, "panic" => Update panics; Query["sig"] names a
// runSignals channel that receives a value on every View.
const runProbeKind ui.RouteKind = "core-test-runprobe"

var runSignals sync.Map // sig name -> chan struct{}

type runProbe struct {
	ui.Component
	route ui.Route
}

func (r *runProbe) Init(*ui.Context) tea.Cmd {
	if r.route.Query["do"] == "quit" {
		return tea.Quit
	}
	return nil
}

func (r *runProbe) Update(*ui.Context, tea.Msg) tea.Cmd {
	if r.route.Query["do"] == "panic" {
		panic("run probe: boom")
	}
	return nil
}

func (r *runProbe) View(ctx *ui.Context, focused bool) string {
	if ch, ok := runSignals.Load(r.route.Query["sig"]); ok {
		select {
		case ch.(chan struct{}) <- struct{}{}:
		default:
		}
	}
	return r.Component.View(ctx, focused)
}

func init() {
	ui.RegisterRoute(runProbeKind, func(_ *ui.Context, r ui.Route) ui.Component {
		return &runProbe{Component: widgets.Placeholder("run probe"), route: r}
	})
}

func runOptions(p config.Paths, do, sig string) app.Options {
	r := ui.Route{Kind: runProbeKind, Query: map[string]string{"do": do, "sig": sig}}
	return app.Options{
		Paths:           p,
		Config:          config.Defaults(),
		Credentials:     credsFor(fixtures.UserSam, "sam"),
		InitialRoute:    &r,
		DisableRealtime: true,
		Notifier:        notify.New(&notify.Recorder{}, notify.OpenLedger(p), slog.New(slog.DiscardHandler)),
	}
}

func testProgramOpts() []tea.ProgramOption {
	return []tea.ProgramOption{
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(io.Discard),
		tea.WithWindowSize(120, 40),
	}
}

// runAsync starts RunProgramForTest on its own goroutine.
func runAsync(ctx context.Context, o app.Options) <-chan error {
	done := make(chan error, 1)
	go func() { done <- app.RunProgramForTest(ctx, o, testProgramOpts()...) }()
	return done
}

func waitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("RunProgramForTest did not return within 10s")
		return nil
	}
}

func tuiRunning(t *testing.T, p config.Paths) bool {
	t.Helper()
	running, err := instance.TUIRunning(p)
	if err != nil {
		t.Fatalf("TUIRunning: %v", err)
	}
	return running
}

func assertSavedFor(t *testing.T, p config.Paths, user string) {
	t.Helper()
	b, err := os.ReadFile(p.State)
	if err != nil {
		t.Fatalf("state.json after Run: %v", err)
	}
	var s app.SavedState
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("state.json after Run: %v\n%s", err, b)
	}
	if s.Version != 1 || s.UserID != user {
		t.Fatalf("state.json after Run = %+v, want version 1 user %s", s, user)
	}
}

func TestRunLifecycle(t *testing.T) {
	t.Run("context canceled", func(t *testing.T) {
		p := statePaths(t)
		frames := make(chan struct{}, 1)
		runSignals.Store(t.Name(), frames)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := runAsync(ctx, runOptions(p, "", t.Name()))
		select {
		case <-frames:
		case err := <-done:
			t.Fatalf("Run returned before the first frame: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("no frame within 10s")
		}
		if !tuiRunning(t, p) {
			t.Fatal("TUIRunning = false while the program runs")
		}
		cancel()
		if err := waitRun(t, done); err != nil {
			t.Fatalf("Run after cancel = %v, want nil", err)
		}
		if tuiRunning(t, p) {
			t.Fatal("TUIRunning = true after Run returned (lock not released)")
		}
		assertSavedFor(t, p, fixtures.UserSam)
	})

	t.Run("quit", func(t *testing.T) {
		p := statePaths(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := waitRun(t, runAsync(ctx, runOptions(p, "quit", ""))); err != nil {
			t.Fatalf("Run after tea.Quit = %v, want nil", err)
		}
		if ctx.Err() != nil {
			t.Fatal("setup: ctx canceled")
		}
		if tuiRunning(t, p) {
			t.Fatal("lock not released after quit")
		}
		assertSavedFor(t, p, fixtures.UserSam)
	})

	t.Run("panic", func(t *testing.T) {
		p := statePaths(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := waitRun(t, runAsync(ctx, runOptions(p, "panic", "")))
		if err == nil || !errors.Is(err, tea.ErrProgramKilled) || !errors.Is(err, tea.ErrProgramPanic) {
			t.Fatalf("Run after a panic = %v, want ErrProgramKilled/ErrProgramPanic", err)
		}
		if tuiRunning(t, p) {
			t.Fatal("lock not released after a panic")
		}
		assertSavedFor(t, p, fixtures.UserSam)
	})

	t.Run("logged out writes no state", func(t *testing.T) {
		p := statePaths(t)
		o := runOptions(p, "", "")
		o.Credentials, o.InitialRoute = nil, nil
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if err := waitRun(t, runAsync(ctx, o)); err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
		if _, err := os.Stat(p.State); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("state.json written while logged out (stat err %v)", err)
		}
	})
}
