package apitool

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// The request editor (C3.6): the URL line ("POST  {{base_url}}/v1/payments
// [보내기 ⏎]"), the tab line "쿼리 · 헤더 · 본문 · 검사 · 추출 · 설정" and the
// shown tab. Tables are KVTable; checks and extracts are shown as rows
// ("status" / "eq 201", "token" / "json $.access_token").

type edTab int

const (
	tabQuery edTab = iota
	tabHeaders
	tabBody
	tabAsserts
	tabExtracts
	tabSettings
	edTabs
)

// editor is the request editor's state; req is the tool's edited request.
type editor struct {
	tab      edTab
	query    *KVTable
	headers  *KVTable
	asserts  *KVTable
	extracts *KVTable
	form     *KVTable
	body     *textArea
	url      *lineInput // the URL being edited (nil = not editing)
	onURL    bool       // the URL line has the focus (else the shown tab)
	setRow   int        // the selected settings row
	setEdit  *lineInput // a settings number being edited
	req      *apitool.Request
	files    map[string]bool // form row ids that are file fields
	lastW    int
	curX     int
	curY     int
	hasCur   bool
}

func newEditor() *editor {
	return &editor{
		tab: tabHeaders,
		// column titles are set from the catalog when drawn (titles)
		query:    NewKVTable(KVCols{}),
		headers:  NewKVTable(KVCols{}),
		asserts:  NewKVTable(KVCols{}),
		extracts: NewKVTable(KVCols{}),
		form:     NewKVTable(KVCols{}),
		body:     newTextArea(""),
	}
}

// load shows r (the tool's edited copy).
func (e *editor) load(r *apitool.Request) {
	e.req = r
	e.url = nil
	e.setEdit = nil
	e.onURL = true // ⏎ sends right away; j/↓ goes into the shown tab
	e.query.SetRows(r.Query)
	e.headers.SetRows(r.Headers)
	e.asserts.SetRows(assertRows(r.Asserts))
	e.extracts.SetRows(extractRows(r.Extracts))
	e.files = map[string]bool{}
	for i := range r.Body.Form {
		f := &r.Body.Form[i]
		if f.ID == "" {
			f.ID = newID() // only rows added in the table are id-less
		}
		if f.File != "" {
			e.files[f.ID] = true
		}
	}
	e.form.SetRows(formRows(r.Body.Form))
	e.body = newTextArea(r.Body.Text)
}

// table is the KVTable of the shown tab (nil for body text and settings).
func (e *editor) table() *KVTable {
	switch e.tab {
	case tabQuery:
		return e.query
	case tabHeaders:
		return e.headers
	case tabAsserts:
		return e.asserts
	case tabExtracts:
		return e.extracts
	case tabBody:
		if e.req != nil && (e.req.Body.Kind == apitool.BodyURLEncoded || e.req.Body.Kind == apitool.BodyMultipart) {
			return e.form
		}
	}
	return nil
}

// capturing reports whether a field takes every key.
func (e *editor) capturing() bool {
	if e.url != nil || e.setEdit != nil || e.body.editing {
		return true
	}
	if tb := e.table(); tb != nil && !e.onURL {
		return tb.CapturesInput()
	}
	return false
}

// store writes the tables back into the request.
func (e *editor) store() {
	r := e.req
	if r == nil {
		return
	}
	r.Query = e.query.Rows()
	r.Headers = e.headers.Rows()
	r.Asserts = parseAssertRows(e.asserts.Rows())
	r.Extracts = parseExtractRows(e.extracts.Rows())
	if r.Body.Kind == apitool.BodyJSON || r.Body.Kind == apitool.BodyRaw {
		r.Body.Text = e.body.text()
	}
	if r.Body.Kind == apitool.BodyURLEncoded || r.Body.Kind == apitool.BodyMultipart {
		r.Body.Form = parseFormRows(e.form.Rows(), r.Body.Form, e.files)
	}
}

// ---- view ----

