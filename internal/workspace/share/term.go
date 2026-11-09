package share

import (
	"encoding/base64"
	"errors"
	"slices"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/wire"
	"github.com/patrache/masil/internal/workspace/wscrypto"
)

// Terminal is a shared terminal on the owner's PC. The UI adapts a P0
// termhost pane to it (Tap = AttachTap, Inject = Pane.Inject("guest:<seat>"),
// Size, SetFixedSize, BracketedPaste, Title).
type Terminal interface {
	// Tap calls fn with every output byte chunk until stop is called. fn
	// must not block.
	Tap(fn func([]byte)) (stop func())
	// Snapshot renders the current screen as bytes that redraw it (a
	// keyframe).
	Snapshot() []byte
	// Inject writes bytes to the program as if typed.
	Inject(p []byte) error
	Size() (cols, rows int)
	// SetFixedSize pins the size (0, 0 = follow the window).
	SetFixedSize(cols, rows int)
	// BracketedPaste reports whether the program turned paste mode on.
	BracketedPaste() bool
	Title() string
}

var (
	ErrNoTerminal = errors.New("share: no such shared terminal")
	ErrBadSize    = errors.New("share: terminal size not offered")
)

// Terminal relay timing and limits (spec B8.2, workspace.md 9).
const (
	// TermBatch / TermBatchDelay: output goes out when this much is waiting
	// or this long after its first byte.
	TermBatch      = 32 << 10
	TermBatchDelay = 50 * time.Millisecond
	// KeyframeEvery is the periodic keyframe interval.
	KeyframeEvery = 10 * time.Second
	// OwnerOutputPerSecond is the owner's own output cap (the hub drops
	// above it); past it the waiting output is dropped and the next tick
	// sends a keyframe instead.
	OwnerOutputPerSecond = 2 << 20
	// tapBufferMax bounds output held between ticks.
	tapBufferMax = 4 << 20
)

// FixedSizes are the pinned terminal sizes offered (JB20); 0x0 follows the
// owner's window.
var FixedSizes = [][2]int{{0, 0}, {120, 40}, {100, 30}, {80, 24}}

// sharedTerm is one terminal the host shares. buf/firstAt/overflow are
// written by the terminal's output callback and guarded by Host.tapMu; the
// rest belongs to Host.mu.
type sharedTerm struct {
	id   uint8
	t    Terminal
	stop func()

	buf      []byte
	firstAt  time.Time
	overflow bool

	needKey   bool
	lastKey   time.Time
	attention bool
	status    wire.TermStatus
	sentOnce  bool
}

// ShareTerminal shares t under id: a keyframe goes out on the next tick,
// then batched output.
func (h *Host) ShareTerminal(id uint8, t Terminal) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.over || h.killed.Load() {
		return ErrEnded
	}
	if h.terms == nil {
		h.terms = map[uint8]*sharedTerm{}
	}
	if old := h.terms[id]; old != nil && old.stop != nil {
		old.stop()
	}
	st := &sharedTerm{id: id, t: t, needKey: true}
	h.terms[id] = st
	st.stop = t.Tap(func(b []byte) { h.tapped(st, b) })
	h.sendStatus(st, true)
	return nil
}

// UnshareTerminal stops sharing terminal id.
func (h *Host) UnshareTerminal(id uint8) {
	h.mu.Lock()
	st := h.terms[id]
	delete(h.terms, id)
	h.mu.Unlock()
	if st != nil && st.stop != nil {
		st.stop()
	}
}

// tapped is the terminal's output callback: it only appends under tapMu.
func (h *Host) tapped(st *sharedTerm, b []byte) {
	if len(b) == 0 || h.killed.Load() {
		return
	}
	now := time.Unix(0, h.tapNow.Load())
	if !h.manual {
		now = h.clock.Now()
	}
	h.tapMu.Lock()
	defer h.tapMu.Unlock()
	if st.overflow {
		return
	}
	if len(st.buf)+len(b) > tapBufferMax {
		st.buf, st.overflow = nil, true
		return
	}
	if len(st.buf) == 0 {
		st.firstAt = now
	}
	st.buf = append(st.buf, b...)
}

