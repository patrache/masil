// Package authui is the login / signup screen (RouteLogin, CA 5.8).
package authui

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/widgets"
)

// LoggedInMsg is emitted after a successful login/signup (credentials already saved);
// the app sets the API token, Store.Me and starts realtime.
type LoggedInMsg struct {
	Credentials *auth.Credentials
	Me          *api.Me
}

func init() {
	ui.RegisterRoute(ui.RouteLogin, func(ctx *ui.Context, r ui.Route) ui.Component { return newScreen(r) })
}

// Route.Arg values and the notice query key (set by the app on logout, plan 03 Task 30).
const (
	argLogin  = "login"
	argSignup = "signup"
	noticeKey = "notice"
)

// submitTimeout bounds one login/signup request.
const submitTimeout = 20 * time.Second

// Card geometry (plan 03 Task 33): a centered bordered card at most cardMaxW wide;
// below cardMinW the form fills the pane without a border.
const (
	cardMaxW      = 56
	cardMinW      = 44
	cardPadX      = 2 // blank cells inside the card border on each side
	barePadX      = 1 // blank cells on each side without the card
	inlineFieldW  = 16
	clientName    = "tui"
	saveFailedMsg = "로그인 정보를 저장하지 못했어요"
)

// formMode is login or signup.
type formMode int

const (
	modeLogin formMode = iota
	modeSignup
)

// elemKind is a focusable element of the form.
type elemKind int

const (
	elemField elemKind = iota
	elemButton
	elemLink
)

// elem is one focusable element (a field, the submit button or the mode link).
type elem struct {
	kind  elemKind
	field fieldID
}

// screenIDs tells the submit results of different instances apart (they are broadcast).
var screenIDs atomic.Int64

// doneMsg is the result of submit generation gen of screen id.
type doneMsg struct {
	id, gen int64
	err     error
	creds   *auth.Credentials
	me      *api.Me
	saveErr error
}

// screen is the RouteLogin component. It captures every key (the app gives the
// logged-out screen all keys but ctrl+c, and ctrl+c too while it captures input).
type screen struct {
	id     int64
	mode   formMode
	fields [numFields]*field
	focus  int // index into elems()

	notice string // logout notice from the route query (until the next submit / mode switch)
	errMsg string // last validation / request error
	busy   bool   // a request is in flight
	gen    int64  // submit generation
	spin   *widgets.Spinner
	size   ui.Size

	hostname func() (string, error)

	// last View: cursor and the element of each pane row (mouse)
	cursor  *tea.Cursor
	rowElem map[int]int
}

var (
	_ ui.Component     = (*screen)(nil)
	_ ui.KeyHandler    = (*screen)(nil)
	_ ui.MouseHandler  = (*screen)(nil)
	_ ui.InputCapturer = (*screen)(nil)
	_ ui.Cursorer      = (*screen)(nil)
)

// newScreen builds the screen for r (Arg "signup" => signup, else login).
func newScreen(r ui.Route) *screen {
	s := &screen{
		id:       screenIDs.Add(1),
		fields:   newFields(),
		spin:     widgets.NewSpinner(),
		hostname: os.Hostname,
	}
	if r.Arg == argSignup {
		s.mode = modeSignup
	}
	s.notice = textwidth.Sanitize(r.Query[noticeKey])
	return s
}

// elems lists the focusable elements of the current mode, top to bottom.
func (s *screen) elems() []elem {
	if s.mode == modeSignup {
		return []elem{{elemField, fieldHandle}, {elemField, fieldDisplay}, {elemField, fieldPassword}, {elemField, fieldConfirm}, {kind: elemButton}, {kind: elemLink}}
	}
	return []elem{{elemField, fieldHandle}, {elemField, fieldPassword}, {kind: elemButton}, {kind: elemLink}}
}

// focused is the focused element.
func (s *screen) focused() elem {
	es := s.elems()
	return es[min(max(s.focus, 0), len(es)-1)]
}

// focusedField is the focused field, or nil when the button or link has the focus.
func (s *screen) focusedField() *field {
	if e := s.focused(); e.kind == elemField {
		return s.fields[e.field]
	}
	return nil
}

// focusField moves the focus to field id.
func (s *screen) focusField(id fieldID) {
	for i, e := range s.elems() {
		if e.kind == elemField && e.field == id {
			s.focus = i
		}
	}
}

