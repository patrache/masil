package share

import "time"

// Clock is the engine's time source. Tests pass a fixed clock and drive the
// engine with Tick (ManualTick); otherwise the wall clock is used.
type Clock interface{ Now() time.Time }

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

const (
	// tickEvery is the internal tick period when ManualTick is off.
	tickEvery = 10 * time.Millisecond
	// reconnectMin and reconnectMax bound the exponential redial backoff.
	reconnectMin = time.Second
	reconnectMax = 30 * time.Second
	// dialTimeout bounds one redial (a connect that hangs).
	dialTimeout = 10 * time.Second
	// rateLimitedWait is the redial delay after a 4008 close.
	rateLimitedWait = 10 * time.Second
	// JoinTimeout is how long a join request waits for the owner.
	JoinTimeout = 5 * time.Minute
	// tamperWindow / tamperCount: this many rejected frames within the
	// window raise EventTamperWarning (spec B9).
	tamperWindow = 10 * time.Second
	tamperCount  = 5
)

func nextBackoff(d time.Duration) time.Duration {
	if d < reconnectMin {
		return reconnectMin
	}
	d *= 2
	if d > reconnectMax {
		d = reconnectMax
	}
	return d
}
