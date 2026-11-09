package apitool

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func drain(t *testing.T, ch <-chan RunEvent) (lines []RunEvent, done RunEvent) {
	t.Helper()
	timeout := time.After(20 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				if !done.Done {
					t.Fatal("channel closed without a Done event")
				}
				return lines, done
			}
			if e.Done {
				if done.Done {
					t.Fatal("two Done events")
				}
				done = e
				continue
			}
			if done.Done {
				t.Fatal("event after Done")
			}
			lines = append(lines, e)
		case <-timeout:
			t.Fatal("runner did not finish")
		}
	}
}

// runCol is a collection over base with requests r1 (root), f1/r2, f1/r3.
func runCol(base string) *Collection {
	return &Collection{
		ID: "col1", Name: "pay",
		Vars: []Var{{Name: "base_url", Value: base}},
		Root: Folder{ID: "root", Items: []Node{
			{Request: "r1"},
			{Folder: &Folder{ID: "f1", Name: "sub", Items: []Node{{Request: "r2"}, {Request: "r3"}}}},
		}},
		Requests: map[string]*Request{
			"r1": {ID: "r1", Name: "one", Method: "GET", URL: "{{base_url}}/1"},
			"r2": {ID: "r2", Name: "two", Method: "POST", URL: "{{base_url}}/2"},
			"r3": {ID: "r3", Name: "three", Method: "GET", URL: "{{base_url}}/3"},
		},
	}
}

func colScope(c *Collection) *Scope { return &Scope{Collection: c.Vars, CollectionID: c.ID} }

func realRunner(t *testing.T) *Runner {
	s := newSender(t)
	return &Runner{Send: s.Send, Dyn: fixedDyn()}
}

func TestRunnerSequentialOrder(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var inflight, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	c := runCol(srv.URL)
	r := realRunner(t)
	lines, done := drain(t, r.Start(context.Background(), Snapshot(c, "", colScope(c)), RunOptions{Iterations: 2}))
	want := []string{"GET /1", "POST /2", "GET /3", "GET /1", "POST /2", "GET /3"}
	if !slices.Equal(paths, want) {
		t.Fatalf("order = %v", paths)
	}
	if peak.Load() != 1 {
		t.Fatalf("peak concurrency = %d", peak.Load())
	}
	if len(lines) != 6 || lines[3].Iter != 1 || lines[3].Index != 0 || lines[1].Name != "two" || lines[1].Status != 200 {
		t.Fatalf("lines = %+v", lines)
	}
	s := done.Summary
	if s == nil || s.Collection != "pay" || s.Requests != 6 || s.Passed != 6 || s.Failed != 0 || done.Err != nil {
		t.Fatalf("done = %+v summary %+v", done, s)
	}

	// A folder runs only its own requests.
	paths = nil
	_, done = drain(t, r.Start(context.Background(), Snapshot(c, "f1", colScope(c)), RunOptions{Iterations: 1}))
	if !slices.Equal(paths, []string{"POST /2", "GET /3"}) || done.Summary.Requests != 2 {
		t.Fatalf("folder run = %v", paths)
	}
}

func fakeSend(n *atomic.Int32) func(ctx context.Context, p *Prepared) (*Response, error) {
	return func(ctx context.Context, p *Prepared) (*Response, error) {
		n.Add(1)
		return &Response{Status: 200, Timing: Timing{Total: time.Millisecond}}, nil
	}
}

func TestRunnerIterationsCap(t *testing.T) {
	c := runCol("https://api.test")
	var n atomic.Int32
	r := &Runner{Send: fakeSend(&n), Dyn: fixedDyn()}
	for _, it := range []int{0, -1, 1001} {
		if err := (RunOptions{Iterations: it}).Validate(); !errors.Is(err, ErrIterations) {
			t.Fatalf("Validate(%d) = %v", it, err)
		}
		lines, done := drain(t, r.Start(context.Background(), Snapshot(c, "", colScope(c)), RunOptions{Iterations: it}))
		if !errors.Is(done.Err, ErrIterations) || len(lines) != 0 || n.Load() != 0 {
			t.Fatalf("Start(%d): err %v, %d lines, %d sends", it, done.Err, len(lines), n.Load())
		}
	}
	if err := (RunOptions{Iterations: 1000}).Validate(); err != nil {
		t.Fatal(err)
	}
	_, done := drain(t, r.Start(context.Background(), Snapshot(c, "", colScope(c)), RunOptions{Iterations: 1000}))
	if done.Err != nil || n.Load() != 3000 || done.Summary.Requests != 3000 {
		t.Fatalf("1000 iterations: %v, %d sends", done.Err, n.Load())
	}
	if time.Duration(done.Summary.Requests)*time.Millisecond != done.Summary.Total {
		t.Fatalf("Total = %v", done.Summary.Total)
	}
}

