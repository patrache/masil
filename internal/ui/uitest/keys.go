package uitest

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// namedKeys maps the key names tea.Key.String() produces to their codes.
var namedKeys = map[string]rune{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"tab":       tea.KeyTab,
	"space":     tea.KeySpace,
	"backspace": tea.KeyBackspace,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
	"delete":    tea.KeyDelete,
	"insert":    tea.KeyInsert,
	"f1":        tea.KeyF1,
	"f2":        tea.KeyF2,
	"f3":        tea.KeyF3,
	"f4":        tea.KeyF4,
	"f5":        tea.KeyF5,
	"f6":        tea.KeyF6,
	"f7":        tea.KeyF7,
	"f8":        tea.KeyF8,
	"f9":        tea.KeyF9,
	"f10":       tea.KeyF10,
	"f11":       tea.KeyF11,
	"f12":       tea.KeyF12,
}

// modPrefixes are the modifier prefixes in tea.Key.Keystroke() order.
var modPrefixes = []struct {
	prefix string
	mod    tea.KeyMod
}{
	{"ctrl+", tea.ModCtrl},
	{"alt+", tea.ModAlt},
	{"shift+", tea.ModShift},
	{"meta+", tea.ModMeta},
	{"hyper+", tea.ModHyper},
	{"super+", tea.ModSuper},
}

// parseKey turns a key name as tea.KeyPressMsg.String() prints it ("ctrl+k",
// "shift+tab", "esc", "space", "G", "한") into the KeyPressMsg a terminal would
// deliver, so parseKey(s).String() == s for every such name.
//
//   - ctrl+/alt+/shift+/meta+/hyper+/super+ prefixes (any order) set Mod.
//   - Named keys (enter, esc, tab, space, backspace, arrows, home, end, pgup,
//     pgdown, delete, insert, f1..f12) use the tea.Key* codes.
//   - Anything else is one grapheme: Code is its rune, or tea.KeyExtended for a
//     multi-rune grapheme (as the real input decoder does).
//   - Text is set like the terminal decoder does: for a printable single rune
//     or space without non-shift modifiers ("space" => " ", "shift+a" => "A"),
//     and always for a multi-rune grapheme; never for other named keys nor for
//     single-rune ctrl/alt combos.
//
// An unparsable string is delivered as one KeyExtended key whose Text is s.
func parseKey(s string) tea.KeyPressMsg {
	if s == "" {
		return tea.KeyPressMsg{}
	}
	var mod tea.KeyMod
	name := s
	for stripped := true; stripped; {
		stripped = false
		for _, p := range modPrefixes {
			if rest, ok := strings.CutPrefix(name, p.prefix); ok && rest != "" {
				mod |= p.mod
				name = rest
				stripped = true
			}
		}
	}
	printable := mod&^tea.ModShift == 0

	if code, ok := namedKeys[name]; ok {
		k := tea.KeyPressMsg{Code: code, Mod: mod}
		if code == tea.KeySpace && printable {
			k.Text = " "
		}
		return k
	}

	g, _ := ansi.FirstGraphemeCluster(name, ansi.GraphemeWidth)
	if g != name {
		return tea.KeyPressMsg{Code: tea.KeyExtended, Text: s}
	}
	k := tea.KeyPressMsg{Mod: mod}
	if r, size := utf8.DecodeRuneInString(name); size == len(name) {
		k.Code = r
		if printable && unicode.IsPrint(r) {
			k.Text = name
			if mod&tea.ModShift != 0 {
				k.Text = string(unicode.ToUpper(r))
			}
		}
		return k
	}
	// Multi-rune grapheme: the decoder always carries its text (Keystroke prints it).
	k.Code = tea.KeyExtended
	k.Text = name
	return k
}
