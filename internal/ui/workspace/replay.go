package workspace

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/workspace/wire"
)

// The participant's terminal cell (spec B6.7, B8.7, JX6): the owner's
// terminal output is replayed into a termhost.Replay — a screen grid only:
// it never answers queries, ignores OSC 52, titles and hyperlinks — and that
// grid is drawn. No byte from the owner reaches the participant's terminal.
// A cell smaller than the owner's terminal shows a window of it that follows
// the cursor; after ctrl+\ [ the arrows move the window (following stops)
// until f.

// replayScrollback is the participant's kept lines (B8.2).
const replayScrollback = 5000

// replayTerm is one shared terminal as the participant sees it.
type replayTerm struct {
	id     uint8
	scr    *termhost.Replay
	status wire.TermStatus
	// the window: its top-left cell, its size as last drawn
	x0, y0       int
	viewW, viewH int
	follow       bool // move the window to keep the cursor in it
	pan          bool // ctrl+\ [: the arrows move the window
}

func newReplayTerm(id uint8) *replayTerm {
	return &replayTerm{id: id, follow: true}
}

// size is the owner's terminal size (80x24 until the status says).
func (rt *replayTerm) size() termhost.Size {
	if rt.status.Cols > 0 && rt.status.Rows > 0 {
		return termhost.Size{W: rt.status.Cols, H: rt.status.Rows}
	}
	return termhost.Size{W: 80, H: 24}
}

// keyframe starts the screen over from a full redraw.
func (rt *replayTerm) keyframe(b []byte) {
	rt.scr = termhost.NewReplay(rt.size(), replayScrollback)
	rt.scr.Write(b)
}

// write feeds output (dropped until the first keyframe: without it the
// screen would be half drawn).
func (rt *replayTerm) write(b []byte) {
	if rt.scr != nil {
		rt.scr.Write(b)
	}
}

// setStatus takes the title, size and attention of the owner's terminal.
func (rt *replayTerm) setStatus(st wire.TermStatus) {
	old := rt.size()
	rt.status = st
	if rt.scr != nil && rt.size() != old {
		rt.scr.Resize(rt.size())
	}
}

// followOn turns following back on (and ends panning).
func (rt *replayTerm) followOn() {
	rt.follow, rt.pan = true, false
	rt.place()
}

// panKey moves the window while panning (true when it was a pan key).
func (rt *replayTerm) panKey(k string) bool {
	switch k {
	case "left", "h":
		rt.x0--
	case "right", "l":
		rt.x0++
	case "up", "k":
		rt.y0--
	case "down", "j":
		rt.y0++
	case "home", "g":
		rt.x0, rt.y0 = 0, 0
	case "f":
		rt.followOn()
		return true
	case "esc", "q":
		rt.pan = false
		return true
	default:
		return false
	}
	rt.follow = false
	rt.clamp()
	return true
}

// clamp keeps the window on the screen.
func (rt *replayTerm) clamp() {
	sz := rt.size()
	rt.x0 = max(0, min(rt.x0, sz.W-rt.viewW))
	rt.y0 = max(0, min(rt.y0, sz.H-rt.viewH))
}

// place moves the window so the cursor is in it (when following).
func (rt *replayTerm) place() {
	if rt.follow && rt.scr != nil {
		x, y, _ := rt.scr.Cursor()
		if x < rt.x0 {
			rt.x0 = x
		}
		if x >= rt.x0+rt.viewW {
			rt.x0 = x - rt.viewW + 1
		}
		if y < rt.y0 {
			rt.y0 = y
		}
		if y >= rt.y0+rt.viewH {
			rt.y0 = y - rt.viewH + 1
		}
	}
	rt.clamp()
}

// hiddenRight is how many columns of the owner's terminal are right of the
// window.
func (rt *replayTerm) hiddenRight() int {
	return max(0, rt.size().W-rt.x0-rt.viewW)
}

// render draws the window w x h.
func (rt *replayTerm) render(w, h int) []string {
	rt.viewW, rt.viewH = w, h
	rt.place()
	if rt.scr == nil || w <= 0 || h <= 0 {
		return nil
	}
	return strings.Split(rt.scr.RenderRegion(rt.x0, rt.y0, w, h), "\n")
}

