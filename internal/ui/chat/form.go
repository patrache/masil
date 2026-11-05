package chat

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// ---- one-line text input rules (CA 5.3.7 / 8.4) ----

// newTextInput is a bubbles textinput for chat dialogs: real cursor (virtual cursor
// off, so Focus emits no blink Cmd), no prompt, ctrl+k removed from
// DeleteAfterCursor (the app's palette key), ctrl+v clipboard paste and suggestions
// unbound (they would bypass normalization / take up/down). Feed it through
// inputUpdate only. (Mirrors the unexported core widgets helper.)
func newTextInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetVirtualCursor(false)
	km := textinput.DefaultKeyMap()
	km.DeleteAfterCursor = withoutKeys(km.DeleteAfterCursor, "ctrl+k")
	none := key.NewBinding(key.WithDisabled())
	km.Paste = none
	km.AcceptSuggestion, km.NextSuggestion, km.PrevSuggestion = none, none, none
	ti.KeyMap = km
	ti.Focus()
	return ti
}

// withoutKeys returns b without the keys in drop.
func withoutKeys(b key.Binding, drop ...string) key.Binding {
	keys := slices.DeleteFunc(slices.Clone(b.Keys()), func(k string) bool { return slices.Contains(drop, k) })
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(b.Help().Key, b.Help().Desc))
}

// oneLine is textwidth.SanitizeInput for a single-line field: newlines become spaces.
func oneLine(s string) string {
	return strings.ReplaceAll(textwidth.SanitizeInput(s), "\n", " ")
}

// isTextKey reports a key that inserts text (no ctrl/alt/meta modifier).
func isTextKey(m tea.KeyPressMsg) bool {
	return m.Text != "" && m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0
}

// inputUpdate feeds msg to ti with normalization: key text and pasted text go
// through oneLine BEFORE the textinput, and the value is re-normalized after every
// edit. ctrl+c clears a non-empty value (handled) and is not handled when empty.
func inputUpdate(ti *textinput.Model, msg tea.Msg) (handled bool, cmd tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if m.String() == "ctrl+c" {
			if ti.Value() == "" {
				return false, nil
			}
			ti.Reset()
			return true, nil
		}
		if isTextKey(m) {
			msg = tea.PasteMsg{Content: oneLine(m.Text)}
		}
	case tea.PasteMsg:
		msg = tea.PasteMsg{Content: oneLine(m.Content)}
	default:
		return false, nil
	}
	*ti, cmd = ti.Update(msg)
	if v := ti.Value(); oneLine(v) != v {
		rs := []rune(v)
		before := oneLine(string(rs[:min(ti.Position(), len(rs))]))
		ti.SetValue(oneLine(v))
		ti.SetCursor(len([]rune(before)))
	}
	return true, cmd
}

// inputLine draws ti's value in exactly w cells (the placeholder, dim, when empty)
// and returns the cursor column. Long values scroll so the cursor stays visible.
func inputLine(t *theme.Theme, ti textinput.Model, placeholder string, w int) (string, int) {
	if w <= 0 {
		return "", 0
	}
	rs := []rune(ti.Value())
	if len(rs) == 0 {
		return cell(t.S.Placeholder.Render, textwidth.Sanitize(placeholder), w), 0
	}
	pos := min(ti.Position(), len(rs))
	before, after := string(rs[:pos]), string(rs[pos:])
	if textwidth.Width(before) >= w {
		before, after = textwidth.TruncateLeft(before, w-1, ""), ""
	}
	col := textwidth.Width(before)
	text := before + textwidth.Truncate(after, w-col, "")
	return t.S.Base.Render(text) + blank(t, w-textwidth.Width(text)), col
}

// ---- form ----

// fieldKind is a form field type.
type fieldKind int

// Field kinds.
const (
	fieldText   fieldKind = iota // one-line textinput
	fieldChoice                  // one of Choices, ←/→ (h/l)
	fieldToggle                  // on/off, space ("true" / "false")
)

