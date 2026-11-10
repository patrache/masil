package leet_test

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"

	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
)

// fgSeq is the SGR foreground parameters of c ("38;2;r;g;b").
func fgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// v0.1.3-1: the built-in editor colors the solution in masil's theme, and
// the settings switch turns it off.
func TestGoeditorHighlight(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	openToday(t, h)
	h.Press("e")
	kw := fgSeq(h.Context().Theme.EditorTokenStyle(chroma.Keyword).GetForeground())
	v, ok := leet.EditorViewForTest(h.Context(), todayInstance(t, h))
	if !ok {
		t.Fatal("no built-in editor")
	}
	if !strings.Contains(v, kw) {
		t.Fatalf("func is not in the keyword color %s:\n%q", kw, v)
	}
	h.Context().Config.UI.CodeHighlight = false
	h.Press("l") // any key: the switch is read again
	v, _ = leet.EditorViewForTest(h.Context(), todayInstance(t, h))
	if strings.Contains(v, kw) {
		t.Fatal("still colored with highlighting off")
	}
}

// Completion: the menu opens after two word chars, tab accepts (the word
// replaces the typed prefix), esc closes the menu before it leaves insert.
func TestGoeditorCompletion(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	h := uitest.New(t)
	openToday(t, h)
	in := func() bool { return leet.CompletionMenuForTest(todayInstance(t, h)) }
	h.Press("e", "o", "b")
	if in() {
		t.Fatal("menu after one char")
	}
	h.Press("u")
	if !in() {
		t.Fatalf("no menu after bu:\n%s", h.Screen())
	}
	if !strings.Contains(h.Screen(), "busiestServers ") {
		t.Fatalf("menu not drawn:\n%s", h.Screen())
	}
	h.Press("tab")
	if in() {
		t.Fatal("menu open after tab")
	}
	// esc with the menu up closes it only: the x after it is typed
	h.Press("enter", "a", "r", "esc")
	if in() {
		t.Fatal("esc did not close the menu")
	}
	h.Press("x", "esc", ":", "w", "enter")
	got := readFile(t, solutionPath(e))
	if !strings.Contains(got, "\nbusiestServers\narx\n") {
		t.Fatalf("file = %q", got)
	}
	// ctrl+n opens it from one char; enter without a menu is a newline
	h.Press("o", "l", "ctrl+n")
	if !in() {
		t.Fatal("ctrl+n did not open the menu")
	}
	h.Press("enter", "enter", "esc", ":", "w", "enter")
	got = readFile(t, solutionPath(e))
	if !strings.Contains(got, "\nload\n\n") {
		t.Fatalf("file = %q", got)
	}
}