// move moves the focus by delta, wrapping around.
func (s *screen) move(delta int) {
	n := len(s.elems())
	s.focus = ((s.focus+delta)%n + n) % n
}

// Init implements ui.Component.
func (s *screen) Init(*ui.Context) tea.Cmd { return nil }

// CapturesInput implements ui.InputCapturer: the form takes every printable key.
func (s *screen) CapturesInput() bool { return true }

// Resize implements ui.Component.
func (s *screen) Resize(_ *ui.Context, size ui.Size) { s.size = size }

// Update implements ui.Component: pastes into the focused field, the submit result,
// the spinner and route updates (a new logout notice).
func (s *screen) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tea.PasteMsg:
		if f := s.focusedField(); f != nil && !s.busy {
			_, cmd := f.update(m)
			return cmd
		}
		return nil
	case doneMsg:
		return s.done(ctx, m)
	case ui.RouteUpdatedMsg:
		if n := textwidth.Sanitize(m.Route.Query[noticeKey]); n != "" {
			s.notice = n
		}
		return nil
	}
	return s.spin.Update(ctx, msg)
}

// HandleKey implements ui.KeyHandler. tab/↓ and shift+tab/↑ move between the
// elements, enter goes to the next field (submits on the last field and the button,
// switches the mode on the link), ctrl+c clears a non-empty focused field (Task 22
// rule) and is otherwise left to the app (quit arming). Every other key is taken.
func (s *screen) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	f := s.focusedField()
	if k == "ctrl+c" {
		if f != nil && !s.busy && f.value() != "" {
			f.clear()
			return true, nil
		}
		return false, nil
	}
	if s.busy {
		return true, nil
	}
	switch {
	case k == "tab" || k == "down" || matches(ctx, msg, keymap.FocusNext):
		s.move(+1)
		return true, nil
	case k == "shift+tab" || k == "up" || matches(ctx, msg, keymap.FocusPrev):
		s.move(-1)
		return true, nil
	case k == "enter":
		return true, s.activate(ctx, true)
	case k == "space" && f == nil:
		return true, s.activate(ctx, false)
	}
	if f != nil {
		_, cmd := f.update(msg)
		return true, cmd
	}
	return true, nil
}

// matches is ctx.Keys.Matches (false without a keymap).
func matches(ctx *ui.Context, msg tea.KeyPressMsg, a keymap.Action) bool {
	return ctx != nil && ctx.Keys != nil && ctx.Keys.Matches(msg, a)
}

// activate is enter (fromKey) or a click on the focused element.
func (s *screen) activate(ctx *ui.Context, fromKey bool) tea.Cmd {
	e := s.focused()
	switch e.kind {
	case elemButton:
		return s.submit(ctx)
	case elemLink:
		s.switchMode()
		return nil
	}
	if !fromKey {
		return nil
	}
	es := s.elems()
	if s.focus+1 < len(es) && es[s.focus+1].kind == elemField {
		s.focus++
		return nil
	}
	return s.submit(ctx) // the last field
}

// switchMode toggles login/signup: the handle stays, passwords are cleared, the
// focus goes to the first field.
func (s *screen) switchMode() {
	if s.mode == modeLogin {
		s.mode = modeSignup
	} else {
		s.mode = modeLogin
	}
	s.fields[fieldPassword].clear()
	s.fields[fieldConfirm].clear()
	s.errMsg, s.notice = "", ""
	s.focus = 0
}

