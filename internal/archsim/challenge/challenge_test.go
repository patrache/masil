package challenge

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim"
)

func TestForDateUTC(t *testing.T) {
	late := time.Date(2026, 11, 9, 23, 59, 0, 0, time.UTC)
	seoul := time.FixedZone("KST", 9*3600)
	// 2026-11-10 00:00 in Seoul is still 2026-11-09 15:00 UTC
	early := time.Date(2026, 11, 10, 0, 0, 0, 0, seoul)
	a, b := ForDate(late), ForDate(early)
	if a.ID != b.ID {
		t.Errorf("same UTC day, different scenarios: %s vs %s", a.ID, b.ID)
	}
	if Date(early) != "2026-11-09" || Date(late) != "2026-11-09" {
		t.Errorf("dates %s %s, want 2026-11-09", Date(early), Date(late))
	}
	next := ForDate(time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	all := Scenarios()
	i := slices.IndexFunc(all, func(s Scenario) bool { return s.ID == a.ID })
	if want := all[(i+1)%len(all)].ID; next.ID != want {
		t.Errorf("next UTC day: %s, want %s", next.ID, want)
	}
	// days before 1970 still pick a scenario
	if s := ForDate(time.Date(1969, 12, 31, 12, 0, 0, 0, time.UTC)); s.ID != all[len(all)-1].ID {
		t.Errorf("1969-12-31: %s, want the last scenario %s", s.ID, all[len(all)-1].ID)
	}
	// every scenario comes up within 15 days
	seen := map[string]bool{}
	for d := range 15 {
		seen[ForDate(late.AddDate(0, 0, d)).ID] = true
	}
	if len(seen) != 15 {
		t.Errorf("15 days showed %d scenarios", len(seen))
	}
}

var reID = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)

func TestScenariosValid(t *testing.T) {
	all := Scenarios()
	if len(all) != 15 {
		t.Fatalf("%d scenarios, want 15", len(all))
	}
	if !slices.IsSortedFunc(all, func(a, b Scenario) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	}) {
		t.Error("scenarios not in ID order")
	}
	seen := map[string]bool{}
	for _, s := range all {
		if !reID.MatchString(s.ID) || seen[s.ID] {
			t.Errorf("bad or repeated id %q", s.ID)
		}
		seen[s.ID] = true
		if is := s.Start.Validate(); len(is) > 0 {
			t.Errorf("%s: start design invalid: %v", s.ID, is)
		}
		if s.GoalP99Ms <= 0 || s.GoalErrPermille < 0 || s.GoalErrPermille > 1000 || s.BudgetUSDMonth <= 0 {
			t.Errorf("%s: goals %d ms %d‰ $%d", s.ID, s.GoalP99Ms, s.GoalErrPermille, s.BudgetUSDMonth)
		}
		if s.Start.Seed != s.Seed {
			t.Errorf("%s: start seed %d, scenario seed %d", s.ID, s.Start.Seed, s.Seed)
		}
		d := s.Design()
		if is := d.Validate(); len(is) > 0 {
			t.Errorf("%s: design invalid: %v", s.ID, is)
		}
		if len(d.Faults) != len(s.Faults) {
			t.Errorf("%s: design has %d faults, scenario %d", s.ID, len(d.Faults), len(s.Faults))
		}
		got, ok := ByID(s.ID)
		if !ok || got.ID != s.ID {
			t.Errorf("ByID(%s) = %v", s.ID, ok)
		}
	}
	if _, ok := ByID("nope"); ok {
		t.Error("ByID found an unknown id")
	}
	// a caller's edits never reach the next copy
	a := Scenarios()[0]
	a.Start.Nodes[0].Label = "changed"
	a.Design().Nodes[0].Label = "changed too"
	if Scenarios()[0].Start.Nodes[0].Label == "changed" || Scenarios()[0].Design().Nodes[0].Label == "changed too" {
		t.Error("scenario copies share memory")
	}
}

