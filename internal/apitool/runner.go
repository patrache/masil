package apitool

import (
	"context"
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"time"
)

// MaxIterations caps a run (C3.13: not a load tester).
const MaxIterations = 1000

// maxRunFailures caps RunSummary.Failures (the card shows 3).
const maxRunFailures = 100

// ErrIterations: RunOptions.Iterations is outside 1..MaxIterations.
var ErrIterations = errors.New("apitool: iterations must be 1..1000")

// Reasons of a failed run line (RunFailure.Reason, RunEvent.Reason); the
// screen turns them into catalog text.
const (
	ReasonAsserts   = "asserts"   // a check failed
	ReasonBlocked   = "blocked"   // a secret would go to a host not allowed
	ReasonPrivate   = "private"   // a foreign request to a private target, not confirmed
	ReasonRedirect  = "redirect"  // a redirect stopped for a secret
	ReasonUndefined = "undefined" // a {{variable}} without a value
	ReasonTimeout   = "timeout"
	ReasonExtract   = "extract" // an extracted value could not be stored
	ReasonError     = "error"   // any other preparing or sending error
)

// RunOptions: Iterations 1..1000, Delay between two requests, StopOnFail.
type RunOptions struct {
	Iterations int
	Delay      time.Duration
	StopOnFail bool
}

// Validate checks the iteration count.
func (o RunOptions) Validate() error {
	if o.Iterations < 1 || o.Iterations > MaxIterations {
		return ErrIterations
	}
	return nil
}

// RunSnapshot is what a run uses, copied when it starts: changes to the live
// (shared) collection during the run do not reach it (C3.7). Extracted plain
// values land in Scope (the copy); secrets in the shared Vault.
type RunSnapshot struct {
	Collection *Collection
	FolderID   string // "" = the whole collection
	Scope      *Scope
}

// Snapshot deep-copies c and s for a run of folderID.
func Snapshot(c *Collection, folderID string, s *Scope) RunSnapshot {
	return RunSnapshot{Collection: cloneCollection(c), FolderID: folderID, Scope: cloneScope(s)}
}

// requests lists the snapshot's requests in tree order.
func (snap RunSnapshot) requests() []*Request {
	if snap.Collection == nil {
		return nil
	}
	f := findFolder(&snap.Collection.Root, snap.FolderID)
	if f == nil {
		return nil
	}
	var out []*Request
	for _, id := range f.RequestIDs() {
		if r := snap.Collection.Requests[id]; r != nil {
			out = append(out, r)
		}
	}
	return out
}

func findFolder(f *Folder, id string) *Folder {
	if id == "" || f.ID == id {
		return f
	}
	for _, n := range f.Items {
		if n.Folder != nil {
			if got := findFolder(n.Folder, id); got != nil {
				return got
			}
		}
	}
	return nil
}

// RunHosts lists the hosts the run would send to (sorted, unique), for the
// confirmation before a shared collection runs (C3.10). Requests whose URL
// cannot be expanded are left out and their errors joined.
func RunHosts(snap RunSnapshot, d Dyn) ([]string, error) {
	set := map[string]bool{}
	var errs []error
	for _, r := range snap.requests() {
		u, _, err := Expand(r.URL, snap.Scope, d)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		u = strings.TrimSpace(u)
		if !strings.Contains(u, "://") {
			u = "http://" + u
		}
		if h := urlHost(u); h != "" {
			set[h] = true
		}
	}
	return slices.Sorted(maps.Keys(set)), errors.Join(errs...)
}

// Runner runs a snapshot sequentially, one request at a time. Send is
// usually Sender.Send; Sleep waits between requests (nil = a timer that
// stops on cancel); Foreign tells who last changed a request (shared
// collections); PrivateOK are the hosts confirmed in the host list.
type Runner struct {
	Send      func(ctx context.Context, p *Prepared) (*Response, error)
	Sleep     func(ctx context.Context, d time.Duration) error
	Dyn       Dyn
	Foreign   func(reqID string) (lastBy string, foreign bool)
	PrivateOK map[string]bool
}

// RunEvent is one finished request, or (Done) the end of the run with its
// Summary; Err on Done is ErrIterations or the context's error. A failed
// line carries its Response (spill file removed) so the screen can open it.
type RunEvent struct {
	Iter, Index     int
	RequestID, Name string
	Status          int
	Time            time.Duration
	Passed, Total   int // checks
	Err             error
	Done            bool
	Summary         *RunSummary

	Failed   bool
	Reason   string
	Response *Response
}

// Start runs snap in its own goroutine. The channel (buffer 64) gets one
// event per request and a final Done event, then closes; the caller drains
// it until closed. Cancel ctx to stop: the request in flight is dropped
// and Done follows.
func (r *Runner) Start(ctx context.Context, snap RunSnapshot, o RunOptions) <-chan RunEvent {
	ch := make(chan RunEvent, 64)
	go func() {
		defer close(ch)
		sum := RunSummary{}
		if snap.Collection != nil {
			sum.Collection = snap.Collection.Name
		}
		if err := o.Validate(); err != nil {
			ch <- RunEvent{Done: true, Err: err, Summary: &sum}
			return
		}
		r.run(ctx, snap, o, &sum, ch)
		ch <- RunEvent{Done: true, Err: ctx.Err(), Summary: &sum}
	}()
	return ch
}

