package uitest

import (
	"slices"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// FakeClock implements ui.Timers and supplies Context.Now.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	seq     uint64
	pending []pendingTimer
}

// pendingTimer is one recorded After: due at deadline, ties broken by seq (registration order).
type pendingTimer struct {
	deadline time.Time
	seq      uint64
	msg      tea.Msg
}

var _ ui.Timers = (*FakeClock)(nil)

// NewFakeClock starts at start.
func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{now: start}
}

// Now returns the fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a Cmd that records (now+d, msg) when RUN, and returns nil.
// It never blocks: the harness delivers msg when Advance reaches the deadline.
func (c *FakeClock) After(d time.Duration, msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.seq++
		c.pending = append(c.pending, pendingTimer{deadline: c.now.Add(d), seq: c.seq, msg: msg})
		return nil
	}
}

// Pending returns remaining delays, ascending (assertions).
func (c *FakeClock) Pending() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]time.Duration, 0, len(c.pending))
	for _, p := range c.sorted() {
		out = append(out, p.deadline.Sub(c.now))
	}
	return out
}

// nextDue removes and returns the earliest pending msg whose deadline is <= until
// (same deadline => registration order) and moves the clock to that deadline
// (never backwards). ok is false, and the clock unchanged, when nothing is due.
func (c *FakeClock) nextDue(until time.Time) (msg tea.Msg, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	best := -1
	for i, p := range c.pending {
		if p.deadline.After(until) {
			continue
		}
		if best < 0 || earlier(p, c.pending[best]) {
			best = i
		}
	}
	if best < 0 {
		return nil, false
	}
	p := c.pending[best]
	c.pending = slices.Delete(c.pending, best, best+1)
	if p.deadline.After(c.now) {
		c.now = p.deadline
	}
	return p.msg, true
}

// sorted returns the pending timers in delivery order. Caller holds mu.
func (c *FakeClock) sorted() []pendingTimer {
	s := slices.Clone(c.pending)
	slices.SortFunc(s, func(a, b pendingTimer) int {
		switch {
		case earlier(a, b):
			return -1
		case earlier(b, a):
			return 1
		}
		return 0
	})
	return s
}

func earlier(a, b pendingTimer) bool {
	if !a.deadline.Equal(b.deadline) {
		return a.deadline.Before(b.deadline)
	}
	return a.seq < b.seq
}