func TestRunnerStopOnFail(t *testing.T) {
	srv, hits := countServer(t, nil)
	c := runCol(srv.URL)
	c.Requests["r2"].Asserts = []Assertion{{ID: "a1", Target: "status", Op: "eq", Value: "201"}, {ID: "a2", Target: "status", Op: "eq", Value: "200"}}
	r := realRunner(t)
	lines, done := drain(t, r.Start(context.Background(), Snapshot(c, "", colScope(c)), RunOptions{Iterations: 3, StopOnFail: true}))
	if hits.Load() != 2 || len(lines) != 2 {
		t.Fatalf("hits = %d, lines = %d", hits.Load(), len(lines))
	}
	l := lines[1]
	if !l.Failed || l.Passed != 1 || l.Total != 2 || l.Reason != ReasonAsserts || l.Response == nil || l.Response.Status != 200 {
		t.Fatalf("failed line = %+v", l)
	}
	if lines[0].Failed || lines[0].Response != nil {
		t.Fatalf("passing line = %+v", lines[0])
	}
	s := done.Summary
	if s.Failed != 1 || s.Passed != 1 || s.Requests != 2 || len(s.Failures) != 1 {
		t.Fatalf("summary = %+v", s)
	}
	if f := s.Failures[0]; f.Name != "two" || f.Method != "POST" || f.Status != 200 || f.Reason != ReasonAsserts {
		t.Fatalf("failure = %+v", f)
	}

	// Without StopOnFail the run goes on.
	hits.Store(0)
	_, done = drain(t, r.Start(context.Background(), Snapshot(c, "", colScope(c)), RunOptions{Iterations: 1}))
	if hits.Load() != 3 || done.Summary.Failed != 1 || done.Summary.Passed != 2 {
		t.Fatalf("no stop: hits %d, %+v", hits.Load(), done.Summary)
	}
}

func TestRunnerUsesSnapshot(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/1" {
			<-release
		}
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	c := runCol(srv.URL)
	sc := colScope(c)
	ch := realRunner(t).Start(context.Background(), Snapshot(c, "", sc), RunOptions{Iterations: 1})
	// Change the live collection while the run is in flight.
	c.Requests["r2"].URL = "{{base_url}}/changed"
	c.Requests["r3"].Method = "DELETE"
	c.Root.Items[1].Folder.Items = c.Root.Items[1].Folder.Items[:1]
	c.Vars[0].Value = "https://elsewhere.test"
	sc.Collection[0].Value = "https://elsewhere.test"
	close(release)
	lines, _ := drain(t, ch)
	if !slices.Equal(paths, []string{"/1", "/2", "/3"}) || len(lines) != 3 || lines[2].Failed {
		t.Fatalf("paths = %v lines %+v", paths, lines)
	}
}

func TestSnapshotDeepCopy(t *testing.T) {
	c := runCol("https://a.test")
	c.Requests["r1"].Headers = []KV{{Key: "A", Value: "1"}}
	c.Envs = []Env{{ID: "e", Vars: []Var{{Name: "x", AllowedHosts: []string{"h"}}}}}
	sc := &Scope{Temp: map[string]string{"t": "1"}, Mine: &Mine{Values: map[string]string{"e/x": "1"}, Hosts: map[string][]string{"e/x": {"h"}}},
		Env: &Env{ID: "e", Vars: []Var{{Name: "x", AllowedHosts: []string{"h"}}}}, Globals: []Var{{Name: "g"}}}
	snap := Snapshot(c, "", sc)
	c.Requests["r1"].Headers[0].Value = "2"
	c.Envs[0].Vars[0].AllowedHosts[0] = "evil"
	sc.Temp["t"] = "2"
	sc.Mine.Values["e/x"] = "2"
	sc.Mine.Hosts["e/x"][0] = "evil"
	sc.Env.Vars[0].AllowedHosts[0] = "evil"
	sc.Globals[0].Name = "changed"
	if snap.Collection.Requests["r1"].Headers[0].Value != "1" || snap.Collection.Envs[0].Vars[0].AllowedHosts[0] != "h" ||
		snap.Scope.Temp["t"] != "1" || snap.Scope.Mine.Values["e/x"] != "1" || snap.Scope.Mine.Hosts["e/x"][0] != "h" ||
		snap.Scope.Env.Vars[0].AllowedHosts[0] != "h" || snap.Scope.Globals[0].Name != "g" {
		t.Fatalf("snapshot shares memory: %+v", snap.Scope)
	}
	if snap.Scope == sc || snap.Collection == c {
		t.Fatal("same pointers")
	}
}

