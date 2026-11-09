package share

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/fsroot"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// HostConfig sets up the owner side of one share.
type HostConfig struct {
	ShareID, OwnerID, Title string
	Dial                    DialFunc
	// Audit is the owner-PC record (nil = no record).
	Audit *audit.Log
	// Clock defaults to the wall clock.
	Clock Clock
	// ManualTick: no internal goroutines; the caller drives Tick (tests).
	ManualTick bool
	// Root is the shared folder (tree listing, file requests).
	Root *fsroot.Root
}

// JoinRequest is a participant waiting for the owner after the key exchange.
type JoinRequest struct {
	Seat   uint8
	UserID string
	SAS    string
	// PeerMatched is the participant's own answer (nil until they press it).
	PeerMatched *bool
	Expires     time.Time
}

// joinState is one key exchange in progress on the owner side.
type joinState struct {
	seat     uint8
	user     string
	commit   []byte
	kp       *wscrypto.KeyPair
	derived  bool
	sas      string
	o2p      *wscrypto.Sealer
	p2o      *wscrypto.Opener
	matched  *bool
	approved *wire.Level // owner said yes with verification; waits for hs_confirm
	expires  time.Time
}

// member is a let-in participant.
type member struct {
	seat      uint8
	user      string
	level     wire.Level
	verified  bool
	connected bool
	goneAt    time.Time
	o2p       *wscrypto.Sealer
	p2o       *wscrypto.Opener
}

// Host is the owner side of a share: it runs the key exchanges, keeps the
// level table and is the first permission check for everything participants
// send. One lock owns the state; Tick (or the internal 10 ms loop) reads the
// socket and runs the timers.
type Host struct {
	link
	cfg HostConfig

	mu     sync.Mutex
	killed atomic.Bool
	over   bool // ended for any reason

	sess   wscrypto.SessionKey
	bseal  *wscrypto.Sealer
	signer *wscrypto.Signer

	users   map[uint8]string // seat -> user id as the server says
	online  map[uint8]bool
	joins   map[uint8]*joinState
	members map[uint8]*member
	hellos  int // srv_hello count (>1 = reconnected)
}

// NewHost prepares a host; Start connects.
func NewHost(cfg HostConfig) *Host {
	return &Host{
		link: newLink(cfg.ShareID, cfg.Dial, cfg.Clock, cfg.ManualTick),
		cfg:  cfg, users: map[uint8]string{}, online: map[uint8]bool{},
		joins: map[uint8]*joinState{}, members: map[uint8]*member{},
	}
}

// Start makes the session and signing keys, connects and records the share
// start. Without ManualTick it starts the 10 ms loop.
func (h *Host) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return nil
	}
	sess, err := wscrypto.NewSessionKey(1)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	signer, err := wscrypto.NewSigner()
	if err != nil {
		h.mu.Unlock()
		return err
	}
	h.sess, h.signer = sess, signer
	h.bseal = wscrypto.NewSealer(sess.Key, wscrypto.DirBroadcast)
	h.mu.Unlock()

	c, err := h.dial(ctx, h.shareID)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.setConn(c)
	h.started = true
	h.record(audit.EvShareStart, 0, "", map[string]any{"title": h.cfg.Title})
	h.mu.Unlock()
	if !h.manual {
		go h.loop()
	}
	return nil
}

func (h *Host) loop() {
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-t.C:
			h.Tick(h.clock.Now())
		}
	}
}

// Tick reads what arrived, redials if needed and runs the timers. Under
// ManualTick the test calls it; now is then the engine time.
func (h *Host) Tick(now time.Time) {
	h.mu.Lock()
	h.tickNow = now
	if !h.started || h.over {
		h.mu.Unlock()
		return
	}
	if h.killed.Load() {
		h.recvAll(func([]byte) {}) // drop everything
		h.mu.Unlock()
		return
	}
	h.recvAll(h.handle)
	if gone, reason := h.connGone(now); gone {
		if reason != "" {
			h.finishLocked(reason, "remote")
			h.mu.Unlock()
			h.flush()
			return
		}
		h.emit(EventReconnecting{})
	}
	if ok, reason := h.redial(now); reason != "" {
		h.finishLocked(reason, "remote")
	} else if ok {
		h.recvAll(h.handle)
	}
	h.tickTimers(now)
	h.mu.Unlock()
	h.flush()
}

