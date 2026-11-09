// Package settings is the settings screen (RouteSettings, CA 5.8).
package settings

import (
	"image/color"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
)

func init() {
	ui.RegisterRoute(ui.RouteSettings, func(ctx *ui.Context, r ui.Route) ui.Component { return newScreen(ctx, r) })
}

// Geometry (plan 03 Task 34): at least wideMin cells wide => a section list navW
// wide on the left + the selected section on the right; narrower => one column of
// section headers and rows.
const (
	wideMin        = 72
	navW           = 22
	numberFieldW   = 5
	passwordFieldW = 24
	valueIndent    = 4 // indent of a value / error / swatch line under its row
	wheelLines     = 3
)

// screen is the RouteSettings component.
type screen struct {
	size ui.Size

	settings    api.Settings // shown server settings (Store.Settings + optimistic changes)
	settingsErr string       // GET /v1/me/settings failed
	blocks      listState[api.BlockEntry]
	sessions    listState[api.Session]
	rowErr      map[string]string // inline error per row id

	num     *field    // focus default minutes
	pw      [3]*field // current, new, confirm
	pwBusy  bool      // POST /v1/auth/password in flight
	editing string    // row id of the edited field ("" = none)

	sel    string // selected row id
	selIdx int    // its index in the last rows() (fallback when the row disappears)
	anchor string // section to jump to on the next sync (Route.Anchor)

	top    int    // first drawn line of the scrolled column
	topKey string // what top scrolls ("wide:<section>" | "narrow"); reset on change
	wheel  bool   // scrolled by the wheel: do not snap to the selection until it moves
	snap   bool   // a section jump: scroll its first row (and header) to the top

	// last View: cursor, row id per pane line, section per left-column line
	cursor  *tea.Cursor
	lineRow []string
	navRow  map[int]string
	navCols int // width of the left column (0 = one column)
}

var (
	_ ui.Component     = (*screen)(nil)
	_ ui.KeyHandler    = (*screen)(nil)
	_ ui.MouseHandler  = (*screen)(nil)
	_ ui.InputCapturer = (*screen)(nil)
	_ ui.Cursorer      = (*screen)(nil)
	_ ui.Helper        = (*screen)(nil)
)

// newScreen builds the screen for r (Anchor = section id to open on).
func newScreen(ctx *ui.Context, r ui.Route) *screen {
	s := &screen{
		rowErr: map[string]string{},
		num:    newNumberField(),
		pw:     [3]*field{newPasswordField(), newPasswordField(), newPasswordField()},
		navRow: map[int]string{},
	}
	if ctx != nil && ctx.Store != nil {
		s.settings = ctx.Store.Settings
	}
	s.jump(r.Anchor)
	return s
}

// Init implements ui.Component: GET /v1/me/settings, /v1/blocks, /v1/auth/sessions.
func (s *screen) Init(ctx *ui.Context) tea.Cmd {
	return tea.Batch(s.fetchSettings(ctx), s.fetchBlocks(ctx), s.fetchSessions(ctx))
}

// CapturesInput implements ui.InputCapturer: true while a field is edited.
func (s *screen) CapturesInput() bool { return s.editing != "" }

// Resize implements ui.Component.
func (s *screen) Resize(_ *ui.Context, size ui.Size) { s.size = size }

// Cursor implements ui.Cursorer: the real cursor in the edited field (last View).
func (s *screen) Cursor() *tea.Cursor {
	if s.cursor == nil {
		return nil
	}
	c := *s.cursor
	return &c
}

// Help implements ui.Helper.
func (s *screen) Help(*ui.Context) []key.Binding {
	if s.editing != "" {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "확인")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "입력 끝내기")),
		}
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "이동")),
		key.NewBinding(key.WithKeys("enter", "space"), key.WithHelp("enter", "바꾸기")),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "값")),
		key.NewBinding(key.WithKeys("pgdown", "pgup"), key.WithHelp("pgdn/pgup", "섹션")),
	}
}

// jump opens section id on the next sync (unknown ids are ignored).
func (s *screen) jump(id string) {
	if sectionIndex(id) < 0 {
		return
	}
	s.anchor = id
	s.stopEdit()
	s.wheel = false
}

// selectID selects row id.
func (s *screen) selectID(id string) {
	if id != s.sel {
		s.wheel = false
	}
	s.sel = id
}

