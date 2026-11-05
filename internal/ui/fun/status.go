package fun

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

// overlayStatus is the status message dialog (no args).
const overlayStatus = "fun.status"

// Status dialog ui.Do tags (+ my user id).
const (
	tagStatusSet   = "fun.status_set:"
	tagStatusClear = "fun.status_clear:"
)

// Status copy and limits.
const (
	textStatusTitle     = "상태 메시지"
	textStatusNow       = "지금"
	textStatusNone      = "설정된 상태가 없어요"
	textStatusCustom    = "직접 입력…"
	textStatusChip      = "이모지"
	textStatusExpiry    = "유지"
	textStatusKeys      = "enter 저장 · tab 유지 시간 · ctrl+x 지우기"
	textStatusSending   = "보내는 중…"
	textStatusTooLong   = "상태 메시지는 64자까지예요"
	textStatusEmpty     = "상태 메시지를 입력해 주세요"
	textStatusChanged   = "상태를 바꿨어요"
	textStatusCleared   = "상태 메시지를 지웠어요"
	statusTextLimit     = 64 // api.md 0.3: status.text 0..64 code points (+ one emoji)
	statusDialogW       = 52
	statusMinExpirySec  = 60     // api.md PUT /v1/me/status: expires_in_sec 60..
	statusWeekSec       = 604800 // "1주"
	statusPresetCount   = 5
	statusFocusChip     = statusPresetCount     // the custom row's emoji chip
	statusFocusInput    = statusPresetCount + 1 // the custom row's text field
	statusFocusExpiry   = statusPresetCount + 2 // the expiry row
	statusDialogRowsMax = 9                     // current, 5 presets, custom, expiry, keys
)

// errStatusTooLong is parseStatusArgs' over-limit error.
var errStatusTooLong = errors.New("status text over 64 characters")

// statusPresets are the dialog's ready-made statuses (CA 6.2, plan 05 Task 16).
var statusPresets = [statusPresetCount]struct{ emoji, text string }{
	{"🎧", "듣는 중"},
	{"🔨", "배포 중"},
	{"🍚", "점심 먹는 중"},
	{"🤒", "아파요"},
	{"🏖", "휴가 중"},
}

// statusExpiries are the expiry choices; sec(now, loc) = expires_in_sec, ok=false = no expiry.
var statusExpiries = []struct {
	label string
	sec   func(now time.Time, loc *time.Location) (int, bool)
}{
	{"없음", func(time.Time, *time.Location) (int, bool) { return 0, false }},
	{"30분", fixedSec(1800)},
	{"1시간", fixedSec(3600)},
	{"4시간", fixedSec(14400)},
	{"오늘", untilEndOfDay},
	{"1주", fixedSec(statusWeekSec)},
}

func fixedSec(n int) func(time.Time, *time.Location) (int, bool) {
	return func(time.Time, *time.Location) (int, bool) { return n, true }
}

// untilEndOfDay is the seconds from now to 23:59:59 local (at least 60).
func untilEndOfDay(now time.Time, loc *time.Location) (int, bool) {
	if loc == nil {
		loc = time.Local
	}
	n := now.In(loc)
	end := time.Date(n.Year(), n.Month(), n.Day(), 23, 59, 59, 0, loc)
	return max(int(end.Sub(n)/time.Second), statusMinExpirySec), true
}

func init() {
	palette.Register(palette.Command{
		ID: "status.set", Title: "상태 메시지 설정", Group: "명령",
		Keywords: []string{"status", "상태", "듣는 중", "배포 중"},
		Run:      runStatusSet,
	})
	palette.Register(palette.Command{
		ID: "status.clear", Title: "상태 메시지 지우기", Group: "명령",
		Keywords:  []string{"status", "clear", "상태"},
		Available: func(ctx *ui.Context) bool { return ctx.Me() != nil && ctx.Me().Status != nil },
		Run: func(ctx *ui.Context, _ string) tea.Cmd {
			return doApply(ctx, clearStatus, func(c *ui.Context, _ struct{}) { applyStatus(c, nil) }, textStatusCleared)
		},
	})
	ui.RegisterOverlay(overlayStatus, func(ctx *ui.Context, _ string) ui.Overlay { return newStatusDialog(ctx) })
}

