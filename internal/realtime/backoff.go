package realtime

import "time"

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