// sync resolves the selection against rows: a pending anchor, a vanished row (the row
// now at its index), no selection (the first row). An edit whose row is not selected
// ends.
func (s *screen) sync(rows []row) {
	if len(rows) == 0 {
		s.sel, s.selIdx = "", 0
		return
	}
	if s.anchor != "" {
		for _, r := range rows {
			if r.sec == s.anchor {
				s.selectID(r.id)
				s.snap = true
				break
			}
		}
		s.anchor = ""
	}
	idx := -1
	for i, r := range rows {
		if r.id == s.sel {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = 0
		if s.sel != "" {
			idx = min(max(s.selIdx, 0), len(rows)-1)
		}
		s.selectID(rows[idx].id)
	}
	s.selIdx = idx
	if s.editing != "" && s.editing != s.sel {
		s.stopEdit()
	}
}

// moveBy moves the selection by delta rows (clamped).
func (s *screen) moveBy(rows []row, delta int) {
	if len(rows) == 0 {
		return
	}
	i := min(max(s.selIdx+delta, 0), len(rows)-1)
	s.selectID(rows[i].id)
	s.selIdx = i
}

// moveSection selects the first row of the next (dir > 0) or the current/previous
// (dir < 0) section.
func (s *screen) moveSection(rows []row, dir int) {
	if len(rows) == 0 {
		return
	}
	cur := rows[s.selIdx].sec
	first := func(sec string) int {
		for i, r := range rows {
			if r.sec == sec {
				return i
			}
		}
		return -1
	}
	ci := sectionIndex(cur)
	target := -1
	if dir > 0 {
		for j := ci + 1; j < len(sections) && target < 0; j++ {
			target = first(sections[j].id)
		}
	} else {
		if f := first(cur); f >= 0 && f < s.selIdx {
			target = f
		}
		for j := ci - 1; j >= 0 && target < 0; j-- {
			target = first(sections[j].id)
		}
	}
	if target >= 0 {
		s.selectID(rows[target].id)
		s.selIdx = target
	}
}

// Update implements ui.Component: pastes into the edited field, route updates (a new
// section anchor), API results and the realtime events that change the lists.
func (s *screen) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.PasteMsg:
		if f := s.editedField(); f != nil {
			return f.update(m)
		}
	case ui.RouteUpdatedMsg:
		s.jump(m.Route.Anchor)
	case ui.ResultMsg[*api.Settings]:
		if m.Tag == tagGet || strings.HasPrefix(m.Tag, tagUpdate) {
			s.onSettings(ctx, m)
		}
	case ui.ResultMsg[[]api.BlockEntry]:
		if strings.HasPrefix(m.Tag, tagBlocks) {
			s.onBlocks(ctx, m)
		}
	case ui.ResultMsg[[]api.Session]:
		if strings.HasPrefix(m.Tag, tagSessions) {
			s.onSessions(m)
		}
	case ui.ResultMsg[struct{}]:
		switch {
		case strings.HasPrefix(m.Tag, tagUnblock):
			s.onUnblock(ctx, m)
		case strings.HasPrefix(m.Tag, tagRevoke):
			s.onRevoke(m)
		case m.Tag == tagPassword:
			return s.onPassword(ctx, m)
		}
	case toolLoginForgetMsg:
		return s.forget(ctx, m.tool)
	case toolLoginForgotMsg:
		s.onToolLoginForgot(ctx, m)
	case ui.EventMsg:
		switch m.Event.Type {
		case "block.updated":
			return s.fetchBlocks(ctx)
		case "settings.updated": // Store.Apply already ran
			if ctx.Store != nil {
				s.settings = ctx.Store.Settings
			}
		}
	}
	return nil
}

// is matches msg against keymap action a, or one of the fallback keys without a keymap.
func is(ctx *ui.Context, msg tea.KeyPressMsg, a keymap.Action, fallback ...string) bool {
	if ctx != nil && ctx.Keys != nil {
		return ctx.Keys.Matches(msg, a)
	}
	k := msg.String()
	for _, f := range fallback {
		if k == f {
			return true
		}
	}
	return false
}