// field is one form field. Value is the initial value (choice: one of Choices,
// default Choices[0]; toggle: "true" = on). Max is the text CharLimit (0 = none).
// Validate returns an error text ("" = ok).
type field struct {
	Key, Label, Placeholder, Value string
	Kind                           fieldKind
	Choices                        []string
	Max                            int
	Validate                       func(string) string
}

// formSpec describes a form. OnSubmit gets the values by Key and returns the ui.Do
// tag it started (and the Cmd); the form then waits for the ResultMsg with that tag:
// an error is shown (api.Describe) and the form stays, success calls OnDone with the
// ResultMsg. tag "" means nothing to wait for: OnDone(ctx, nil) runs right away.
// OnDone may return an overlay to morph into; else the form closes.
type formSpec struct {
	Title, Submit string
	Fields        []field
	OnSubmit      func(ctx *ui.Context, vals map[string]string) (tag string, cmd tea.Cmd)
	OnDone        func(ctx *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay)
}

// form is newForm's overlay.
type form struct {
	morph
	spec    formSpec
	inputs  []textinput.Model // per field (text fields only use theirs)
	choice  []int             // per choice field: chosen index
	on      []bool            // per toggle field
	focus   int
	busy    bool
	tag     string
	errMsg  string
	spinner *widgets.Spinner
}

var (
	_ ui.Overlay       = (*form)(nil)
	_ ui.KeyHandler    = (*form)(nil)
	_ ui.Cursorer      = (*form)(nil)
	_ ui.Titled        = (*form)(nil)
	_ ui.Helper        = (*form)(nil)
	_ ui.InputCapturer = (*form)(nil)
)

// newForm is a modal form: a label row and a value row per field, then the status
// row. tab/shift+tab/↓/↑ move between fields, enter validates and submits, esc closes.
func newForm(spec formSpec) ui.Overlay {
	f := &form{spec: spec, spinner: widgets.NewSpinner()}
	for _, fd := range spec.Fields {
		ti := newTextInput()
		if fd.Max > 0 {
			ti.CharLimit = fd.Max
		}
		idx, on := 0, false
		switch fd.Kind {
		case fieldText:
			ti.SetValue(oneLine(fd.Value))
			ti.CursorEnd()
		case fieldChoice:
			if i := slices.Index(fd.Choices, fd.Value); i >= 0 {
				idx = i
			}
		case fieldToggle:
			on = fd.Value == "true"
		}
		f.inputs = append(f.inputs, ti)
		f.choice = append(f.choice, idx)
		f.on = append(f.on, on)
	}
	return f
}

// values are the current values by Key.
func (f *form) values() map[string]string {
	out := make(map[string]string, len(f.spec.Fields))
	for i, fd := range f.spec.Fields {
		out[fd.Key] = f.value(i)
	}
	return out
}

func (f *form) value(i int) string {
	fd := f.spec.Fields[i]
	switch fd.Kind {
	case fieldChoice:
		if len(fd.Choices) == 0 {
			return ""
		}
		return fd.Choices[f.choice[i]]
	case fieldToggle:
		if f.on[i] {
			return "true"
		}
		return "false"
	default:
		return f.inputs[i].Value()
	}
}

// onText reports whether the focused field is a text field.
func (f *form) onText() bool {
	return f.focus >= 0 && f.focus < len(f.spec.Fields) && f.spec.Fields[f.focus].Kind == fieldText
}

func (f *form) Init(*ui.Context) tea.Cmd { return nil }

func (f *form) Resize(ctx *ui.Context, size ui.Size) {
	f.size = size
	if f.next != nil {
		f.next.Resize(ctx, size)
	}
}

func (f *form) Bounds(body layout.Rect) layout.Rect {
	if f.next != nil {
		return f.next.Bounds(body)
	}
	return centered(body, min(body.W, formMaxW), len(f.spec.Fields)*2+4)
}

func (f *form) Modal() bool {
	if f.next != nil {
		return f.next.Modal()
	}
	return true
}

