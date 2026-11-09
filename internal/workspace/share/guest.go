package share

import (
	"context"
	"crypto/ed25519"
	"errors"
	"sync"
	"time"

	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// GuestConfig sets up the participant side of one share.
type GuestConfig struct {
	ShareID, OwnerID, UserID string
	Dial                     DialFunc
	Clock                    Clock
	ManualTick               bool
}

type guestPhase int

const (
	phaseIdle      guestPhase = iota
	phaseCommitted            // hs_commit sent, waiting for hs_owner_pub
	phaseDerived              // keys and check number known, waiting for the grant
	phaseGranted
	phaseResuming // hs_resume sent after a reconnect
)

// Guest is the participant side of a share.
type Guest struct {
	link
	cfg GuestConfig

	mu       sync.Mutex
	over     bool
	haveSeat bool
	seat     uint8
	phase    guestPhase
	confirm  *bool // my answer, once given

	kp  *wscrypto.KeyPair
	sas string
	p2o *wscrypto.Sealer
	o2p *wscrypto.Opener

	// Session (broadcast) key; prevOpen keeps the previous epoch for frames
	// still in flight across a rekey.
	sess     wscrypto.SessionKey
	bopen    *wscrypto.Opener
	prevOpen *wscrypto.Opener
	signPub  ed25519.PublicKey

	level    wire.Level
	verified bool
	kbUntil  *time.Time

	docs map[string]*RemoteDoc
}

// NewGuest prepares a guest; Start connects.
func NewGuest(cfg GuestConfig) *Guest {
	return &Guest{link: newLink(cfg.ShareID, cfg.Dial, cfg.Clock, cfg.ManualTick), cfg: cfg, docs: map[string]*RemoteDoc{}}
}

// Start connects; the key exchange starts when the server says hello.
func (g *Guest) Start(ctx context.Context) error {
	c, err := g.dial(ctx, g.shareID)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.setConn(c)
	g.started = true
	g.mu.Unlock()
	if !g.manual {
		go g.loop()
	}
	return nil
}

func (g *Guest) loop() {
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-t.C:
			g.Tick(g.clock.Now())
		}
	}
}

// Tick reads what arrived, redials if needed and runs the timers.
func (g *Guest) Tick(now time.Time) {
	g.mu.Lock()
	g.tickNow = now
	if !g.started || g.over {
		g.mu.Unlock()
		return
	}
	g.recvAll(g.handle)
	if !g.over {
		if gone, reason := g.connGone(now); gone {
			if reason != "" {
				g.endLocked(reason)
			} else {
				g.emit(EventReconnecting{})
			}
		}
	}
	if !g.over {
		if ok, reason := g.redial(now, g.mu.Unlock, g.mu.Lock, func() bool { return g.over }); reason != "" {
			g.endLocked(reason)
		} else if ok {
			g.recvAll(g.handle)
		}
	}
	if !g.over {
		g.tickMore(now)
	}
	g.mu.Unlock()
	g.flush()
}

// Drain returns the events since the last call.
func (g *Guest) Drain() []Event { return g.drainEvents() }

// SAS is the check number to compare ("" before the key exchange).
func (g *Guest) SAS() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sas
}

// Confirm sends my answer to the check-number comparison. false ends the
// attempt (EventDeclined{"mismatch"}).
func (g *Guest) Confirm(match bool) error {
	g.mu.Lock()
	err := g.confirmLocked(match)
	g.mu.Unlock()
	g.flush()
	if !match && err == nil {
		g.mu.Lock()
		g.declineLocked(ReasonMismatch)
		g.mu.Unlock()
	}
	return err
}

func (g *Guest) confirmLocked(match bool) error {
	if g.over {
		return ErrEnded
	}
	if g.p2o == nil || g.sas == "" {
		return ErrNotReady
	}
	if g.confirm != nil {
		return nil
	}
	m := match
	g.confirm = &m
	return g.sendOwner(wire.KindHsConfirm, mustJSON(wire.HsConfirm{Match: match}))
}

// Leave lowers my level to none and disconnects.
func (g *Guest) Leave() {
	g.mu.Lock()
	if g.over {
		g.mu.Unlock()
		return
	}
	if g.phase == phaseGranted {
		_ = g.sendOwner(wire.KindSelfLevel, mustJSON(wire.SelfLevel{Level: ""}))
	}
	g.mu.Unlock()
	g.flush()
	g.mu.Lock()
	g.endLocked(ReasonLeft)
	g.mu.Unlock()
}

// ---- frames (g.mu held) ----