// HandleKey implements ui.KeyHandler. Rows: j/k (↑/↓) move, home/G first/last row,
// pgdown/pgup (ctrl+d/ctrl+u) next/previous section, enter/space toggle or open,
// ←/→ (h/l) cycle values. While a field is edited every key is taken except ctrl+c on
// an empty field (Task 22 rule).
func (s *screen) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	rows := s.rows(ctx)
	s.sync(rows)
	if len(rows) == 0 {
		return false, nil
	}
	if f := s.editedField(); f != nil {
		return s.editKey(ctx, rows, f, msg)
	}
	r := rows[s.selIdx]
	k := msg.String()
	switch {
	case k == "ctrl+c":
		return false, nil
	case is(ctx, msg, keymap.ListDown, "j", "down"):
		s.moveBy(rows, +1)
	case is(ctx, msg, keymap.ListUp, "k", "up"):
		s.moveBy(rows, -1)
	case is(ctx, msg, keymap.ListTop, "home"):
		s.moveBy(rows, -len(rows))
	case is(ctx, msg, keymap.ListBottom, "G", "end"):
		s.moveBy(rows, len(rows))
	case is(ctx, msg, keymap.ListPageDown, "pgdown", "ctrl+d"):
		s.moveSection(rows, +1)
	case is(ctx, msg, keymap.ListPageUp, "pgup", "ctrl+u"):
		s.moveSection(rows, -1)
	case k == "enter" || k == "space":
		return true, s.activate(ctx, r, +1)
	case (k == "right" || k == "l") && r.kind == kindCycle:
		return true, s.activate(ctx, r, +1)
	case (k == "left" || k == "h") && r.kind == kindCycle:
		return true, s.activate(ctx, r, -1)
	default:
		return false, nil
	}
	return true, nil
}

// editKey handles a key while field f is edited: enter commits (number) or goes to
// the next password field (the last one submits), esc ends the edit, ↑/↓ move (and
// keep editing when they land on another password field).
func (s *screen) editKey(ctx *ui.Context, rows []row, f *field, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if f.value() != "" {
			f.clear()
			return true, nil
		}
		return false, nil
	case "esc":
		if s.editing == rowFocusMin {
			delete(s.rowErr, rowFocusMin)
		}
		s.stopEdit()
		return true, nil
	case "enter":
		if s.editing == rowFocusMin {
			return true, s.commitNumber(ctx)
		}
		if i := indexOf(passwordRows, s.editing); i >= 0 && i+1 < len(passwordRows) {
			next := passwordRows[i+1]
			s.selectID(next)
			s.startEdit(next)
			return true, nil
		}
		return true, s.submitPassword(ctx)
	case "up", "down":
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		wasPassword := indexOf(passwordRows, s.editing) >= 0
		s.stopEdit()
		s.moveBy(rows, delta)
		if wasPassword && indexOf(passwordRows, s.sel) >= 0 {
			s.startEdit(s.sel)
		}
		return true, nil
	}
	return true, f.update(msg)
}

// indexOf is the index of v in vs (-1 = none).
func indexOf(vs []string, v string) int {
	for i, x := range vs {
		if x == v {
			return i
		}
	}
	return -1
}

// HandleMouse implements ui.MouseHandler: the wheel scrolls; a left click on the
// section list opens that section; a click on a row selects it, a click on the
// selected row presses it.
func (s *screen) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	switch m := msg.(type) {
	case tea.MouseWheelMsg:
		switch m.Button {
		case tea.MouseWheelDown:
			s.top += wheelLines
		case tea.MouseWheelUp:
			s.top -= wheelLines
		}
		s.wheel = true
		return nil
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil
		}
		if s.navCols > 0 && local.X < s.navCols {
			if sec, ok := s.navRow[local.Y]; ok {
				s.jump(sec)
			}
			return nil
		}
		if local.Y < 0 || local.Y >= len(s.lineRow) || s.lineRow[local.Y] == "" {
			return nil
		}
		id := s.lineRow[local.Y]
		rows := s.rows(ctx)
		s.sync(rows)
		if id != s.sel {
			s.stopEdit()
			s.selectID(id)
			s.sync(rows)
			return nil
		}
		if s.editing == id {
			return nil
		}
		return s.activate(ctx, rows[s.selIdx], +1)
	}
	return nil
}

// ---- rendering ----

