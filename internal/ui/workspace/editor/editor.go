// Package editor wraps goeditor for the workspace code cell (v0.1.3 B6.6,
// B7.1, P2 Task 19): vim keys from goeditor's core, edits turned into rune OT
// operations, other people's edits and cursors laid over it, and undo, save
// and close taken out so the caller runs them through OT.
//
// What goeditor v0.19.0 offers (checked against its source, B7.1):
//
//  1. Content and cursor from outside: yes — core.Editor.SetContent and
//     Buffer.SetCursor. SetContent resets the view to the top, so this
//     wrapper puts State.TopLine back afterwards. Its buffer drops one
//     trailing "\n" ("a\n" and "a" read back the same), so the wrapper keeps
//     that newline itself (like Vim's 'fixeol').
//  2. Scroll position: State.TopLine (first line); there is no horizontal
//     scroll (its Model wraps lines). This wrapper draws itself, so it keeps
//     its own first column.
//  3. Turning its undo off: there is no switch; SetMaxHistory(0) drops every
//     step and the wrapper intercepts every undo/redo key and ex command
//     (u, U, ctrl+r, :u, :undo, :redo) before goeditor sees them.
//
// Not used from goeditor: its bubbletea Model (its key bridge takes only the
// first byte of a key's text, so Hangul cannot be typed, and its View cannot
// be painted over), the clipboard (an in-memory one stands in), its file
// commands (:w, :q, :rename, :delete are intercepted). goeditor treats a
// character as a rune plus its combining marks (not a grapheme cluster), so
// the wrapper moves the cursor off the inside of ZWJ emoji sequences and
// other clusters after every key. No fork (decision 6) was needed.
package editor

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/ionut-t/goeditor/core"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/workspace/ot"
)

// UndoMsg asks the caller to undo my latest edit through OT (u, U, :u).
type UndoMsg struct{}

// RedoMsg asks the caller to redo (ctrl+r, :redo).
type RedoMsg struct{}

// SaveMsg asks the caller to save the document (:w); Close is set for :wq
// and :x (save, then close).
type SaveMsg struct{ Close bool }

// CloseMsg asks the caller to close the document (:q, :q!).
type CloseMsg struct{}

// Peer is someone else's cursor and selection, as rune indexes in the text.
type Peer struct {
	Name                   string
	Color                  color.Color
	Cursor, SelFrom, SelTo int
}

// Editor is one open document's editor.
type Editor struct {
	th        *theme.Theme
	core      core.Editor
	text      []rune // the document as of the last key / remote edit
	trailNL   bool   // text ends in "\n" (goeditor's buffer drops it)
	readOnly  bool
	ops       []ot.Op
	peers     []Peer
	w, h      int
	left      int // first shown cell column of the text area
	cmdResult string
	as        assist // syntax colors and completion (assist.go)
}

// memClip is the editor's own clipboard (yank/put stay inside masil).
type memClip struct{ s string }

func (c *memClip) Write(s string) error  { c.s = s; return nil }
func (c *memClip) Read() (string, error) { return c.s, nil }

// New makes an editor over text.
func New(th *theme.Theme, text string, readOnly bool) *Editor {
	e := &Editor{th: th, core: core.New(&memClip{}), readOnly: readOnly, as: assist{highlight: true, complete: true}}
	e.core.SetMaxHistory(0)
	e.core.DisableInsertMode(readOnly)
	e.load([]rune(text), core.Position{})
	return e
}

// load puts text into goeditor with the cursor at pos, keeping the scroll.
func (e *Editor) load(text []rune, pos core.Position) {
	top := e.core.GetState().TopLine
	visual := e.core.GetState().VisualStart
	e.text = text
	e.as.ver++
	body := text
	e.trailNL = len(text) > 0 && text[len(text)-1] == '\n'
	if e.trailNL {
		body = text[:len(text)-1]
	}
	e.core.SetContent([]byte(string(body)))
	st := e.core.GetState()
	st.TopLine = top
	st.VisualStart = visual
	e.core.SetState(st)
	e.setCursor(pos)
}

func (e *Editor) setCursor(pos core.Position) {
	b := e.core.GetBuffer()
	n := b.LineCount()
	pos.Row = max(0, min(pos.Row, n-1))
	pos.Col = max(0, min(pos.Col, b.LineRuneCount(pos.Row)))
	b.SetCursor(core.Cursor{Position: pos, Preferred: pos.Col})
	e.core.ScrollViewport()
}

// bufferText reads goeditor's buffer back as the document.
func (e *Editor) bufferText() []rune {
	s := e.core.GetBuffer().GetCurrentContent()
	if e.trailNL {
		s += "\n"
	}
	return []rune(s)
}

// Resize sets the drawn size.
func (e *Editor) Resize(w, h int) {
	e.w, e.h = max(w, 0), max(h, 0)
	st := e.core.GetState()
	st.ViewportHeight = max(e.h, 1)
	st.ViewportWidth = max(e.textWidth(), 1)
	st.AvailableWidth = st.ViewportWidth
	e.core.SetState(st)
	e.core.ScrollViewport()
	e.scrollColumn()
}

