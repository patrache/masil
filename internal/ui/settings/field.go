package settings

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
)

// One-line text fields of the settings screen: the focus default minutes (digits)
// and the three password fields.

// Field limits.
const (
	inputLimit  = 256 // a huge paste is cut here; the server rules are checked on submit
	numberLimit = 3   // 1..720
	maskRune    = "*" // single-width in every terminal
)

// field is one single-line text field.
type field struct {
	secret bool                // password: masked, never normalized
	norm   func(string) string // input normalization (nil for secret fields)
	ti     textinput.Model
}

// newField is a bubbles textinput with the pane input rules of plan 03 Task 28
// (duplicated because widgets.newTextInput is unexported): real cursor (virtual cursor
// off, so Focus emits no blink Cmd), no prompt, ctrl+k removed from
// DeleteAfterCursor (the palette key), clipboard paste and suggestions unbound (they
// would bypass normalization / take up/down). The model stays focused; the screen
// routes input to the edited field only.
func newField(secret bool, norm func(string) string, limit int) *field {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = limit
	ti.SetVirtualCursor(false)
	km := textinput.DefaultKeyMap()
	km.DeleteAfterCursor = withoutKeys(km.DeleteAfterCursor, "ctrl+k")
	none := key.NewBinding(key.WithDisabled())
	km.Paste = none
	km.AcceptSuggestion, km.NextSuggestion, km.PrevSuggestion = none, none, none
	ti.KeyMap = km
	if secret {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '*'
	}
	ti.Focus()
	return &field{secret: secret, norm: norm, ti: ti}
}

// newPasswordField is a masked field whose value is never normalized.
func newPasswordField() *field { return newField(true, nil, inputLimit) }

// newNumberField keeps only ASCII digits (after the input normalization).
func newNumberField() *field { return newField(false, digitsOnly, numberLimit) }

// withoutKeys returns b without the keys in drop.
func withoutKeys(b key.Binding, drop ...string) key.Binding {
	keys := slices.DeleteFunc(slices.Clone(b.Keys()), func(k string) bool { return slices.Contains(drop, k) })
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(b.Help().Key, b.Help().Desc))
}

// digitsOnly is textwidth.SanitizeInput (CA 8.4) reduced to ASCII digits.
func digitsOnly(s string) string {
	s = textwidth.SanitizeInput(s)
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// secretPaste keeps a pasted password as is except line breaks (a copied password
// often ends in one).
var secretPaste = strings.NewReplacer("\r", "", "\n", "")

// isTextKey reports a key that inserts text (no ctrl/alt/meta modifier).
func isTextKey(m tea.KeyPressMsg) bool {
	return m.Text != "" && m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0
}

// value is the field value.
func (f *field) value() string { return f.ti.Value() }

// clear empties the field.
func (f *field) clear() { f.ti.Reset() }

// set replaces the value and puts the cursor at the end.
func (f *field) set(v string) {
	f.ti.SetValue(v)
	f.ti.CursorEnd()
}

// update feeds an input msg to the field: key text and pasted text are normalized
// BEFORE they reach the textinput (secret fields: line breaks dropped from pastes
// only). Non-input msgs are ignored.
func (f *field) update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if isTextKey(m) && f.norm != nil {
			msg = tea.PasteMsg{Content: f.norm(m.Text)}
		}
	case tea.PasteMsg:
		if f.norm != nil {
			msg = tea.PasteMsg{Content: f.norm(m.Content)}
		} else {
			msg = tea.PasteMsg{Content: secretPaste.Replace(m.Content)}
		}
	default:
		return nil
	}
	var cmd tea.Cmd
	f.ti, cmd = f.ti.Update(msg)
	return cmd
}

// display is what the field draws: the value (masked for secret fields) split at the
// cursor.
func (f *field) display() (before, after string) {
	rs := []rune(f.ti.Value())
	pos := min(f.ti.Position(), len(rs))
	if f.secret {
		return strings.Repeat(maskRune, pos), strings.Repeat(maskRune, len(rs)-pos)
	}
	return string(rs[:pos]), string(rs[pos:])
}
