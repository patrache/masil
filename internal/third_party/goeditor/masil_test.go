package goeditor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
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