// urlLine is "METHOD  url ... [⚠ 인증서 검사 끔] [보내기 ⏎]".
func (e *editor) urlLine(ctx *ui.Context, t *Tool, w int) string {
	a := ctx.T.Apitest
	if e.req == nil {
		return dim(ctx.Theme, textwidth.Truncate(a.HubStatusEmpty(), w, "…"))
	}
	right := a.SendButton()
	if t.running(jobSend) {
		right = a.Sending()
	}
	if e.req.Settings.Insecure {
		right = a.TlsOffWarning() + "  " + right
	}
	method := textwidth.PadRight(methodOf(e.req), 6)
	room := max(w-textwidth.Width(method)-textwidth.Width(right)-1, 1)
	var u string
	if e.url != nil {
		u = e.url.view(room)
		e.curX, e.curY, e.hasCur = textwidth.Width(method)+e.url.cursorX(room), 0, true
	} else {
		u = textwidth.Truncate(textwidth.Sanitize(e.req.URL), room, "…")
	}
	line := bold(ctx.Theme, method) + u
	if w < textwidth.Width(method)+textwidth.Width(right)+4 {
		// Too narrow for both: the warning stays whole (no_truncate), the
		// button goes first.
		if e.req.Settings.Insecure {
			return textwidth.Truncate(a.TlsOffWarning()+" "+methodOf(e.req)+" "+textwidth.Sanitize(e.req.URL), w, "…")
		}
		return textwidth.Truncate(line, w, "…")
	}
	return textwidth.PadRight(line, w-textwidth.Width(right)) + right
}

// tabLine is the editor tab line with the shown tab highlighted.
func (e *editor) tabLine(ctx *ui.Context, w int) string {
	a := ctx.T.Apitest
	r := e.req
	kind := a.BodyNone()
	if r.Body.Kind != "" && r.Body.Kind != apitool.BodyNone {
		kind = bodyKindLabel(r.Body.Kind)
	}
	labels := []string{
		a.EdQuery(len(r.Query)), a.EdHeaders(len(r.Headers)), a.EdBody(kind),
		a.EdAsserts(len(r.Asserts)), a.EdExtracts(len(r.Extracts)), a.EdSettings(),
	}
	var b strings.Builder
	for i, l := range labels {
		if i > 0 {
			b.WriteString(dim(ctx.Theme, " · "))
		}
		if edTab(i) == e.tab {
			l = selected(ctx.Theme, l)
		}
		b.WriteString(l)
	}
	return textwidth.Truncate(b.String(), w, "…")
}

// view draws the editor in w x h: the file_changed band (when the
// collection changed outside masil), the URL line, the tab line and the tab.
func (e *editor) view(ctx *ui.Context, t *Tool, w, h int, focused bool) []string {
	e.lastW, e.hasCur = w, false
	if h <= 0 {
		return nil
	}
	var band []string
	if t.fileChanged && h > 2 {
		text := textwidth.Truncate(ctx.T.Apitest.FileChanged()+" (R)", w, "…")
		band = []string{style(ctx.Theme, func(s theme.Styles) lipgloss.Style { return s.Warning }, text)}
	}
	out := e.rows(ctx, t, w, h-len(band), focused)
	if e.hasCur {
		e.curY += len(band)
	}
	return textwidth.Fit(append(band, out...), w, h)
}

// titles sets the tables' column titles in the shown language.
func (e *editor) titles(ctx *ui.Context) {
	a := ctx.T.Apitest
	e.query.SetTitles(a.ColKey(), a.ColValue())
	e.headers.SetTitles(a.ColHeader(), a.ColValue())
	e.asserts.SetTitles(a.ColTarget(), a.ColCheck())
	e.extracts.SetTitles(a.ColVar(), a.ColFrom())
	e.form.SetTitles(a.ColField(), a.ColValue())
}

// rows are the URL line, the tab line and the tab in w x h.
func (e *editor) rows(ctx *ui.Context, t *Tool, w, h int, focused bool) []string {
	out := []string{e.urlLine(ctx, t, w)}
	if e.req == nil || h == 1 {
		return textwidth.Fit(out, w, h)
	}
	out = append(out, e.tabLine(ctx, w))
	bodyH := max(h-2, 0)
	inTab := focused && !e.onURL && e.url == nil
	if tb := e.table(); tb != nil {
		e.titles(ctx)
		out = append(out, tb.View(ctx, w, bodyH, inTab)...)
		if c := tb.Cursor(); c != nil && inTab {
			e.curX, e.curY, e.hasCur = c.X, c.Y+2, true
		}
	} else if e.tab == tabBody {
		out = append(out, e.bodyView(ctx, w, bodyH, inTab)...)
	} else {
		out = append(out, e.settingsView(ctx, w, bodyH, inTab)...)
	}
	return textwidth.Fit(out, w, h)
}

