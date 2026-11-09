package arch

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/archsim/challenge"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// shortChallenge makes the scenario of every day today's with a 60-second
// run: space computes the run as a harness Cmd (1s limit, -race too).
func shortChallenge(t *testing.T) challenge.Scenario {
	t.Helper()
	s := challenge.ForDate(uitest.Start)
	s.Start.DurationS = 60
	old := scenarioOf
	scenarioOf = func(time.Time) challenge.Scenario { return s }
	t.Cleanup(func() { scenarioOf = old })
	return s
}

// openToday runs arch.challenge and returns the instance it opened.
func openToday(t *testing.T, h *uitest.Harness, s challenge.Scenario) *canvasModel {
	t.Helper()
	h.Send(ui.RunCommandMsg{ID: "arch.challenge"})
	date := challenge.Date(uitest.Start)
	return canvasOf(t, h, toolRoute(keyChallenge+date, map[string]string{queryChallenge: s.ID, queryDate: date}))
}

// TestChallengeTitles: every scenario has a title in both catalogs.
func TestChallengeTitles(t *testing.T) {
	for _, l := range []i18n.Locale{i18n.Ko, i18n.En} {
		tr := i18n.For(l)
		for _, s := range challenge.Scenarios() {
			got := challengeTitle(tr, s.ID)
			if got == s.ID || got == "" {
				t.Errorf("%s: no %s title for %s", l, l, s.ID)
			}
		}
		if got := challengeTitle(tr, "no-such"); got != "no-such" {
			t.Errorf("unknown scenario title %q", got)
		}
	}
}