func (g *Guest) handle(frame []byte) {
	if g.over {
		return
	}
	hd, body, err := wire.Parse(frame)
	if err != nil {
		return
	}
	if hd.From == wire.SeatServer {
		if hd.Kind == wire.KindSrvHello {
			var m wire.SrvHello
			if unmarshal(body, &m) {
				g.onHello(m)
			}
		}
		return
	}
	if hd.From != wire.SeatOwner || !g.haveSeat {
		return
	}
	switch wire.ClassFor(hd.Kind, wire.SeatOwner) {
	case wire.ClassHandshake:
		switch hd.Kind {
		case wire.KindHsOwnerPub:
			var m wire.HsPub
			if unmarshal(body, &m) {
				g.onOwnerPub(m.Pub)
			}
		case wire.KindHsDecline:
			var m wire.HsDecline
			if unmarshal(body, &m) {
				g.onDecline(m.Reason)
			}
		}
	case wire.ClassToPeer:
		if g.o2p == nil {
			return
		}
		h2, pt, err := g.o2p.Open(frame, 0)
		if err != nil {
			g.rejected()
			return
		}
		g.assemble(h2, pt)
	case wire.ClassBroadcast:
		if g.bopen == nil {
			return
		}
		h2, pt, err := g.openBroadcast(frame)
		if err != nil {
			g.rejected()
			return
		}
		g.assemble(h2, pt)
	}
}

func (g *Guest) openBroadcast(frame []byte) (wire.Header, []byte, error) {
	stripped, err := wscrypto.Verify(g.signPub, frame)
	if err != nil {
		return wire.Header{}, nil, err
	}
	h, pt, err := g.bopen.Open(stripped, wscrypto.SignatureLen)
	if errors.Is(err, wscrypto.ErrAuth) && g.prevOpen != nil {
		h, pt, err = g.prevOpen.Open(stripped, wscrypto.SignatureLen)
	}
	return h, pt, err
}

func (g *Guest) rejected() {
	if g.noteReject(g.now()) {
		g.emit(EventTamperWarning{})
	}
}

func (g *Guest) assemble(h wire.Header, pt []byte) {
	full, done, err := g.reasm.Add(h, pt)
	if err != nil || !done {
		return
	}
	g.dispatch(h.Kind, full)
}

func (g *Guest) onHello(m wire.SrvHello) {
	if m.ShareID != g.shareID {
		return
	}
	g.seat, g.haveSeat = m.Seat, true
	if g.bopen != nil && g.p2o != nil {
		g.phase = phaseResuming
		_ = g.sendOwner(wire.KindHsResume, mustJSON(wire.HsResume{}))
		return
	}
	g.startHandshake()
}

func (g *Guest) startHandshake() {
	kp, err := wscrypto.NewKeyPair()
	if err != nil {
		return
	}
	g.kp, g.sas, g.p2o, g.o2p, g.confirm = kp, "", nil, nil, nil
	g.bopen, g.prevOpen, g.signPub = nil, nil, nil
	g.phase = phaseCommitted
	_ = g.sendPlain(wire.KindHsCommit, g.seat, wire.SeatOwner, wire.HsCommit{Commit: wscrypto.Commit(g.shareID, kp.Public())})
}

func (g *Guest) onOwnerPub(pub []byte) {
	if g.phase != phaseCommitted || g.kp == nil {
		return
	}
	mine := g.kp.Public()
	keys, err := wscrypto.Derive(g.kp, pub, wscrypto.Transcript{
		ShareID: g.shareID, OwnerID: g.cfg.OwnerID, ParticipantID: g.cfg.UserID,
		OwnerPub: pub, ParticipantPub: mine,
	})
	if err != nil {
		return
	}
	g.p2o = wscrypto.NewSealer(keys.P2O, wscrypto.DirP2O)
	g.o2p = wscrypto.NewOpener(keys.O2P, wscrypto.DirO2P)
	g.sas = keys.SAS
	g.phase = phaseDerived
	_ = g.sendPlain(wire.KindHsReveal, g.seat, wire.SeatOwner, wire.HsPub{Pub: mine})
	g.emit(EventSASReady{SAS: keys.SAS})
}

func (g *Guest) onDecline(reason string) {
	if reason == reasonResumeBad && g.phase == phaseResuming {
		g.startHandshake()
		return
	}
	if g.phase == phaseGranted {
		return // a decline after the grant is not from this exchange
	}
	g.declineLocked(reason)
}

func (g *Guest) dispatch(kind wire.Kind, p []byte) {
	switch kind {
	case wire.KindHsGrant:
		var m wire.HsGrant
		if unmarshal(p, &m) {
			g.onGrant(m)
		}
	default:
		g.dispatchMore(kind, p)
	}
}

