package authui

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
)

// Login/signup form fields (handle, display name, password, confirmation) and the
// client-side validation (api.md 2장 handle / password rules).

// fieldID names a form field.
type fieldID int

const (
	fieldHandle fieldID = iota
	fieldDisplay
	fieldPassword
	fieldConfirm
	numFields
)

// Field limits. inputLimit caps what a field holds (a huge paste is cut there);
// the server rules are checked on submit.
const (
	inputLimit     = 256
	passwordMin    = 8   // api.md: 8..128 code points
	passwordMax    = 128 //
	deviceNameMax  = 64  // api.md 5.1: device_name 0..64
	maskRune       = "*" // single-width in every terminal (no ambiguous-width bullet)
	handlePattern  = `^[a-z0-9_]{3,20}$`
	displayNameMax = 32
)

var handleRE = regexp.MustCompile(handlePattern)

// Validation texts.
const (
	errHandleEmpty   = "아이디를 입력해 주세요"
	errHandleFormat  = "아이디는 영문 소문자·숫자·_ 3~20자예요"
	errPasswordEmpty = "비밀번호를 입력해 주세요"
	errPasswordShort = "비밀번호는 8자 이상이에요"
	errPasswordLong  = "비밀번호는 128자 이하예요"
	errPasswordSame  = "비밀번호는 아이디와 달라야 해요"
	errConfirm       = "비밀번호가 서로 달라요"
	errDisplayLong   = "표시 이름은 32자 이하예요"
)

// field is one single-line text field.
type field struct {
	id          fieldID
	label       string
	placeholder string
	secret      bool                // password: masked, never normalized
	norm        func(string) string // input normalization (nil for secret fields)
	ti          textinput.Model
}

// newFields builds the four fields (each mode shows a subset).
func newFields() [numFields]*field {
	lower := func(s string) string { return oneLine(strings.ToLower(oneLine(s))) }
	return [numFields]*field{
		fieldHandle:   newField(fieldHandle, "아이디", "영문 소문자·숫자·_", false, lower),
		fieldDisplay:  newField(fieldDisplay, "표시 이름", "선택 · 비우면 아이디", false, oneLine),
		fieldPassword: newField(fieldPassword, "비밀번호", "", true, nil),
		fieldConfirm:  newField(fieldConfirm, "비밀번호 확인", "", true, nil),
	}
}

// newField is a bubbles textinput with the dialog input rules of widgets (plan 03
// Task 28 newTextInput, duplicated because it is unexported there): real cursor
// (virtual cursor off, so Focus emits no blink Cmd), no prompt, ctrl+k removed from
// DeleteAfterCursor (the palette key), ctrl+v clipboard paste and suggestions unbound
// (they would bypass normalization / take up/down). The model stays focused; the
// screen routes input to the active field only.
func newField(id fieldID, label, placeholder string, secret bool, norm func(string) string) *field {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = inputLimit
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
	return &field{id: id, label: label, placeholder: placeholder, secret: secret, norm: norm, ti: ti}
}

// withoutKeys returns b without the keys in drop.
func withoutKeys(b key.Binding, drop ...string) key.Binding {
	keys := slices.DeleteFunc(slices.Clone(b.Keys()), func(k string) bool { return slices.Contains(drop, k) })
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(b.Help().Key, b.Help().Desc))
}

// oneLine is the input normalization (textwidth.SanitizeInput, CA 8.4) for a
// single-line field: newlines become spaces.
func oneLine(s string) string {
	return strings.ReplaceAll(textwidth.SanitizeInput(s), "\n", " ")
}

// secretPaste keeps a pasted password as is except line breaks (a copied password
// often ends in one).
var secretPaste = strings.NewReplacer("\r", "", "\n", "")

// isTextKey reports a key that inserts text (no ctrl/alt/meta modifier).
func isTextKey(m tea.KeyPressMsg) bool {
	return m.Text != "" && m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0
}

// value is the field value (already normalized for non-secret fields).
func (f *field) value() string { return f.ti.Value() }

// clear empties the field.
func (f *field) clear() { f.ti.Reset() }

// update feeds an input msg to the field: key text and pasted text are normalized
// BEFORE they reach the textinput (secret fields: line breaks dropped from pastes
// only), and a non-secret value is re-normalized after every edit. handled reports
// whether msg was input for the field.
func (f *field) update(msg tea.Msg) (handled bool, cmd tea.Cmd) {
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
		return false, nil
	}
	f.ti, cmd = f.ti.Update(msg)
	f.renormalize()
	return true, cmd
}

// renormalize re-applies norm to the whole value when an edit joined graphemes,
// keeping the cursor after the normalized text before it.
func (f *field) renormalize() {
	if f.norm == nil {
		return
	}
	v := f.ti.Value()
	n := f.norm(v)
	if n == v {
		return
	}
	rs := []rune(v)
	before := f.norm(string(rs[:min(f.ti.Position(), len(rs))]))
	f.ti.SetValue(n)
	f.ti.SetCursor(utf8.RuneCountInString(before))
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

// validate checks the form of mode m on the client (api.md rules); it returns the
// message and the field to focus, or "" when the form may be sent.
func validate(m formMode, fs [numFields]*field) (string, fieldID) {
	h := fs[fieldHandle].value()
	pw := fs[fieldPassword].value()
	switch {
	case h == "":
		return errHandleEmpty, fieldHandle
	case !handleRE.MatchString(h):
		return errHandleFormat, fieldHandle
	}
	if m == modeSignup && utf8.RuneCountInString(strings.TrimSpace(fs[fieldDisplay].value())) > displayNameMax {
		return errDisplayLong, fieldDisplay
	}
	n := utf8.RuneCountInString(pw)
	switch {
	case n == 0:
		return errPasswordEmpty, fieldPassword
	case n < passwordMin:
		return errPasswordShort, fieldPassword
	case n > passwordMax:
		return errPasswordLong, fieldPassword
	}
	if m == modeSignup {
		if pw == h {
			return errPasswordSame, fieldPassword
		}
		if fs[fieldConfirm].value() != pw {
			return errConfirm, fieldConfirm
		}
	}
	return "", 0
}

// deviceName is the session's device name: the hostname cut to deviceNameMax code
// points ("" when unknown).
func deviceName(hostname func() (string, error)) string {
	n, err := hostname()
	if err != nil {
		return ""
	}
	n = strings.TrimSpace(n)
	if rs := []rune(n); len(rs) > deviceNameMax {
		n = string(rs[:deviceNameMax])
	}
	return n
}