// runStatusSet is status.set: no args => the dialog; else parseStatusArgs and PUT.
func runStatusSet(ctx *ui.Context, args string) tea.Cmd {
	if strings.TrimSpace(args) == "" {
		return ui.OpenRegisteredOverlay(overlayStatus, "")
	}
	emoji, text, err := parseStatusArgs(args)
	if err != nil {
		return ui.Toast(textStatusTooLong, ui.ToastError)
	}
	req := api.SetStatusRequest{Emoji: emoji, Text: text}
	return doApply(ctx, setStatus(req), applyStatus, textStatusChanged)
}

// parseStatusArgs splits "/status" arguments: the first grapheme is the emoji
// when its first rune is a symbol (unicode.So) or at least U+1F000, and the
// trimmed rest is the text (0..64 code points, else errStatusTooLong). Input is
// normalized first (textwidth.SanitizeInput, newlines to spaces).
func parseStatusArgs(args string) (emoji api.Opt[string], text string, err error) {
	s := strings.TrimSpace(oneLine(args))
	if s != "" {
		g, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if r, _ := utf8.DecodeRuneInString(g); r >= 0x1F000 || unicode.Is(unicode.So, r) {
			emoji = api.Some(g)
			s = strings.TrimSpace(s[len(g):])
		}
	}
	if utf8.RuneCountInString(s) > statusTextLimit {
		return api.Opt[string]{}, "", errStatusTooLong
	}
	return emoji, s, nil
}

func setStatus(req api.SetStatusRequest) func(context.Context, *api.Client) (*api.UserStatus, error) {
	return func(c context.Context, a *api.Client) (*api.UserStatus, error) {
		if a == nil {
			return nil, &api.Error{Code: "network"}
		}
		return a.SetStatus(c, req)
	}
}

func clearStatus(c context.Context, a *api.Client) (struct{}, error) {
	if a == nil {
		return struct{}{}, &api.Error{Code: "network"}
	}
	return struct{}{}, a.ClearStatus(c)
}

// applyStatus stores my new status (nil = cleared) and re-observes me so every
// cached copy of my UserSummary shows it (UI goroutine only).
func applyStatus(ctx *ui.Context, st *api.UserStatus) {
	if ctx == nil || ctx.Store == nil || ctx.Store.Me == nil {
		return
	}
	ctx.Store.Me.Status = st
	ctx.Store.ObserveUser(ctx.Store.Me.UserSummary)
}

// oneLine is textwidth.SanitizeInput for a single-line field (newlines -> spaces).
func oneLine(s string) string {
	return strings.ReplaceAll(textwidth.SanitizeInput(s), "\n", " ")
}

// newLineInput is a one-line textinput with the dialog rules (CA 5.3.7): real
// cursor, no prompt, ctrl+k (palette) out of DeleteAfterCursor, clipboard paste
// and suggestions unbound (they would bypass normalization).
func newLineInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetVirtualCursor(false)
	km := textinput.DefaultKeyMap()
	var keep []string
	for _, k := range km.DeleteAfterCursor.Keys() {
		if k != "ctrl+k" {
			keep = append(keep, k)
		}
	}
	km.DeleteAfterCursor.SetKeys(keep...)
	none := key.NewBinding(key.WithDisabled())
	km.Paste = none
	km.AcceptSuggestion, km.NextSuggestion, km.PrevSuggestion = none, none, none
	ti.KeyMap = km
	return ti
}

// isTextKey reports a key that types text (no ctrl/alt/meta/super/hyper).
func isTextKey(m tea.KeyPressMsg) bool {
	return m.Text != "" && m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0
}

