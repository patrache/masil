package share

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// Errors shared by Host and Guest.
var (
	ErrNotStarted  = errors.New("share: not started")
	ErrEnded       = errors.New("share: share is over")
	ErrNeedsVerify = errors.New("share: this level needs the check numbers compared")
	ErrNoRequest   = errors.New("share: no join request for that seat")
	ErrLevel       = errors.New("share: level does not allow this")
	ErrNotReady    = errors.New("share: key exchange not finished")
	errOffline     = errors.New("share: not connected")
)

// queued is one outgoing frame bound to the socket it was sealed for.
type queued struct {
	conn  Conn
	frame []byte
}

// link is the transport both Host and Guest sit on: the current socket, an
// ordered outbox, redial timing, chunk reassembly and the rejected-frame
// window.
//
// Lock order: the owner's state lock (Host.mu / Guest.mu) -> DocManager.mu ->
// outMu / evMu. sendMu is only ever taken alone (flush).
type link struct {
	shareID string
	dial    DialFunc
	manual  bool
	clock   Clock

	ctx    context.Context
	cancel context.CancelFunc

	// outMu guards conn and outbox. Sealing happens under it so outbox
	// order is counter order.
	outMu  sync.Mutex
	conn   Conn
	outbox []queued
	// bseal and bsign are the owner's broadcast sealer (K_s) and signer;
	// they live under outMu so a rekey swaps them between two frames.
	bseal *wscrypto.Sealer
	bsign *wscrypto.Signer
	// sendMu serialises flush so frames reach the socket in outbox order.
	sendMu sync.Mutex

	// The fields below belong to the owner's state lock.
	tickNow  time.Time
	started  bool
	down     bool // waiting to redial
	nextDial time.Time
	backoff  time.Duration
	reasm    wire.Reassembler
	rejects  []time.Time

	evMu sync.Mutex
	evs  []Event
}