// line is one drawn line of the scrolled column.
type line struct {
	text   string
	row    string // row id ("" = header / blank)
	cursor int    // cursor column, -1 = none
}

// View implements ui.Component: exactly size.H lines of size.W cells.
func (s *screen) View(ctx *ui.Context, focused bool) string {
	s.cursor, s.navRow, s.navCols = nil, map[int]string{}, 0
	w, h := s.size.W, s.size.H
	s.lineRow = make([]string, max(h, 0))
	if w <= 0 || h <= 0 {
		return ""
	}
	t := themeOf(ctx)
	rows := s.rows(ctx)
	s.sync(rows)
	out := make([]string, h)
	if w >= wideMin && len(rows) > 0 {
		s.navCols = navW
		cur := rows[s.selIdx].sec
		left := s.navLines(ctx, t, rows, cur, h)
		sep := t.S.Base.Foreground(t.P.Line).Render("│")
		right := s.column(ctx, t, rows, cur, w-navW-1, h, focused, navW+1)
		for y := range out {
			out[y] = left[y] + sep + right[y]
		}
	} else {
		copy(out, s.column(ctx, t, rows, "", w, h, focused, 0))
	}
	return strings.Join(out, "\n")
}

// navLines is the left section list (exactly h lines of navW cells), scrolled so the
// current section stays visible. The tool logins section is listed only while it
// has rows.
func (s *screen) navLines(ctx *ui.Context, t *theme.Theme, rows []row, cur string, h int) []string {
	shown := sections
	if !slices.ContainsFunc(rows, func(r row) bool { return r.sec == secTools }) {
		shown = slices.DeleteFunc(slices.Clone(sections), func(sec section) bool { return sec.id == secTools })
	}
	ci := max(slices.IndexFunc(shown, func(sec section) bool { return sec.id == cur }), 0)
	start := 0
	if len(shown) > h {
		start = min(max(ci-h/2, 0), len(shown)-h)
	}
	out := make([]string, h)
	for y := range out {
		i := start + y
		if i >= len(shown) {
			out[y] = pad(t, navW)
			continue
		}
		sec := shown[i]
		text := textwidth.PadRight(" "+sectionTitle(ctx, sec), navW)
		if sec.id == cur {
			out[y] = t.S.Selected.Render(text)
		} else {
			out[y] = t.S.Base.Render(text)
		}
		s.navRow[y] = sec.id
	}
	return out
}

// column draws the rows (sec != "": that section under a fixed title; "": every
// section with headers) in exactly h lines of w cells; x0 is the column's x in the
// pane (cursor, mouse).
func (s *screen) column(ctx *ui.Context, t *theme.Theme, rows []row, sec string, w, h int, focused bool, x0 int) []string {
	padX := 1
	if w < 6 {
		padX = 0
	}
	cw := max(1, w-2*padX)
	var head []string
	if sec != "" && h >= 6 {
		title := sectionTitle(ctx, sections[max(sectionIndex(sec), 0)])
		head = []string{t.S.Title.Render(title), ""}
	}
	labelW := labelWidths(rows, cw)

	var lines []line
	selA, selB := -1, -1
	prevSec := ""
	for _, r := range rows {
		if sec != "" && r.sec != sec {
			continue
		}
		a := len(lines)
		if sec == "" && r.sec != prevSec {
			if prevSec != "" {
				lines = append(lines, line{cursor: -1})
			}
			a = len(lines)
			lines = append(lines, line{text: t.S.Title.Render(sectionTitle(ctx, sections[max(sectionIndex(r.sec), 0)])), cursor: -1})
		}
		prevSec = r.sec
		selected := r.id == s.sel
		rl, cl, cc := s.renderRow(t, r, selected, focused, cw, labelW[r.sec])
		for i, txt := range rl {
			ln := line{text: txt, row: r.id, cursor: -1}
			if i == cl {
				ln.cursor = cc
			}
			lines = append(lines, ln)
		}
		if selected {
			selA, selB = a, len(lines)
		}
	}

	bh := h - len(head)
	key := "narrow"
	if sec != "" {
		key = "wide:" + sec
	}
	if key != s.topKey {
		s.top, s.topKey, s.wheel = 0, key, false
	}
	if s.snap && selA >= 0 { // a jump to a section shows it from its top
		s.top, s.snap = selA, false
	}
	if !s.wheel && selA >= 0 {
		if selB > s.top+bh {
			s.top = selB - bh
		}
		if selA < s.top {
			s.top = selA
		}
	}
	s.top = min(max(s.top, 0), max(0, len(lines)-bh))

	out := make([]string, 0, h)
	for _, hl := range head {
		out = append(out, pad(t, padX)+fit(t, hl, cw)+pad(t, w-padX-cw))
	}
	for y := 0; y < bh; y++ {
		i := s.top + y
		py := len(head) + y
		if i >= len(lines) {
			out = append(out, pad(t, w))
			continue
		}
		ln := lines[i]
		out = append(out, pad(t, padX)+fit(t, ln.text, cw)+pad(t, w-padX-cw))
		s.lineRow[py] = ln.row
		if ln.cursor >= 0 && focused && ln.cursor < cw {
			s.cursor = tea.NewCursor(x0+padX+ln.cursor, py)
		}
	}
	return out
}