// bodyView is the body kind line and the text.
func (e *editor) bodyView(ctx *ui.Context, w, h int, focused bool) []string {
	if h <= 0 {
		return nil
	}
	out := e.body.view(ctx, w, h, focused)
	if c := e.body.cursor(); c != nil && focused {
		e.curX, e.curY, e.hasCur = c.X, c.Y+2, true
	}
	return out
}

// settingsView lists the request settings (C3.7).
func (e *editor) settingsView(ctx *ui.Context, w, h int, focused bool) []string {
	rows := e.settingRows(ctx)
	var out []string
	for i, r := range rows {
		line := "  " + r
		if i == e.setRow && focused {
			line = selected(ctx.Theme, textwidth.PadRight("› "+r, w))
		}
		out = append(out, textwidth.Truncate(line, w, "…"))
	}
	return textwidth.Fit(out, w, h)
}

// cursor is the editor's text cursor (relative to its view).
func (e *editor) cursor() *tea.Cursor {
	if !e.hasCur {
		return nil
	}
	return tea.NewCursor(e.curX, e.curY)
}

// methodOf is the request's method (GET when empty), sanitized: it is drawn
// and may come from an imported or shared file (Prepare rejects what is not
// a valid token).
func methodOf(r *apitool.Request) string {
	m := strings.ToUpper(strings.TrimSpace(textwidth.Sanitize(r.Method)))
	if m == "" {
		return "GET"
	}
	return m
}

// bodyKindLabel is the shown body kind (not translated).
func bodyKindLabel(k apitool.BodyKind) string {
	switch k {
	case apitool.BodyJSON:
		return "JSON"
	case apitool.BodyURLEncoded:
		return "form"
	case apitool.BodyMultipart:
		return "multipart"
	case apitool.BodyRaw:
		return "raw"
	}
	return string(k)
}

// ---- checks / extracts / form as table rows ----

func assertRows(as []apitool.Assertion) []apitool.KV {
	out := make([]apitool.KV, 0, len(as))
	for _, a := range as {
		k := a.Target
		if a.Path != "" {
			k += " " + a.Path
		}
		v := a.Op
		if a.Value != "" {
			v += " " + a.Value
		}
		out = append(out, apitool.KV{ID: a.ID, Key: k, Value: v})
	}
	return out
}

func parseAssertRows(rows []apitool.KV) []apitool.Assertion {
	var out []apitool.Assertion
	for _, r := range rows {
		target, path := cut(r.Key)
		op, val := cut(r.Value)
		if target == "" {
			continue
		}
		out = append(out, apitool.Assertion{ID: r.ID, Target: target, Path: path, Op: op, Value: val})
	}
	return out
}

func extractRows(es []apitool.Extract) []apitool.KV {
	out := make([]apitool.KV, 0, len(es))
	for _, x := range es {
		out = append(out, apitool.KV{ID: x.ID, Key: x.Var, Value: strings.TrimSpace(x.From + " " + x.Path)})
	}
	return out
}

func parseExtractRows(rows []apitool.KV) []apitool.Extract {
	var out []apitool.Extract
	for _, r := range rows {
		from, path := cut(r.Value)
		if strings.TrimSpace(r.Key) == "" {
			continue
		}
		out = append(out, apitool.Extract{ID: r.ID, Var: strings.TrimSpace(r.Key), From: from, Path: path})
	}
	return out
}

func formRows(fs []apitool.FormField) []apitool.KV {
	out := make([]apitool.KV, 0, len(fs))
	for _, f := range fs {
		kv := f.KV
		if f.File != "" {
			kv.Value = "@" + f.File
		}
		out = append(out, kv)
	}
	return out
}