// submit validates the form and sends login/signup; the response is saved
// (auth.Save) on the request goroutine and comes back as a doneMsg.
func (s *screen) submit(ctx *ui.Context) tea.Cmd {
	if s.busy {
		return nil
	}
	if msg, id := validate(s.mode, s.fields); msg != "" {
		s.errMsg = msg
		s.focusField(id)
		return nil
	}
	if ctx == nil || ctx.API == nil {
		return nil
	}
	s.busy, s.errMsg, s.notice = true, "", ""
	s.gen++
	id, gen, mode := s.id, s.gen, s.mode
	client, paths := ctx.API, ctx.Paths
	now := ctx.Now
	if now == nil {
		now = time.Now
	}
	handle, password := s.fields[fieldHandle].value(), s.fields[fieldPassword].value()
	display := strings.TrimSpace(s.fields[fieldDisplay].value())
	device := deviceName(s.hostname)
	work := func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), submitTimeout)
		defer cancel()
		var res *api.AuthResult
		var err error
		if mode == modeSignup {
			res, err = client.Signup(c, api.SignupRequest{Handle: handle, Password: password, DisplayName: display, Client: clientName, DeviceName: device})
		} else {
			res, err = client.Login(c, api.LoginRequest{Handle: handle, Password: password, Client: clientName, DeviceName: device})
		}
		out := doneMsg{id: id, gen: gen, err: err}
		if err != nil || res == nil {
			return out
		}
		cr := &auth.Credentials{
			Version: 1, Server: client.BaseURL(), Token: res.Token, SessionID: res.Session.ID,
			UserID: res.User.ID, Handle: res.User.Handle, SavedAt: now(),
		}
		me := res.User
		out.creds, out.me = cr, &me
		out.saveErr = auth.Save(paths, cr)
		return out
	}
	return tea.Batch(s.spin.Start(ctx), work)
}

// done applies a submit result of this screen's current generation.
func (s *screen) done(_ *ui.Context, m doneMsg) tea.Cmd {
	if m.id != s.id || m.gen != s.gen || !s.busy {
		return nil
	}
	s.busy = false
	s.spin.Stop()
	if m.err != nil || m.creds == nil {
		s.errMsg = api.Describe(m.err)
		if s.errMsg == "" {
			s.errMsg = api.Describe(&api.Error{Code: "internal_error"})
		}
		switch api.CodeOf(m.err) {
		case "invalid_credentials":
			s.fields[fieldPassword].clear()
			s.fields[fieldConfirm].clear()
			s.focusField(fieldPassword)
		case "handle_taken":
			s.focusField(fieldHandle)
		}
		return nil
	}
	s.fields[fieldPassword].clear()
	s.fields[fieldConfirm].clear()
	msg := LoggedInMsg{Credentials: m.creds, Me: m.me}
	cmds := []tea.Cmd{func() tea.Msg { return msg }}
	if m.saveErr != nil {
		slog.Warn("save credentials", "err", m.saveErr)
		cmds = append(cmds, ui.Toast(saveFailedMsg, ui.ToastError))
	}
	return tea.Batch(cmds...)
}

// HandleMouse implements ui.MouseHandler: a left click focuses the element on that
// row and presses the button / link.
func (s *screen) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	c, ok := msg.(tea.MouseClickMsg)
	if !ok || c.Button != tea.MouseLeft || s.busy {
		return nil
	}
	i, ok := s.rowElem[local.Y]
	if !ok {
		return nil
	}
	s.focus = i
	return s.activate(ctx, false)
}

// Cursor implements ui.Cursorer: the real cursor in the focused field (last View).
func (s *screen) Cursor() *tea.Cursor {
	if s.cursor == nil {
		return nil
	}
	c := *s.cursor
	return &c
}

// ---- rendering ----

// Row priorities: when the rows do not fit, the lowest go first.
const (
	prioSpacer = iota
	prioSubtitle
	prioServer
	prioTitle
	prioMust
)

// row is one content row of the form, at most iw cells wide.
type row struct {
	text   string
	center bool
	prio   int
	elem   int // element index, -1 = none
	cursor int // cursor column inside the row, -1 = none
}

