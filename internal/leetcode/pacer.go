package leetcode

import "time"

// The gap LeetCode tolerates between runs or submits (A10.2, leetgo's values).
const (
	FreeInterval    = 10 * time.Second
	PremiumInterval = time.Second
)

// Pacer keeps runs and submits apart. It only measures: the UI shows the
// remaining seconds and waits with ui.After.
type Pacer struct {
	Interval time.Duration
	last     time.Time
}

// NewPacer is a pacer for a free (10s) or premium (1s) account.
func NewPacer(premium bool) *Pacer {
	if premium {
		return &Pacer{Interval: PremiumInterval}
	}
	return &Pacer{Interval: FreeInterval}
}

// Remaining is how long to wait before the next run or submit (0 = now). A
// clock that went back waits the whole interval.
func (p *Pacer) Remaining(now time.Time) time.Duration {
	if p.last.IsZero() {
		return 0
	}
	elapsed := now.Sub(p.last)
	if elapsed < 0 {
		return p.Interval
	}
	if elapsed >= p.Interval {
		return 0
	}
	return p.Interval - elapsed
}

// Mark records a run or submit sent at now.
func (p *Pacer) Mark(now time.Time) { p.last = now }