// Update takes non-key messages (none are needed today; goeditor's signals
// are drained so its channel never fills).
func (e *Editor) Update(tea.Msg) tea.Cmd {
	e.drainSignals()
	return nil
}

func (e *Editor) drainSignals() {
	ch := e.core.GetUpdateSignalChan()
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// CapturesInput is true while typed text goes into the document or the
// command/search line (Insert, Command and Search modes).
func (e *Editor) CapturesInput() bool {
	return e.core.IsInsertMode() || e.core.IsCommandMode() || e.core.IsSearchMode()
}

// Mode is goeditor's mode name ("normal", "insert", …).
func (e *Editor) Mode() string { return string(e.core.GetState().Mode) }

// Text is the document.
func (e *Editor) Text() string { return string(e.text) }

// SetReadOnly turns editing off or on.
func (e *Editor) SetReadOnly(ro bool) {
	e.readOnly = ro
	e.core.DisableInsertMode(ro)
	if ro && e.core.IsInsertMode() {
		e.core.SetNormalMode()
	}
	if ro {
		e.closeMenu()
	}
}

// ReadOnly reports whether editing is off.
func (e *Editor) ReadOnly() bool { return e.readOnly }

// SetPeers replaces the other people's cursors.
func (e *Editor) SetPeers(p []Peer) { e.peers = append([]Peer(nil), p...) }

// TakeOps returns my edits since the last call (each against the text the
// previous one produced).
func (e *Editor) TakeOps() []ot.Op {
	ops := e.ops
	e.ops = nil
	return ops
}

// CursorIndex is my cursor as a rune index into Text.
func (e *Editor) CursorIndex() int {
	return indexOf(e.text, e.core.GetBuffer().GetCursor().Position)
}

// SetCursorIndex moves my cursor to rune index i.
func (e *Editor) SetCursorIndex(i int) {
	e.setCursor(positionOf(e.text, i))
	e.scrollColumn()
}

// Selection is my visual selection as rune indexes [from, to) (ok=false
// without one).
func (e *Editor) Selection() (from, to int, ok bool) {
	st := e.core.GetState()
	if st.VisualStart.Row < 0 || !(e.core.IsVisualMode() || e.core.IsVisualLineMode()) {
		return 0, 0, false
	}
	a := indexOf(e.text, st.VisualStart)
	b := e.CursorIndex()
	if a > b {
		a, b = b, a
	}
	return a, min(b+1, len(e.text)), true
}

// ApplyRemote lays someone else's edit over the document: the text changes,
// my cursor and selection move with it, and nothing is reported by TakeOps.
func (e *Editor) ApplyRemote(op ot.Op) {
	next, err := op.Apply(e.text)
	if err != nil {
		return
	}
	cur := ot.TransformIndex(op, e.CursorIndex())
	st := e.core.GetState()
	if st.VisualStart.Row >= 0 {
		vi := ot.TransformIndex(op, indexOf(e.text, st.VisualStart))
		st.VisualStart = positionOf(next, vi)
		e.core.SetState(st)
	}
	e.load(next, positionOf(next, cur))
	e.scrollColumn()
	if e.as.open {
		e.fillMenu(true)
	}
}

// Reset replaces the whole document (a reload), keeping the cursor index
// where it can.
func (e *Editor) Reset(text string) {
	cur := e.CursorIndex()
	r := []rune(text)
	e.load(r, positionOf(r, min(cur, len(r))))
	e.ops = nil
	e.closeMenu()
	e.scrollColumn()
}

// HandleKey runs one key. handled=false only for keys the editor leaves to
// the caller (none of the vim keys; esc in Normal mode).
func (e *Editor) HandleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	defer e.drainSignals()
	ks := msg.String()
	if e.as.open && !e.menuLive() {
		e.closeMenu() // stale (the mode or the level changed under it)
	}
	if e.as.open && e.menuKey(ks) {
		return true, nil
	}
	if (ks == "ctrl+n" || ks == "ctrl+space" || ks == "ctrl+@") && e.core.IsInsertMode() {
		if e.as.complete && !e.readOnly {
			e.as.forced = true
			e.refreshMenu()
			e.ask()
		}
		return true, nil
	}
	normal := e.core.IsNormalMode()
	switch {
	case normal && (ks == "u" || ks == "U"):
		return true, emit(UndoMsg{})
	case normal && ks == "ctrl+r":
		return true, emit(RedoMsg{})
	case normal && ks == "esc" && e.core.GetState().PendingCount == nil:
		return false, nil
	case e.core.IsCommandMode() && msg.Code == tea.KeyEnter:
		if cmd, ok := e.exCommand(); ok {
			return true, cmd
		}
	}
	before := e.CursorIndex()
	beforeRow := e.core.GetBuffer().GetCursor().Position.Row
	for _, ev := range convertKey(msg) {
		if err := e.core.HandleKey(ev); err != nil {
			_ = err // goeditor reports e.g. "no more lines"; nothing to show
		}
	}
	e.snapCursor(before, beforeRow)
	e.afterKey()
	e.afterAssistKey(msg.Code, msg.Text)
	return true, nil
}