// Drain returns the events since the last call. It never blocks on the
// engine.
func (h *Host) Drain() []Event { return h.drainEvents() }

// Pending lists the join requests waiting for the owner, by seat.
func (h *Host) Pending() []JoinRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []JoinRequest
	for _, j := range h.joins {
		if j.derived {
			out = append(out, j.request())
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Seat < out[b].Seat })
	return out
}

func (j *joinState) request() JoinRequest {
	var m *bool
	if j.matched != nil {
		v := *j.matched
		m = &v
	}
	return JoinRequest{Seat: j.seat, UserID: j.user, SAS: j.sas, PeerMatched: m, Expires: j.expires}
}

// Approve lets a waiting participant in at level. Without verified only the
// viewer and prompter levels are allowed (ErrNeedsVerify); with verified the
// grant waits for the participant's own "match".
func (h *Host) Approve(seat uint8, level wire.Level, verified bool) error {
	h.mu.Lock()
	err := h.approveLocked(seat, level, verified)
	h.mu.Unlock()
	h.flushManual()
	return err
}

func (h *Host) approveLocked(seat uint8, level wire.Level, verified bool) error {
	if h.over || h.killed.Load() {
		return ErrEnded
	}
	if level == wire.LevelKeyboard {
		return ErrUseGrant
	}
	if level < wire.LevelViewer || level > wire.LevelEditor {
		return ErrLevel
	}
	j := h.joins[seat]
	if j == nil || !j.derived {
		return ErrNoRequest
	}
	if !verified {
		if level > wire.LevelPrompter {
			return ErrNeedsVerify
		}
		h.grant(j, level, false)
		return nil
	}
	if j.matched != nil && *j.matched {
		h.grant(j, level, true)
		return nil
	}
	l := level
	j.approved = &l
	return nil
}

// Decline turns a join request down; mismatch means the check numbers
// differed (MITM warning and record).
func (h *Host) Decline(seat uint8, mismatch bool) {
	h.mu.Lock()
	if j := h.joins[seat]; j != nil {
		reason := ReasonDeclined
		if mismatch {
			reason = ReasonMismatch
			h.record(audit.EvSAS, int(seat), j.user, map[string]any{"result": "mismatch"})
			h.emit(EventMITMWarning{Seat: seat})
		}
		h.record(audit.EvJoinDecline, int(seat), j.user, map[string]any{"reason": reason})
		_ = h.sendPlain(wire.KindHsDecline, wire.SeatOwner, seat, wire.HsDecline{Reason: reason})
		delete(h.joins, seat)
	}
	h.mu.Unlock()
	h.flushManual()
}

// ---- frames (h.mu held) ----

func (h *Host) handle(frame []byte) {
	hd, body, err := wire.Parse(frame)
	if err != nil {
		return
	}
	if hd.From == wire.SeatServer {
		h.handleServer(hd.Kind, body)
		return
	}
	seat := hd.From
	if seat < 1 || seat > wire.MaxSeat {
		return
	}
	switch wire.ClassFor(hd.Kind, seat) {
	case wire.ClassHandshake:
		switch hd.Kind {
		case wire.KindHsCommit:
			var m wire.HsCommit
			if unmarshal(body, &m) {
				h.onCommit(seat, m.Commit)
			}
		case wire.KindHsReveal:
			var m wire.HsPub
			if unmarshal(body, &m) {
				h.onReveal(seat, m.Pub)
			}
		}
	case wire.ClassToOwner:
		h.handleSealed(seat, hd.Kind, frame)
	default:
		h.rejectFrame(seat, hd.Kind, "class", false)
	}
}