// inputUpdate feeds msg to ti with normalization (typed and pasted text through
// oneLine first, the whole value re-normalized after the edit). ctrl+c clears a
// non-empty value (handled) and is not handled on an empty one.
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

// statusEmojiMsg is the emoji picked for the custom row (widgets.EmojiPicker).
type statusEmojiMsg struct{ emoji string }

// statusDialog is fun.status: the current status, five presets, a custom row
// (emoji chip + text field) and the expiry row. j/k (↑/↓ in the text field)
// move between rows, tab jumps to the expiry row and back, ←/→ pick the expiry,
// enter on the chip opens the emoji picker, enter elsewhere saves the chosen
// row, ctrl+x clears the status. A failed request is a toast and the dialog
// stays.
type statusDialog struct {
	focus  int // 0..4 presets, statusFocusChip, statusFocusInput, statusFocusExpiry
	choice int // the row saved by enter: 0..4 a preset, statusFocusChip/Input the custom row
	back   int // the focus tab returns to from the expiry row
	expiry int // index into statusExpiries
	emoji  string
	in     textinput.Model
	busy   bool
	size   ui.Size
	rowY   map[int]int // focus stop -> inner row of the last View
	chipX  [2]int      // chip cells [from, to) of the last View
	cursor *tea.Cursor // of the last View, inner-rect relative
}

var (
	_ ui.Overlay       = (*statusDialog)(nil)
	_ ui.KeyHandler    = (*statusDialog)(nil)
	_ ui.MouseHandler  = (*statusDialog)(nil)
	_ ui.InputCapturer = (*statusDialog)(nil)
	_ ui.Cursorer      = (*statusDialog)(nil)
	_ ui.Titled        = (*statusDialog)(nil)
	_ ui.Helper        = (*statusDialog)(nil)
)

func newStatusDialog(*ui.Context) *statusDialog {
	return &statusDialog{in: newLineInput(), rowY: map[int]int{}}
}

func (d *statusDialog) Init(*ui.Context) tea.Cmd { return nil }

func (d *statusDialog) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.PasteMsg:
		if d.focus == statusFocusInput && !d.busy {
			_, cmd := inputUpdate(&d.in, m)
			return cmd
		}
	case statusEmojiMsg:
		if e := textwidth.Sanitize(m.emoji); e != "" {
			d.emoji, d.choice = e, statusFocusChip
		}
	case ui.ResultMsg[*api.UserStatus]:
		if m.Tag != meTag(ctx, tagStatusSet) || !d.busy {
			return nil
		}
		d.busy = false
		if m.Err != nil || m.Value == nil {
			return d.fail(m.Err)
		}
		applyStatus(ctx, m.Value)
		return tea.Batch(closeOverlay, ui.Toast(textStatusChanged, ui.ToastSuccess))
	case ui.ResultMsg[struct{}]:
		if m.Tag != meTag(ctx, tagStatusClear) || !d.busy {
			return nil
		}
		d.busy = false
		if m.Err != nil {
			return d.fail(m.Err)
		}
		applyStatus(ctx, nil)
		return tea.Batch(closeOverlay, ui.Toast(textStatusCleared, ui.ToastSuccess))
	}
	return nil
}

func (d *statusDialog) fail(err error) tea.Cmd {
	if err == nil {
		err = &api.Error{Code: "network"}
	}
	return func() tea.Msg { return ui.ErrorMsg{Err: err} }
}

// setFocus moves the focus to stop f (clamped), remembering a choice row.
func (d *statusDialog) setFocus(f int) tea.Cmd {
	f = max(0, min(f, statusFocusExpiry))
	d.focus = f
	if f < statusFocusExpiry {
		d.choice = f
	}
	if f == statusFocusInput {
		return d.in.Focus()
	}
	d.in.Blur()
	return nil
}

