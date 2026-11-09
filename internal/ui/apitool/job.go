package apitool

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// Network and big file work (sending, the runner, indexing a JSON body) runs
// in a goroutine the tool owns; the screen learns the result only by polling
// with ui.After (no Cmd ever waits on the network, CA 5.3.2).

// jobPoll is how often a running job is looked at.
const jobPoll = 50 * time.Millisecond

// job is one goroutine's work: out is set before done is closed.
type job struct {
	id     int
	kind   jobKind
	cancel context.CancelFunc
	done   chan struct{}
	out    func(ctx *ui.Context) tea.Cmd
	// poll, when set, runs on every tick on the UI goroutine while the job
	// runs (the runner drains its event channel there).
	poll func()
}

// jobKind tells what a finished job's out is.
type jobKind int

const (
	jobSend jobKind = iota + 1
	jobRun
	jobFile
)

// jobTickMsg asks tool t to look at job id.
type jobTickMsg struct {
	t  *Tool
	id int
}

// finished reports whether the job ended (never blocks).
func (j *job) finished() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

// startJob runs fn in a goroutine and starts polling it. A job already
// running of the same kind is canceled first.
func (t *Tool) startJob(ctx *ui.Context, kind jobKind, fn func(c context.Context) func(*ui.Context) tea.Cmd) (*job, tea.Cmd) {
	if old := t.jobs[kind]; old != nil {
		old.cancel()
	}
	t.jobSeq++
	c, cancel := context.WithCancel(context.Background())
	j := &job{id: t.jobSeq, kind: kind, cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(j.done)
		j.out = fn(c)
	}()
	t.jobs[kind] = j
	return j, ui.After(ctx, jobPoll, jobTickMsg{t: t, id: j.id})
}

// onJobTick polls the job of m; a finished job is handed to its handler.
func (t *Tool) onJobTick(ctx *ui.Context, m jobTickMsg) tea.Cmd {
	for kind, j := range t.jobs {
		if j == nil || j.id != m.id {
			continue
		}
		if j.poll != nil {
			j.poll()
		}
		if !j.finished() {
			return ui.After(ctx, jobPoll, m)
		}
		j.cancel()
		delete(t.jobs, kind)
		return t.jobDone(ctx, j)
	}
	return nil
}

// running reports whether a job of kind is in flight.
func (t *Tool) running(kind jobKind) bool { return t.jobs[kind] != nil }

// cancelJob stops the job of kind; its tick still reports it (as canceled).
func (t *Tool) cancelJob(kind jobKind) bool {
	j := t.jobs[kind]
	if j == nil {
		return false
	}
	j.cancel()
	return true
}

// jobDone runs a finished job's continuation on the UI goroutine: every job
// returns a func(*ui.Context) tea.Cmd that applies its result.
func (t *Tool) jobDone(ctx *ui.Context, j *job) tea.Cmd {
	if j.out == nil {
		return nil
	}
	return j.out(ctx)
}
