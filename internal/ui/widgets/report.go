package widgets

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// ---- one-line text input shared by the dialogs (CA 5.3.7) ----

// newTextInput is a bubbles textinput for dialogs: real cursor (virtual cursor off,
// so Focus emits no blink Cmd), no prompt, ctrl+k removed from DeleteAfterCursor
// (the app's palette key), ctrl+v clipboard paste and suggestions unbound (they
// would bypass normalization / take up/down). Feed it through inputUpdate only.
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
	return ti
}

// oneLine is the input normalization (textwidth.SanitizeInput, CA 8.4) for a
// single-line field: newlines become spaces.
func oneLine(s string) string {
	return strings.ReplaceAll(textwidth.SanitizeInput(s), "\n", " ")
}

// inputUpdate feeds msg to ti with normalization: key text and pasted text go through
// oneLine BEFORE reaching the textinput, and the whole value is re-normalized after
// every edit (keeping the cursor after the normalized text before it). ctrl+c follows
// the Task 22 rule: a non-empty value is cleared (handled), an empty one is not
// handled. handled reports whether msg was an input message for the field.
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
	renormalize(ti)
	return true, cmd
}

// renormalize re-applies oneLine to the whole value when an edit joined graphemes.
func renormalize(ti *textinput.Model) {
	v := ti.Value()
	n := oneLine(v)
	if n == v {
		return
	}
	rs := []rune(v)
	before := oneLine(string(rs[:min(ti.Position(), len(rs))]))
	ti.SetValue(n)
	ti.SetCursor(len([]rune(before)))
}

// inputLine draws ti's value in exactly w cells (the placeholder, dim, when empty)
// and returns the line and the cursor column. Long values scroll so the cursor stays
// visible.
func inputLine(t *theme.Theme, ti textinput.Model, placeholder string, w int) (string, int) {
	if w <= 0 {
		return "", 0
	}
	rs := []rune(ti.Value())
	if len(rs) == 0 {
		ph := textwidth.Truncate(textwidth.Sanitize(placeholder), w, "…")
		if ph == "" {
			return pad(t, w), 0
		}
		return fitLine(t, t.S.Placeholder.Render(ph), w), 0
	}
	pos := min(ti.Position(), len(rs))
	before, after := string(rs[:pos]), string(rs[pos:])
	if textwidth.Width(before) >= w {
		before = textwidth.TruncateLeft(before, w-1, "")
		after = ""
	}
	col := textwidth.Width(before)
	text := before + textwidth.Truncate(after, w-col, "")
	return fitLine(t, t.S.Base.Render(text), w), col
}

// ---- report dialog ----

// reportReasons are api.md 5.21 reasons with their Korean labels.
var reportReasons = []struct{ id, label string }{
	{"spam", "스팸·광고"},
	{"harassment", "괴롭힘·혐오"},
	{"illegal", "불법 콘텐츠"},
	{"sexual", "음란물"},
	{"violence", "폭력·위협"},
	{"other", "기타"},
}

// reportNoteLimit is the note cap in code points (api.md 5.21: note <= 1000).
const reportNoteLimit = 1000

// Report dialog texts.
const (
	reportDoneText    = "신고했어요"
	reportAlreadyText = "이미 신고한 대상이에요"
)

// reportDialog is widgets.ReportDialog.
type reportDialog struct {
	targetType, targetID, label string

	reason int  // chosen reason (moves with the cursor)
	onNote bool // the note field has the focus (else the reason list)
	note   textinput.Model
	busy   bool   // a submit is in flight
	errMsg string // inline error of the last submit
	size   ui.Size
}

var (
	_ ui.Overlay    = (*reportDialog)(nil)
	_ ui.KeyHandler = (*reportDialog)(nil)
	_ ui.Cursorer   = (*reportDialog)(nil)
	_ ui.Helper     = (*reportDialog)(nil)
)

// ReportDialog: reason list (spam|harassment|illegal|sexual|violence|other, Korean labels)
// + optional note (textinput, CA 5.3.7 rules), submits api.Report via ui.Do tag
// "core.report:<targetID>"; 201 => ToastSuccess "신고했어요", 409 already_exists =>
// ToastInfo "이미 신고한 대상이에요". targetType: user|message|post|story|guestbook_entry|space
// (api.md 5.21). label = what is shown as the target ("민지의 메시지").
//
// Keys: ↑/↓ (k/j) pick the reason, enter on the list goes to the note, tab/shift+tab
// switch list and note, enter in the note submits; esc (unhandled) closes it through
// the app. Any other error stays inline and the dialog stays open.
func ReportDialog(targetType, targetID, label string) ui.Overlay {
	ti := newTextInput()
	ti.CharLimit = reportNoteLimit
	return &reportDialog{targetType: targetType, targetID: targetID, label: label, note: ti}
}

func (d *reportDialog) tag() string                         { return "core.report:" + d.targetID }
func (d *reportDialog) Init(*ui.Context) tea.Cmd            { return nil }
func (d *reportDialog) Resize(_ *ui.Context, size ui.Size)  { d.size = size }
func (d *reportDialog) Bounds(body layout.Rect) layout.Rect { return centered(body, reportW, reportH) }
func (d *reportDialog) Modal() bool                         { return true }

