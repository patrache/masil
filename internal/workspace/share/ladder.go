package share

import (
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// Keyboard level timing (spec B8.4).
var KeyboardDurations = []time.Duration{10 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour}

const (
	DefaultKeyboard = 30 * time.Minute
	MaxKeyboard     = 2 * time.Hour
	// OwnerIdlePause: no owner input for this long pauses keyboard input.
	OwnerIdlePause = 15 * time.Minute
	// PeerGoneDemote: a keyboard holder away longer than this drops to editor.
	PeerGoneDemote = 60 * time.Second
)

// Keyboard demotion reasons (audit keyboard_expire.reason, EventKeyboardEnded).
const (
	DemoteExpired = "expired"
	DemoteGone    = "gone"
	DemoteRestart = "restart"
	DemoteRekey   = "rekey"
	DemoteOwner   = "owner"
)

// needsRekey reports whether the broadcast counter reached RekeyAfter (a
// variable so tests can stand in for 2^40 frames).
var needsRekey = (*wscrypto.Sealer).NeedsRekey

var (
	ErrNotMember   = errors.New("share: no such participant")
	ErrBadDuration = errors.New("share: keyboard duration not offered")
	ErrNotKeyboard = errors.New("share: participant has no running keyboard grant")
)

// Member is one let-in participant as the owner sees it.
type Member struct {
	Seat          uint8
	UserID        string
	Level         wire.Level
	Verified      bool
	KeyboardUntil time.Time // zero unless Level is keyboard
	Connected     bool
	LastInput     time.Time // last keyboard input that reached the terminal
}

// Members lists the participants by seat.
func (h *Host) Members() []Member {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.membersLocked()
}

func (h *Host) membersLocked() []Member {
	out := make([]Member, 0, len(h.members))
	for _, mb := range h.members {
		out = append(out, Member{Seat: mb.seat, UserID: mb.user, Level: mb.level, Verified: mb.verified,
			KeyboardUntil: mb.kbUntil, Connected: mb.connected, LastInput: mb.lastInput})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Seat < out[b].Seat })
	return out
}

// SetLevel changes a participant's level. Raising to editor needs a verified
// participant (ErrNeedsVerify: compare the check numbers again); the
// keyboard level goes through GrantKeyboard (ErrUseGrant). Lowering takes
// effect here at once, then the server is told.
func (h *Host) SetLevel(seat uint8, level wire.Level) error {
	h.mu.Lock()
	err := h.setLevelLocked(seat, level)
	h.mu.Unlock()
	h.flushManual()
	return err
}

func (h *Host) setLevelLocked(seat uint8, level wire.Level) error {
	if h.over || h.killed.Load() {
		return ErrEnded
	}
	if level == wire.LevelKeyboard {
		return ErrUseGrant
	}
	if level > wire.LevelKeyboard {
		return ErrLevel
	}
	mb := h.members[seat]
	if mb == nil {
		return ErrNotMember
	}
	h.lastOwner = h.now()
	if level > mb.level && level >= wire.LevelEditor && !mb.verified {
		return ErrNeedsVerify
	}
	if level == mb.level {
		return nil
	}
	h.changeLevel(mb, level, "owner")
	h.rekeyIfDue()
	return nil
}

// changeLevel sets mb's level (not keyboard) and tells everyone.
func (h *Host) changeLevel(mb *member, level wire.Level, by string) {
	from := mb.level
	mb.level = level
	if level != wire.LevelKeyboard {
		mb.kbUntil = time.Time{}
	}
	if from == wire.LevelKeyboard && level != wire.LevelKeyboard {
		h.rekeyDue = true // keyboard taken back (spec B9)
	}
	h.record(audit.EvLevel, int(mb.seat), mb.user, map[string]any{"from": from.String(), "to": level.String(), "by": by})
	if level == wire.LevelNone {
		// Out of the people list: its waiting proposals and open documents go.
		h.dropProposals(mb.seat)
		if h.docs != nil {
			h.closeSeatDocs(mb.seat)
		}
	}
	h.sendLevels()
	h.membersChanged()
}

func validKeyboard(d time.Duration) bool { return slices.Contains(KeyboardDurations, d) }

// GrantKeyboard gives a verified participant the keyboard level for d (one
// of KeyboardDurations).
func (h *Host) GrantKeyboard(seat uint8, d time.Duration) error {
	h.mu.Lock()
	err := h.grantKeyboardLocked(seat, d, false)
	h.mu.Unlock()
	h.flushManual()
	return err
}

// ExtendKeyboard moves a running keyboard grant's end to now+d, before it
// expires.
func (h *Host) ExtendKeyboard(seat uint8, d time.Duration) error {
	h.mu.Lock()
	err := h.grantKeyboardLocked(seat, d, true)
	h.mu.Unlock()
	h.flushManual()
	return err
}

func (h *Host) grantKeyboardLocked(seat uint8, d time.Duration, extend bool) error {
	if h.over || h.killed.Load() {
		return ErrEnded
	}
	if !validKeyboard(d) {
		return ErrBadDuration
	}
	mb := h.members[seat]
	if mb == nil {
		return ErrNotMember
	}
	if !mb.verified {
		return ErrNeedsVerify
	}
	now := h.now()
	h.lastOwner = now
	if extend && (mb.level != wire.LevelKeyboard || !now.Before(mb.kbUntil)) {
		return ErrNotKeyboard
	}
	until := now.Add(d)
	if until.After(now.Add(MaxKeyboard)) {
		return ErrBadDuration
	}
	from := mb.level
	mb.level, mb.kbUntil = wire.LevelKeyboard, until
	h.record(audit.EvKeyboardGrant, int(seat), mb.user, map[string]any{
		"from": from.String(), "until": until.UTC().Format(time.RFC3339), "minutes": int(d / time.Minute), "extend": extend,
	})
	h.sendLevels()
	h.membersChanged()
	return nil
}

