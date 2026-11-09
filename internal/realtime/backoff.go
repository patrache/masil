package realtime

import (
	"math/rand/v2"
	"time"
)

// Reconnect backoff (events.md 1.3): delay = rand(0, min(30s, 0.5s × 2^n))
// (full jitter), n = consecutive failures, reset after 60s live.
const (
	backoffBase  = 500 * time.Millisecond
	backoffCap   = 30 * time.Second
	liveResetAge = 60 * time.Second
)

// backoffDelay returns the jittered delay for the n-th consecutive failure;
// rnd is a uniform sample in [0, 1).
func backoffDelay(n int, rnd float64) time.Duration {
	if n < 0 {
		n = 0
	}
	ceil := backoffCap
	if n < 6 { // 0.5s << 6 = 32s already exceeds the cap (and avoids overflow)
		ceil = min(backoffCap, backoffBase<<n)
	}
	if rnd <= 0 {
		return 0
	}
	if rnd >= 1 {
		return ceil
	}
	return time.Duration(rnd * float64(ceil))
}

// Backoff is the reconnect delay sequence above for another socket (the room
// document socket, internal/collab): Next gives the delay before the next
// attempt and counts a failure; Reset after a connection that went live.
type Backoff struct {
	n   int
	rnd func() float64
}

// NewBackoff returns a Backoff drawing its jitter from rnd (uniform in
// [0, 1)); nil rnd = math/rand/v2.
func NewBackoff(rnd func() float64) *Backoff {
	if rnd == nil {
		rnd = rand.Float64
	}
	return &Backoff{rnd: rnd}
}

// Next returns the delay for the next attempt and counts one failure.
func (b *Backoff) Next() time.Duration {
	d := backoffDelay(b.n, b.rnd())
	if b.n < 64 {
		b.n++
	}
	return d
}

// Reset forgets the failures (the next delay is the first one again).
func (b *Backoff) Reset() { b.n = 0 }