func TestRunnerHostsPreview(t *testing.T) {
	c := runCol("https://b.test")
	c.Requests["r2"].URL = "https://A.test:8443/x"
	c.Requests["r3"].URL = "http://a.test/y"
	c.Root.Items = append(c.Root.Items, Node{Request: "r4"})
	c.Requests["r4"] = &Request{ID: "r4", Method: "GET", URL: "{{base_url}}/again"}
	hosts, err := RunHosts(Snapshot(c, "", colScope(c)), fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(hosts, []string{"a.test", "b.test"}) {
		t.Fatalf("hosts = %v", hosts)
	}
	hosts, err = RunHosts(Snapshot(c, "f1", colScope(c)), fixedDyn())
	if err != nil || !slices.Equal(hosts, []string{"a.test"}) {
		t.Fatalf("folder hosts = %v %v", hosts, err)
	}
	// An undefined variable is reported; the other hosts are still listed.
	c.Requests["r3"].URL = "{{nope}}/y"
	hosts, err = RunHosts(Snapshot(c, "", colScope(c)), fixedDyn())
	var ue *UndefinedError
	if !errors.As(err, &ue) || ue.Name != "nope" || !slices.Equal(hosts, []string{"a.test", "b.test"}) {
		t.Fatalf("hosts = %v err %v", hosts, err)
	}
}

func TestRunnerDelayAndCancel(t *testing.T) {
	c := runCol("https://api.test")
	var n atomic.Int32
	var sleeps []time.Duration
	var order []string
	r := &Runner{
		Send: func(ctx context.Context, p *Prepared) (*Response, error) {
			n.Add(1)
			order = append(order, "send")
			return &Response{Status: 200}, nil
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			order = append(order, "sleep")
			return nil
		},
		Dyn: fixedDyn(),
	}
	_, done := drain(t, r.Start(context.Background(), Snapshot(c, "", colScope(c)), RunOptions{Iterations: 2, Delay: 7 * time.Millisecond}))
	if done.Err != nil || n.Load() != 6 || len(sleeps) != 5 || sleeps[0] != 7*time.Millisecond {
		t.Fatalf("sends %d sleeps %v err %v", n.Load(), sleeps, done.Err)
	}
	if order[0] != "send" || order[len(order)-1] != "send" {
		t.Fatalf("order = %v", order)
	}

	// No delay: Sleep is never called.
	sleeps = nil
	drain(t, r.Start(context.Background(), Snapshot(c, "", colScope(c)), RunOptions{Iterations: 1}))
	if len(sleeps) != 0 {
		t.Fatalf("sleeps without delay = %v", sleeps)
	}

	// Cancel after the first send: a Done event, nothing more sent.
	n.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Send = func(ctx context.Context, p *Prepared) (*Response, error) {
		n.Add(1)
		cancel()
		return &Response{Status: 200}, nil
	}
	lines, done := drain(t, r.Start(ctx, Snapshot(c, "", colScope(c)), RunOptions{Iterations: 5, Delay: time.Millisecond}))
	if n.Load() != 1 || !errors.Is(done.Err, context.Canceled) || done.Summary == nil || done.Summary.Requests > 1 || len(lines) > 1 {
		t.Fatalf("cancel: sends %d err %v summary %+v lines %d", n.Load(), done.Err, done.Summary, len(lines))
	}

	// Cancel during a send: the interrupted request is not a failure.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	r.Send = func(ctx context.Context, p *Prepared) (*Response, error) {
		cancel2()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	_, done = drain(t, r.Start(ctx2, Snapshot(c, "", colScope(c)), RunOptions{Iterations: 1}))
	if !errors.Is(done.Err, context.Canceled) || done.Summary.Failed != 0 || done.Summary.Requests != 0 {
		t.Fatalf("cancel mid-send: %+v %v", done.Summary, done.Err)
	}

	// The default sleeper stops on cancel.
	r2 := &Runner{Send: fakeSend(&n), Dyn: fixedDyn()}
	ctx3, cancel3 := context.WithCancel(context.Background())
	n.Store(0)
	ch := r2.Start(ctx3, Snapshot(c, "", colScope(c)), RunOptions{Iterations: 1, Delay: time.Hour})
	time.AfterFunc(50*time.Millisecond, cancel3)
	_, done = drain(t, ch)
	if n.Load() != 1 || !errors.Is(done.Err, context.Canceled) {
		t.Fatalf("default sleep cancel: sends %d err %v", n.Load(), done.Err)
	}
}

func TestRunnerExtractChains(t *testing.T) {
	var gotAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"tok-1","plain":"p-9"}`)
		default:
			gotAuth = append(gotAuth, r.Header.Get("Authorization")+"|"+r.Header.Get("X-Plain"))
		}
	}))
	t.Cleanup(srv.Close)
	host := urlHost(srv.URL)
	c := &Collection{ID: "c", Name: "chain",
		Root: Folder{Items: []Node{{Request: "login"}, {Request: "me"}}},
		Requests: map[string]*Request{
			"login": {ID: "login", Method: "POST", URL: "{{base_url}}/login", Extracts: []Extract{
				{From: "json", Path: "$.access_token", Var: "token"}, {From: "json", Path: "$.plain", Var: "plain"}}},
			"me": {ID: "me", Method: "GET", URL: "{{base_url}}/me", Headers: []KV{
				{Key: "Authorization", Value: "Bearer {{token}}"}, {Key: "X-Plain", Value: "{{plain}}"}}},
		},
	}
	vault := testVault(t)
	sc := &Scope{Vault: vault, Env: &Env{ID: "e1", Vars: []Var{
		{Name: "base_url", Value: srv.URL},
		{Name: "token", Secret: true, AllowedHosts: []string{host}},
		{Name: "plain", Value: "old"},
	}}}
	snap := Snapshot(c, "", sc)
	_, done := drain(t, realRunner(t).Start(context.Background(), snap, RunOptions{Iterations: 1}))
	if done.Summary.Failed != 0 || len(gotAuth) != 1 || gotAuth[0] != "Bearer tok-1|p-9" {
		t.Fatalf("auth = %v summary %+v", gotAuth, done.Summary)
	}
	if v, err := vault.Get(SecretKey("", "e1", "token")); err != nil || v.Reveal() != "tok-1" {
		t.Fatalf("vault = %v %v", v, err)
	}
	if sc.Env.Vars[2].Value != "old" || snap.Scope.Env.Vars[2].Value != "p-9" {
		t.Fatalf("plain value: live %q snapshot %q", sc.Env.Vars[2].Value, snap.Scope.Env.Vars[2].Value)
	}
}

func TestRunnerGuardErrorIsFailure(t *testing.T) {
	evil, hits := countServer(t, nil)
	c := runCol("")
	c.Root.Items = c.Root.Items[:1]
	c.Requests["r1"].Headers = []KV{{Key: "X-Api-Key", Value: "{{key}}"}}
	sc := secretScope(t, evil.URL, "api.test")
	sc.CollectionID = c.ID
	r := realRunner(t)
	lines, done := drain(t, r.Start(context.Background(), Snapshot(c, "", sc), RunOptions{Iterations: 1}))
	if hits.Load() != 0 {
		t.Fatalf("evil got %d requests", hits.Load())
	}
	var he *HostNotAllowedError
	if len(lines) != 1 || !lines[0].Failed || lines[0].Reason != ReasonBlocked || !errors.As(lines[0].Err, &he) {
		t.Fatalf("lines = %+v", lines)
	}
	if done.Summary.Failed != 1 || done.Summary.Failures[0].Reason != ReasonBlocked || done.Err != nil {
		t.Fatalf("summary = %+v", done.Summary)
	}

	// A foreign request to a private target fails as "private" unless that
	// host was confirmed before the run.
	c2 := runCol("http://internal.example")
	c2.Root.Items = c2.Root.Items[:1]
	s := newSender(t)
	s.Resolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	r2 := &Runner{Send: s.Send, Dyn: fixedDyn(), Foreign: func(id string) (string, bool) { return "hana", true }}
	lines, _ = drain(t, r2.Start(context.Background(), Snapshot(c2, "", colScope(c2)), RunOptions{Iterations: 1}))
	var pe *PrivateTargetError
	if len(lines) != 1 || lines[0].Reason != ReasonPrivate || !errors.As(lines[0].Err, &pe) || pe.LastBy != "hana" {
		t.Fatalf("private lines = %+v", lines)
	}
	r2.PrivateOK = map[string]bool{"internal.example": true}
	lines, _ = drain(t, r2.Start(context.Background(), Snapshot(c2, "", colScope(c2)), RunOptions{Iterations: 1}))
	if len(lines) != 1 || lines[0].Reason == ReasonPrivate {
		t.Fatalf("confirmed host still blocked: %+v", lines)
	}

	// An undefined variable is a failed line, not a crash.
	c3 := runCol("x")
	c3.Requests["r1"].URL = "{{missing}}/x"
	c3.Root.Items = c3.Root.Items[:1]
	lines, _ = drain(t, r.Start(context.Background(), Snapshot(c3, "", colScope(c3)), RunOptions{Iterations: 1}))
	if lines[0].Reason != ReasonUndefined || !strings.Contains(lines[0].Err.Error(), "missing") {
		t.Fatalf("undefined line = %+v", lines[0])
	}
}