func (g *Guest) onGrant(m wire.HsGrant) {
	if len(m.SessionKey) != 32 || len(m.SignPub) != ed25519.PublicKeySize {
		return
	}
	lvl, ok := wire.ParseLevel(m.Level)
	if !ok {
		return
	}
	var key [32]byte
	copy(key[:], m.SessionKey)
	if g.bopen == nil || m.Epoch != g.sess.Epoch || key != g.sess.Key {
		g.prevOpen = g.bopen
		g.bopen = wscrypto.NewOpener(key, wscrypto.DirBroadcast)
		g.sess = wscrypto.SessionKey{Epoch: m.Epoch, Key: key}
	}
	g.signPub = append(ed25519.PublicKey(nil), m.SignPub...)
	g.level, g.verified, g.kbUntil = lvl, m.Verified, m.KeyboardUntil
	resumed := g.phase == phaseResuming
	g.phase = phaseGranted
	if resumed {
		g.emit(EventResumed{})
		g.afterResume()
		return
	}
	g.emit(EventGranted{Level: lvl, Verified: m.Verified, KeyboardUntil: m.KeyboardUntil})
}

// sendOwner seals body for the owner with my P2O key.
func (g *Guest) sendOwner(kind wire.Kind, body []byte) error {
	if g.p2o == nil {
		return ErrNotReady
	}
	_, err := g.sendSealed(g.p2o, nil, kind, 0, g.seat, wire.SeatOwner, body)
	return err
}

func (g *Guest) declineLocked(reason string) {
	if g.over {
		return
	}
	g.emit(EventDeclined{Reason: reason})
	g.stopLocked()
}

func (g *Guest) endLocked(reason string) {
	if g.over {
		return
	}
	g.emit(EventEnded{Reason: reason})
	g.stopLocked()
}

// stopLocked closes the socket and drops every key.
func (g *Guest) stopLocked() {
	g.over = true
	g.down = false
	g.cancel()
	if c := g.curConn(); c != nil {
		g.setConn(nil)
		go c.Close(1000, "")
	}
	clear(g.sess.Key[:])
	g.kp, g.p2o, g.o2p, g.bopen, g.prevOpen, g.signPub = nil, nil, nil, nil, nil, nil
}

// dispatchMore handles the owner's messages past the grant.
func (g *Guest) dispatchMore(kind wire.Kind, p []byte) {
	switch kind {
	case wire.KindRoster:
		var m wire.Roster
		if unmarshal(p, &m) {
			g.onRoster(m)
		}
	case wire.KindRekey:
		var m wire.Rekey
		if unmarshal(p, &m) && len(m.Key) == 32 {
			var key [32]byte
			copy(key[:], m.Key)
			g.prevOpen = g.bopen
			g.bopen = wscrypto.NewOpener(key, wscrypto.DirBroadcast)
			clear(g.sess.Key[:])
			g.sess = wscrypto.SessionKey{Epoch: m.Epoch, Key: key}
		}
	case wire.KindEnded:
		var m wire.Ended
		if unmarshal(p, &m) {
			g.endLocked(ReasonEnded)
		}
	case wire.KindNotice:
		var m wire.Notice
		if !unmarshal(p, &m) {
			return
		}
		if m.Code == NoticeResumed {
			g.emit(EventResumed{})
			g.afterResume()
			return
		}
		g.onNotice(m)
	case wire.KindTermOut, wire.KindTermKey:
		g.onTerm(kind, p)
	case wire.KindTermStatus:
		var m wire.TermStatus
		if unmarshal(p, &m) {
			g.emit(EventTermStatus{TermStatus: m})
		}
	case wire.KindPropResult:
		var m wire.PropResult
		if unmarshal(p, &m) {
			g.emit(EventPropResult{PropResult: m})
		}
	case wire.KindDocState:
		var m wire.DocState
		if unmarshal(p, &m) {
			g.onDocState(m)
		}
	case wire.KindDocAck:
		var m wire.DocAck
		if unmarshal(p, &m) {
			g.onDocAck(m)
		}
	case wire.KindDocBcast:
		var m wire.DocBcast
		if unmarshal(p, &m) {
			g.onDocBcast(m)
		}
	case wire.KindDocReject:
		var m wire.DocReject
		if unmarshal(p, &m) {
			g.onDocReject(m)
		}
	case wire.KindDocPresence:
		var m wire.DocPresence
		if unmarshal(p, &m) && m.Seat != g.seat {
			g.emit(EventPresence{DocPresence: m})
		}
	case wire.KindTreeList:
		var m wire.TreeList
		if unmarshal(p, &m) {
			g.emit(EventTree{TreeList: m})
		}
	case wire.KindFsResult:
		var m wire.FsResult
		if unmarshal(p, &m) {
			g.emit(EventFsResult{FsResult: m})
		}
	}
}

// Hooks later parts of the engine fill in.

func (g *Guest) tickMore(now time.Time) { g.tickDocs(now) }
func (g *Guest) onNotice(m wire.Notice) { g.emit(EventNotice{Notice: m}) }