// View implements ui.Component: exactly size.H lines of size.W cells.
func (s *screen) View(ctx *ui.Context, focused bool) string {
	s.cursor, s.rowElem = nil, map[int]int{}
	w, h := s.size.W, s.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	t := theme.Get(theme.Default)
	if ctx != nil && ctx.Theme != nil {
		t = ctx.Theme
	}
	server := ""
	if ctx != nil && ctx.API != nil {
		server = ctx.API.BaseURL()
	}

	// Bordered card when wide and tall enough; else the bare form over the whole pane.
	bordered := w >= cardMinW
	cw := w
	if bordered {
		cw = min(cardMaxW, w)
	}
	pad := barePadX
	if bordered {
		pad = cardPadX
	} else if cw-2*pad < 1 {
		pad = 0
	}
	iw := max(1, cw-2*pad-borderCells(bordered))
	rows := s.rows(t, server, iw)
	must := 0
	for _, r := range rows {
		if r.prio == prioMust {
			must++
		}
	}
	if bordered && must+2 > h { // the border does not fit: drop it
		bordered, cw, pad = false, w, barePadX
		if cw-2*pad < 1 {
			pad = 0
		}
		iw = max(1, cw-2*pad)
		rows = s.rows(t, server, iw)
	}
	avail := h - borderCells(bordered)
	rows = fitRows(rows, avail)
	rows = s.window(rows, avail)

	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = padCells(t, pad) + fitRow(t, r, iw) + padCells(t, cw-borderCells(bordered)-pad-iw)
	}
	cardH := len(rows) + borderCells(bordered)
	var card []string
	if bordered {
		card = strings.Split(widgets.Box(t, t.S.PaneFocused, ui.Size{W: cw, H: cardH}, strings.Join(lines, "\n")), "\n")
	} else {
		card = lines
	}
	top := max(0, (h-cardH)/2)
	left := max(0, (w-cw)/2)
	inset := 0
	if bordered {
		inset = 1
	}
	for i, r := range rows {
		y := top + inset + i
		if r.elem >= 0 {
			s.rowElem[y] = r.elem
		}
		if focused && r.cursor >= 0 && !s.busy {
			s.cursor = tea.NewCursor(left+inset+pad+r.cursor, y)
		}
	}

	out := make([]string, h)
	blank := padCells(t, w)
	for y := range out {
		out[y] = blank
		if i := y - top; i >= 0 && i < len(card) {
			out[y] = padCells(t, left) + card[i] + padCells(t, w-left-cw)
		}
	}
	return strings.Join(out, "\n")
}

// borderCells is the cells a border takes on one axis (2 when bordered).
func borderCells(bordered bool) int {
	if bordered {
		return 2
	}
	return 0
}

// rows builds every content row for inner width iw.
func (s *screen) rows(t *theme.Theme, server string, iw int) []row {
	text := func(str string, center bool, prio int) row {
		return row{text: str, center: center, prio: prio, elem: -1, cursor: -1}
	}
	spacer := text("", false, prioSpacer)
	sub := "터미널 마실 — 로그인"
	if s.mode == modeSignup {
		sub = "터미널 마실 — 가입"
	}
	out := []row{
		text(t.S.Title.Render("masil"), true, prioTitle),
		text(t.S.Subtitle.Render(sub), true, prioSubtitle),
		spacer,
		text(t.S.Dim.Render(textwidth.Truncate("서버: "+textwidth.Sanitize(server), iw, "…")), false, prioServer),
		spacer,
	}
	es := s.elems()
	lw := 0
	for _, e := range es {
		if e.kind == elemField {
			lw = max(lw, textwidth.Width(s.fields[e.field].label))
		}
	}
	const marker = 2
	inline := iw-marker-lw-1 >= inlineFieldW
	var button, link row
	for i, e := range es {
		focused := i == s.focus
		switch e.kind {
		case elemField:
			f := s.fields[e.field]
			mark, label := t.S.Base.Render("  "), t.S.Base.Render(f.label)
			if focused {
				mark, label = t.S.Title.Render("› "), t.S.Title.Render(f.label)
			}
			if inline {
				aw := iw - marker - lw - 1
				in, col := fieldLine(t, f, focused, aw)
				r := row{text: mark + label + padCells(t, lw-textwidth.Width(f.label)+1) + in, prio: prioMust, elem: i, cursor: -1}
				if focused {
					r.cursor = marker + lw + 1 + col
				}
				out = append(out, r)
				continue
			}
			out = append(out, row{text: mark + label, prio: prioMust, elem: i, cursor: -1})
			in, col := fieldLine(t, f, focused, max(1, iw-marker))
			r := row{text: padCells(t, marker) + in, prio: prioMust, elem: i, cursor: -1}
			if focused {
				r.cursor = marker + col
			}
			out = append(out, r)
		case elemButton:
			label := "[ 로그인 ]"
			if s.mode == modeSignup {
				label = "[ 가입하기 ]"
			}
			st := t.S.Bold
			if focused {
				st = t.S.Pill
			}
			button = row{text: st.Render(label), center: true, prio: prioMust, elem: i, cursor: -1}
		case elemLink:
			q, a := "계정이 없나요? ", "가입하기"
			if s.mode == modeSignup {
				q, a = "이미 계정이 있나요? ", "로그인"
			}
			txt := t.S.Dim.Render(q) + t.S.Link.Render(a)
			if focused {
				txt = t.S.Selected.Render(q + a)
			}
			link = row{text: txt, center: true, prio: prioMust, elem: i, cursor: -1}
		}
	}
	out = append(out, spacer, button, link, spacer)
	out = append(out, s.messageRows(t, iw)...)
	return out
}

