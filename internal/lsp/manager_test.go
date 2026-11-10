package lsp

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// managerFixture is a Manager whose Find answers the fake server and whose
// Start counts and remembers the clients it starts.
type managerFixture struct {
	m    *Manager
	clk  *fakeClock
	root string
	mu   sync.Mutex
	runs []*Client
	errs int
	// finds counts Find calls per language; a non-nil findGate makes Find
	// wait for it to close.
	finds    map[string]int
	findGate chan struct{}
}

func newManagerFixture(t *testing.T, modes ...string) *managerFixture {
	t.Helper()
	f := &managerFixture{clk: newFakeClock(), root: t.TempDir(), finds: map[string]int{}}
	srv := fakeServer(t, modes...)
	opts := Options{Clock: f.clk, Logger: slog.New(slog.DiscardHandler)}
	f.m = NewManager(ManagerOptions{
		Client: opts,
		Find: func(lang string) (Server, bool) {
			f.mu.Lock()
			f.finds[lang]++
			gate := f.findGate
			f.mu.Unlock()
			if gate != nil {
				<-gate
			}
			if lang != "go" {
				return Server{}, false
			}
			return srv, true
		},
		Start: func(ctx context.Context, s Server, root string) (*Client, error) {
			c, err := opts.Start(ctx, s, root)
			f.mu.Lock()
			defer f.mu.Unlock()
			if err != nil {
				f.errs++
				return nil, err
			}
			f.runs = append(f.runs, c)
			return c, nil
		},
	})
	t.Cleanup(func() { f.m.StopAll(context.Background()) })
	return f
}

func (f *managerFixture) starts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs) + f.errs
}

func (f *managerFixture) last() *Client {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.runs) == 0 {
		return nil
	}
	return f.runs[len(f.runs)-1]
}

// waitClient waits until path is served by a running client.
func (f *managerFixture) waitClient(t *testing.T, path string) *Client {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c := f.m.Client(path); c != nil {
			return c
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no client started")
	return nil
}

func waitDone(t *testing.T, c *Client) {
	t.Helper()
	select {
	case <-c.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("server still running")
	}
}

// completeEventually retries Complete while the server starts (each try is
// bounded by CompletionTimeout).
func completeEventually(t *testing.T, m *Manager, path string, line, col int) List {
	t.Helper()
	var err error
	for range 20 {
		var l List
		l, err = m.Complete(context.Background(), path, line, col)
		if err == nil {
			return l
		}
	}
	t.Fatalf("Complete kept failing: %v", err)
	return List{}
}

func TestManagerOpenCompleteClose(t *testing.T) {
	f := newManagerFixture(t)
	path := filepath.Join(f.root, "main.go")
	start := time.Now()
	if err := f.m.Open(f.root, path, "fmt.Pr"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("Open blocked for %v (the server must start in the background)", d)
	}
	l := completeEventually(t, f.m, path, 0, 6)
	if len(l.Items) != 2 || l.Items[0].Detail != "prefix=Pr" {
		t.Fatalf("items %+v", l.Items)
	}
	// A second file of the same language and root shares the server.
	path2 := filepath.Join(f.root, "sub", "b.go")
	if err := f.m.Open(f.root, path2, "x"); err != nil {
		t.Fatal(err)
	}
	f.m.Change(path, "fmt.Prin")
	l = completeEventually(t, f.m, path, 0, 8)
	if l.Items[0].Detail != "prefix=Prin" {
		t.Errorf("change not seen: %+v", l.Items[0])
	}
	c := f.waitClient(t, path2)
	st := state(t, c)
	if len(st.Docs) != 2 || f.starts() != 1 {
		t.Errorf("docs %v starts %d", st.Docs, f.starts())
	}
	f.m.Close(path2)
	if _, ok := state(t, c).Docs[URIFromPath(path2)]; ok {
		t.Error("closed doc still open on the server")
	}
	if _, err := f.m.Complete(context.Background(), path2, 0, 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Complete on a closed path: %v", err)
	}
}

func TestManagerRefusesNonLocal(t *testing.T) {
	f := newManagerFixture(t)
	outside := filepath.Join(t.TempDir(), "x.go")
	for name, args := range map[string][2]string{
		"relative root":  {"rel", filepath.Join(f.root, "a.go")},
		"relative path":  {f.root, "a.go"},
		"outside root":   {f.root, outside},
		"root itself":    {f.root, f.root},
		"empty":          {"", ""},
		"dotdot in path": {f.root, filepath.Join(f.root, "..", "x.go")},
	} {
		if err := f.m.Open(args[0], args[1], "x"); !errors.Is(err, ErrNotLocal) {
			t.Errorf("%s: %v, want ErrNotLocal", name, err)
		}
	}
	// A root that is not a folder, and a language without a server, are
	// found out on the start goroutine (Open does no file-system work): Open
	// accepts the file and Complete answers ErrUnavailable.
	file := filepath.Join(f.root, "f.go")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][2]string{
		"missing root":         {filepath.Join(f.root, "nope"), filepath.Join(f.root, "nope", "a.go")},
		"a file as root":       {file, filepath.Join(file, "a.go")},
		"no server for python": {f.root, filepath.Join(f.root, "a.py")},
	} {
		if err := f.m.Open(args[0], args[1], "x"); err != nil {
			t.Errorf("%s: Open %v", name, err)
		}
		if _, err := f.m.Complete(context.Background(), args[1], 0, 0); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: Complete %v, want ErrUnavailable", name, err)
		}
	}
	if err := f.m.Open(f.root, filepath.Join(f.root, "notes.txt"), "x"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("unknown language: %v", err)
	}
	if f.starts() != 0 {
		t.Errorf("%d servers started for refused files", f.starts())
	}
}

