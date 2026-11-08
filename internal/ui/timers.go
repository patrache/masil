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

// realTimers is the production Timers. This file is the only UI file allowed
// to call tea.Tick (timer rule, CA 5.3.2; checked by TestNoRawTimersInUI).
type realTimers struct{}

func (realTimers) After(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
}

// RealTimers returns the production Timers.
func RealTimers() Timers { return realTimers{} }

// After is ctx.Timers.After(d, msg). ctx.Timers is never nil by contract; a
// nil ctx or Timers falls back to RealTimers rather than dropping the message.
func After(ctx *Context, d time.Duration, msg tea.Msg) tea.Cmd {
	if ctx == nil || ctx.Timers == nil {
		return RealTimers().After(d, msg)
	}
	return ctx.Timers.After(d, msg)
}
