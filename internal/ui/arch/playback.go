package arch

import (
	"context"
	"errors"
	"maps"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
)

// playback is the timed run on screen (P3 Task 19): the run is computed in
// the background (archsim.RunTimed), then its frames play at speedX (one
// frame = one simulated second). pos is the frame shown.
type playback struct {
	tl        *archsim.Timeline
	pos       int
	running   bool // playing
	computing bool // RunTimed is working
	acc       int  // tenths of a frame played since the last frame
	cancel    context.CancelFunc
}

// timedDoneMsg is a finished background run of edit generation gen; a run of
// an older generation (the design was edited since) is dropped.
type timedDoneMsg struct {
	instance string
	gen      int
	tl       *archsim.Timeline
	err      error
}

// playTickMsg advances the playback of generation gen.
type playTickMsg struct {
	instance string
	gen      int
}

// playTick is the playback's real-time step.
const playTick = 100 * time.Millisecond

// speedOf is the playback speed (×1 until < > picked one).
func (m *canvasModel) speedOf() int { return max(m.speedX, 1) }

// designChanged drops the run of the old design: a run in flight is
// cancelled and its result ignored (the generation moved on).
func (m *canvasModel) designChanged() {
	m.runGen++
	if m.pb.cancel != nil {
		m.pb.cancel()
	}
	m.pb = playback{}
}

// toggleRun is space (and arch.run): start the run, then pause and play.
func (m *canvasModel) toggleRun(ctx *ui.Context) tea.Cmd {
	switch {
	case m.pb.computing:
		m.designChanged() // space again stops the computation
		return nil
	case m.pb.tl == nil:
		return m.startRun(ctx)
	case m.pb.running:
		m.pb.running = false
		return nil
	}
	if m.pb.pos >= len(m.pb.tl.Frames)-1 {
		m.pb.pos = 0
	}
	m.pb.running, m.pb.acc = true, 0
	return m.tick(ctx)
}

// startRun computes the timed run of the design in the background.
func (m *canvasModel) startRun(ctx *ui.Context) tea.Cmd {
	d, _ := archdoc.FromObjects(m.objs, nil)
	if d == nil {
		return nil
	}
	c, cancel := context.WithCancel(context.Background())
	m.pb = playback{computing: true, cancel: cancel}
	inst, gen := m.route.ID, m.runGen
	return func() tea.Msg {
		tl, err := archsim.RunTimed(c, d, archsim.TimedOptions{})
		return timedDoneMsg{instance: inst, gen: gen, tl: tl, err: err}
	}
}

func (m *canvasModel) tick(ctx *ui.Context) tea.Cmd {
	if ctx == nil || ctx.Timers == nil {
		return nil
	}
	return ctx.Timers.After(playTick, playTickMsg{instance: m.route.ID, gen: m.runGen})
}

// runDone takes a finished run (when it is still this design's).
func (m *canvasModel) runDone(ctx *ui.Context, msg timedDoneMsg) tea.Cmd {
	if msg.gen != m.runGen || !m.pb.computing {
		return nil
	}
	m.pb.computing, m.pb.cancel = false, nil
	if msg.err != nil || msg.tl == nil || len(msg.tl.Frames) == 0 {
		m.pb = playback{}
		if errors.Is(msg.err, context.Canceled) {
			return nil
		}
		return ui.Toast(ctx.T.Archcanvas.RunFailed(), ui.ToastError)
	}
	m.pb.tl, m.pb.pos, m.pb.running, m.pb.acc = msg.tl, 0, true, 0
	return m.tick(ctx)
}

// playStep advances the playback by one tick at its speed.
func (m *canvasModel) playStep(ctx *ui.Context, msg playTickMsg) tea.Cmd {
	if msg.gen != m.runGen || !m.pb.running || m.pb.tl == nil {
		return nil
	}
	m.pb.acc += m.speedOf()
	for m.pb.acc >= 10 {
		m.pb.acc -= 10
		m.pb.pos++
	}
	if last := len(m.pb.tl.Frames) - 1; m.pb.pos >= last {
		m.pb.pos, m.pb.running = last, false
		return nil
	}
	return m.tick(ctx)
}

// stepTime moves a paused playback by one frame (←/→).
func (m *canvasModel) stepTime(d int) {
	if m.pb.tl == nil || m.pb.running {
		return
	}
	m.pb.pos = min(max(m.pb.pos+d, 0), len(m.pb.tl.Frames)-1)
}

// frame is the frame on screen (nil = the instant analysis).
func (m *canvasModel) frame() *archsim.Frame {
	if m.pb.tl == nil || len(m.pb.tl.Frames) == 0 {
		return nil
	}
	return &m.pb.tl.Frames[min(max(m.pb.pos, 0), len(m.pb.tl.Frames)-1)]
}