func (f *form) Title(ctx *ui.Context) (string, ui.Badge) {
	if f.next != nil {
		return f.morph.title(ctx)
	}
	return f.spec.Title, ui.Badge{}
}

func (f *form) Help(ctx *ui.Context) []key.Binding {
	if f.next != nil {
		return f.morph.help(ctx)
	}
	submit := f.spec.Submit
	if submit == "" {
		submit = "확인"
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", submit)),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "다음 칸")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기")),
	}
}

func (f *form) CapturesInput() bool {
	if f.next != nil {
		return f.captures()
	}
	return f.onText()
}

// move moves the focus by d fields (wrapping).
func (f *form) move(d int) {
	if n := len(f.spec.Fields); n > 0 {
		f.focus = ((f.focus+d)%n + n) % n
	}
}

// submit validates every field and calls OnSubmit.
func (f *form) submit(ctx *ui.Context) tea.Cmd {
	for i, fd := range f.spec.Fields {
		if fd.Validate == nil {
			continue
		}
		if msg := fd.Validate(f.value(i)); msg != "" {
			f.errMsg, f.focus = msg, i
			return nil
		}
	}
	f.errMsg = ""
	if f.spec.OnSubmit == nil {
		return f.done(ctx, nil)
	}
	tag, cmd := f.spec.OnSubmit(ctx, f.values())
	if tag == "" {
		return tea.Batch(cmd, f.done(ctx, nil))
	}
	f.busy, f.tag = true, tag
	return tea.Batch(cmd, f.spinner.Start(ctx))
}

// done finishes the form through OnDone (morph or close).
func (f *form) done(ctx *ui.Context, result tea.Msg) tea.Cmd {
	if f.spec.OnDone == nil {
		return closeOverlay
	}
	cmd, o := f.spec.OnDone(ctx, result)
	return f.finish(ctx, cmd, o)
}

func (f *form) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if f.next != nil {
		return f.morph.key(ctx, msg)
	}
	s := msg.String()
	if s == "esc" || s == "ctrl+k" {
		return false, nil // the app closes the form (and opens the palette for ctrl+k)
	}
	if f.busy {
		return true, nil
	}
	switch s {
	case "tab", "down":
		f.move(1)
		return true, nil
	case "shift+tab", "up":
		f.move(-1)
		return true, nil
	case "enter":
		return true, f.submit(ctx)
	}
	if f.focus >= len(f.spec.Fields) {
		return false, nil
	}
	fd := f.spec.Fields[f.focus]
	switch fd.Kind {
	case fieldChoice:
		if n := len(fd.Choices); n > 0 {
			switch s {
			case "left", "h":
				f.choice[f.focus] = (f.choice[f.focus] + n - 1) % n
				return true, nil
			case "right", "l", "space":
				f.choice[f.focus] = (f.choice[f.focus] + 1) % n
				return true, nil
			}
		}
		return true, nil
	case fieldToggle:
		if s == "space" || s == "left" || s == "right" || s == "h" || s == "l" {
			f.on[f.focus] = !f.on[f.focus]
		}
		return true, nil
	default:
		handled, cmd := inputUpdate(&f.inputs[f.focus], msg)
		if handled {
			f.errMsg = ""
		}
		return handled, cmd
	}
}

func (f *form) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if f.next != nil {
		return f.next.Update(ctx, msg)
	}
	if p, ok := msg.(tea.PasteMsg); ok {
		if f.onText() && !f.busy {
			_, cmd := inputUpdate(&f.inputs[f.focus], p)
			return cmd
		}
		return nil
	}
	if tag, err, ok := resultOf(msg); ok {
		if !f.busy || tag != f.tag {
			return nil
		}
		f.busy = false
		f.spinner.Stop()
		if err != nil {
			f.errMsg = api.Describe(err)
			return nil
		}
		return f.done(ctx, msg)
	}
	return f.spinner.Update(ctx, msg)
}

