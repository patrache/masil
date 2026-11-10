package workspace

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/ui/uitest"
)

// v0.1.3-1 (owner on Warp: "작업공간에서 커서가 안보였다"): wherever the code
// cell or a terminal panel has the focus, in every layout mode that draws
// them, the app's real cursor sits on the right screen cell — a block in
// normal mode, a bar in insert mode — and the code cell also draws that
// cell in reverse video. With the focus elsewhere neither is there.

// cellAt is the grapheme drawn at screen cell x, y ("" for none or the
// right half of a wide one).
func cellAt(screen string, x, y int) string {
	lines := strings.Split(screen, "\n")
	if y < 0 || y >= len(lines) {
		return ""
	}
	s, col := lines[y], 0
	for len(s) > 0 && col <= x {
		g, w := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if col == x {
			return g
		}
		col += w
		s = s[len(g):]
	}
	return ""
}

func reversedAt(h *uitest.Harness, x, y int) bool {
	for _, c := range uitest.ReverseCells(h.Styled()) {
		if c.X == x && c.Y == y {
			return true
		}
	}
	return false
}

// wantScreenCursor checks the real cursor's shape and that it sits on text,
// drawn reversed.
func wantScreenCursor(t *testing.T, h *uitest.Harness, shape tea.CursorShape, text string) *tea.Cursor {
	t.Helper()
	c := h.Cursor()
	if c == nil {
		t.Fatalf("%v: no cursor:\n%s", h.Context().Mode, h.Screen())
	}
	if c.Shape != shape {
		t.Fatalf("%v: cursor shape %v, want %v", h.Context().Mode, c.Shape, shape)
	}
	if got := cellAt(h.Screen(), c.X, c.Y); got != text {
		t.Fatalf("%v: the cursor at %d,%d is on %q, want %q:\n%s", h.Context().Mode, c.X, c.Y, got, text, h.Screen())
	}
	if !reversedAt(h, c.X, c.Y) {
		t.Fatalf("%v: the cursor cell %d,%d is not drawn in reverse video", h.Context().Mode, c.X, c.Y)
	}
	return c
}

func TestCodeCellCursorEveryMode(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	s := openFolder(t, h, "workspace:test", copyFixture(t))
	h.Press("j", "enter", "j", "enter", "j", "enter") // src/server/hub.ts
	if s.code.current() == nil {
		t.Fatalf("not open:\n%s", h.Screen())
	}
	h.Press("j", "w") // line 2 `import { DurableObject } …`: on {
	// Hangul before the cursor: two cells each.
	h.Press("O")
	h.Type("가나 x")
	h.Press("esc") // normal mode, on x
	for _, sz := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 80, H: 24}} {
		h.Resize(sz.W, sz.H)
		wantScreenCursor(t, h, tea.CursorBlock, "x")
		h.Press("0")
		wantScreenCursor(t, h, tea.CursorBlock, "가")
		h.Press("l")
		wantScreenCursor(t, h, tea.CursorBlock, "나")
		h.Press("j", "w")
		wantScreenCursor(t, h, tea.CursorBlock, "{")
		h.Press("i")
		wantScreenCursor(t, h, tea.CursorBar, "{")
		h.Press("esc")
		// scrolled down and back: the cursor follows the view
		h.Press("G")
		c := wantScreenCursor(t, h, tea.CursorBlock, "}")
		h.Press("g", "g")
		if c2 := h.Cursor(); c2 == nil || c2.Y >= c.Y {
			t.Fatalf("%v: after gg the cursor is at %+v (G: %+v)", h.Context().Mode, c2, c)
		}
		h.Press("j", "$") // back on the Hangul line's x
		wantScreenCursor(t, h, tea.CursorBlock, "x")
		// The tree takes the focus: no cursor, no reversed cell in the code.
		h.Press("ctrl+\\", "1")
		if c := h.Cursor(); c != nil {
			t.Fatalf("%v: tree focused, cursor %+v", h.Context().Mode, c)
		}
		for _, rc := range uitest.ReverseCells(h.Styled()) {
			if cellAt(h.Screen(), rc.X, rc.Y) == "x" {
				t.Fatalf("%v: the unfocused code cell still draws its cursor cell at %+v", h.Context().Mode, rc)
			}
		}
		h.Press("ctrl+\\", "2")
		wantScreenCursor(t, h, tea.CursorBlock, "x")
	}
	h.Check()
}

// A terminal panel that has the keyboard shows its program's cursor, at the
// program's cell, in every layout mode that draws it.
func TestTermCursorEveryMode(t *testing.T) {
	fake := setup(t)
	fake.SetCursor(0, 0) // on the "s" of the fake pane's "sh0 pane WxH"
	h := uitest.New(t)
	s := openFolder(t, h, "workspace:test", copyFixture(t))
	h.Send(launchMsgForTest(s, []string{"sh0"}))
	if s.term.current() == nil || s.focus != PaneTerm || !s.GrabsKeys() {
		t.Fatalf("terminal not focused and typing (focus %v):\n%s", s.focus, h.Screen())
	}
	for _, sz := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 80, H: 24}} {
		h.Resize(sz.W, sz.H)
		c := h.Cursor()
		if c == nil {
			t.Fatalf("%v: no cursor in the terminal:\n%s", h.Context().Mode, h.Screen())
		}
		if got := cellAt(h.Screen(), c.X, c.Y); got != "s" || cellAt(h.Screen(), c.X+1, c.Y) != "h" {
			t.Fatalf("%v: cursor %d,%d on %q, want the program's first cell:\n%s", h.Context().Mode, c.X, c.Y, got, h.Screen())
		}
	}
	h.Check()
}