// guestTermView draws the terminal cell: a header (the terminals, the size,
// the pan mark, the unsaved count), the replay window and, for proposers,
// the proposal line.
func (s *Surface) guestTermView(ctx *ui.Context, w, h int, focused bool) []string {
	g := s.guest
	th := ctx.Theme
	t := ctx.T.Workspace
	dim := lipgloss.NewStyle().Foreground(th.P.Dim)
	line := lipgloss.NewStyle().Foreground(th.P.Line)
	cur := g.current()
	var tabs []string
	for _, id := range g.termIDs() {
		rt := g.terms[id]
		sz := rt.size()
		label := strconv.Itoa(int(id)) + " " + textwidth.Sanitize(rt.status.Title) + " · " + strconv.Itoa(sz.W) + "x" + strconv.Itoa(sz.H)
		if rt.status.Attention {
			label += " " + t.AttentionWaiting() + " " + t.OwnerOnlyAnswer()
		}
		switch {
		case rt == cur && focused:
			tabs = append(tabs, th.S.TabActive.Render(" "+label+" "))
		case rt == cur:
			tabs = append(tabs, th.S.Title.Render(" "+label+" "))
		default:
			tabs = append(tabs, dim.Render(" "+label+" "))
		}
	}
	body := h - 1
	composing := g.composing()
	if composing {
		body--
	}
	var shown []string
	if cur != nil {
		shown = cur.render(w, max(body, 0))
	}
	var tail []string
	if cur != nil {
		if n := cur.hiddenRight(); n > 0 {
			tail = append(tail, t.PanMore(n))
		}
		if !cur.follow {
			tail = append(tail, t.FollowOffHint(ctx.Keys.Binding(keymap.TermhostEscape).Help().Key))
		}
	}
	head := line.Render("─") + strings.Join(tabs, line.Render("─"))
	tailText := ""
	if len(tail) > 0 {
		tailText = " " + strings.Join(tail, " · ") + " "
	}
	head = textwidth.Truncate(head, max(w-textwidth.Width(tailText)-1, 1), "…")
	fill := max(w-textwidth.Width(head)-textwidth.Width(tailText), 0)
	head += line.Render(strings.Repeat("─", fill)) + th.S.Warning.Render(tailText)
	lines := []string{textwidth.Truncate(head, w, "")}
	switch {
	case cur == nil:
		lines = append(lines, "", " "+dim.Render(textwidth.Truncate(t.GuestTermEmpty(), max(w-2, 1), "…")))
	default:
		lines = append(lines, shown...)
	}
	if composing {
		lines = textwidth.Fit(lines, w, max(h-1, 0))
		lines = append(lines, g.compose.view(ctx, w, focused))
	}
	return lines
}

// guestTermCursor is the composer's cursor in the terminal cell.
func (s *Surface) guestTermCursor(w, h int) *tea.Cursor {
	g := s.guest
	if !g.composing() {
		return nil
	}
	x := min(g.compose.cursorX(w), w-1)
	return tea.NewCursor(x, h-1)
}

// keyBytes is what a key types into a terminal (the usual xterm bytes; the
// participant's keyboard level sends them to the owner's program).
func keyBytes(msg tea.KeyPressMsg) []byte {
	switch msg.String() {
	case "enter":
		return []byte{'\r'}
	case "tab":
		return []byte{'\t'}
	case "shift+tab":
		return []byte("\x1b[Z")
	case "backspace":
		return []byte{0x7f}
	case "esc":
		return []byte{0x1b}
	case "space":
		return []byte{' '}
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	case "home":
		return []byte("\x1b[H")
	case "end":
		return []byte("\x1b[F")
	case "pgup":
		return []byte("\x1b[5~")
	case "pgdown":
		return []byte("\x1b[6~")
	case "delete":
		return []byte("\x1b[3~")
	case "insert":
		return []byte("\x1b[2~")
	}
	if msg.Mod == tea.ModCtrl {
		switch c := msg.Code; {
		case c >= 'a' && c <= 'z':
			return []byte{byte(c - 'a' + 1)}
		case c == '@' || c == ' ':
			return []byte{0}
		case c == '[':
			return []byte{0x1b}
		case c == '\\':
			return []byte{0x1c}
		case c == ']':
			return []byte{0x1d}
		}
		return nil
	}
	if msg.Text == "" {
		return nil
	}
	if msg.Mod&tea.ModAlt != 0 {
		return append([]byte{0x1b}, msg.Text...)
	}
	return []byte(msg.Text)
}
