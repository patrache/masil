package goeditor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ionut-t/goeditor/core"
)

// masil's patch: with WithDarkBackground, New never asks the terminal for its
// background (on Windows that query opens the console, puts it in raw mode
// and reads it for up to 2s while Bubble Tea is reading it too).
func TestNewWithDarkBackgroundSkipsQuery(t *testing.T) {
	asked := 0
	saved := hasDarkBackground
	hasDarkBackground = func() bool { asked++; return true }
	t.Cleanup(func() { hasDarkBackground = saved })
	for _, dark := range []bool{true, false} {
		m := New(80, 20, WithDarkBackground(dark))
		if want := DefaultTheme(dark); m.theme.StatusLineStyle.GetForeground() != want.StatusLineStyle.GetForeground() {
			t.Fatalf("dark=%v: theme does not follow the option", dark)
		}
	}
	if asked != 0 {
		t.Fatalf("the terminal was asked %d times", asked)
	}
	New(80, 20) // without the option: upstream's query
	if asked != 1 {
		t.Fatalf("asked = %d without the option", asked)
	}
}

// masil's patch: SetSyntaxStyle colors tokens with masil's theme instead of
// a Chroma style, and CompletionMenuVisible tells the wrapper whether the
// menu is up (it drives the menu itself: no debounce timers run).
func TestSyntaxStyleAndMenuVisible(t *testing.T) {
	m := New(40, 5, WithDarkBackground(true))
	m.SetContent("func x")
	m.Focus()
	m.SetLanguage("go", "")
	marked := lipgloss.NewStyle().Foreground(lipgloss.Color("#123456"))
	m.SetSyntaxStyle(func(tt chroma.TokenType) lipgloss.Style {
		if tt.InCategory(chroma.Keyword) {
			return marked
		}
		return lipgloss.NewStyle()
	})
	if v := m.View(); !strings.Contains(v, "38;2;18;52;86") { // #123456 (the line's own background is laid over it)
		t.Fatalf("keyword not in the given style:\n%q", v)
	}
	if m.CompletionMenuVisible() {
		t.Fatal("menu visible at start")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	var ctx core.CompletionContext
	for done := false; !done; {
		select {
		case s := <-m.GetEditor().GetUpdateSignalChan():
			if r, ok := s.(core.CompletionRequestSignal); ok {
				ctx = r.Context()
			}
		default:
			done = true
		}
	}
	m, _ = m.Update(CompletionResponseMsg{Completions: []core.Completion{{Text: "xyz", Label: "xyz"}}, Context: ctx})
	if !m.CompletionMenuVisible() {
		t.Fatal("menu not visible after a response")
	}
}

// masil's patch (v0.1.3-1): SetCursorCellStyle draws the cursor cell in one
// style in every mode, and CursorPosition is where View draws it — the app
// puts the terminal's real cursor there.
func TestCursorPositionMatchesCell(t *testing.T) {
	m := New(20, 6, WithDarkBackground(true))
	m.SetContent("ab\n한글x\n" + strings.Repeat("0123456789", 3) + "Z\nend")
	m.Focus()
	mark := lipgloss.NewStyle().Background(lipgloss.Color("#010203"))
	m.SetCursorCellStyle(&mark)
	drawn := func() (int, int, bool) {
		for y, l := range strings.Split(m.View(), "\n") {
			if i := strings.Index(l, "48;2;1;2;3"); i >= 0 {
				j := strings.LastIndex(l[:i], "\x1b[")
				return ansi.StringWidth(l[:j]), y, true
			}
		}
		return 0, 0, false
	}
	keys := []tea.KeyPressMsg{
		{}, {Code: 'l', Text: "l"}, {Code: 'j', Text: "j"}, {Code: 'l', Text: "l"},
		{Code: 'j', Text: "j"}, {Code: '$', Text: "$"}, // a wrapped line's end
		{Code: 'j', Text: "j"}, {Code: 'A', Text: "A"}, // insert, past the end
		{Code: tea.KeyEscape}, {Code: 'g', Text: "g"}, {Code: 'g', Text: "g"},
	}
	for i, k := range keys {
		if k.Code != 0 {
			m, _ = m.Update(k)
		}
		x, y, ok := m.CursorPosition()
		dx, dy, dok := drawn()
		if !ok || !dok || x != dx || y != dy {
			t.Fatalf("key %d (%q): CursorPosition = %d,%d,%v; drawn at %d,%d,%v\n%s", i, k.String(), x, y, ok, dx, dy, dok, m.View())
		}
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: ':', Text: ":"})
	m, _ = m.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	if x, y, ok := m.CursorPosition(); !ok || y != 5 || x != 2 {
		t.Fatalf("command line: %d,%d,%v, want 2,5", x, y, ok)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Blur()
	if _, _, ok := m.CursorPosition(); ok {
		t.Fatal("a blurred editor reports a cursor")
	}
}

// masil's patch (v0.1.3-1): a typed Korean syllable goes in as itself
// (upstream took the first byte of the key's text).
func TestTypesHangul(t *testing.T) {
	m := New(20, 4, WithDarkBackground(true))
	m.Focus()
	m, _ = m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	for _, r := range "가나" {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := m.GetCurrentContent(); got != "가나" {
		t.Fatalf("content = %q", got)
	}
}

// masil's patch (v0.1.3-1): SelectedCompletion is the menu's selected item,
// so the wrapper can apply a language server's own text edit for it.
func TestSelectedCompletion(t *testing.T) {
	m := New(40, 5, WithDarkBackground(true))
	m.SetContent("")
	m.Focus()
	if _, ok := m.SelectedCompletion(); ok {
		t.Fatal("selected with no menu")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	var ctx core.CompletionContext
	for done := false; !done; {
		select {
		case s := <-m.GetEditor().GetUpdateSignalChan():
			if r, ok := s.(core.CompletionRequestSignal); ok {
				ctx = r.Context()
			}
		default:
			done = true
		}
	}
	m, _ = m.Update(CompletionResponseMsg{Completions: []core.Completion{{Text: "a", Label: "a"}, {Text: "b", Label: "b"}}, Context: ctx})
	if c, ok := m.SelectedCompletion(); !ok || c.Label != "a" {
		t.Fatalf("selected %+v %v", c, ok)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if c, ok := m.SelectedCompletion(); !ok || c.Label != "b" {
		t.Fatalf("after down: %+v %v", c, ok)
	}
}
