package uitest

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// FakeClock implements ui.Timers and supplies Context.Now.
type FakeClock struct { /* now, pending (deadline, seq, msg) */
}

var _ ui.Timers = (*FakeClock)(nil)

// NewFakeClock starts at start.
func NewFakeClock(start time.Time) *FakeClock {
	// TODO(core): Task 20
	return nil
}

// Now returns the fake time.
func (c *FakeClock) Now() time.Time {
	// TODO(core): Task 20
	return time.Time{}
}

// After returns a Cmd that records (now+d, msg) when RUN, and returns nil.
func (c *FakeClock) After(d time.Duration, msg tea.Msg) tea.Cmd {
	// TODO(core): Task 20
	return nil
}

// Pending returns remaining delays, ascending (assertions).
func (c *FakeClock) Pending() []time.Duration {
	// TODO(core): Task 20
	return nil
}
