package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui/uitest"
)

// v0.1.3-1 (owner on Warp: "작업공간에서 커서가 안보였다"): the code cell's
// cursor is the terminal's real cursor — a block in normal mode, a bar in
// insert mode — and also its own cell drawn in reverse video (high contrast
// in every theme), only while the cell has the focus.

// ownCell is the one reverse-video cell of a focused view (fails on none or
// more than one).
func ownCell(t *testing.T, e *Editor) uitest.ReverseCell {
	t.Helper()
	cells := uitest.ReverseCells(e.View(true))
	if len(cells) != 1 {
		t.Fatalf("reverse cells = %+v, want exactly the cursor's", cells)
	}
	return cells[0]
}

// wantCursor checks the real cursor's place and shape, and that the
// reverse-video cell sits under it holding text.
func wantCursor(t *testing.T, e *Editor, x, y int, shape tea.CursorShape, text string) {
	t.Helper()
	c := e.Cursor()
	if c == nil {
		t.Fatalf("no cursor (want %d,%d)", x, y)
	}
	if c.X != x || c.Y != y || c.Shape != shape {
		t.Fatalf("cursor = %d,%d shape %v, want %d,%d shape %v", c.X, c.Y, c.Shape, x, y, shape)
	}
	if cell := ownCell(t, e); cell.X != x || cell.Y != y || cell.Text != text {
		t.Fatalf("cursor cell = %+v, want %q at %d,%d", cell, text, x, y)
	}
}

func TestCursorFollowsMoves(t *testing.T) {
	for _, name := range []theme.Name{theme.Amber, theme.Cream, theme.Midnight, theme.Charm} {
		t.Run(string(name), func(t *testing.T) {
			e := New(theme.Get(name), "abc\n한글x\n", false)
			e.Resize(30, 5)
			g := e.gutterWidth()
			wantCursor(t, e, g, 0, tea.CursorBlock, "a")
			press(t, e, "l")
			wantCursor(t, e, g+1, 0, tea.CursorBlock, "b")
			press(t, e, "j") // column 1 of "한글x": 글, two cells right of the gutter
			wantCursor(t, e, g+2, 1, tea.CursorBlock, "글")
			press(t, e, "l")
			wantCursor(t, e, g+4, 1, tea.CursorBlock, "x")
			press(t, e, "a") // after x: past the line's end
			wantCursor(t, e, g+5, 1, tea.CursorBar, " ")
			press(t, e, "esc", "0")
			wantCursor(t, e, g, 1, tea.CursorBlock, "한")
			press(t, e, "i")
			wantCursor(t, e, g, 1, tea.CursorBar, "한")
		})
	}
}

func TestCursorScrolls(t *testing.T) {
	var b strings.Builder
	for i := range 100 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\n")
	}
	e := newEd(b.String(), false)
	e.Resize(30, 6)
	g := e.gutterWidth()
	press(t, e, "G")
	wantCursor(t, e, g, 5, tea.CursorBlock, "l")
	press(t, e, "g", "g")
	wantCursor(t, e, g, 0, tea.CursorBlock, "l")
	// A line longer than the pane: the view scrolls sideways with it.
	e = newEd("short\n"+strings.Repeat("abcdefghij", 8)+"Z\n", false)
	e.Resize(30, 6)
	press(t, e, "j", "$")
	c := e.Cursor()
	if c == nil || c.X >= 30 || c.Y != 1 {
		t.Fatalf("cursor at the end of a long line = %+v", c)
	}
	wantCursor(t, e, c.X, 1, tea.CursorBlock, "Z")
}

func TestCursorHiddenWhenUnfocused(t *testing.T) {
	e := newEd("abc\n", false)
	if cells := uitest.ReverseCells(e.View(false)); len(cells) != 0 {
		t.Fatalf("unfocused view draws a cursor cell: %+v", cells)
	}
	ownCell(t, e)
}

// The command line takes the cursor (a bar after the typed text); the text
// area draws no cursor cell meanwhile.
func TestCursorOnCommandLine(t *testing.T) {
	e := newEd("abc\n", false)
	press(t, e, ":", "w")
	c := e.Cursor()
	if c == nil || c.Y != e.h-1 || c.X != 2 || c.Shape != tea.CursorBar {
		t.Fatalf("command-line cursor = %+v", c)
	}
	if cells := uitest.ReverseCells(e.View(true)); len(cells) != 0 {
		t.Fatalf("cursor cell drawn while the command line is open: %+v", cells)
	}
}