func TestManagerRestartLimit(t *testing.T) {
	f := newManagerFixture(t, "crash") // every completion kills the server
	path := filepath.Join(f.root, "main.go")
	if err := f.m.Open(f.root, path, "x"); err != nil {
		t.Fatal(err)
	}
	for i := range 1 + MaxRestarts {
		c := f.waitClient(t, path)
		if _, err := f.m.Complete(context.Background(), path, 0, 1); err == nil {
			t.Fatalf("run %d: completion on a crashing server succeeded", i)
		}
		waitDone(t, c)
		// The watcher drops the dead client; the next use restarts it.
		for f.m.Client(path) == c {
			time.Sleep(time.Millisecond)
		}
		if i < MaxRestarts {
			f.m.Change(path, "y") // next use restarts it
			nc := f.waitClient(t, path)
			if st := state(t, nc); st.Docs[URIFromPath(path)].Text != "y" {
				t.Fatalf("restart %d did not reopen the document with its text: %+v", i+1, st.Docs)
			}
		}
	}
	if got := f.starts(); got != 1+MaxRestarts {
		t.Fatalf("starts = %d, want %d", got, 1+MaxRestarts)
	}
	// Given up: quietly unavailable, no more starts.
	f.m.Change(path, "z")
	if _, err := f.m.Complete(context.Background(), path, 0, 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("after giving up: %v", err)
	}
	if err := f.m.Open(f.root, path, "z"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Open after giving up: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := f.starts(); got != 1+MaxRestarts {
		t.Errorf("started again after giving up: %d", got)
	}
}

func TestManagerStartFailuresCountAsCrashes(t *testing.T) {
	f := newManagerFixture(t, "crashstart")
	path := filepath.Join(f.root, "main.go")
	if err := f.m.Open(f.root, path, "x"); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		_, _ = f.m.Complete(context.Background(), path, 0, 0)
	}
	if got := f.starts(); got != 1+MaxRestarts {
		t.Errorf("starts = %d, want %d", got, 1+MaxRestarts)
	}
}

func TestManagerIdleShutdown(t *testing.T) {
	f := newManagerFixture(t)
	path := filepath.Join(f.root, "main.go")
	if err := f.m.Open(f.root, path, "x"); err != nil {
		t.Fatal(err)
	}
	c := f.waitClient(t, path)

	f.clk.Advance(6 * time.Minute)
	completeEventually(t, f.m, path, 0, 1) // used at minute 6
	f.clk.Advance(6 * time.Minute)         // minute 12: only 6 unused
	select {
	case <-c.Done():
		t.Fatal("stopped although used 6 minutes ago")
	default:
	}
	f.clk.Advance(4 * time.Minute) // minute 16: 10 unused
	waitDone(t, c)
	if !c.Stopping() || c.Err() != nil {
		t.Errorf("idle stop was not a clean shutdown: %v", c.Err())
	}
	if f.m.Client(path) != nil {
		t.Error("idle server still listed")
	}

	// Next use starts a fresh server (not counted as a restart) with the document reopened.
	f.m.Change(path, "after idle")
	nc := f.waitClient(t, path)
	if nc == c || f.starts() != 2 {
		t.Fatalf("starts %d", f.starts())
	}
	if d := state(t, nc).Docs[URIFromPath(path)]; d.Text != "after idle" || d.Version != 1 {
		t.Errorf("reopened doc %+v", d)
	}
	f.m.mu.Lock()
	e := f.m.servers[serverKey{"go", f.root}]
	f.m.mu.Unlock()
	if e == nil || e.restarts != 0 {
		t.Errorf("idle restart counted as a crash restart: %+v", e)
	}
}

