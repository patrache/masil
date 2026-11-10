package arch

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/archsim/challenge"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
)

// The daily challenge (P3 Task 28, spec C2.12) is a palette command of its
// own, arch.challenge, never one screen with LeetCode's daily problem (JX12).
// Spec C2.12 opens it from the hub, but P0's hub has no tool hook P3 may add
// to, so for now the palette is the only way in (exec-log P3). It opens
// today's scenario (UTC day) as a new, unsaved design. A timed run is scored
// against the scenario; the score goes out on an arch.sim card. No
// leaderboard.

// Challenge instance query keys and id prefix.
const (
	queryChallenge = "challenge" // the scenario id
	queryDate      = "date"      // its UTC day, YYYY-MM-DD
	keyChallenge   = "arch:challenge:"
)

// scenarioOf is the scenario of a day (a test swaps it for a shorter run).
var scenarioOf = challenge.ForDate

// challengeState is a challenge instance's scenario and its last score.
type challengeState struct {
	s    challenge.Scenario
	date string
	// the design the last run scored (Apply'd) and its outcome; nil until
	// a run finishes and again after any edit
	ran *archdoc.Design
	out *challenge.Outcome
}

// challengeTitle is scenario id's name in the catalog (the id itself when
// the catalog has none — a card from a newer client).
func challengeTitle(t *i18n.T, id string) string {
	if s, ok := t.Lookup("archcanvas", "challenge_"+id+"_title", nil); ok && s != "" {
		return s
	}
	return id
}

// openChallenge is arch.challenge: today's scenario (one instance per day).
func openChallenge(ctx *ui.Context) tea.Cmd {
	now := time.Now
	if ctx != nil && ctx.Now != nil {
		now = ctx.Now
	}
	at := now()
	s, date := scenarioOf(at), challenge.Date(at)
	return ui.OpenTool(ui.ToolArch, keyChallenge+date, map[string]string{queryChallenge: s.ID, queryDate: date})
}

// initChallenge starts the instance on the scenario of its query; a day or
// scenario this build does not know starts an empty design.
func (m *canvasModel) initChallenge(ctx *ui.Context) tea.Cmd {
	q := m.route.Query
	var d *archdoc.Design
	at, err := time.Parse(time.DateOnly, q[queryDate])
	if err == nil {
		if s := scenarioOf(at); s.ID == q[queryChallenge] {
			m.chal = &challengeState{s: s, date: challenge.Date(at)}
			d = s.Design()
			d.Title = cutCodePoints(challengeTitle(ctx.T, s.ID), archdoc.MaxTitle)
		}
	}
	if d == nil {
		d = archdoc.New("")
	}
	m.setSink(ctx, &localSink{instance: m.route.ID, objs: d.ToObjects(), timers: ctx.Timers})
	if m.chal == nil {
		return nil
	}
	s := m.chal.s
	return ui.Toast(ctx.T.Archcanvas.ChallengeIntro(challengeTitle(ctx.T, s.ID), s.GoalP99Ms, pct1(s.GoalErrPermille),
		humanize.Grouped(int64(s.BudgetUSDMonth))), ui.ToastInfo)
}

// challengeRun is the design a challenge run uses: the scenario's seed,
// faults and client traffic over my design. A design the challenge cannot
// score (a client gone, traffic that stops short of a server, a fault's
// target gone) comes back nil with the toast that says why.
func (m *canvasModel) challengeRun(ctx *ui.Context, d *archdoc.Design) (*archdoc.Design, tea.Cmd) {
	if m.chal == nil {
		return d, nil
	}
	run, err := challenge.Apply(d, m.chal.s)
	if err != nil {
		a := ctx.T.Archcanvas
		msg := a.ChallengeClientMissing()
		switch {
		case errors.Is(err, challenge.ErrUnrouted):
			msg = a.ChallengeUnrouted()
		case errors.Is(err, challenge.ErrFaultTargetMissing):
			msg = a.ChallengeFaultTargetMissing()
		}
		return nil, ui.Toast(msg, ui.ToastInfo)
	}
	m.chal.ran, m.chal.out = nil, nil
	return run, nil
}

// challengeScored scores a finished run of design run.
func (m *canvasModel) challengeScored(ctx *ui.Context, run *archdoc.Design, tl *archsim.Timeline) tea.Cmd {
	if m.chal == nil || run == nil {
		return nil
	}
	o := challenge.Evaluate(tl, m.chal.s)
	m.chal.ran, m.chal.out = run, &o
	return ui.Toast(ctx.T.Archcanvas.ChallengeScored(o.Score, o.KeptS, o.TotalS), ui.ToastSuccess)
}

// challengeEdited drops the score: the design is no longer what was scored.
func (m *canvasModel) challengeEdited() {
	if m.chal != nil {
		m.chal.ran, m.chal.out = nil, nil
	}
}

// challengeCard is the scored run for the send dialog (nil = none).
func (m *canvasModel) challengeCard() (*archdoc.Design, *cardChallenge) {
	if m.chal == nil || m.chal.out == nil || m.chal.ran == nil {
		return nil, nil
	}
	return m.chal.ran, &cardChallenge{Date: m.chal.date, Scenario: m.chal.s.ID, Score: m.chal.out.Score}
}