func (h *Host) handleServer(kind wire.Kind, body []byte) {
	now := h.now()
	switch kind {
	case wire.KindSrvHello:
		var m wire.SrvHello
		if !unmarshal(body, &m) {
			return
		}
		h.hellos++
		seen := map[uint8]bool{}
		for _, s := range m.Seats {
			if s.Seat >= 1 && s.Seat <= wire.MaxSeat {
				h.users[s.Seat] = s.UserID
				seen[s.Seat] = true
			}
		}
		for s := uint8(1); s <= wire.MaxSeat; s++ {
			h.setOnline(s, seen[s], now)
		}
		if h.hellos > 1 {
			h.onReconnected()
		}
	case wire.KindSrvPeer:
		var m wire.SrvPeer
		if !unmarshal(body, &m) || m.Seat < 1 || m.Seat > wire.MaxSeat {
			return
		}
		switch m.State {
		case wire.PeerJoined:
			h.users[m.Seat] = m.UserID
			h.setOnline(m.Seat, true, now)
		case wire.PeerLeft:
			h.setOnline(m.Seat, false, now)
			h.reasm.Reset(m.Seat)
			if mb := h.members[m.Seat]; mb != nil {
				h.emit(EventPeerLeft{Seat: m.Seat, UserID: mb.user})
			}
		}
	}
}

func (h *Host) setOnline(seat uint8, on bool, now time.Time) {
	was := h.online[seat]
	h.online[seat] = on
	if mb := h.members[seat]; mb != nil {
		if was && !on || !on && mb.connected {
			mb.goneAt = now
		}
		changed := mb.connected != on
		mb.connected = on
		if changed {
			h.membersChanged()
		}
	}
}

// onReconnected runs after the owner's socket came back within the grace
// period: same keys, the server is told the level table again.
func (h *Host) onReconnected() {
	h.sendLevels()
	h.afterReconnect()
}

func (h *Host) onCommit(seat uint8, commit []byte) {
	user := h.users[seat]
	if user == "" || len(commit) == 0 {
		return
	}
	kp, err := wscrypto.NewKeyPair()
	if err != nil {
		return
	}
	j := &joinState{seat: seat, user: user, commit: append([]byte(nil), commit...), kp: kp,
		expires: h.now().Add(JoinTimeout)}
	h.joins[seat] = j
	if mb := h.members[seat]; mb != nil {
		// A new handshake from a member means it restarted.
		h.memberRestarted(mb)
	}
	_ = h.sendPlain(wire.KindHsOwnerPub, wire.SeatOwner, seat, wire.HsPub{Pub: kp.Public()})
}

func (h *Host) onReveal(seat uint8, pub []byte) {
	j := h.joins[seat]
	if j == nil || j.derived {
		return
	}
	if !wscrypto.VerifyCommit(h.shareID, j.commit, pub) {
		h.record(audit.EvSAS, int(seat), j.user, map[string]any{"result": "mismatch", "reason": ReasonCommit})
		h.record(audit.EvJoinDecline, int(seat), j.user, map[string]any{"reason": ReasonCommit})
		_ = h.sendPlain(wire.KindHsDecline, wire.SeatOwner, seat, wire.HsDecline{Reason: ReasonCommit})
		h.emit(EventMITMWarning{Seat: seat})
		delete(h.joins, seat)
		return
	}
	keys, err := wscrypto.Derive(j.kp, pub, wscrypto.Transcript{
		ShareID: h.shareID, OwnerID: h.cfg.OwnerID, ParticipantID: j.user,
		OwnerPub: j.kp.Public(), ParticipantPub: pub,
	})
	if err != nil {
		_ = h.sendPlain(wire.KindHsDecline, wire.SeatOwner, seat, wire.HsDecline{Reason: ReasonCommit})
		delete(h.joins, seat)
		return
	}
	j.derived, j.sas = true, keys.SAS
	j.o2p = wscrypto.NewSealer(keys.O2P, wscrypto.DirO2P)
	j.p2o = wscrypto.NewOpener(keys.P2O, wscrypto.DirP2O)
	j.expires = h.now().Add(JoinTimeout)
	h.record(audit.EvJoinRequest, int(seat), j.user, nil)
	h.emit(EventJoinRequest{JoinRequest: j.request()})
}