func run(t *testing.T, d *archdoc.Design) *archsim.Timeline {
	t.Helper()
	tl, err := archsim.RunTimed(context.Background(), d, archsim.TimedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestScoreDeterministic(t *testing.T) {
	for _, s := range Scenarios() {
		t.Run(s.ID, func(t *testing.T) {
			d, err := Apply(s.Design(), s)
			if err != nil {
				t.Fatal(err)
			}
			a, b := Score(run(t, d), s), Score(run(t, d), s)
			if a != b || a < 0 {
				t.Fatalf("scores %d, %d", a, b)
			}
			o := Evaluate(run(t, d), s)
			if o.Score != a {
				t.Errorf("Evaluate %d, Score %d", o.Score, a)
			}
			// the start design leaves room: it never already keeps every goal
			// for the whole run at the budget
			if o.KeptS >= o.TotalS && o.CostUSDMonth <= s.BudgetUSDMonth && o.Dropped == 0 {
				t.Errorf("start design already perfect: %+v", o)
			}
			t.Logf("%s: %+v", s.ID, o)
		})
	}
}

func frame(t10, rps, p99, err int) archsim.Frame {
	return archsim.Frame{T10: t10, RPS: rps, P99Ms: p99, ErrPermille: err}
}

func TestScoreArithmetic(t *testing.T) {
	s := Scenario{ID: "x", GoalP99Ms: 200, GoalErrPermille: 10, BudgetUSDMonth: 1000}
	tl := &archsim.Timeline{
		Frames: []archsim.Frame{
			frame(10, 100, 150, 0),   // kept
			frame(20, 100, 250, 0),   // p99 over
			frame(30, 1000, 100, 20), // error over: 1000 × 2% = 20 dropped
			frame(40, 100, 200, 10),  // kept (goals are inclusive), 1 dropped
		},
		Summary: &archsim.Result{Cost: archsim.Cost{TotalUSDMonth: 800}},
	}
	o := Evaluate(tl, s)
	if o.KeptS != 2 || o.TotalS != 4 || o.Dropped != 21 || o.CostUSDMonth != 800 || o.Score != 2000-21 {
		t.Errorf("under budget: %+v", o)
	}
	tl.Summary.Cost.TotalUSDMonth = 4000 // a quarter of the efficiency
	if o := Evaluate(tl, s); o.Score != 500-21 {
		t.Errorf("4× over budget: %+v", o)
	}
	// the floor is 0
	tl.Frames = append(tl.Frames, frame(50, 2_000_000_000, 9000, 1000))
	if o := Evaluate(tl, s); o.Score != 0 || o.Dropped != 2_000_000_021 {
		t.Errorf("floor: %+v", o)
	}
	// degenerate timelines score 0
	if Score(nil, s) != 0 || Score(&archsim.Timeline{}, s) != 0 {
		t.Error("an empty run scored")
	}
	if o := Evaluate(&archsim.Timeline{Frames: []archsim.Frame{frame(10, 10, 1, 0)}}, s); o.Score != 1000 {
		t.Errorf("no summary (no cost): %+v", o)
	}
	if o := Evaluate(&archsim.Timeline{Frames: []archsim.Frame{frame(10, -5, 1, -3)}}, s); o.Score != 1000 || o.Dropped != 0 {
		t.Errorf("negative numbers: %+v", o)
	}
}

func TestApplyRestoresScenario(t *testing.T) {
	s := ForDate(time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC))
	d := s.Design()
	d.Seed = 7
	d.DurationS = 60
	d.Faults = nil
	var client string
	for i := range d.Nodes {
		if d.Nodes[i].Kind == archdoc.KindClient {
			client = d.Nodes[i].ID
			d.Nodes[i].Traffic.RPS = 1 // a gentler crowd is not the challenge
		}
	}
	got, err := Apply(d, s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Seed != s.Seed || got.DurationS != s.Start.DurationS || len(got.Faults) != len(s.Faults) {
		t.Errorf("seed %d duration %d faults %d", got.Seed, got.DurationS, len(got.Faults))
	}
	for _, n := range got.Nodes {
		if n.ID == client && n.Traffic.RPS == 1 {
			t.Error("the client's traffic was not restored")
		}
	}
	if d.Seed != 7 || d.Faults != nil {
		t.Error("Apply changed its input")
	}
	// removing a scenario client is not allowed
	var kept []archdoc.Node
	for _, n := range d.Nodes {
		if n.ID != client {
			kept = append(kept, n)
		}
	}
	d.Nodes = kept
	if _, err := Apply(d, s); !errors.Is(err, ErrClientMissing) {
		t.Errorf("missing client: %v", err)
	}
	if _, err := Apply(nil, s); !errors.Is(err, ErrClientMissing) {
		t.Errorf("nil design: %v", err)
	}
}