// exCommand intercepts the ex commands masil runs itself.
func (e *Editor) exCommand() (tea.Cmd, bool) {
	line := strings.TrimSpace(strings.TrimPrefix(e.core.GetState().CommandLine, ":"))
	name := line
	if i := strings.IndexByte(line, ' '); i >= 0 {
		name = line[:i]
	}
	var cmd tea.Cmd
	switch name {
	case "w", "write":
		cmd = emit(SaveMsg{})
	case "wq", "wq!", "x", "xit":
		cmd = emit(SaveMsg{Close: true})
	case "q", "quit", "q!", "quit!":
		cmd = emit(CloseMsg{})
	case "u", "undo":
		cmd = emit(UndoMsg{})
	case "red", "redo":
		cmd = emit(RedoMsg{})
	case "rename", "delete", "del", "e", "edit":
		// File commands go through the workspace (proposals, the tree).
	default:
		return nil, false
	}
	e.core.SetNormalMode()
	return cmd, true
}

// afterKey turns a text change into an op (or undoes it when read-only).
func (e *Editor) afterKey() {
	now := e.bufferText()
	if !equalRunes(now, e.text) {
		if e.readOnly {
			pos := e.core.GetBuffer().GetCursor().Position
			e.load(e.text, pos)
		} else {
			e.ops = append(e.ops, ot.Diff(e.text, now))
			e.text = now
			e.as.ver++
		}
	}
	e.core.ScrollViewport()
	e.scrollColumn()
}

// snapCursor keeps the cursor on a grapheme boundary: a move right that
// landed inside a cluster goes on to the next one, anything else goes back
// to the cluster's start.
func (e *Editor) snapCursor(before, beforeRow int) {
	b := e.core.GetBuffer()
	c := b.GetCursor()
	line := b.GetLineRunes(c.Position.Row)
	starts := clusterStarts(line)
	col := c.Position.Col
	if col >= len(line) || isStart(starts, col) {
		return
	}
	forward := c.Position.Row == beforeRow && indexOf(e.bufferText(), c.Position) > before
	prev, next := around(starts, col)
	target := prev
	if forward && next < len(line) {
		target = next
	} else if forward && e.core.IsInsertMode() {
		target = next
	}
	c.Position.Col = target
	c.Preferred = target
	b.SetCursor(c)
}

func emit(m tea.Msg) tea.Cmd { return func() tea.Msg { return m } }

func equalRunes(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// convertKey turns a bubbletea key into goeditor key events: each rune of the
// key's text is its own event (goeditor's bridge keeps only the first byte).
func convertKey(msg tea.KeyPressMsg) []core.KeyEvent {
	var mods core.KeyModifiers
	if msg.Mod&tea.ModAlt != 0 {
		mods |= core.ModAlt
	}
	if msg.Mod&tea.ModCtrl != 0 {
		mods |= core.ModCtrl
	}
	special := map[rune]core.KeyCode{
		tea.KeyEnter: core.KeyEnter, tea.KeyEscape: core.KeyEscape, tea.KeyBackspace: core.KeyBackspace,
		tea.KeyUp: core.KeyUp, tea.KeyDown: core.KeyDown, tea.KeyLeft: core.KeyLeft, tea.KeyRight: core.KeyRight,
		tea.KeyHome: core.KeyHome, tea.KeyEnd: core.KeyEnd, tea.KeyDelete: core.KeyDelete,
		tea.KeyPgUp: core.KeyPageUp, tea.KeyPgDown: core.KeyPageDown,
	}
	switch msg.Code {
	case tea.KeySpace:
		return []core.KeyEvent{{Key: core.KeySpace, Rune: ' ', Modifiers: mods}}
	case tea.KeyTab:
		return []core.KeyEvent{{Key: core.KeyTab, Rune: '\t', Modifiers: mods}}
	}
	if k, ok := special[msg.Code]; ok {
		return []core.KeyEvent{{Key: k, Modifiers: mods}}
	}
	if mods&core.ModCtrl != 0 {
		if msg.Code > 0 && msg.Code < 0x80 {
			return []core.KeyEvent{core.Ctrl(msg.Code)}
		}
		return nil
	}
	var out []core.KeyEvent
	for _, r := range msg.Text {
		out = append(out, core.KeyEvent{Rune: r, Modifiers: mods})
	}
	return out
}

// indexOf is the rune index of pos in text (rows split at "\n").
func indexOf(text []rune, pos core.Position) int {
	row, i := 0, 0
	for i < len(text) && row < pos.Row {
		if text[i] == '\n' {
			row++
		}
		i++
	}
	for c := 0; c < pos.Col && i < len(text) && text[i] != '\n'; c++ {
		i++
	}
	return i
}

// positionOf is the row and column of rune index i in text.
func positionOf(text []rune, i int) core.Position {
	i = max(0, min(i, len(text)))
	var p core.Position
	for _, r := range text[:i] {
		if r == '\n' {
			p.Row++
			p.Col = 0
		} else {
			p.Col++
		}
	}
	return p
}