// SetTermSize pins terminal id to one of FixedSizes (0, 0 = follow the
// window) and tells the participants.
func (h *Host) SetTermSize(id uint8, cols, rows int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.terms[id]
	if st == nil {
		return ErrNoTerminal
	}
	ok := false
	for _, s := range FixedSizes {
		ok = ok || s == [2]int{cols, rows}
	}
	if !ok {
		return ErrBadSize
	}
	st.t.SetFixedSize(cols, rows)
	st.needKey = true
	h.sendStatus(st, true)
	return nil
}

// SetAttention marks terminal id as waiting for the owner (a hook or a
// bell): the participants see it and the owner's other devices get the
// workspace_attention alert.
func (h *Host) SetAttention(id uint8, on bool) {
	h.mu.Lock()
	st := h.terms[id]
	if st != nil && st.attention != on {
		st.attention = on
		h.sendStatus(st, true)
		if on {
			_ = h.sendPlain(wire.KindCtlAttention, wire.SeatOwner, wire.SeatServer, struct{}{})
		}
	}
	h.mu.Unlock()
	h.flushManual()
}

// SetUnsaved is the number of unsaved shared documents, shown on every
// terminal title (programs read only the disk).
func (h *Host) SetUnsaved(n int) {
	h.mu.Lock()
	h.unsaved = n
	for _, id := range h.termIDs() {
		h.sendStatus(h.terms[id], false)
	}
	h.mu.Unlock()
	h.flushManual()
}