func (d *statusDialog) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	s := msg.String()
	if s == "esc" || s == "ctrl+k" {
		return false, nil // the app closes the dialog (and opens the palette for ctrl+k)
	}
	if d.busy {
		return true, nil
	}
	inInput := d.focus == statusFocusInput
	switch s {
	case "enter":
		if d.focus == statusFocusChip {
			return true, d.pickEmoji()
		}
		return true, d.submit(ctx)
	case "ctrl+x":
		return true, d.clear(ctx)
	case "tab", "shift+tab":
		if d.focus == statusFocusExpiry {
			return true, d.setFocus(d.back)
		}
		d.back = d.focus
		return true, d.setFocus(statusFocusExpiry)
	case "up":
		return true, d.setFocus(d.focus - 1)
	case "down":
		return true, d.setFocus(d.focus + 1)
	case "k":
		if !inInput {
			return true, d.setFocus(d.focus - 1)
		}
	case "j":
		if !inInput {
			return true, d.setFocus(d.focus + 1)
		}
	case "left", "right":
		if d.focus == statusFocusExpiry {
			step := 1
			if s == "left" {
				step = -1
			}
			d.expiry = max(0, min(d.expiry+step, len(statusExpiries)-1))
			return true, nil
		}
	}
	if inInput {
		handled, cmd := inputUpdate(&d.in, msg)
		return handled || s != "ctrl+c", cmd
	}
	return true, nil // modal: nothing below gets the key
}

// HandleMouse: a click on a row focuses it (and chooses it); a click on the
// chip opens the emoji picker; a click on the expiry row steps the expiry.
func (d *statusDialog) HandleMouse(_ *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft || d.busy {
		return nil
	}
	for f, y := range d.rowY {
		if y != local.Y {
			continue
		}
		if f == statusFocusChip || f == statusFocusInput {
			if local.X >= d.chipX[0] && local.X < d.chipX[1] {
				d.setFocus(statusFocusChip)
				return d.pickEmoji()
			}
			return d.setFocus(statusFocusInput)
		}
		if f == statusFocusExpiry && d.focus == statusFocusExpiry {
			d.expiry = (d.expiry + 1) % len(statusExpiries)
			return nil
		}
		if f == statusFocusExpiry {
			d.back = d.focus
		}
		return d.setFocus(f)
	}
	return nil
}

func (d *statusDialog) pickEmoji() tea.Cmd {
	return ui.OpenOverlay(widgets.EmojiPicker(func(e string) tea.Cmd {
		return func() tea.Msg { return statusEmojiMsg{emoji: e} }
	}))
}

// request is the PUT body of the chosen row (ok=false: a toast cmd instead).
func (d *statusDialog) request(ctx *ui.Context) (api.SetStatusRequest, tea.Cmd) {
	var req api.SetStatusRequest
	if d.choice < statusPresetCount {
		p := statusPresets[d.choice]
		req.Emoji, req.Text = api.Some(p.emoji), p.text
	} else {
		var err error
		if d.emoji != "" {
			req.Emoji, req.Text = api.Some(d.emoji), strings.TrimSpace(oneLine(d.in.Value()))
			if utf8.RuneCountInString(req.Text) > statusTextLimit {
				err = errStatusTooLong
			}
		} else {
			req.Emoji, req.Text, err = parseStatusArgs(d.in.Value())
		}
		if err != nil {
			return req, ui.Toast(textStatusTooLong, ui.ToastError)
		}
		if !req.Emoji.Set && req.Text == "" {
			return req, ui.Toast(textStatusEmpty, ui.ToastError)
		}
	}
	if sec, ok := statusExpiries[d.expiry].sec(now(ctx), ctx.Loc); ok {
		req.ExpiresInSec = api.Some(sec)
	}
	return req, nil
}

func now(ctx *ui.Context) time.Time {
	if ctx != nil && ctx.Now != nil {
		return ctx.Now()
	}
	return time.Now()
}

func (d *statusDialog) submit(ctx *ui.Context) tea.Cmd {
	req, bad := d.request(ctx)
	if bad != nil {
		return bad
	}
	d.busy = true
	return ui.Do(ctx, meTag(ctx, tagStatusSet), setStatus(req))
}