func (d *reportDialog) Help(*ui.Context) []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "신고")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "메모")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "취소")),
	}
}

// setNote moves the focus to (true) or away from the note field.
func (d *reportDialog) setNote(on bool) tea.Cmd {
	d.onNote = on
	if on {
		return d.note.Focus()
	}
	d.note.Blur()
	return nil
}

func (d *reportDialog) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	s := msg.String()
	if s == "esc" || s == "ctrl+k" {
		return false, nil // the app closes the dialog (and opens the palette for ctrl+k)
	}
	if d.busy {
		return true, nil
	}
	switch s {
	case "tab", "shift+tab":
		return true, d.setNote(!d.onNote)
	}
	if d.onNote {
		if s == "enter" {
			return true, d.submit(ctx)
		}
		return inputUpdate(&d.note, msg)
	}
	switch s {
	case "up", "k":
		d.reason = max(0, d.reason-1)
	case "down", "j":
		d.reason = min(len(reportReasons)-1, d.reason+1)
	case "enter", "space":
		return true, d.setNote(true)
	default:
		return false, nil
	}
	return true, nil
}

// submit sends the report (tag core.report:<targetID>).
func (d *reportDialog) submit(ctx *ui.Context) tea.Cmd {
	if ctx == nil || ctx.API == nil {
		d.errMsg = errtext.Describe(tOf(ctx), &api.Error{Code: "network"})
		return nil
	}
	d.busy, d.errMsg = true, ""
	req := api.ReportRequest{
		TargetType: d.targetType,
		TargetID:   d.targetID,
		Reason:     reportReasons[d.reason].id,
		Note:       strings.TrimSpace(d.note.Value()),
	}
	return ui.Do(ctx, d.tag(), func(c context.Context, cl *api.Client) (string, error) {
		return cl.Report(c, req)
	})
}

func (d *reportDialog) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.PasteMsg:
		if d.onNote && !d.busy {
			_, cmd := inputUpdate(&d.note, m)
			return cmd
		}
	case ui.ResultMsg[string]:
		if m.Tag != d.tag() || !d.busy {
			return nil
		}
		d.busy = false
		switch {
		case m.Err == nil:
			return tea.Batch(closeOverlay, ui.Toast(reportDoneText, ui.ToastSuccess))
		case api.IsCode(m.Err, "already_exists"):
			return tea.Batch(closeOverlay, ui.Toast(reportAlreadyText, ui.ToastInfo))
		default:
			d.errMsg = errtext.Describe(ctx.T, m.Err)
		}
	}
	return nil
}

// layout builds the rows and the note cursor (row, column) relative to the inner rect.
func (d *reportDialog) layout(t *theme.Theme) (rows []string, curX, curY int) {
	w, h := d.size.W, d.size.H
	p := dialogPad(w)
	cw := max(1, w-2*p)
	head := []string{t.S.Title.Render(textwidth.Truncate(textwidth.Sanitize("신고 · "+d.label), cw, "…"))}
	mid := []string{"", t.S.Dim.Render("사유")}
	focus := 0
	for i, r := range reportReasons {
		mark, cur := "○", "  "
		if i == d.reason {
			mark = "●"
		}
		st := t.S.Base
		if i == d.reason && !d.onNote {
			cur, st = "› ", t.S.Selected
			focus = len(mid)
		}
		mid = append(mid, t.S.Base.Render(cur)+st.Render(mark+" "+r.label))
	}
	mid = append(mid, "", t.S.Dim.Render("메모 (선택)"))
	prefix := "  "
	if d.onNote {
		prefix = "› "
	}
	noteRow := len(mid)
	line, col := inputLine(t, d.note, "자세한 내용 (최대 1000자)", max(1, cw-2))
	mid = append(mid, t.S.Base.Render(prefix)+line)
	if d.onNote {
		focus = noteRow
	}
	var status string
	switch {
	case d.errMsg != "":
		status = t.S.Error.Render(textwidth.Truncate("✕ "+textwidth.Sanitize(d.errMsg), cw, "…"))
	case d.busy:
		status = t.S.Dim.Render("보내는 중…")
	default:
		status = t.S.Dim.Render(textwidth.Truncate("enter 신고 · tab 메모 · esc 취소", cw, "…"))
	}
	rows, midTop := stack(head, mid, []string{"", status}, focus, h)
	return rows, p + 2 + col, midTop + noteRow
}

func (d *reportDialog) View(ctx *ui.Context, _ bool) string {
	if d.size.W <= 0 || d.size.H <= 0 {
		return ""
	}
	t := themeOf(ctx)
	rows, _, _ := d.layout(t)
	return renderRows(t, rows, d.size)
}

// Cursor is the note cursor (relative to the inner rect) while the note has the focus.
func (d *reportDialog) Cursor() *tea.Cursor {
	if !d.onNote || d.busy || d.size.W <= 0 || d.size.H <= 0 {
		return nil
	}
	_, x, y := d.layout(theme.Get(theme.Default))
	if y < 0 || y >= d.size.H || x >= d.size.W {
		return nil
	}
	return tea.NewCursor(x, y)
}

// tOf is ctx.T (nil when ctx is nil — errtext then says it in ko).
func tOf(ctx *ui.Context) *i18n.T {
	if ctx == nil {
		return nil
	}
	return ctx.T
}