// messageRows is the notice / error / progress line (always at least one row, at
// most two, so the form does not jump).
func (s *screen) messageRows(t *theme.Theme, iw int) []row {
	var st lipgloss.Style
	var msg, prefix string
	switch {
	case s.busy:
		st, msg = t.S.Dim, "로그인하는 중…"
		if s.mode == modeSignup {
			msg = "가입하는 중…"
		}
		prefix = s.spin.View(t) + t.S.Base.Render(" ")
		iw -= 2
	case s.errMsg != "":
		st, msg = t.S.Error, s.errMsg
	case s.notice != "":
		st, msg = t.S.Warning, s.notice
	}
	if msg == "" {
		return []row{{prio: prioMust, elem: -1, cursor: -1}}
	}
	lines := textwidth.Wrap(textwidth.Sanitize(msg), max(1, iw))
	if len(lines) > 2 {
		lines = []string{lines[0], textwidth.Truncate(strings.Join(lines[1:], " "), max(1, iw), "…")}
	}
	out := make([]row, len(lines))
	for i, l := range lines {
		txt := st.Render(l)
		if i == 0 {
			txt = prefix + txt
		}
		out[i] = row{text: txt, prio: prioMust, elem: -1, cursor: -1}
	}
	return out
}

// fieldLine draws a field's input area in exactly w cells and returns its cursor
// column. Long values scroll so the cursor stays visible; an empty field shows its
// placeholder (dim).
func fieldLine(t *theme.Theme, f *field, focused bool, w int) (string, int) {
	st := t.S.StatusBar // chip background
	if focused {
		st = t.S.Selected
	}
	if w <= 0 {
		return "", 0
	}
	before, after := f.display()
	if before == "" && after == "" {
		ph := textwidth.Truncate(textwidth.Sanitize(f.placeholder), w, "…")
		return st.Foreground(t.P.Dim).Render(textwidth.PadRight(ph, w)), 0
	}
	if textwidth.Width(before) >= w {
		before = textwidth.TruncateLeft(before, w-1, "")
		after = ""
	}
	col := textwidth.Width(before)
	text := before + textwidth.Truncate(after, w-col, "")
	return st.Render(textwidth.PadRight(text, w)), col
}

// fitRows drops rows by priority (spacers first, then subtitle, server, title) until
// they fit in h; the must rows always stay (window() scrolls them).
func fitRows(rows []row, h int) []row {
	for p := prioSpacer; p < prioMust && len(rows) > h; p++ {
		kept := rows[:0:0]
		for _, r := range rows {
			if r.prio != p {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	return rows
}

// window keeps at most h rows around the focused element when they still do not fit.
func (s *screen) window(rows []row, h int) []row {
	if len(rows) <= h || h <= 0 {
		if h <= 0 {
			return nil
		}
		return rows
	}
	first, last := -1, -1
	for i, r := range rows {
		if r.elem == s.focus {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	start := 0
	if first >= 0 {
		start = min(max(0, last-h+1), first)
		start = max(0, min(start, len(rows)-h))
	}
	return rows[start : start+h]
}

// fitRow pads (or centers) a row to exactly w cells.
func fitRow(t *theme.Theme, r row, w int) string {
	txt := textwidth.Truncate(r.text, w, "")
	gap := w - textwidth.Width(txt)
	if r.text == "" {
		return padCells(t, w)
	}
	if r.center {
		l := gap / 2
		return padCells(t, l) + txt + padCells(t, gap-l)
	}
	return txt + padCells(t, gap)
}

// padCells is n Base styled blanks ("" for n <= 0).
func padCells(t *theme.Theme, n int) string {
	if n <= 0 {
		return ""
	}
	return t.S.Base.Render(strings.Repeat(" ", n))
}