func (d *statusDialog) clear(ctx *ui.Context) tea.Cmd {
	d.busy = true
	return ui.Do(ctx, meTag(ctx, tagStatusClear), clearStatus)
}

func (d *statusDialog) CapturesInput() bool { return d.focus == statusFocusInput }

func (d *statusDialog) Resize(ctx *ui.Context, size ui.Size) {
	d.size = size
	_ = d.View(ctx, true) // cursor geometry for Cursor (which has no Context)
}

func (d *statusDialog) Bounds(body layout.Rect) layout.Rect {
	return centered(body, statusDialogW, statusDialogRowsMax+2)
}

// centered is a w×h box (shrunk to body minus a margin) centred in body.
func centered(body layout.Rect, w, h int) layout.Rect {
	w, h = max(min(w, body.W-4, body.W), 0), max(min(h, body.H-2, body.H), 0)
	return layout.Rect{X: body.X + (body.W-w)/2, Y: body.Y + (body.H-h)/2, W: w, H: h}
}

func (d *statusDialog) Modal() bool { return true }

func (d *statusDialog) Title(*ui.Context) (string, ui.Badge) { return textStatusTitle, ui.Badge{} }

func (d *statusDialog) Help(*ui.Context) []key.Binding {
	b := func(k, desc string) key.Binding { return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc)) }
	return []key.Binding{b("enter", "저장"), b("tab", "유지 시간"), b("ctrl+x", "지우기"), b("esc", "취소")}
}

func (d *statusDialog) Cursor() *tea.Cursor {
	if d.cursor == nil || d.focus != statusFocusInput || d.busy {
		return nil
	}
	c := *d.cursor
	return &c
}

// statusLabel is "🎧 듣는 중" for st (sanitized; "" for nil).
func statusLabel(st *api.UserStatus) string {
	if st == nil {
		return ""
	}
	s := textwidth.Sanitize(st.Text)
	if st.Emoji != nil {
		if e := textwidth.Sanitize(*st.Emoji); e != "" {
			s = strings.TrimSpace(e + " " + s)
		}
	}
	return s
}

// rowStyle is the prefix and text style of a row: focused rows get "› " in the
// accent, the chosen row the accent colour.
func rowStyle(t *theme.Theme, focused, chosen bool) (prefix string, st func(...string) string) {
	switch {
	case focused:
		return t.S.Base.Foreground(t.P.Acc).Render("› "), t.S.Base.Foreground(t.P.Acc).Bold(true).Render
	case chosen:
		return t.S.Base.Render("  "), t.S.Base.Foreground(t.P.Acc).Render
	}
	return t.S.Base.Render("  "), t.S.Base.Render
}

// inputLine draws the field value in exactly w cells (the hint, dim, while
// empty) and returns the cursor column; long values scroll to keep the cursor.
func inputLine(t *theme.Theme, ti textinput.Model, hint string, w int) (string, int) {
	if w <= 0 {
		return "", 0
	}
	rs := []rune(ti.Value())
	if len(rs) == 0 {
		return fit(t, t.S.Placeholder.Render(textwidth.Truncate(hint, w, "…")), w), 0
	}
	pos := min(ti.Position(), len(rs))
	before, after := textwidth.Sanitize(string(rs[:pos])), textwidth.Sanitize(string(rs[pos:]))
	if textwidth.Width(before) >= w {
		before = textwidth.TruncateLeft(before, w-1, "")
		after = ""
	}
	col := textwidth.Width(before)
	return fit(t, t.S.Base.Render(before+textwidth.Truncate(after, w-col, "")), w), col
}