// parseFormRows maps the rows back. Whether a row is a file field is kept
// by row id (files), never read from the text: "@path" names a file only in
// a file row (its path may be edited) or in a row the user added in the
// table (no id yet). A loaded text value starting with "@" — an imported or
// shared collection's — stays text, so it can never upload a file of this
// computer.
func parseFormRows(rows []apitool.KV, old []apitool.FormField, files map[string]bool) []apitool.FormField {
	var out []apitool.FormField
	for _, r := range rows {
		f := apitool.FormField{KV: r}
		if i := slices.IndexFunc(old, func(o apitool.FormField) bool { return o.ID != "" && o.ID == r.ID }); i >= 0 {
			f.FileName = old[i].FileName
		}
		if p, ok := strings.CutPrefix(r.Value, "@"); ok && (r.ID == "" || files[r.ID]) {
			f.File, f.Value = p, ""
		}
		out = append(out, f)
	}
	return out
}

// cut splits "first rest" at the first space.
func cut(s string) (string, string) {
	s = strings.TrimSpace(s)
	a, b, _ := strings.Cut(s, " ")
	return a, strings.TrimSpace(b)
}

// ---- one-line field ----

// lineInput is a one-line text field (sanitized input, horizontal scroll).
type lineInput struct {
	buf []rune
	pos int
	off int
}

func newLineInput(s string) *lineInput {
	r := []rune(textwidth.Sanitize(s))
	return &lineInput{buf: r, pos: len(r)}
}

func (l *lineInput) text() string { return string(l.buf) }

func (l *lineInput) insert(s string) {
	r := []rune(s)
	l.buf = slices.Insert(l.buf, l.pos, r...)
	l.pos += len(r)
}

// paste inserts s on one line.
func (l *lineInput) paste(s string) {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	l.insert(textwidth.SanitizeInput(s))
}

// key edits the field; it reports whether the key was an edit key.
func (l *lineInput) key(m tea.KeyPressMsg) bool {
	switch m.String() {
	case "backspace":
		if l.pos > 0 {
			l.buf = slices.Delete(l.buf, l.pos-1, l.pos)
			l.pos--
		}
	case "delete":
		if l.pos < len(l.buf) {
			l.buf = slices.Delete(l.buf, l.pos, l.pos+1)
		}
	case "left":
		l.pos = max(l.pos-1, 0)
	case "right":
		l.pos = min(l.pos+1, len(l.buf))
	case "home", "ctrl+a":
		l.pos = 0
	case "end", "ctrl+e":
		l.pos = len(l.buf)
	case "ctrl+u":
		l.buf, l.pos = l.buf[l.pos:], 0
	default:
		if m.Text == "" || m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) != 0 {
			return false
		}
		l.insert(textwidth.SanitizeInput(m.Text))
	}
	return true
}

// view shows the field in w cells, scrolled so the cursor is visible.
func (l *lineInput) view(w int) string {
	if w <= 0 {
		return ""
	}
	if l.pos < l.off {
		l.off = l.pos
	}
	for l.off < l.pos && textwidth.Width(string(l.buf[l.off:l.pos])) >= w {
		l.off++
	}
	return textwidth.Truncate(string(l.buf[l.off:]), w, "")
}

func (l *lineInput) cursorX(w int) int {
	l.view(w)
	return min(textwidth.Width(string(l.buf[l.off:l.pos])), max(w-1, 0))
}

// ---- multi-line text (the body) ----

// textArea is a small multi-line field for the request body: enter starts
// editing, esc ends it; sanitized input keeps line breaks.
type textArea struct {
	lines   [][]rune
	row     int
	col     int
	top     int
	left    int
	editing bool
	h, w    int
}

func newTextArea(s string) *textArea {
	a := &textArea{}
	a.set(s)
	return a
}

func (a *textArea) set(s string) {
	a.lines = nil
	for _, l := range strings.Split(textwidth.SanitizeInput(s), "\n") {
		a.lines = append(a.lines, []rune(l))
	}
	a.row, a.col, a.top, a.left = 0, 0, 0, 0
}