// labelWidths is the label column width per section (fields, toggles and cycles),
// at most half the column.
func labelWidths(rows []row, cw int) map[string]int {
	out := map[string]int{}
	for _, r := range rows {
		switch r.kind {
		case kindToggle, kindCycle, kindNumber, kindPassword:
			out[r.sec] = max(out[r.sec], textwidth.Width(r.label))
		}
	}
	for k, v := range out {
		out[k] = min(v, max(1, cw/2))
	}
	return out
}

// renderRow draws row r in lines of at most cw cells; curLine/curCol give the cursor
// of an edited field (-1 = none).
func (s *screen) renderRow(t *theme.Theme, r row, selected, focused bool, cw, labelW int) (lines []string, curLine, curCol int) {
	curLine, curCol = -1, -1
	mark := t.S.Base.Render("  ")
	labelSt := t.S.Base
	if selected {
		mark = t.S.Dim.Render("› ")
		if focused {
			mark = t.S.Title.Render("› ")
		}
		labelSt = t.S.Title
	}
	const markW = 2
	indent := pad(t, valueIndent)
	wrapErr := func() {
		if r.err == "" {
			return
		}
		for _, l := range textwidth.Wrap(textwidth.Sanitize(r.err), max(1, cw-valueIndent)) {
			lines = append(lines, indent+t.S.Error.Render(l))
		}
	}

	// state rows: loading / empty / load error
	if r.info != "" {
		st := t.S.Dim
		if r.bad {
			st = t.S.Error
		}
		for i, l := range textwidth.Wrap(textwidth.Sanitize(r.info), max(1, cw-markW)) {
			m := mark
			if i > 0 {
				m = pad(t, markW)
			}
			lines = append(lines, m+st.Render(l))
		}
		wrapErr()
		return lines, curLine, curCol
	}

	if r.kind == kindButton {
		st := t.S.Bold
		if selected {
			st = t.S.Pill
		}
		l := mark + st.Render(r.label)
		if r.sub != "" {
			l += t.S.Dim.Render(" " + r.sub)
		}
		lines = append(lines, l)
		wrapErr()
		return lines, curLine, curCol
	}

	label := textwidth.Truncate(r.label, max(1, cw-markW), "…")
	head := mark + labelSt.Render(label)
	if gap := labelW - textwidth.Width(label); gap > 0 && r.kind != kindAction {
		head += pad(t, gap)
	}
	headW := textwidth.Width(head)

	// value part (and the cursor column inside it)
	var value string
	valCursor := -1
	editing := s.editing == r.id
	switch r.kind {
	case kindToggle:
		if r.on {
			value = t.S.Success.Render(textOn)
		} else {
			value = t.S.Dim.Render(textOff)
		}
	case kindCycle:
		if selected {
			value = t.S.Title.Render("‹ ") + t.S.Bold.Render(r.value) + t.S.Title.Render(" ›")
		} else {
			value = t.S.Dim.Render("‹ ") + t.S.Base.Render(r.value) + t.S.Dim.Render(" ›")
		}
	case kindNumber:
		if editing {
			in, col := fieldLine(t, r.field, true, numberFieldW, "")
			value, valCursor = in+t.S.Base.Render("분"), col
		} else {
			value = t.S.Base.Render(r.value)
		}
	case kindPassword:
		fw := min(passwordFieldW, max(1, cw-headW-1))
		if fw < passwordFieldW && cw-valueIndent > fw {
			fw = min(passwordFieldW, cw-valueIndent)
		}
		in, col := fieldLine(t, r.field, editing, fw, textPwEditHint)
		value = in
		if editing {
			valCursor = col
		}
	case kindAction:
		if r.value != "" {
			value = t.S.Dim.Render(r.value)
		}
	}
	// tail: " · sub · good"; tailOwn is the same without the leading separator (a line
	// of its own)
	var tail, tailOwn string
	if r.sub != "" {
		tail += t.S.Dim.Render(" · " + r.sub)
		tailOwn = t.S.Dim.Render(r.sub)
	}
	if r.good != "" {
		tail += t.S.Success.Render(" · " + r.good)
		if tailOwn == "" {
			tailOwn = t.S.Success.Render(r.good)
		} else {
			tailOwn += t.S.Success.Render(" · " + r.good)
		}
	}

	valW, tailW := textwidth.Width(value), textwidth.Width(tail)
	switch {
	case value == "" && headW+tailW <= cw:
		lines = append(lines, head+tail)
	case value == "" && r.wrapSub:
		lines = append(lines, head)
		for _, l := range textwidth.Wrap(textwidth.Sanitize(r.sub), max(1, cw-valueIndent)) {
			lines = append(lines, indent+t.S.Dim.Render(l))
		}
	case value == "":
		lines = append(lines, head, indent+tailOwn)
	case headW+1+valW+tailW <= cw:
		lines = append(lines, head+pad(t, 1)+value+tail)
		if valCursor >= 0 {
			curLine, curCol = 0, headW+1+valCursor
		}
	default: // the value goes under the label
		lines = append(lines, head, indent+value+tail)
		if valCursor >= 0 {
			curLine, curCol = 1, valueIndent+valCursor
		}
	}
	if r.id == rowTheme {
		lines = append(lines, swatches(t, cw)...)
	}
	wrapErr()
	return lines, curLine, curCol
}