func (r *Runner) run(ctx context.Context, snap RunSnapshot, o RunOptions, sum *RunSummary, ch chan<- RunEvent) {
	reqs := snap.requests()
	scope := snap.Scope
	if scope == nil {
		scope = &Scope{}
	}
	colID := ""
	if snap.Collection != nil {
		colID = snap.Collection.ID
	}
	first := true
	for iter := 0; iter < o.Iterations; iter++ {
		for idx, req := range reqs {
			if ctx.Err() != nil {
				return
			}
			if !first && o.Delay > 0 {
				if err := r.sleep(ctx, o.Delay); err != nil || ctx.Err() != nil {
					return
				}
			}
			first = false
			ev := r.one(ctx, colID, req, scope)
			if ctx.Err() != nil && ev.Err != nil {
				return // interrupted, not a failure
			}
			ev.Iter, ev.Index = iter, idx
			sum.Requests++
			sum.Total += ev.Time
			if ev.Failed {
				sum.Failed++
				if len(sum.Failures) < maxRunFailures {
					sum.Failures = append(sum.Failures, RunFailure{Name: ev.Name, Method: method(req), Status: ev.Status, Reason: ev.Reason})
				}
			} else {
				sum.Passed++
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
			if ev.Failed && o.StopOnFail {
				return
			}
		}
	}
}

func method(r *Request) string {
	if m := strings.ToUpper(strings.TrimSpace(r.Method)); m != "" {
		return m
	}
	return "GET"
}

func (r *Runner) sleep(ctx context.Context, d time.Duration) error {
	if r.Sleep != nil {
		return r.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// one prepares, guards, sends, checks and extracts one request.
func (r *Runner) one(ctx context.Context, colID string, req *Request, scope *Scope) RunEvent {
	ev := RunEvent{RequestID: req.ID, Name: req.Name, Total: len(req.Asserts)}
	fail := func(reason string, err error) RunEvent {
		ev.Failed, ev.Reason, ev.Err = true, reason, err
		return ev
	}
	p, err := Prepare(req, colID, scope, r.Dyn)
	if err != nil {
		return fail(errReason(err), err)
	}
	if r.Foreign != nil {
		p.LastBy, p.Foreign = r.Foreign(req.ID)
	}
	p.PrivateOK = r.PrivateOK[urlHost(p.URL)]
	if err := CheckSecretHosts(p); err != nil {
		return fail(ReasonBlocked, err)
	}
	resp, err := r.Send(ctx, p)
	if resp != nil && resp.Spill != "" {
		_ = os.Remove(resp.Spill)
		resp.Spill = ""
	}
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return fail(ReasonTimeout, err)
		}
		return fail(errReason(err), err)
	}
	ev.Status = resp.Status
	ev.Time = resp.Timing.Total
	for _, a := range Check(req.Asserts, resp) {
		if a.Pass {
			ev.Passed++
		}
	}
	if vals, _ := ExtractAll(req.Extracts, resp); len(vals) > 0 {
		if _, err := ApplyExtracts(vals, scope); err != nil {
			ev.Response = resp
			return fail(ReasonExtract, err)
		}
	}
	switch {
	case resp.RedirectBlocked != nil:
		ev.Response = resp
		return fail(ReasonRedirect, nil)
	case ev.Passed < ev.Total:
		ev.Response = resp
		return fail(ReasonAsserts, nil)
	}
	return ev
}

func errReason(err error) string {
	var he *HostNotAllowedError
	var pe *PrivateTargetError
	var ue *UndefinedError
	switch {
	case errors.As(err, &he):
		return ReasonBlocked
	case errors.As(err, &pe):
		return ReasonPrivate
	case errors.As(err, &ue):
		return ReasonUndefined
	}
	return ReasonError
}

// --- deep copies ---

func cloneVars(vs []Var) []Var {
	out := slices.Clone(vs)
	for i := range out {
		out[i].AllowedHosts = slices.Clone(out[i].AllowedHosts)
	}
	return out
}

func cloneEnv(e *Env) *Env {
	if e == nil {
		return nil
	}
	out := *e
	out.Vars = cloneVars(e.Vars)
	return &out
}

func cloneFolder(f Folder) Folder {
	out := f
	out.Items = slices.Clone(f.Items)
	for i, n := range out.Items {
		if n.Folder != nil {
			sub := cloneFolder(*n.Folder)
			out.Items[i].Folder = &sub
		}
	}
	return out
}

func cloneCollection(c *Collection) *Collection {
	if c == nil {
		return nil
	}
	out := *c
	out.Vars = cloneVars(c.Vars)
	out.Root = cloneFolder(c.Root)
	if c.Requests != nil {
		out.Requests = make(map[string]*Request, len(c.Requests))
		for id, r := range c.Requests {
			if r != nil {
				cp := cloneRequest(r)
				out.Requests[id] = &cp
			}
		}
	}
	out.Envs = slices.Clone(c.Envs)
	for i := range out.Envs {
		out.Envs[i].Vars = cloneVars(out.Envs[i].Vars)
	}
	return &out
}

func cloneScope(s *Scope) *Scope {
	if s == nil {
		return &Scope{}
	}
	out := *s
	out.Temp = maps.Clone(s.Temp)
	if s.Mine != nil {
		m := Mine{Values: maps.Clone(s.Mine.Values)}
		if s.Mine.Hosts != nil {
			m.Hosts = make(map[string][]string, len(s.Mine.Hosts))
			for k, v := range s.Mine.Hosts {
				m.Hosts[k] = slices.Clone(v)
			}
		}
		out.Mine = &m
	}
	out.Env = cloneEnv(s.Env)
	out.Collection = cloneVars(s.Collection)
	out.Globals = cloneVars(s.Globals)
	return &out
}