func newLink(shareID string, dial DialFunc, clock Clock, manual bool) link {
	if clock == nil {
		clock = wallClock{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	return link{shareID: shareID, dial: dial, clock: clock, manual: manual, ctx: ctx, cancel: cancel,
		tickNow: clock.Now(), backoff: reconnectMin}
}

// now is the engine time: the last Tick's time under ManualTick, else the
// clock. State lock held.
func (l *link) now() time.Time {
	if l.manual {
		return l.tickNow
	}
	return l.clock.Now()
}

func (l *link) emit(e Event) {
	l.evMu.Lock()
	l.evs = append(l.evs, e)
	l.evMu.Unlock()
}

// Drain hands out the events gathered since the last call.
func (l *link) drainEvents() []Event {
	l.evMu.Lock()
	defer l.evMu.Unlock()
	out := l.evs
	l.evs = nil
	return out
}

func (l *link) setConn(c Conn) {
	l.outMu.Lock()
	l.conn = c
	l.outMu.Unlock()
}

func (l *link) curConn() Conn {
	l.outMu.Lock()
	defer l.outMu.Unlock()
	return l.conn
}

// flush writes the outbox to the sockets in order. Never call it with a
// state lock held (a real socket write can block).
func (l *link) flush() {
	l.sendMu.Lock()
	defer l.sendMu.Unlock()
	for {
		l.outMu.Lock()
		batch := l.outbox
		l.outbox = nil
		l.outMu.Unlock()
		if len(batch) == 0 {
			return
		}
		for _, q := range batch {
			if q.conn != nil {
				_ = q.conn.Send(q.frame)
			}
		}
	}
}

// flushManual flushes at once under ManualTick (tests pump the relay right
// after a call); otherwise the tick loop flushes within 10 ms.
func (l *link) flushManual() {
	if l.manual {
		l.flush()
	}
}

// sendPlain queues a plaintext JSON frame (Server and Handshake classes).
func (l *link) sendPlain(kind wire.Kind, from, to uint8, body any) error {
	p, err := json.Marshal(body)
	if err != nil {
		return err
	}
	h := wire.Header{Kind: kind, From: from, To: to, Len: uint32(len(p))}
	hb := h.Marshal()
	f := append(hb[:], p...)
	if len(f) > wire.MaxFrame {
		return wire.ErrTooLarge
	}
	l.outMu.Lock()
	defer l.outMu.Unlock()
	if l.conn == nil {
		return errOffline
	}
	l.outbox = append(l.outbox, queued{conn: l.conn, frame: f})
	return nil
}

// sendSealed seals pt with s (and signs each frame when signer is set),
// chunked to the frame limit, and queues the frames. Only the first chunk
// keeps FlagGroupStart. It returns the bytes queued.
func (l *link) sendSealed(s *wscrypto.Sealer, signer *wscrypto.Signer, kind wire.Kind, flags, from, to uint8, pt []byte) (int, error) {
	l.outMu.Lock()
	defer l.outMu.Unlock()
	return l.sealLocked(s, signer, kind, flags, from, to, pt)
}

// sendBroadcast seals pt with the current K_s, signs it and queues it for
// every member (To = SeatAll).
func (l *link) sendBroadcast(kind wire.Kind, flags uint8, pt []byte) (int, error) {
	l.outMu.Lock()
	defer l.outMu.Unlock()
	if l.bseal == nil || l.bsign == nil {
		return 0, ErrNotReady
	}
	return l.sealLocked(l.bseal, l.bsign, kind, flags, wire.SeatOwner, wire.SeatAll, pt)
}

// sealLocked is sendSealed with outMu held.
func (l *link) sealLocked(s *wscrypto.Sealer, signer *wscrypto.Signer, kind wire.Kind, flags, from, to uint8, pt []byte) (int, error) {
	if s == nil {
		return 0, ErrNotReady
	}
	if l.conn == nil {
		return 0, errOffline
	}
	extra := 0
	if signer != nil {
		extra = wscrypto.SignatureLen
	}
	max := wscrypto.MaxPlaintext(extra)
	total := 0
	for first := true; ; first = false {
		n := min(max, len(pt))
		piece := pt[:n]
		pt = pt[n:]
		fl := flags &^ wire.FlagMore
		if !first {
			fl &^= wire.FlagGroupStart
		}
		if len(pt) > 0 {
			fl |= wire.FlagMore
		}
		f, err := s.Seal(wire.Header{Kind: kind, Flags: fl, From: from, To: to}, piece, extra)
		if err != nil {
			return total, err
		}
		if signer != nil {
			f = signer.Sign(f)
		}
		l.outbox = append(l.outbox, queued{conn: l.conn, frame: f})
		total += len(f)
		if len(pt) == 0 {
			return total, nil
		}
	}
}

// noteReject records a rejected frame at now and reports whether the
// tamper threshold was reached (the window is then cleared, so one burst
// warns once).
func (l *link) noteReject(now time.Time) bool {
	cut := now.Add(-tamperWindow)
	kept := l.rejects[:0]
	for _, t := range l.rejects {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	l.rejects = append(kept, now)
	if len(l.rejects) >= tamperCount {
		l.rejects = nil
		return true
	}
	return false
}

// recvAll hands every frame already received to handle, without blocking.
func (l *link) recvAll(handle func([]byte)) {
	c := l.curConn()
	if c == nil {
		return
	}
	for {
		select {
		case f, ok := <-c.Recv():
			if !ok {
				return
			}
			handle(f)
		default:
			return
		}
	}
}

// connGone checks whether the current socket ended. It returns the end
// reason when the share is over for this side ("" when the engine should
// redial, which it schedules).
func (l *link) connGone(now time.Time) (gone bool, endReason string) {
	c := l.curConn()
	if c == nil {
		return false, ""
	}
	select {
	case <-c.Done():
	default:
		return false, ""
	}
	l.setConn(nil)
	l.reasm = wire.Reassembler{}
	switch CloseCodeOf(c.Err()) {
	case CloseEnded:
		return true, ReasonEnded
	case CloseKicked:
		return true, ReasonKicked
	case CloseReplaced:
		return true, ReasonReplaced
	case CloseProtocol:
		return true, ReasonProtocol
	case CloseRateLimited:
		l.down, l.nextDial = true, now.Add(rateLimitedWait)
	default:
		l.down, l.nextDial = true, now.Add(l.backoff)
		l.backoff = nextBackoff(l.backoff)
	}
	return true, ""
}

// redial dials when the backoff has passed. It returns an end reason when
// the share is gone (404), "" otherwise; ok reports a new socket.
func (l *link) redial(now time.Time) (ok bool, endReason string) {
	if !l.down || now.Before(l.nextDial) {
		return false, ""
	}
	c, err := l.dial(l.ctx, l.shareID)
	if err != nil {
		var ae *api.Error
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			l.down = false
			return false, ReasonNotFound
		}
		l.nextDial = now.Add(l.backoff)
		l.backoff = nextBackoff(l.backoff)
		return false, ""
	}
	l.down = false
	l.backoff = reconnectMin
	l.setConn(c)
	return true, ""
}

// closeConn closes the socket (code 1000 unless given) and forgets it.
func (l *link) closeConn(code int, reason string) {
	c := l.curConn()
	l.setConn(nil)
	if c != nil {
		c.Close(code, reason)
	}
}

// unmarshal decodes an inner JSON body.
func unmarshal(p []byte, v any) bool { return json.Unmarshal(p, v) == nil }

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