func TestChallengeCard(t *testing.T) {
	s := shortChallenge(t)
	h := newHarness(t)
	cardServer(h)
	h.Resize(120, 40)
	c := openToday(t, h, s)
	a := h.Context().T.Archcanvas
	title := challengeTitle(h.Context().T, s.ID)
	if c.chal == nil || c.chal.s.ID != s.ID || c.chal.date != "2026-11-05" {
		t.Fatalf("challenge state %+v", c.chal)
	}
	if d := designOf(t, c); d.Title != title || len(d.Nodes) != len(s.Start.Nodes) || len(d.Faults) != len(s.Faults) {
		t.Fatalf("start design: title %q, %d nodes, %d faults", d.Title, len(d.Nodes), len(d.Faults))
	}
	intro := a.ChallengeIntro(title, s.GoalP99Ms, pct1(s.GoalErrPermille), humanize.Grouped(int64(s.BudgetUSDMonth)))
	if scr := h.Screen(); !shown(scr, intro) || !strings.Contains(intro, "추정") {
		t.Errorf("no challenge intro toast:\n%s", scr)
	}
	// the run scores the design against the scenario
	h.Press("space")
	if c.chal.out == nil {
		t.Fatalf("no score after the run:\n%s", h.Screen())
	}
	run, err := challenge.Apply(designOf(t, c), s)
	if err != nil {
		t.Fatal(err)
	}
	tl, err := archsim.RunTimed(context.Background(), run, archsim.TimedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := challenge.Evaluate(tl, s)
	if *c.chal.out != want {
		t.Fatalf("score %+v, want %+v", *c.chal.out, want)
	}
	if scr := h.Screen(); !shown(scr, a.ChallengeScored(want.Score, want.KeptS, want.TotalS)) {
		t.Errorf("no score toast:\n%s", scr)
	}
	// the sim card carries the challenge
	h.Send(ui.RunCommandMsg{ID: "arch.send_card"})
	if c.sendDlg == nil || !c.sendDlg.sim {
		t.Fatal("arch.send_card after a challenge run is not the sim card")
	}
	pickChannel(t, h, c.sendDlg, "#일반")
	h.Press("enter")
	line := a.CardChallenge("2026-11-05", title, want.Score)
	if scr := h.Screen(); !strings.Contains(scr, line) {
		t.Errorf("the preview lacks %q:\n%s", line, scr)
	}
	h.Check()
	h.Press("enter")
	got := sentCards(t, h)
	if len(got) != 1 || got[0].Card == nil || got[0].Card.Type != ui.CardArchSim || got[0].channel != fixtures.ChGeneral {
		t.Fatalf("sends %+v", got)
	}
	var ch cardChallenge
	if err := json.Unmarshal(got[0].Card.Data["challenge"], &ch); err != nil {
		t.Fatalf("challenge %s: %v", got[0].Card.Data["challenge"], err)
	}
	if ch != (cardChallenge{Date: "2026-11-05", Scenario: s.ID, Score: want.Score}) {
		t.Errorf("challenge %+v", ch)
	}
	// the card's numbers replay: its seed is the scenario's, its copy the run's design
	var seed string
	_ = json.Unmarshal(got[0].Card.Data["seed"], &seed)
	if want := s.Seed; seed != strconv.FormatUint(want, 10) {
		t.Errorf("seed %s, want %d", seed, want)
	}
	m := cardMessage("01JDCHL0000000000000000002", sentAsCard(t, got[0]), got[0].Body)
	cr, _ := ui.CardFor(ui.CardArchSim)
	if lines := ansi.Strip(strings.Join(cr.Lines(h.Context(), &m, 100), "\n")); !strings.Contains(lines, line) {
		t.Errorf("the chat card lacks %q:\n%s", line, lines)
	}
}

// TestChallengeEditDropsScore: an edit after the run is not what was scored.
func TestChallengeEditDropsScore(t *testing.T) {
	s := shortChallenge(t)
	h := newHarness(t)
	h.Resize(120, 40)
	c := openToday(t, h, s)
	h.Press("space")
	if c.chal.out == nil {
		t.Fatal("no score")
	}
	var other string
	for _, n := range s.Start.Nodes {
		if n.Kind != archdoc.KindClient {
			other = n.ID
		}
	}
	c.sel = other
	h.Press("d")
	if c.chal.out != nil {
		t.Error("the score survived an edit")
	}
	h.Send(ui.RunCommandMsg{ID: "arch.send_card"})
	if c.sendDlg == nil || c.sendDlg.chal != nil {
		t.Errorf("send dialog after the edit carries a challenge: %+v", c.sendDlg)
	}
}

// TestChallengeNeedsClients: a design without the scenario's client is not
// run nor scored.
func TestChallengeNeedsClients(t *testing.T) {
	s := shortChallenge(t)
	h := newHarness(t)
	h.Resize(120, 40)
	c := openToday(t, h, s)
	for _, n := range s.Start.Nodes {
		if n.Kind == archdoc.KindClient {
			c.sel = n.ID
			h.Press("d")
		}
	}
	h.Press("space")
	if c.pb.tl != nil || c.pb.computing || c.chal.out != nil {
		t.Error("a design without its clients ran")
	}
	if scr := h.Screen(); !shown(scr, h.Context().T.Archcanvas.ChallengeClientMissing()) {
		t.Errorf("no missing-client toast:\n%s", scr)
	}
}

// sentAsCard is the card of a send the harness's server got.
func sentAsCard(t *testing.T, s sentCard) api.Card {
	t.Helper()
	data, err := json.Marshal(s.Card.Data)
	if err != nil {
		t.Fatal(err)
	}
	return api.Card{Type: s.Card.Type, V: s.Card.V, Data: data}
}

func assertAvailable(t *testing.T, h *uitest.Harness, id string, want bool) {
	t.Helper()
	c, ok := palette.Lookup(id)
	if !ok {
		t.Fatalf("%s not registered", id)
	}
	if got := c.Available != nil && c.Available(h.Context()); got != want {
		t.Errorf("%s available = %v, want %v", id, got, want)
	}
}

// TestChallengeOnlyWithFlag: arch.challenge is a flagged command (the hub
// has no row for it).
func TestChallengeOnlyWithFlag(t *testing.T) {
	h := newHarness(t)
	assertAvailable(t, h, "arch.challenge", true)
	t.Setenv("MASIL_FEATURES", "")
	assertAvailable(t, h, "arch.challenge", false)
}