func (h *Host) termIDs() []uint8 {
	ids := make([]uint8, 0, len(h.terms))
	for id := range h.terms {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// sendStatus broadcasts term_status when it changed (or always with force).
func (h *Host) sendStatus(st *sharedTerm, force bool) {
	cols, rows := st.t.Size()
	s := wire.TermStatus{Term: st.id, Title: st.t.Title(), Cols: cols, Rows: rows, Attention: st.attention, Unsaved: h.unsaved}
	if !force && st.sentOnce && s == st.status {
		return
	}
	st.status, st.sentOnce = s, true
	_, _ = h.broadcast(wire.KindTermStatus, 0, mustJSON(s))
}

// keyframeAll asks every terminal for a keyframe on the next tick (a new
// participant, a resume).
func (h *Host) keyframeAll() {
	for _, st := range h.terms {
		st.needKey = true
	}
}

// spend reserves n bytes of this second's own-output budget. Once a second
// runs over, nothing more goes out until the next one (where the keyframe
// that replaces the dropped output goes first).
func (h *Host) spend(now time.Time, n int) bool {
	if h.outStart.IsZero() || now.Sub(h.outStart) >= time.Second {
		h.outStart, h.outBytes, h.outFull = now, 0, false
	}
	if h.outFull {
		return false
	}
	if h.outBytes+n > OwnerOutputPerSecond {
		h.outFull = true
		return false
	}
	h.outBytes += n
	return true
}

// termFrameSize is the wire size of a broadcast carrying n plaintext bytes.
func termFrameSize(n int) int {
	per := wscrypto.MaxPlaintext(wscrypto.SignatureLen)
	frames := max(1, (n+per-1)/per)
	return n + frames*(wire.HeaderLen+wscrypto.TagLen+wscrypto.SignatureLen)
}

// tickTerms sends batched output and keyframes.
func (h *Host) tickTerms(now time.Time) {
	h.tapNow.Store(now.UnixNano())
	for _, id := range h.termIDs() {
		st := h.terms[id]
		h.sendStatus(st, false)
		h.tapMu.Lock()
		buf, first, over := st.buf, st.firstAt, st.overflow
		h.tapMu.Unlock()
		if over {
			st.needKey = true
		}
		if !st.needKey && now.Sub(st.lastKey) >= KeyframeEvery {
			st.needKey = true
		}
		if st.needKey {
			h.takeBuf(st, len(buf))
			snap := wire.TermPayload(id, st.t.Snapshot())
			if !h.spend(now, termFrameSize(len(snap))) {
				continue // next second
			}
			// The status goes again with every keyframe: a participant who
			// joined after the last change has never seen it.
			h.sendStatus(st, true)
			_, _ = h.broadcast(wire.KindTermKey, wire.FlagGroupStart, snap)
			st.needKey, st.lastKey = false, now
			continue
		}
		sent := 0
		for len(buf)-sent >= TermBatch || len(buf) > sent && now.Sub(first) >= TermBatchDelay {
			n := min(TermBatch, len(buf)-sent)
			p := wire.TermPayload(id, buf[sent:sent+n])
			if !h.spend(now, termFrameSize(len(p))) {
				sent = len(buf) // drop what waits; a keyframe follows
				st.needKey = true
				break
			}
			_, _ = h.broadcast(wire.KindTermOut, 0, p)
			sent += n
		}
		h.takeBuf(st, sent)
	}
}

// takeBuf removes the first n waiting bytes (all of them clears overflow).
func (h *Host) takeBuf(st *sharedTerm, n int) {
	h.tapMu.Lock()
	defer h.tapMu.Unlock()
	if n >= len(st.buf) {
		st.buf, st.overflow = nil, false
		return
	}
	st.buf = append([]byte(nil), st.buf[n:]...)
	// The rest keeps its first-byte time: it has already waited.
}

// onTermInput is a keyboard-level participant's input (h.mu held). Every
// condition is checked right before the terminal write.
func (h *Host) onTermInput(seat uint8, p []byte) {
	term, b, err := wire.SplitTerm(p)
	if err != nil || len(b) == 0 {
		return
	}
	h.deliverInput(seat, term, b)
}

// deliverInput is the only path from a participant to a terminal.
func (h *Host) deliverInput(seat, term uint8, b []byte) {
	if h.killed.Load() {
		return
	}
	mb := h.members[seat]
	now := h.now()
	switch {
	case mb == nil || mb.level != wire.LevelKeyboard:
		h.rejectFrame(seat, wire.KindTermInput, "level", false)
		return
	case !now.Before(mb.kbUntil):
		h.rejectFrame(seat, wire.KindTermInput, DemoteExpired, false)
		return
	case h.paused:
		h.rejectFrame(seat, wire.KindTermInput, "paused", false)
		return
	}
	st := h.terms[term]
	if st == nil {
		return
	}
	h.record(audit.EvKeyboardInput, int(seat), mb.user, map[string]any{"term": int(term), "b64": base64.StdEncoding.EncodeToString(b)})
	mb.lastInput = now
	if h.killed.Load() {
		return
	}
	_ = st.t.Inject(b)
	h.emit(EventInput{Seat: seat, Term: term, Data: append([]byte(nil), b...)})
}

// Input sends my keystrokes to the owner's terminal term (keyboard level
// only).
func (g *Guest) Input(term uint8, p []byte) error {
	g.mu.Lock()
	err := g.inputLocked(term, p)
	g.mu.Unlock()
	g.flushManual()
	return err
}

func (g *Guest) inputLocked(term uint8, p []byte) error {
	if g.over {
		return ErrEnded
	}
	if g.phase != phaseGranted || g.level < wire.LevelKeyboard {
		return ErrLevel
	}
	return g.sendOwner(wire.KindTermInput, wire.TermPayload(term, p))
}

// ---- guest side ----

func (g *Guest) onTerm(kind wire.Kind, p []byte) {
	term, b, err := wire.SplitTerm(p)
	if err != nil {
		return
	}
	b = append([]byte(nil), b...)
	if kind == wire.KindTermKey {
		g.emit(EventTermKeyframe{Term: term, Bytes: b})
		return
	}
	g.emit(EventTermOut{Term: term, Bytes: b})
}