// handleSealed opens a ToOwner frame of seat and dispatches it.
func (h *Host) handleSealed(seat uint8, kind wire.Kind, frame []byte) {
	var op *wscrypto.Opener
	mb := h.members[seat]
	if kind == wire.KindHsConfirm {
		if j := h.joins[seat]; j != nil {
			op = j.p2o
		}
	} else if mb != nil {
		op = mb.p2o
	}
	if op == nil {
		h.rejectFrame(seat, kind, "no_key", false)
		return
	}
	hd, pt, err := op.Open(frame, 0)
	if err != nil {
		h.rejectFrame(seat, kind, cryptoReason(err), true)
		return
	}
	if kind.MinLevel() != wire.LevelNone {
		if mb == nil || !mb.level.Allows(kind) {
			h.rejectFrame(seat, kind, "level", false)
			return
		}
	}
	full, done, err := h.reasm.Add(hd, pt)
	if err != nil {
		h.rejectFrame(seat, kind, "too_large", false)
		return
	}
	if !done {
		return
	}
	h.dispatch(seat, kind, full)
}

func cryptoReason(err error) string {
	switch {
	case errors.Is(err, wscrypto.ErrReplay):
		return "replay"
	case errors.Is(err, wscrypto.ErrSignature):
		return "signature"
	default:
		return "auth"
	}
}

// rejectFrame records a dropped frame; crypto failures also count toward
// the tamper warning.
func (h *Host) rejectFrame(seat uint8, kind wire.Kind, reason string, crypto bool) {
	h.record(audit.EvFrameRejected, int(seat), h.users[seat], map[string]any{"reason": reason, "kind": kind.String()})
	if crypto && h.noteReject(h.now()) {
		h.record(audit.EvTamperWarning, 0, "", nil)
		h.emit(EventTamperWarning{})
	}
}

// dispatch handles one opened, permitted, reassembled message from seat.
func (h *Host) dispatch(seat uint8, kind wire.Kind, p []byte) {
	switch kind {
	case wire.KindHsConfirm:
		var m wire.HsConfirm
		if unmarshal(p, &m) {
			h.onConfirm(seat, m.Match)
		}
	case wire.KindHsResume:
		h.onResume(seat)
	default:
		h.dispatchMore(seat, kind, p)
	}
}

func (h *Host) onConfirm(seat uint8, match bool) {
	j := h.joins[seat]
	if j == nil {
		return
	}
	if !match {
		h.record(audit.EvSAS, int(seat), j.user, map[string]any{"result": "mismatch", "by": "peer"})
		h.record(audit.EvJoinDecline, int(seat), j.user, map[string]any{"reason": ReasonMismatch})
		h.emit(EventMITMWarning{Seat: seat})
		_ = h.sendPlain(wire.KindHsDecline, wire.SeatOwner, seat, wire.HsDecline{Reason: ReasonMismatch})
		delete(h.joins, seat)
		return
	}
	t := true
	j.matched = &t
	if j.approved != nil {
		h.grant(j, *j.approved, true)
	}
}

// onResume answers a member's hs_resume (already opened with its P2O key):
// the current grant again, no check numbers.
func (h *Host) onResume(seat uint8) {
	mb := h.members[seat]
	if mb == nil {
		return
	}
	h.sendGrant(mb)
	h.peerResumed(mb)
}

// grant turns a finished key exchange into a member at level.
func (h *Host) grant(j *joinState, level wire.Level, verified bool) {
	mb := h.members[j.seat]
	if mb == nil {
		mb = &member{seat: j.seat, user: j.user}
		h.members[j.seat] = mb
	}
	mb.o2p, mb.p2o = j.o2p, j.p2o
	mb.level, mb.verified = level, verified
	mb.connected = h.online[j.seat]
	h.memberGranted(mb)
	delete(h.joins, j.seat)
	result := "skipped"
	if verified {
		result = "match"
	}
	h.record(audit.EvSAS, int(mb.seat), mb.user, map[string]any{"result": result})
	h.record(audit.EvJoinAllow, int(mb.seat), mb.user, map[string]any{"level": level.String(), "verified": verified})
	h.sendGrant(mb)
	h.sendLevels()
	h.emit(EventPeerJoined{Seat: mb.seat, UserID: mb.user})
	h.membersChanged()
	h.afterGrant(mb)
}

