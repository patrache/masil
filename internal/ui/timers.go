package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Timers schedules a delayed message. It is the ONLY way UI code may wait.
type Timers interface {
	// After returns a Cmd that, when run, delivers msg once d has elapsed.
	// RealTimers: tea.Tick(d, func(time.Time) tea.Msg { return msg }).
	// uitest.FakeClock: the Cmd records (now+d, msg) and returns nil immediately; h.Advance delivers it.
	After(d time.Duration, msg tea.Msg) tea.Cmd
}

// RealTimers returns the production Timers.
func RealTimers() Timers {
	// TODO(core): Task 19
	return nil
}

// After is ctx.Timers.After(d, msg).
func After(ctx *Context, d time.Duration, msg tea.Msg) tea.Cmd {
	// TODO(core): Task 19
	return nil
}
