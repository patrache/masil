package termhost

import "sync"

// Tap receives a copy of everything a pane's program writes (the base for
// sharing a terminal, A7.7). Output runs on the tap's own goroutine; a tap
// that falls more than tapBuffer bytes behind is detached. masil overlays are
// drawn outside the pane and never reach a tap.
type Tap interface{ Output(b []byte) }

// tapBuffer bounds the bytes waiting for one tap.
const tapBuffer = 1 << 20

// tapSink buffers output for one Tap between the pane reader (producer,
// never waits) and the tap goroutine (consumer).
type tapSink struct {
	tap  Tap
	mu   sync.Mutex
	buf  []byte
	dead bool
	wake chan struct{}
	quit chan struct{}
	once sync.Once
}

func newTapSink(t Tap) *tapSink {
	s := &tapSink{tap: t, wake: make(chan struct{}, 1), quit: make(chan struct{})}
	go s.loop()
	return s
}

// push queues b; false means the sink overflowed (it is now dead and its
// pending bytes are dropped).
func (s *tapSink) push(b []byte) bool {
	s.mu.Lock()
	if s.dead {
		s.mu.Unlock()
		return false
	}
	if len(s.buf)+len(b) > tapBuffer {
		s.mu.Unlock()
		s.close()
		return false
	}
	s.buf = append(s.buf, b...)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return true
}

func (s *tapSink) loop() {
	for {
		select {
		case <-s.quit:
			return
		case <-s.wake:
		}
		s.mu.Lock()
		chunk := s.buf
		s.buf = nil
		s.mu.Unlock()
		if len(chunk) > 0 {
			select {
			case <-s.quit:
				return
			default:
			}
			s.tap.Output(chunk)
		}
	}
}

func (s *tapSink) close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.dead = true
		s.buf = nil
		s.mu.Unlock()
		close(s.quit)
	})
}

// AttachTap starts copying the program's output to t. snapshot redraws the
// current screen and restores the state later output depends on (feed it to
// NewReplay of the same size, then the tap bytes; see screen.Snapshot for the
// few things it cannot carry); every byte after it goes to t, with nothing
// lost or repeated in between. detach stops the copy (idempotent). Taps are
// off unless a sharing feature attaches one.
func (p *Pane) AttachTap(t Tap) (snapshot []byte, detach func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot = p.scr.Snapshot()
	if p.closed {
		return snapshot, func() {}
	}
	s := newTapSink(t)
	p.taps = append(p.taps, s)
	return snapshot, func() {
		p.mu.Lock()
		p.removeTapLocked(s)
		p.mu.Unlock()
		s.close()
	}
}

// feedTaps copies chunk to every tap, detaching the ones that overflow.
// Called by the reader under p.mu, right after the screen took the chunk.
func (p *Pane) feedTaps(chunk []byte) {
	if len(p.taps) == 0 {
		return
	}
	for _, s := range append([]*tapSink(nil), p.taps...) {
		if !s.push(chunk) {
			p.removeTapLocked(s)
			p.log.Warn("termhost pane tap fell behind, detached", "program", p.name())
		}
	}
}

func (p *Pane) removeTapLocked(s *tapSink) {
	for i, q := range p.taps {
		if q == s {
			p.taps = append(p.taps[:i], p.taps[i+1:]...)
			return
		}
	}
}

// closeTapsLocked detaches every tap (the pane is gone).
func (p *Pane) closeTapsLocked() {
	for _, s := range p.taps {
		s.close()
	}
	p.taps = nil
}

// Shared reports whether at least one tap is attached.
func (p *Pane) Shared() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.taps) > 0
}

// Inject writes b to the program as input from another source (a sharing
// peer). source names it for diagnostics; the bytes are never logged. Like
// SendKey it never blocks: when the input queue is full, b is dropped.
func (p *Pane) Inject(source string, b []byte) {
	if len(b) == 0 {
		return
	}
	select {
	case <-p.exited:
		return
	default:
	}
	select {
	case p.in <- append([]byte(nil), b...):
	default:
		if p.dropped.Add(1) == 1 {
			p.log.Warn("termhost pane injected input dropped", "program", p.name(), "source", source)
		}
	}
}
