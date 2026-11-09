// Package challenge is the optional daily architecture challenge (v0.1.3 spec
// C2.12): 15 built-in scenarios (a start design with its traffic, scheduled
// faults and seed, plus p99, error and monthly budget goals), one per UTC day,
// and the integer score of a timed run against them. There is no leaderboard;
// a score is shared on an arch.sim card. Scenario names live in the
// archcanvas catalog (challenge_<id>_title); this package has no text.
package challenge

import (
	"embed"
	"encoding/json"
	"errors"
	"math"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim"
)

// Scenario is one challenge. Start carries Seed and Faults too, so the canvas
// opened on it shows the scheduled faults.
type Scenario struct {
	ID              string
	Seed            uint64
	Start           archdoc.Design
	Faults          []archdoc.Fault
	GoalP99Ms       int // a second is kept when its p99 is at most this …
	GoalErrPermille int // … and its error share at most this
	BudgetUSDMonth  int // over budget, the score shrinks by budget/cost
}

// ErrClientMissing: the design lost a client of the scenario (its traffic is
// part of the challenge).
var ErrClientMissing = errors.New("challenge: a scenario client is missing")

// ErrUnrouted: some of a scenario client's traffic stops at a node that does
// not serve requests itself — a client, load balancer, CDN or queue whose
// outgoing edges do not carry all of it, or that only loops back. The engine
// counts traffic that ends there as served, at no latency or cost.
var ErrUnrouted = errors.New("challenge: a scenario client's traffic does not reach a server")

// ErrFaultTargetMissing: the design lost what a scheduled fault hits (the
// fault is part of the challenge, and the run design must stay valid).
var ErrFaultTargetMissing = errors.New("challenge: a scheduled fault's target is missing")

//go:embed scenarios/*.json
var files embed.FS

// file is one scenarios/<id>.json.
type file struct {
	ID              string          `json:"id"`
	GoalP99Ms       int             `json:"goal_p99_ms"`
	GoalErrPermille int             `json:"goal_err_permille"`
	BudgetUSDMonth  int             `json:"budget_usd_month"`
	Start           json.RawMessage `json:"start"`
}

// all are the scenario files in ID order, read once.
var all = func() []file {
	ents, err := files.ReadDir("scenarios")
	if err != nil {
		panic(err)
	}
	var out []file
	for _, e := range ents {
		data, err := files.ReadFile(path.Join("scenarios", e.Name()))
		if err != nil {
			panic(err)
		}
		var f file
		if err := json.Unmarshal(data, &f); err != nil || f.ID+".json" != e.Name() {
			panic("challenge: bad scenario file " + e.Name())
		}
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b file) int { return strings.Compare(a.ID, b.ID) })
	return out
}()

// scenario decodes f into a fresh Scenario (nothing shared with other copies).
func (f file) scenario() Scenario {
	d, err := archdoc.Decode(f.Start)
	if err != nil {
		panic("challenge: scenario " + f.ID + ": " + err.Error())
	}
	return Scenario{
		ID: f.ID, Seed: d.Seed, Start: *d, Faults: slices.Clone(d.Faults),
		GoalP99Ms: f.GoalP99Ms, GoalErrPermille: f.GoalErrPermille, BudgetUSDMonth: f.BudgetUSDMonth,
	}
}

// Scenarios are the 15 scenarios in ID order (fresh copies).
func Scenarios() []Scenario {
	out := make([]Scenario, len(all))
	for i, f := range all {
		out[i] = f.scenario()
	}
	return out
}

// ByID is scenario id.
func ByID(id string) (Scenario, bool) {
	for _, f := range all {
		if f.ID == id {
			return f.scenario(), true
		}
	}
	return Scenario{}, false
}

// ForDate is the scenario of t's UTC day: days since 1970-01-01 mod 15, so
// everyone gets the same one on the same day.
func ForDate(t time.Time) Scenario {
	day := t.UTC().Unix() / 86400
	if t.UTC().Unix() < 0 && t.UTC().Unix()%86400 != 0 {
		day-- // floor, not truncation, before 1970
	}
	n := int64(len(all))
	return all[((day%n)+n)%n].scenario()
}

// Date is t's UTC day as YYYY-MM-DD (the card's challenge date).
func Date(t time.Time) string { return t.UTC().Format(time.DateOnly) }

// Design is a fresh copy of the start design.
func (s Scenario) Design() *archdoc.Design { return clone(&s.Start) }

// clone deep-copies d through its canonical encoding (a design that does not
// encode comes back as an empty one).
func clone(d *archdoc.Design) *archdoc.Design {
	b, err := d.Encode()
	if err == nil {
		if c, err := archdoc.Decode(b); err == nil {
			return c
		}
	}
	return &archdoc.Design{SchemaVersion: archdoc.SchemaVersion}
}

// Apply is d as the challenge runs it: the scenario's seed, duration and
// faults, and the scenario clients' traffic (by node id). Everything else is
// the player's. d is not changed. Every scenario client must still be there
// (ErrClientMissing) and send all of its reads and writes on to non-client
// nodes (ErrUnrouted), and every fault target must still be there
// (ErrFaultTargetMissing) — so the run design is valid and no part of the
// challenge is dodged by deleting it.
func Apply(d *archdoc.Design, s Scenario) (*archdoc.Design, error) {
	if d == nil {
		return nil, ErrClientMissing
	}
	out := clone(d)
	out.Seed, out.DurationS = s.Seed, s.Start.DurationS
	out.Faults = slices.Clone(s.Faults)
	for _, sc := range s.Start.Nodes {
		if sc.Kind != archdoc.KindClient {
			continue
		}
		i := slices.IndexFunc(out.Nodes, func(n archdoc.Node) bool { return n.ID == sc.ID && n.Kind == archdoc.KindClient })
		if i < 0 {
			return nil, ErrClientMissing
		}
		if sc.Traffic != nil {
			tr := *sc.Traffic
			out.Nodes[i].Traffic = &tr
		}
	}
	if !routed(out, s) {
		return nil, ErrUnrouted
	}
	for _, is := range out.Validate() {
		if strings.HasPrefix(is.Path, "faults[") && strings.HasSuffix(is.Path, ".target") {
			return nil, ErrFaultTargetMissing
		}
	}
	return out, nil
}

