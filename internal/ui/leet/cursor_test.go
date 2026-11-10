package leet_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/ui/uitest"
)

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

// v0.1.3-1 (owner: the cursor was hard to see): the built-in editor puts
// the terminal's real cursor on its cursor cell — a block in normal mode, a
// bar in insert mode — and draws that cell in reverse video; with the
// editor not focused there is no cursor.
func TestGoeditorRealCursor(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	openToday(t, h)
	if c := h.Cursor(); c != nil {
		t.Fatalf("cursor before the editor has the keys: %+v", c)
	}
	h.Press("e", "O")
	h.Type("가나 x")
	h.Press("esc") // on x
	want := func(shape tea.CursorShape, text string) {
		t.Helper()
		c := h.Cursor()
		if c == nil {
			t.Fatalf("no cursor:\n%s", h.Screen())
		}
		if c.Shape != shape {
			t.Fatalf("shape %v, want %v", c.Shape, shape)
		}
		if got := cellAt(h.Screen(), c.X, c.Y); got != text {
			t.Fatalf("cursor %d,%d on %q, want %q:\n%s", c.X, c.Y, got, text, h.Screen())
		}
		found := false
		for _, rc := range uitest.ReverseCells(h.Styled()) {
			found = found || rc.X == c.X && rc.Y == c.Y
		}
		if !found {
			t.Fatalf("the cursor cell %d,%d is not reversed", c.X, c.Y)
		}
	}
	for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 200, H: 60}, {W: 80, H: 24}} {
		h.Resize(sz.W, sz.H)
		want(tea.CursorBlock, "x")
		h.Press("0")
		want(tea.CursorBlock, "가")
		h.Press("l")
		want(tea.CursorBlock, "나")
		h.Press("i")
		want(tea.CursorBar, "나")
		h.Press("esc", "$") // esc steps back onto 가
		want(tea.CursorBlock, "x")
	}
	h.Press("esc") // normal-mode esc: the tool has the keys again
	if c := h.Cursor(); c != nil {
		t.Fatalf("cursor after leaving the editor: %+v", c)
	}
	h.Check()
}

// Korean typed into the built-in editor goes in as itself, also when the IME
// commits two syllables in one key (v0.1.3-1: goeditor read a key's first
// byte).
func TestGoeditorTypesHangul(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e", "O")
	h.Type("가")
	h.Send(tea.KeyPressMsg{Code: tea.KeyExtended, Text: "나다"})
	h.Press("esc", ":", "w", "enter")
	if got := readFile(t, solutionPath(e)); !strings.HasPrefix(got, "가나다\n") {
		t.Fatalf("file = %q", got)
	}
}