// layout builds the rows and the focused text field's cursor (relative to the inner rect).
func (f *form) layout(t *theme.Theme) (rows []string, curX, curY int) {
	w, h := f.size.W, f.size.H
	p := overlayPad(w)
	cw := max(1, w-2*p)
	var mid []string
	focusRow := 0
	for i, fd := range f.spec.Fields {
		label := cell(t.S.Dim.Render, textwidth.Sanitize(fd.Label), cw)
		marker := blank(t, 2)
		if i == f.focus {
			label = cell(t.S.Title.Render, textwidth.Sanitize(fd.Label), cw)
			marker = t.S.Title.Render("› ")
		}
		mid = append(mid, label)
		vw := max(1, cw-2)
		var val string
		switch fd.Kind {
		case fieldChoice:
			choice := ""
			if len(fd.Choices) > 0 {
				choice = textwidth.Sanitize(fd.Choices[f.choice[i]])
			}
			st := t.S.Base
			if i == f.focus {
				st = t.S.Selected
			}
			val = cell(st.Render, "< "+choice+" >", vw)
		case fieldToggle:
			mark := "[ ] 꺼짐"
			if f.on[i] {
				mark = "[x] 켜짐"
			}
			st := t.S.Base
			if i == f.focus {
				st = t.S.Selected
			}
			val = cell(st.Render, mark, vw)
		default:
			var col int
			val, col = inputLine(t, f.inputs[i], fd.Placeholder, vw)
			if i == f.focus {
				curX = p + 2 + col
			}
		}
		if i == f.focus {
			focusRow = len(mid)
		}
		mid = append(mid, marker+val)
	}
	var status string
	switch {
	case f.errMsg != "":
		status = cell(t.S.Error.Render, "✕ "+textwidth.Sanitize(f.errMsg), cw)
	case f.busy:
		status = f.spinner.View(t) + cell(t.S.Dim.Render, " 보내는 중…", cw-1)
	default:
		submit := f.spec.Submit
		if submit == "" {
			submit = "확인"
		}
		status = cell(t.S.Dim.Render, "enter "+textwidth.Sanitize(submit)+" · tab 다음 칸 · esc 닫기", cw)
	}
	foot := []string{"", status}
	// The title is on the border; with spare rows (degraded to the whole body, no
	// border) it is drawn inside.
	var head []string
	if h >= len(mid)+len(foot)+2 {
		head = []string{cell(t.S.Title.Render, textwidth.Sanitize(f.spec.Title), cw), ""}
	}
	if len(head)+len(mid)+len(foot) > h {
		foot = foot[1:] // drop the spacer first
	}
	avail := max(0, h-len(head)-len(foot))
	if avail == 0 && h > 0 { // the focused value row wins over the status row
		foot, avail = nil, h-len(head)
	}
	off := 0
	if focusRow >= avail {
		off = focusRow - avail + 1
	}
	off = max(0, min(off, len(mid)-avail))
	rows = append(rows, head...)
	midTop := len(rows) - off
	for i := off; i < off+avail; i++ {
		if i < len(mid) {
			rows = append(rows, mid[i])
		} else {
			rows = append(rows, "")
		}
	}
	rows = append(rows, foot...)
	return rows, curX, midTop + focusRow
}

func (f *form) View(ctx *ui.Context, focused bool) string {
	if f.next != nil {
		return f.next.View(ctx, focused)
	}
	if f.size.W <= 0 || f.size.H <= 0 {
		return ""
	}
	t := themeOf(ctx)
	rows, _, _ := f.layout(t)
	return renderOverlayRows(t, rows, f.size)
}

// Cursor is the focused text field's cursor (relative to the inner rect), nil
// while submitting or on a choice/toggle field.
func (f *form) Cursor() *tea.Cursor {
	if f.next != nil {
		return f.morph.cursor()
	}
	if !f.onText() || f.busy || f.size.W <= 0 || f.size.H <= 0 {
		return nil
	}
	_, x, y := f.layout(theme.Get(theme.Default))
	if y < 0 || y >= f.size.H || x >= f.size.W {
		return nil
	}
	return tea.NewCursor(x, y)
}