func TestManagerStopAll(t *testing.T) {
	f := newManagerFixture(t)
	root2 := t.TempDir()
	p1, p2 := filepath.Join(f.root, "a.go"), filepath.Join(root2, "b.go")
	if err := f.m.Open(f.root, p1, "x"); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Open(root2, p2, "x"); err != nil {
		t.Fatal(err)
	}
	c1, c2 := f.waitClient(t, p1), f.waitClient(t, p2)
	if c1 == c2 {
		t.Fatal("two roots share a server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f.m.StopAll(ctx)
	for _, c := range []*Client{c1, c2} {
		select {
		case <-c.Done():
		default:
			t.Fatal("StopAll returned with a server running")
		}
	}
	if err := f.m.Open(f.root, p1, "x"); !errors.Is(err, ErrClosed) {
		t.Errorf("Open after StopAll: %v", err)
	}
	if _, err := f.m.Complete(context.Background(), p1, 0, 0); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Complete after StopAll: %v", err)
	}
	if f.clk.pending() != 0 {
		t.Errorf("%d timers left after StopAll", f.clk.pending())
	}
}

func TestManagerStopAllDuringStart(t *testing.T) {
	f := newManagerFixture(t)
	path := filepath.Join(f.root, "a.go")
	if err := f.m.Open(f.root, path, "x"); err != nil {
		t.Fatal(err)
	}
	f.m.StopAll(context.Background()) // likely while the start is in flight
	if c := f.last(); c != nil {
		waitDone(t, c)
	}
}

func TestSharedStopAll(t *testing.T) {
	StopAll(context.Background()) // never used: a no-op
	if Shared() != Shared() {
		t.Error("Shared is not one manager")
	}
	StopAll(context.Background())
	if err := Shared().Open(t.TempDir(), filepath.Join(t.TempDir(), "x.go"), ""); !errors.Is(err, ErrNotLocal) && !errors.Is(err, ErrClosed) {
		t.Errorf("after StopAll: %v", err)
	}
}

// TestManagerOpenDoesNoLookup: Open never runs Find (a PATH scan; on
// Windows every PATHEXT per folder) on the caller's goroutine, and a
// language without a server is looked up once, not on every Open.
func TestManagerOpenDoesNoLookup(t *testing.T) {
	f := newManagerFixture(t)
	gate := make(chan struct{})
	f.mu.Lock()
	f.findGate = gate
	f.mu.Unlock()
	path := filepath.Join(f.root, "main.go")
	opened := make(chan error, 1)
	go func() { opened <- f.m.Open(f.root, path, "fmt.Pr") }()
	select {
	case err := <-opened:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(gate)
		t.Fatal("Open waited for the PATH lookup")
	}
	close(gate)
	if l := completeEventually(t, f.m, path, 0, 6); len(l.Items) != 2 {
		t.Fatalf("items %+v", l.Items)
	}

	py := filepath.Join(f.root, "a.py")
	for range 3 {
		_ = f.m.Open(f.root, py, "x")
		_, _ = f.m.Complete(context.Background(), py, 0, 0)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finds["python"] != 1 || f.finds["go"] != 1 {
		t.Errorf("Find calls %v, want one per language", f.finds)
	}
}

// TestManagerStopAllKillsStartingServer: a server still in its initialize
// handshake (jdtls, metals) is killed within StopAll's budget, not left
// running for InitTimeout after masil exits.
func TestManagerStopAllKillsStartingServer(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	f := newManagerFixture(t, "slowinit", pidfileMode+pidfile)
	path := filepath.Join(f.root, "a.go")
	if err := f.m.Open(f.root, path, "x"); err != nil {
		t.Fatal(err)
	}
	pid := 0
	for deadline := time.Now().Add(15 * time.Second); pid == 0; {
		if b, err := os.ReadFile(pidfile); err == nil && len(b) > 0 {
			pid, _ = strconv.Atoi(string(b))
		}
		if time.Now().After(deadline) {
			t.Fatal("fake server never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	f.m.StopAll(ctx)
	if d := time.Since(start); d >= 2*time.Second {
		t.Errorf("StopAll used its whole budget (%v)", d)
	}
	if processAlive(pid) {
		t.Fatalf("StopAll returned with the starting server (pid %d) still running", pid)
	}
}