func (a *textArea) text() string {
	parts := make([]string, len(a.lines))
	for i, l := range a.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

func (a *textArea) insert(s string) {
	for i, part := range strings.Split(s, "\n") {
		if i > 0 {
			rest := slices.Clone(a.lines[a.row][a.col:])
			a.lines[a.row] = a.lines[a.row][:a.col]
			a.row++
			a.lines = slices.Insert(a.lines, a.row, rest)
			a.col = 0
		}
		r := []rune(part)
		a.lines[a.row] = slices.Insert(a.lines[a.row], a.col, r...)
		a.col += len(r)
	}
}

// key edits while editing; reports a change.
func (a *textArea) key(m tea.KeyPressMsg) bool {
	switch m.String() {
	case "esc":
		a.editing = false
	case "enter":
		a.insert("\n")
		return true
	case "backspace":
		switch {
		case a.col > 0:
			a.lines[a.row] = slices.Delete(a.lines[a.row], a.col-1, a.col)
			a.col--
		case a.row > 0:
			a.col = len(a.lines[a.row-1])
			a.lines[a.row-1] = append(a.lines[a.row-1], a.lines[a.row]...)
			a.lines = slices.Delete(a.lines, a.row, a.row+1)
			a.row--
		default:
			return false
		}
		return true
	case "up":
		a.row = max(a.row-1, 0)
	case "down":
		a.row = min(a.row+1, len(a.lines)-1)
	case "left":
		a.col = max(a.col-1, 0)
	case "right":
		a.col = min(a.col+1, len(a.lines[a.row]))
	case "home", "ctrl+a":
		a.col = 0
	case "end", "ctrl+e":
		a.col = len(a.lines[a.row])
	default:
		if m.Text == "" || m.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) != 0 {
			return false
		}
		a.insert(textwidth.SanitizeInput(m.Text))
		return true
	}
	a.col = min(a.col, len(a.lines[a.row]))
	return false
}

// view draws the visible lines (cut, not wrapped: bodies are code).
func (a *textArea) view(ctx *ui.Context, w, h int, focused bool) []string {
	a.w, a.h = w, h
	if a.row < a.top {
		a.top = a.row
	}
	if h > 0 && a.row >= a.top+h {
		a.top = a.row - h + 1
	}
	if a.col < a.left {
		a.left = a.col
	}
	if w > 1 && a.col-a.left >= w-1 {
		a.left = a.col - w + 2
	}
	var out []string
	for i := a.top; i < len(a.lines) && len(out) < h; i++ {
		l := a.lines[i]
		from := min(a.left, len(l))
		out = append(out, textwidth.Truncate(textwidth.Sanitize(string(l[from:])), w, "…"))
	}
	if len(a.lines) == 1 && len(a.lines[0]) == 0 && !a.editing {
		out = []string{dim(ctx.Theme, textwidth.Truncate(ctx.T.Apitest.KvEmpty(), w, "…"))}
	}
	return textwidth.Fit(out, w, h)
}

func (a *textArea) cursor() *tea.Cursor {
	if !a.editing {
		return nil
	}
	from := min(a.left, a.col)
	x := textwidth.Width(textwidth.Sanitize(string(a.lines[a.row][from:a.col])))
	return tea.NewCursor(min(x, max(a.w-1, 0)), a.row-a.top)
}

// ---- keys ----

// methods is the method cycle of m on the URL line.
var methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// bodyKinds is the body kind cycle of b on the body tab.
var bodyKinds = []apitool.BodyKind{apitool.BodyNone, apitool.BodyJSON, apitool.BodyURLEncoded, apitool.BodyMultipart, apitool.BodyRaw}

// settings rows (C3.7).
const (
	setTimeout = iota
	setFollow
	setMax
	setTLS
	setRows
)

