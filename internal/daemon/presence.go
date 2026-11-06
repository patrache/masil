package daemon

import (
	"sync"
	"time"
)

// tuiGrace is how long after the server reports tui_connected:false an alert
// is held before being judged again (events.md 7, CA 9.2).
const tuiGrace = 3 * time.Second

// presence judges whether a TUI is running on this device (CA 9.2). The local
// tui.lock probe wins whenever it answers; only when it fails does the server
// signal (hello.local_tui_connected / device.tui_state) decide, with a grace
// period after a true->false transition. Safe for concurrent use: observe runs
// on the realtime goroutine and decide on the handler goroutine.
type presence struct {
	probe func() (bool, error)
	now   func() time.Time

	mu        sync.Mutex
	server    *bool     // last server signal; nil = unknown
	flippedAt time.Time // when the server signal last went true -> false
}

func newPresence(probe func() (bool, error), now func() time.Time) *presence {
	return &presence{probe: probe, now: now}
}

// observe records the server's TUI signal from realtime State.TUIOnDevice. The
// grace period starts only on a true->false transition; a first false (from
// hello) means no TUI was there, so it needs no grace. nil keeps the last known
// signal.
func (p *presence) observe(tuiOnDevice *bool) {
	if tuiOnDevice == nil {
		return
	}
	v := *tuiOnDevice
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.server != nil && *p.server && !v {
		p.flippedAt = p.now()
	}
	p.server = &v
}

// decide reports whether a TUI is present. wait > 0 means the answer is "not
// present" only provisionally: the caller should judge again after wait.
func (p *presence) decide() (present bool, wait time.Duration) {
	if running, err := p.probe(); err == nil {
		return running, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.server == nil:
		// Unknown: rather show a duplicate than miss the alert.
		return false, 0
	case *p.server:
		return true, 0
	case !p.flippedAt.IsZero():
		if elapsed := p.now().Sub(p.flippedAt); elapsed < tuiGrace {
			return false, tuiGrace - elapsed
		}
	}
	return false, 0
}
