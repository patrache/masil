package share

import (
	"encoding/base64"
	"errors"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/wire"
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

var ErrNoTerminal = errors.New("share: no such shared terminal")

// sharedTerm is one terminal the host shares.
type sharedTerm struct {
	id uint8
	t  Terminal
}

// ShareTerminal shares t under id.
func (h *Host) ShareTerminal(id uint8, t Terminal) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.over || h.killed.Load() {
		return ErrEnded
	}
	if h.terms == nil {
		h.terms = map[uint8]*sharedTerm{}
	}
	h.terms[id] = &sharedTerm{id: id, t: t}
	return nil
}

// UnshareTerminal stops sharing terminal id.
func (h *Host) UnshareTerminal(id uint8) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.terms, id)
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
