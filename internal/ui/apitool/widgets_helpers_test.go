package apitool

import (
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
)

// recTimers records ui.After calls; the Cmd returns the message right away
// so a test delivers it by running the Cmd.
type recTimers struct{ delays []time.Duration }

func (r *recTimers) After(d time.Duration, msg tea.Msg) tea.Cmd {
	r.delays = append(r.delays, d)
	return func() tea.Msg { return msg }
}

func testCtx() (*ui.Context, *recTimers) {
	rt := &recTimers{}
	return &ui.Context{T: i18n.KoT(), Lang: i18n.Ko, Timers: rt, Now: func() time.Time { return time.Unix(0, 0) }}, rt
}

var testKeyCodes = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "space": tea.KeySpace,
	"backspace": tea.KeyBackspace, "up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft,
	"right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd, "delete": tea.KeyDelete,
}

// key is the KeyPressMsg a terminal sends for s ("a", "tab", "shift+tab", "V").
func key(s string) tea.KeyPressMsg {
	if s == "shift+tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	if c, ok := testKeyCodes[s]; ok {
		k := tea.KeyPressMsg{Code: c}
		if c == tea.KeySpace {
			k.Text = " "
		}
		return k
	}
	r, _ := utf8.DecodeRuneInString(s)
	return tea.KeyPressMsg{Code: r, Text: s}
}

func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}