// handleKey is the request editor's keys. On the URL line: ⏎ sends, u edits
// the URL, m cycles the method, 1–6 pick a tab, j/↓ go into it. In a tab:
// the table keys (k/↑ on the first row goes back to the URL line), body
// ⏎ edits / b cycles the kind, settings space toggles / ⏎ edits a number.
func (e *editor) handleKey(ctx *ui.Context, t *Tool, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if e.req == nil {
		return false, nil
	}
	k := msg.String()
	switch {
	case e.url != nil:
		switch k {
		case "enter":
			e.req.URL = strings.TrimSpace(e.url.text())
			e.url = nil
		case "esc":
			e.url = nil
		default:
			e.url.key(msg)
		}
		return true, nil
	case e.setEdit != nil:
		switch k {
		case "enter":
			e.applySetting(e.setEdit.text())
			e.setEdit = nil
		case "esc":
			e.setEdit = nil
		default:
			if msg.Text == "" || (msg.Text >= "0" && msg.Text <= "9") {
				e.setEdit.key(msg)
			}
		}
		return true, nil
	case e.body.editing:
		e.body.key(msg)
		e.req.Body.Text = e.body.text()
		return true, nil
	}
	if tb := e.table(); tb != nil && !e.onURL && tb.CapturesInput() {
		_, changed, cmd := tb.HandleKey(ctx, msg)
		if changed {
			e.store()
		}
		return true, cmd
	}
	if len(k) == 1 && k >= "1" && k <= "6" {
		e.tab = edTab(k[0] - '1')
		e.setRow = 0
		return true, nil
	}
	if e.onURL {
		switch k {
		case "enter":
			return true, t.send(ctx)
		case "u":
			e.url = newLineInput(e.req.URL)
		case "m":
			i := slices.Index(methods, methodOf(e.req))
			e.req.Method = methods[(i+1)%len(methods)]
		case "j", "down":
			e.onURL = false
		case "h", "left":
			e.tab = (e.tab + edTabs - 1) % edTabs
		case "l", "right":
			e.tab = (e.tab + 1) % edTabs
		default:
			return false, nil
		}
		return true, nil
	}
	if e.tab == tabBody && k == "b" {
		i := slices.Index(bodyKinds, e.req.Body.Kind)
		e.store()
		e.req.Body.Kind = bodyKinds[(max(i, 0)+1)%len(bodyKinds)]
		e.form.SetRows(formRows(e.req.Body.Form))
		return true, nil
	}
	if tb := e.table(); tb != nil {
		if (k == "k" || k == "up") && tb.cur == 0 {
			e.onURL = true
			return true, nil
		}
		handled, changed, cmd := tb.HandleKey(ctx, msg)
		if changed {
			e.store()
		}
		return handled, cmd
	}
	if e.tab == tabBody {
		switch k {
		case "enter":
			if e.req.Body.Kind == "" || e.req.Body.Kind == apitool.BodyNone {
				e.req.Body.Kind = apitool.BodyJSON
			}
			e.body.editing = true
		case "k", "up":
			e.onURL = true
		default:
			return false, nil
		}
		return true, nil
	}
	switch k {
	case "j", "down":
		e.setRow = min(e.setRow+1, setRows-1)
	case "k", "up":
		if e.setRow == 0 {
			e.onURL = true
		}
		e.setRow = max(e.setRow-1, 0)
	case "space", "enter":
		s := &e.req.Settings
		switch e.setRow {
		case setFollow:
			s.NoFollow = !s.NoFollow
		case setTLS:
			s.Insecure = !s.Insecure
		case setTimeout:
			e.setEdit = newLineInput(strconv.Itoa(s.Effective().TimeoutMS))
		case setMax:
			e.setEdit = newLineInput(strconv.Itoa(s.Effective().MaxRedirects))
		}
	default:
		return false, nil
	}
	return true, nil
}

// applySetting stores the edited number of the selected settings row.
func (e *editor) applySetting(v string) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return
	}
	switch e.setRow {
	case setTimeout:
		e.req.Settings.TimeoutMS = n
	case setMax:
		e.req.Settings.MaxRedirects = n
	}
}

// settingRows are the settings tab's rows.
func (e *editor) settingRows(ctx *ui.Context) []string {
	if e.req == nil {
		return nil
	}
	a := ctx.T.Apitest
	s := e.req.Settings.Effective()
	state := func(on bool) string {
		if on {
			return a.StateOn()
		}
		return a.StateOff()
	}
	rows := []string{
		a.SetTimeout(s.TimeoutMS),
		a.SetFollow(state(!s.NoFollow)),
		a.SetMax(s.MaxRedirects),
		a.SetTls(state(!s.Insecure)),
	}
	if e.setEdit != nil {
		rows[e.setRow] = e.setEdit.text() + "▏"
	}
	return rows
}