func (h *Host) sendGrant(mb *member) {
	g := wire.HsGrant{Epoch: h.sess.Epoch, SessionKey: h.sess.Key[:], SignPub: h.signer.Public(),
		Level: mb.level.String(), Verified: mb.verified}
	g.KeyboardUntil = h.keyboardUntilPtr(mb)
	_, _ = h.sendSealed(mb.o2p, nil, wire.KindHsGrant, 0, wire.SeatOwner, mb.seat, mustJSON(g))
}

// sendToPeer seals body for one member.
func (h *Host) sendToPeer(mb *member, kind wire.Kind, body []byte) {
	if mb == nil || mb.o2p == nil {
		return
	}
	_, _ = h.sendSealed(mb.o2p, nil, kind, 0, wire.SeatOwner, mb.seat, body)
}

// broadcast seals pt with K_s, signs it and sends it to every member.
func (h *Host) broadcast(kind wire.Kind, flags uint8, pt []byte) (int, error) {
	return h.sendSealed(h.bseal, h.signer, kind, flags, wire.SeatOwner, wire.SeatAll, pt)
}

// sendLevels tells the server the whole level table (ctl_levels).
func (h *Host) sendLevels() {
	m := wire.CtlLevels{Levels: map[string]string{}, Verified: map[string]bool{}}
	for s, mb := range h.members {
		if mb.level == wire.LevelNone {
			continue
		}
		k := strconv.Itoa(int(s))
		m.Levels[k] = mb.level.String()
		m.Verified[k] = mb.verified
	}
	_ = h.sendPlain(wire.KindCtlLevels, wire.SeatOwner, wire.SeatServer, m)
}

func (h *Host) tickTimers(now time.Time) {
	for s, j := range h.joins {
		if now.Before(j.expires) {
			continue
		}
		delete(h.joins, s)
		if !j.derived {
			continue
		}
		h.record(audit.EvJoinExpired, int(s), j.user, nil)
		_ = h.sendPlain(wire.KindHsDecline, wire.SeatOwner, s, wire.HsDecline{Reason: ReasonExpired})
		h.emit(EventJoinExpired{Seat: s, UserID: j.user})
	}
	h.tickMore(now)
}

// finishLocked ends the share on this side (h.mu held): keys dropped,
// socket closed, record written.
func (h *Host) finishLocked(reason, by string) {
	if h.over {
		return
	}
	h.over = true
	h.killed.Store(true)
	h.record(audit.EvShareEnd, 0, "", map[string]any{"reason": reason, "by": by})
	h.forgetKeys()
	h.cancel()
	h.emit(EventEnded{Reason: reason})
}

// forgetKeys overwrites the key material the host holds and drops every
// sealer and opener (h.mu held).
func (h *Host) forgetKeys() {
	clear(h.sess.Key[:])
	h.bseal, h.signer = nil, nil
	for _, j := range h.joins {
		j.o2p, j.p2o, j.kp = nil, nil, nil
	}
	for _, mb := range h.members {
		mb.o2p, mb.p2o = nil, nil
	}
}

// record writes one audit line (h.mu held). Errors are ignored: the record
// never stops the share.
func (h *Host) record(ev string, seat int, user string, data map[string]any) {
	if h.cfg.Audit == nil {
		return
	}
	_ = h.cfg.Audit.Write(audit.Entry{At: h.now().UTC(), Ev: ev, Seat: seat, User: user, Data: data})
}

// ErrUseGrant: the keyboard level is given with GrantKeyboard only.
var ErrUseGrant = errors.New("share: use GrantKeyboard for the keyboard level")

// Hooks later parts of the engine fill in.

func (h *Host) memberRestarted(*member)               {}
func (h *Host) memberGranted(*member)                 {}
func (h *Host) membersChanged()                       {}
func (h *Host) afterReconnect()                       {}
func (h *Host) afterGrant(*member)                    {}
func (h *Host) peerResumed(*member)                   {}
func (h *Host) keyboardUntilPtr(*member) *time.Time   { return nil }
func (h *Host) dispatchMore(uint8, wire.Kind, []byte) {}
func (h *Host) tickMore(time.Time)                    {}