// swatches is the theme preview: one line per theme with its letter, four palette
// colors and its label; the current theme is marked.
func swatches(cur *theme.Theme, cw int) []string {
	out := make([]string, 0, 4)
	for _, th := range theme.All() {
		m := "  "
		if th.Name == cur.Name {
			m = "● "
		}
		var sw strings.Builder
		for _, c := range []color.Color{th.P.BG, th.P.Acc, th.P.Acc2, th.P.Acc3} {
			sw.WriteString(cur.S.Base.Background(c).Render("  "))
		}
		l := pad(cur, valueIndent) + cur.S.Title.Render(m) + cur.S.Bold.Render(th.Letter+" ") + sw.String() + cur.S.Base.Render(" "+th.Label)
		out = append(out, textwidth.Truncate(l, cw, ""))
	}
	return out
}

// fieldLine draws a field's input area in exactly w cells and returns its cursor
// column. Long values scroll so the cursor stays visible; an empty field shows ph
// (dim).
func fieldLine(t *theme.Theme, f *field, editing bool, w int, ph string) (string, int) {
	st := t.S.StatusBar // chip background
	if editing {
		st = t.S.Selected
	}
	if w <= 0 || f == nil {
		return "", 0
	}
	before, after := f.display()
	if before == "" && after == "" {
		text := ""
		if !editing {
			text = textwidth.Truncate(ph, w, "…")
		}
		return st.Foreground(t.P.Dim).Render(textwidth.PadRight(text, w)), 0
	}
	if textwidth.Width(before) >= w {
		before = textwidth.TruncateLeft(before, w-1, "")
		after = ""
	}
	col := textwidth.Width(before)
	return st.Render(textwidth.PadRight(before+textwidth.Truncate(after, w-col, ""), w)), col
}

// fit truncates styled s to w cells and pads it with Base blanks to exactly w.
func fit(t *theme.Theme, s string, w int) string {
	s = textwidth.Truncate(s, w, "")
	return s + pad(t, w-textwidth.Width(s))
}

// pad is n Base styled blanks ("" for n <= 0).
func pad(t *theme.Theme, n int) string {
	if n <= 0 {
		return ""
	}
	return t.S.Base.Render(strings.Repeat(" ", n))
}