// expiryLine is "유지  없음 · 30분 · [1시간] · …" in cw cells, or the compact
// "유지  ‹ 1시간 ›" when the full row does not fit.
func (d *statusDialog) expiryLine(t *theme.Theme, cw int) string {
	focused := d.focus == statusFocusExpiry
	prefix, label := rowStyle(t, focused, false)
	sel := t.S.Base.Foreground(t.P.Acc).Bold(true).Reverse(focused)
	head := label(textStatusExpiry) + t.S.Base.Render("  ")
	full, plain := head, textStatusExpiry+"  "
	for i, e := range statusExpiries {
		if i > 0 {
			full += t.S.Dim.Render(" · ")
			plain += " · "
		}
		plain += e.label
		if i == d.expiry {
			full += sel.Render(e.label)
		} else {
			full += t.S.Base.Render(e.label)
		}
	}
	if textwidth.Width(plain) <= cw-2 {
		return prefix + full
	}
	return prefix + head + t.S.Dim.Render("‹ ") + sel.Render(statusExpiries[d.expiry].label) + t.S.Dim.Render(" ›")
}

// View is the current status, the presets, the custom row, the expiry row and
// the key hint, cut to the inner size so the focused row stays visible.
func (d *statusDialog) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := d.size.W, d.size.H
	d.cursor, d.chipX = nil, [2]int{}
	clear(d.rowY)
	if w <= 0 || h <= 0 {
		return ""
	}
	pad := 0
	if w >= 12 {
		pad = 1
	}
	cw := max(w-2*pad, 1)
	var rows []string
	curCol := -1
	rowOf := map[int]int{}

	cur := t.S.Dim.Render(textStatusNone)
	if me := ctx.Me(); me != nil && me.Status != nil {
		cur = t.S.Base.Render(statusLabel(me.Status))
	}
	rows = append(rows, t.S.Dim.Render(textStatusNow+"  ")+cur)

	for i, p := range statusPresets {
		prefix, st := rowStyle(t, d.focus == i, d.choice == i)
		rowOf[i] = len(rows)
		rows = append(rows, prefix+st(p.emoji+" "+p.text))
	}

	custom := d.focus == statusFocusChip || d.focus == statusFocusInput
	prefix, _ := rowStyle(t, custom, d.choice >= statusFocusChip && d.choice < statusFocusExpiry)
	chip := "[" + textStatusChip + "]"
	if d.emoji != "" {
		chip = "[" + d.emoji + "]"
	}
	chipSt := t.S.Base
	if d.focus == statusFocusChip {
		chipSt = t.S.Base.Foreground(t.P.Acc).Bold(true).Reverse(true)
	} else if d.choice == statusFocusChip || d.choice == statusFocusInput {
		chipSt = t.S.Base.Foreground(t.P.Acc)
	}
	chipW := textwidth.Width(chip)
	fieldX := 2 + chipW + 1
	field, col := inputLine(t, d.in, textStatusCustom, cw-fieldX)
	rowOf[statusFocusChip], rowOf[statusFocusInput] = len(rows), len(rows)
	d.chipX = [2]int{pad + 2, pad + 2 + chipW}
	if d.focus == statusFocusInput {
		curCol = pad + fieldX + col
	}
	rows = append(rows, prefix+chipSt.Render(chip)+t.S.Base.Render(" ")+field)

	rowOf[statusFocusExpiry] = len(rows)
	rows = append(rows, d.expiryLine(t, cw))

	hint := textStatusKeys
	if d.busy {
		hint = textStatusSending
	}
	rows = append(rows, t.S.Dim.Render(textwidth.Truncate(hint, cw, "…")))

	focusRow := rowOf[d.focus]
	top := 0
	if focusRow >= h {
		top = focusRow - h + 1
	}
	for f, y := range rowOf {
		if y-top >= 0 && y-top < h {
			d.rowY[f] = y - top
		}
	}
	if curCol >= 0 && curCol < w && focusRow-top >= 0 && focusRow-top < h {
		d.cursor = tea.NewCursor(curCol, focusRow-top)
	}
	side := t.S.Base.Render(strings.Repeat(" ", pad))
	out := make([]string, 0, h)
	for i := top; i < top+h; i++ {
		l := ""
		if i < len(rows) {
			l = rows[i]
		}
		out = append(out, fit(t, side+fit(t, l, cw)+side, w))
	}
	return strings.Join(out, "\n")
}