// OwnerActivity tells the engine the owner pressed a key (any key in
// masil). After OwnerIdlePause without it, keyboard input pauses; the next
// activity resumes it.
func (h *Host) OwnerActivity(now time.Time) {
	h.mu.Lock()
	if now.After(h.lastOwner) {
		h.lastOwner = now
	}
	if h.paused {
		h.paused = false
		h.emit(EventKeyboardResumed{})
	}
	h.mu.Unlock()
}

// demote drops a keyboard holder to editor.
func (h *Host) demote(mb *member, reason string) {
	if mb.level != wire.LevelKeyboard {
		return
	}
	mb.level, mb.kbUntil = wire.LevelEditor, time.Time{}
	if reason != DemoteRekey {
		h.rekeyDue = true // keyboard taken back (spec B9)
	}
	h.record(audit.EvKeyboardExpire, int(mb.seat), mb.user, map[string]any{"reason": reason})
	h.emit(EventKeyboardEnded{Seat: mb.seat, Reason: reason})
	h.sendLevels()
	h.membersChanged()
}

// memberRestarted: a member started a new key exchange (its masil
// restarted) — keyboard drops to editor.
func (h *Host) memberRestarted(mb *member) { h.demote(mb, DemoteRestart) }

// keyboardUntilPtr is mb's keyboard end for the wire (nil when none).
func (h *Host) keyboardUntilPtr(mb *member) *time.Time {
	if mb.level != wire.LevelKeyboard || mb.kbUntil.IsZero() {
		return nil
	}
	t := mb.kbUntil.UTC()
	return &t
}

func (h *Host) anyKeyboard() bool {
	for _, mb := range h.members {
		if mb.level == wire.LevelKeyboard {
			return true
		}
	}
	return false
}

// tickLadder runs keyboard expiry, the owner-idle pause and the gone-peer
// demotion.
func (h *Host) tickLadder(now time.Time) {
	if !h.paused && h.anyKeyboard() && now.Sub(h.lastOwner) >= OwnerIdlePause {
		h.paused = true
		h.record(audit.EvKeyboardPause, 0, "", nil)
		h.emit(EventKeyboardPaused{})
	}
	online := h.curConn() != nil // while the owner is away, peer presence is unknown
	for _, s := range h.seatsLocked() {
		mb := h.members[s]
		if mb.level != wire.LevelKeyboard {
			continue
		}
		switch {
		case !now.Before(mb.kbUntil):
			h.demote(mb, DemoteExpired)
		case online && !mb.connected && !mb.goneAt.IsZero() && now.Sub(mb.goneAt) > PeerGoneDemote:
			h.demote(mb, DemoteGone)
		}
	}
}

func (h *Host) seatsLocked() []uint8 {
	out := make([]uint8, 0, len(h.members))
	for s := range h.members {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// membersChanged broadcasts the roster (the people list, with keyboard
// end times).
func (h *Host) membersChanged() {
	r := wire.Roster{Members: []wire.RosterEntry{}}
	for _, s := range h.seatsLocked() {
		mb := h.members[s]
		r.Members = append(r.Members, wire.RosterEntry{Seat: s, UserID: mb.user, Level: mb.level.String(),
			Verified: mb.verified, KeyboardUntil: h.keyboardUntilPtr(mb), Connected: mb.connected})
	}
	_, _ = h.broadcast(wire.KindRoster, 0, mustJSON(r))
}

// onSelfLevel: a participant lowered its own level ("" = left).
func (h *Host) onSelfLevel(seat uint8, name string) {
	mb := h.members[seat]
	lvl, ok := wire.ParseLevel(name)
	if mb == nil || !ok || lvl >= mb.level {
		return
	}
	h.changeLevel(mb, lvl, "self")
	if lvl == wire.LevelNone {
		h.emit(EventPeerLeft{Seat: seat, UserID: mb.user})
	}
}

// ---- guest side ----

// Level is my current level.
func (g *Guest) Level() wire.Level {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.level
}

// LowerSelf lowers my own level (LevelNone leaves the people list).
func (g *Guest) LowerSelf(level wire.Level) error {
	g.mu.Lock()
	err := g.lowerSelfLocked(level)
	g.mu.Unlock()
	g.flushManual()
	return err
}

func (g *Guest) lowerSelfLocked(level wire.Level) error {
	if g.over {
		return ErrEnded
	}
	if g.phase != phaseGranted || level >= g.level {
		return ErrLevel
	}
	if err := g.sendOwner(wire.KindSelfLevel, mustJSON(wire.SelfLevel{Level: level.String()})); err != nil {
		return err
	}
	g.level = level
	if level != wire.LevelKeyboard {
		g.kbUntil = nil
	}
	return nil
}

// onRoster takes the owner's people list; my own entry is my level.
func (g *Guest) onRoster(r wire.Roster) {
	out := make([]Member, 0, len(r.Members))
	mine := false
	for _, e := range r.Members {
		lvl, ok := wire.ParseLevel(e.Level)
		if !ok {
			continue
		}
		m := Member{Seat: e.Seat, UserID: e.UserID, Level: lvl, Verified: e.Verified, Connected: e.Connected}
		if e.KeyboardUntil != nil {
			m.KeyboardUntil = *e.KeyboardUntil
		}
		if e.Seat == g.seat {
			mine = true
			g.level, g.verified, g.kbUntil = lvl, e.Verified, e.KeyboardUntil
		}
		out = append(out, m)
	}
	if !mine {
		g.level, g.kbUntil = wire.LevelNone, nil
	}
	g.emit(EventRoster{Members: out})
}