// frameSec is the shown frame's time in whole seconds.
func (m *canvasModel) frameSec() int {
	if f := m.frame(); f != nil {
		return f.T10 / 10
	}
	return 0
}

// totalSec is the run's length in seconds.
func (m *canvasModel) totalSec() int {
	if m.pb.tl == nil || len(m.pb.tl.Frames) == 0 {
		return 0
	}
	return m.pb.tl.Frames[len(m.pb.tl.Frames)-1].T10 / 10
}

// shownBadges are the node badges on screen: the instant analysis's, or the
// playing frame's use and down state.
func (m *canvasModel) shownBadges() map[string]archdraw.NodeBadge {
	f := m.frame()
	if f == nil {
		return m.badges
	}
	out := maps.Clone(m.badges)
	if out == nil {
		out = map[string]archdraw.NodeBadge{}
	}
	for id := range f.Nodes {
		out[id] = m.badgeOf(id)
	}
	return out
}

// badgeOf is node id's badge on screen (the playing frame's use and state).
func (m *canvasModel) badgeOf(id string) archdraw.NodeBadge {
	b := m.badges[id]
	f := m.frame()
	if f == nil {
		return b
	}
	fn, in := f.Nodes[id]
	if !in {
		return b
	}
	b.BarPermille = fn.UtilPermille
	b.Danger = fn.Down || fn.UtilPermille >= 1000
	b.Warn = b.Warn || fn.UtilPermille >= 850
	b.Text = ""
	if fn.Instances >= 2 {
		b.Text = "×" + strconvItoa(fn.Instances)
	}
	return b
}

// totals are the design's numbers on screen (the frame's while playing).
func (m *canvasModel) totals() (rps, p50, p99, errPermille int) {
	if f := m.frame(); f != nil {
		return f.RPS, f.P50Ms, f.P99Ms, f.ErrPermille
	}
	r := m.result
	if r == nil {
		return 0, 0, 0, 0
	}
	return r.RPS, r.P50Ms, r.P99Ms, r.ErrorPermille
}

// engineText is the header's engine state.
func (m *canvasModel) engineText(t *i18n.T) string {
	a := t.Archcanvas
	switch {
	case m.pb.computing:
		return a.EngineRunning()
	case m.pb.tl != nil && m.pb.running:
		return a.EngineTimed(mmss(m.frameSec()), mmss(m.totalSec()), m.speedOf())
	case m.pb.tl != nil:
		return a.EnginePaused(mmss(m.frameSec()), mmss(m.totalSec()))
	}
	return a.EngineInstant()
}

// eventText is one run event in words.
func (m *canvasModel) eventText(t *i18n.T, e archsim.Event) string {
	a := t.Archcanvas
	target := m.targetLabel(t, e.Target)
	switch e.Kind {
	case archsim.EventFaultStart:
		for _, f := range m.design.Faults {
			if f.Target == e.Target && f.AtS*10 == e.T10 {
				switch f.Kind {
				case archdoc.FaultNodeOff:
					return a.EvNodeOff(target)
				case archdoc.FaultInstancesOff:
					return a.EvInstancesOff(target, e.Value)
				case archdoc.FaultLatency:
					return a.EvLatency(target, e.Value)
				case archdoc.FaultAZOff, archdoc.FaultRegionOff:
					return a.EvBoxOff(target)
				}
			}
		}
		return a.EvFaultStart(target)
	case archsim.EventFaultEnd:
		return a.EvFaultEnd(target)
	case archsim.EventLBReroute:
		return a.EvLbReroute(target, e.Value)
	case archsim.EventFailoverStart:
		return a.EvFailoverStart(target, e.Value)
	case archsim.EventFailoverDone:
		return a.EvFailoverDone(target)
	case archsim.EventWritesFailing:
		return a.EvWritesFailing(target)
	case archsim.EventScaleOut:
		return a.EvScaleOut(target, e.Value)
	case archsim.EventScaleIn:
		return a.EvScaleIn(target, e.Value)
	case archsim.EventQueueDrop:
		return a.EvQueueDrop(target, e.Value)
	}
	return target
}

// eventLine is the latest run event up to the shown frame: "mm:ss <what>"
// (events of the same moment joined with →); "" before the first.
func (m *canvasModel) eventLine(t *i18n.T) string {
	f := m.frame()
	if f == nil {
		return ""
	}
	at := -1
	for _, e := range m.pb.tl.Events {
		if e.T10 <= f.T10 && e.T10 > at {
			at = e.T10
		}
	}
	if at < 0 {
		return ""
	}
	var parts []string
	for _, e := range m.pb.tl.Events {
		if e.T10 == at {
			parts = append(parts, m.eventText(t, e))
		}
	}
	return mmss(at/10) + " " + strings.Join(parts, " → ")
}