// forwards reports a kind that only passes requests on: whatever it does not
// send on ends there, which the engine counts as served.
func forwards(k archdoc.NodeKind) bool {
	switch k {
	case archdoc.KindClient, archdoc.KindLB, archdoc.KindCDN, archdoc.KindQueue:
		return true
	}
	return false
}

// share is an edge's permille for reads (c = 0) or writes (c = 1).
func share(e archdoc.Edge, c int) int {
	if c == 0 {
		return max(e.ReadPermille, 0)
	}
	return max(e.WritePermille, 0)
}

// routed reports whether every scenario client's traffic reaches nodes that
// serve it: each forwarding node (forwards) that gets reads or writes from a
// scenario client sends at least 1000‰ of that class on, and none of them
// sits on a loop of such edges (the engine drops a loop's closing edge, so
// its share would end at a forwarding node).
func routed(d *archdoc.Design, s Scenario) bool {
	kind := make(map[string]archdoc.NodeKind, len(d.Nodes))
	for _, n := range d.Nodes {
		kind[n.ID] = n.Kind
	}
	out := map[string][]archdoc.Edge{}
	for _, e := range d.Edges {
		if _, ok := kind[e.To]; ok {
			out[e.From] = append(out[e.From], e)
		}
	}
	// gets[id][c]: node id receives class c from a scenario client
	gets := map[string]*[2]bool{}
	var todo []string
	for _, sc := range s.Start.Nodes {
		if sc.Kind != archdoc.KindClient {
			continue
		}
		read := 1000
		if sc.Traffic != nil {
			read = min(max(sc.Traffic.ReadPermille, 0), 1000)
		}
		gets[sc.ID] = &[2]bool{read > 0, read < 1000}
		todo = append(todo, sc.ID)
	}
	for len(todo) > 0 {
		u := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		for _, e := range out[u] {
			for c := range 2 {
				if !gets[u][c] || share(e, c) == 0 {
					continue
				}
				g := gets[e.To]
				if g == nil {
					g = &[2]bool{}
					gets[e.To] = g
				}
				if !g[c] {
					g[c] = true
					todo = append(todo, e.To)
				}
			}
		}
	}
	for id, g := range gets {
		if !forwards(kind[id]) {
			continue
		}
		for c := range 2 {
			if !g[c] {
				continue
			}
			sum := 0
			for _, e := range out[id] {
				sum += share(e, c)
			}
			if sum < 1000 {
				return false
			}
		}
		if loops(id, out, gets) {
			return false
		}
	}
	return true
}

// loops reports whether id reaches itself over edges that carry traffic
// between nodes a scenario client reaches.
func loops(id string, out map[string][]archdoc.Edge, gets map[string]*[2]bool) bool {
	seen := map[string]bool{}
	todo := []string{id}
	for len(todo) > 0 {
		u := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		for _, e := range out[u] {
			if gets[e.To] == nil || share(e, 0)+share(e, 1) == 0 {
				continue
			}
			if e.To == id {
				return true
			}
			if !seen[e.To] {
				seen[e.To] = true
				todo = append(todo, e.To)
			}
		}
	}
	return false
}

// Outcome is a run's result against a scenario.
type Outcome struct {
	KeptS        int   // seconds that kept both goals
	TotalS       int   // the run's seconds
	Dropped      int64 // requests that failed (frame rate × error share × length)
	CostUSDMonth int   // the run's average monthly cost
	Score        int
}

// Evaluate scores tl: kept seconds × 1000 × min(1, budget/cost) − failed
// requests, an integer at least 0 (spec C2.12).
func Evaluate(tl *archsim.Timeline, s Scenario) Outcome {
	var o Outcome
	if tl == nil {
		return o
	}
	var kept10, total10, dropped10k int64
	prev := 0
	for _, f := range tl.Frames {
		dt := int64(max(f.T10-prev, 0))
		prev = max(f.T10, prev)
		total10 += dt
		if f.P99Ms <= s.GoalP99Ms && f.ErrPermille <= s.GoalErrPermille {
			kept10 += dt
		}
		dropped10k += int64(max(f.RPS, 0)) * int64(min(max(f.ErrPermille, 0), 1000)) * dt
	}
	o.KeptS, o.TotalS = int(kept10/10), int(total10/10)
	o.Dropped = dropped10k / 10_000
	if tl.Summary != nil {
		o.CostUSDMonth = max(tl.Summary.Cost.TotalUSDMonth, 0)
	}
	base := kept10 * 100 // kept seconds × 1000
	if c := int64(o.CostUSDMonth); c > int64(s.BudgetUSDMonth) {
		base = base * int64(max(s.BudgetUSDMonth, 0)) / c
	}
	o.Score = int(min(max(base-o.Dropped, 0), math.MaxInt32))
	return o
}

// Score is Evaluate's score.
func Score(tl *archsim.Timeline, s Scenario) int { return Evaluate(tl, s).Score }
