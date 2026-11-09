package apitool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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

// The response (C3.7): the status line, the tabs 본문 · 헤더 · 검사 · 원문
// and the body — a folding JSON tree (built in the send job) or text drawn
// only where it is visible. Everything from the server is sanitized before
// it is drawn; HTML is text.

type respTab int

const (
	rtBody respTab = iota
	rtHeaders
	rtChecks
	rtRaw
	respTabs
)

// textPiece is how many bytes of a body line one text piece holds at most:
// a long line (minified JSON, 10MB on one line) is cut into pieces that are
// wrapped one by one, only while visible.
const textPiece = 1024

// respState is one request's last answer.
type respState struct {
	resp     *apitool.Response
	tree     *Tree
	checks   []apitool.AssertResult
	asserts  []apitool.Assertion // what checks were checked against
	pieces   []int               // start offsets of the body's text pieces
	err      error
	errText  string
	canceled bool
	method   string
	url      string
	// What a result card needs (P4 Task 20; set by a send, not by a run).
	req  *apitool.Request  // the sent request (its template URL)
	prep *apitool.Prepared // what went out (the values to mask)
	env  string            // the environment's name
	ref  apitool.CardRef
}

// passed is the number of passed checks.
func (s *respState) passed() int {
	n := 0
	for _, c := range s.checks {
		if c.Pass {
			n++
		}
	}
	return n
}

// respView shows the response of the edited request.
type respView struct {
	key    string
	states map[string]*respState
	order  []string // keys, oldest first (at most respKeep kept)
	folds  map[string]FoldState
	tab    respTab
	raw    bool // the body tab shows the text, not the tree
	top    int
	sel    int
	h, w   int
	cur    *tea.Cursor

	th    *theme.Theme
	find  *lineInput // the find field (/), nil when closed
	query string     // the last search
	hit   int        // text view: the piece of the last hit (-1 none)
	hitAt int        // text view: the byte offset of the last hit
}

// respKeep is how many requests keep their last response in memory.
const respKeep = 6

func newRespView() *respView {
	return &respView{states: map[string]*respState{}, folds: map[string]FoldState{}, hit: -1}
}

// show switches to the response of request key.
func (v *respView) show(key string) {
	if v.key != key {
		v.top, v.sel, v.hit = 0, 0, -1
	}
	v.key = key
}

func (v *respView) current() *respState { return v.states[v.key] }

// set stores st as request key's response (and shows it when key is shown).
func (v *respView) set(key string, st *respState) {
	if old := v.states[key]; old != nil && old != st {
		removeSpill(old)
	}
	v.states[key] = st
	v.order = slices.DeleteFunc(v.order, func(k string) bool { return k == key })
	v.order = append(v.order, key)
	for len(v.order) > respKeep {
		k := v.order[0]
		v.order = v.order[1:]
		removeSpill(v.states[k])
		delete(v.states, k)
	}
	if key == v.key {
		v.top, v.sel, v.hit = 0, 0, -1
	}
}

// release forgets every response (the tool closes).
func (v *respView) release() {
	for _, s := range v.states {
		removeSpill(s)
	}
	clear(v.states)
	v.order = nil
}

func removeSpill(s *respState) {
	if s != nil && s.resp != nil && s.resp.Spill != "" {
		_ = os.Remove(s.resp.Spill)
	}
}

func (v *respView) fold() FoldState {
	f := v.folds[v.key]
	if f == nil {
		f = FoldState{}
		v.folds[v.key] = f
	}
	return f
}

func (v *respView) cursorAt() *tea.Cursor { return v.cur }

// ---- status line ----

// humanSize is a byte count as "512B", "1.2KB", "3.4MB".
func humanSize(n int64) string {
	switch {
	case n < 1024:
		return strconv.FormatInt(n, 10) + "B"
	case n < 1<<20:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/1024)) + "KB"
	case n < 1<<30:
		return trimZero(fmt.Sprintf("%.1f", float64(n)/(1<<20))) + "MB"
	}
	return trimZero(fmt.Sprintf("%.1f", float64(n)/(1<<30))) + "GB"
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

func ms(d interface{ Milliseconds() int64 }) int { return int(d.Milliseconds()) }

// statusText is the status line in w cells: the long form when it fits and
// short is false, else the short one.
func statusText(ctx *ui.Context, s *respState, w int, short bool) string {
	a := ctx.T.Apitest
	r := s.resp
	mark := ""
	if len(s.checks) > 0 {
		mark = "✕"
		if s.passed() == len(s.checks) {
			mark = "✓"
		}
	}
	tm := r.Timing
	if !short {
		phases := a.Phases(ms(tm.DNS), ms(tm.Connect), ms(tm.TLS), ms(tm.FirstByte))
		size := humanSize(r.Size)
		if r.WireSize > 0 && r.WireSize != r.Size {
			size += "/" + humanSize(r.WireSize)
		}
		long := strings.TrimSpace(a.StatusLine(r.Status, textwidth.Sanitize(r.StatusText), ms(tm.Total), phases, size, s.passed(), len(s.checks), mark))
		if textwidth.Width(long) <= w {
			return statusStyle(ctx.Theme, r.Status, long)
		}
	}
	return statusStyle(ctx.Theme, r.Status, textwidth.Truncate(a.StatusLineShort(r.Status, "", ms(tm.Total), "", "", s.passed(), len(s.checks), mark), w, "…"))
}

// statusStyle colors a status line by its class (2xx ok, 3xx note, else error).
func statusStyle(th *theme.Theme, code int, s string) string {
	switch {
	case code >= 200 && code < 300:
		return style(th, func(s theme.Styles) lipgloss.Style { return s.Success }, s)
	case code >= 300 && code < 400:
		return style(th, func(s theme.Styles) lipgloss.Style { return s.Warning }, s)
	}
	return style(th, func(s theme.Styles) lipgloss.Style { return s.Error }, s)
}

// ---- view ----

// view draws the response pane: the status line, the tab line and the tab.
// withURL: the tabbed view's response tab (the URL line is drawn above).
func (v *respView) view(ctx *ui.Context, t *Tool, w, h int, focused, _ bool) []string {
	v.w, v.h, v.cur, v.th = w, h, nil, ctx.Theme
	if h <= 0 {
		return nil
	}
	a := ctx.T.Apitest
	s := v.current()
	switch {
	case t.running(jobSend):
		return textwidth.Fit([]string{dim(ctx.Theme, a.Sending())}, w, h)
	case s == nil:
		return textwidth.Fit([]string{dim(ctx.Theme, textwidth.Truncate(a.RespEmpty(), w, "…"))}, w, h)
	case s.resp == nil:
		return textwidth.Fit(textwidth.Wrap(textwidth.Sanitize(s.errText), w), w, h)
	}
	var findLine []string
	if v.find != nil && h > 3 {
		prompt := a.FindPrompt() + "/"
		pw := textwidth.Width(prompt)
		findLine = []string{prompt + v.find.view(max(w-pw, 1))}
		v.cur = tea.NewCursor(pw+v.find.cursorX(max(w-pw, 1)), h-1)
		h--
	}
	out := []string{statusText(ctx, s, w, w < narrowW)}
	if h == 1 {
		return textwidth.Fit(out, w, h)
	}
	out = append(out, v.tabLine(ctx, s, w))
	bodyH := max(h-2, 0)
	var body []string
	switch v.tab {
	case rtHeaders:
		body = v.headerLines(ctx, s, w, bodyH)
	case rtChecks:
		body = v.checkLines(ctx, s, w, bodyH)
	case rtRaw:
		body = v.rawLines(ctx, s, w, bodyH)
	default:
		body = v.bodyLines(ctx, s, w, bodyH, focused)
	}
	return append(textwidth.Fit(append(out, body...), w, h), findLine...)
}

// tabLine is "[본문]  헤더 12  검사 4/4  원문".
func (v *respView) tabLine(ctx *ui.Context, s *respState, w int) string {
	a := ctx.T.Apitest
	labels := []string{a.RespBody(), a.RespHeaders(len(s.resp.Header)), a.RespChecks(s.passed(), len(s.checks)), a.RespRaw()}
	var b strings.Builder
	for i, l := range labels {
		if i > 0 {
			b.WriteString("  ")
		}
		if respTab(i) == v.tab {
			b.WriteString(bold(ctx.Theme, "["+l+"]"))
		} else {
			b.WriteString(dim(ctx.Theme, l))
		}
	}
	return textwidth.Truncate(b.String(), w, "…")
}

// indent is the JSON tree indent: 1 on narrow screens (C3.6).
func (v *respView) indent() int {
	if v.w < narrowW {
		return 1
	}
	return 2
}

// bodyLines draws the body tab.
func (v *respView) bodyLines(ctx *ui.Context, s *respState, w, h int, focused bool) []string {
	a := ctx.T.Apitest
	r := s.resp
	var out []string
	if r.Truncated {
		out = append(out, dim(ctx.Theme, textwidth.Truncate(a.BigBody(), w, "…")))
	}
	if r.Binary {
		ct := textwidth.Sanitize(r.ContentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		out = append(out, textwidth.Wrap(a.BinaryBody(ct, humanSize(r.Size)), w)...)
		return textwidth.Fit(out, w, h)
	}
	rest := max(h-len(out), 0)
	if s.tree != nil && !v.raw {
		f := v.fold()
		n := s.tree.Count(f)
		v.sel = min(v.sel, max(n-1, 0))
		v.scrollTo(v.sel, rest)
		v.top = min(v.top, max(n-rest, 0))
		for i, row := range s.tree.Window(f, v.top, rest, v.indent()) {
			line := textwidth.Truncate(RowText(ctx.T, row), w, "…")
			if focused && v.top+i == v.sel {
				line = selected(ctx.Theme, textwidth.PadRight(line, w))
			}
			out = append(out, line)
		}
		return textwidth.Fit(out, w, h)
	}
	return textwidth.Fit(append(out, v.textLines(s, r.Body, w, rest)...), w, h)
}

// scrollTo keeps row sel inside the h visible rows.
func (v *respView) scrollTo(sel, h int) {
	if sel < v.top {
		v.top = sel
	}
	if h > 0 && sel >= v.top+h {
		v.top = sel - h + 1
	}
	v.top = max(v.top, 0)
}

// textLines draws body text from piece v.top: each piece is sanitized and
// wrapped only when it is visible.
func (v *respView) textLines(s *respState, body []byte, w, h int) []string {
	if s.pieces == nil {
		s.pieces = textPieces(body)
	}
	v.top = min(v.top, max(len(s.pieces)-1, 0))
	var out []string
	for i := v.top; i < len(s.pieces) && len(out) < h; i++ {
		end := len(body)
		if i+1 < len(s.pieces) {
			end = s.pieces[i+1]
		}
		piece := strings.TrimRight(string(body[s.pieces[i]:end]), "\r\n")
		for j, l := range textwidth.Wrap(textwidth.Sanitize(piece), w) {
			if len(out) >= h {
				break
			}
			if j == 0 && i == v.hit && s.resp != nil {
				l = selected(v.th, textwidth.PadRight(l, w))
			}
			out = append(out, l)
		}
	}
	return out
}

// textPieces indexes body into pieces: each line, and long lines cut every
// textPiece bytes at a rune boundary.
func textPieces(body []byte) []int {
	out := []int{0}
	start := 0
	for i := 0; i < len(body); {
		if body[i] == '\n' {
			i++
			if i < len(body) {
				out = append(out, i)
			}
			start = i
			continue
		}
		if i-start >= textPiece && body[i]&0xC0 != 0x80 {
			out = append(out, i)
			start = i
		}
		i++
	}
	return out
}

// headerLines lists the response headers ("Name: value"), sorted.
func (v *respView) headerLines(ctx *ui.Context, s *respState, w, h int) []string {
	names := make([]string, 0, len(s.resp.Header))
	for n := range s.resp.Header {
		names = append(names, n)
	}
	slices.Sort(names)
	var all []string
	for _, n := range names {
		for _, val := range s.resp.Header[n] {
			all = append(all, textwidth.Truncate(textwidth.Sanitize(n)+": "+textwidth.Sanitize(val), w, "…"))
		}
	}
	v.top = min(v.top, max(len(all)-h, 0))
	return textwidth.Fit(all[min(v.top, len(all)):], w, h)
}

// checkLines lists each check with ✓/✕, what came back and why it failed.
func (v *respView) checkLines(ctx *ui.Context, s *respState, w, h int) []string {
	req := s.asserts
	var all []string
	for i, c := range s.checks {
		mark := "✕"
		if c.Pass {
			mark = "✓"
		}
		desc := ""
		if i < len(req) {
			desc = strings.TrimSpace(req[i].Target + " " + req[i].Path + " " + req[i].Op + " " + req[i].Value)
		}
		line := mark + " " + desc
		if !c.Pass {
			if c.Got != "" {
				line += " ← " + c.Got
			}
			if c.Err != "" {
				line += " (" + c.Err + ")"
			}
		}
		all = append(all, textwidth.Truncate(textwidth.Sanitize(line), w, "…"))
	}
	v.top = min(v.top, max(len(all)-h, 0))
	return textwidth.Fit(all[min(v.top, len(all)):], w, h)
}

// rawLines is the status line, the headers and the body text as received.
func (v *respView) rawLines(ctx *ui.Context, s *respState, w, h int) []string {
	r := s.resp
	head := []string{textwidth.Sanitize(fmt.Sprintf("%s %d %s", r.Proto, r.Status, r.StatusText))}
	names := make([]string, 0, len(r.Header))
	for n := range r.Header {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		for _, val := range r.Header[n] {
			head = append(head, textwidth.Truncate(textwidth.Sanitize(n+": "+val), w, "…"))
		}
	}
	head = append(head, "")
	if v.top < len(head) {
		out := append(head[v.top:], v.textLines(&respState{pieces: s.pieces}, r.Body, w, h)...)
		return textwidth.Fit(out, w, h)
	}
	top := v.top
	v.top -= len(head)
	out := v.textLines(s, r.Body, w, h)
	v.top = top
	return textwidth.Fit(out, w, h)
}

// handleKey is the response pane's keys: 1–4 / t tabs, j/k move, ⏎ or
// space fold (⏎ elsewhere sends), h/l fold / unfold, / find (n/N), o text
// ↔ tree, y copy, w save to a file.
func (v *respView) handleKey(ctx *ui.Context, t *Tool, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if v.find != nil {
		switch k {
		case "enter":
			v.query = v.find.text()
			v.find = nil
			return true, v.runFind(ctx, 1)
		case "esc":
			v.find = nil
		default:
			v.find.key(msg)
		}
		return true, nil
	}
	s := v.current()
	switch k {
	case "1", "2", "3", "4":
		v.tab = respTab(k[0] - '1')
		v.top, v.sel = 0, 0
	case "t":
		v.tab = (v.tab + 1) % respTabs
		v.top, v.sel = 0, 0
	case "j", "down":
		v.move(+1)
	case "k", "up":
		v.move(-1)
	case "enter", "space":
		if row, ok := v.selRow(); ok && row.Foldable {
			v.setFold(row.Path, !row.Folded)
			return true, nil
		}
		if k == "enter" {
			return true, t.send(ctx)
		}
	case "h", "l":
		if row, ok := v.selRow(); ok && row.Foldable {
			v.setFold(row.Path, k == "h")
		}
	case "/":
		if s != nil && s.resp != nil && v.tab == rtBody && !s.resp.Binary {
			v.find = newLineInput("")
		}
	case "n", "N":
		d := 1
		if k == "N" {
			d = -1
		}
		return true, v.runFind(ctx, d)
	case "o":
		v.raw = !v.raw
		v.top, v.sel, v.hit = 0, 0, -1
	case "y":
		return true, v.copyBody(ctx)
	case "w":
		return true, t.openSaveBody(ctx)
	case "s":
		return true, t.openShareResult(ctx)
	default:
		return false, nil
	}
	return true, nil
}

// treeShown reports whether the body tab shows s as the JSON tree.
func (v *respView) treeShown(s *respState) bool {
	return s != nil && s.resp != nil && s.tree != nil && !v.raw && v.tab == rtBody
}

// selRow is the selected tree row (ok false outside the tree view).
func (v *respView) selRow() (Row, bool) {
	s := v.current()
	if !v.treeShown(s) {
		return Row{}, false
	}
	rows := s.tree.Window(v.fold(), v.sel, 1, 0)
	if len(rows) == 0 {
		return Row{}, false
	}
	return rows[0], true
}

// setFold folds (or unfolds) the container at path for this request.
func (v *respView) setFold(path string, on bool) {
	f := v.fold()
	if on {
		f[path] = true
	} else {
		delete(f, path)
	}
}

// runFind looks for the query from the selection (tree) or the top piece
// (text) in direction dir.
func (v *respView) runFind(ctx *ui.Context, dir int) tea.Cmd {
	s := v.current()
	if v.query == "" || s == nil || s.resp == nil || s.resp.Binary {
		return nil
	}
	none := ui.Toast(ctx.T.Apitest.FindNone(textwidth.Sanitize(v.query)), ui.ToastInfo)
	if v.treeShown(s) {
		row, ok := s.tree.Find(v.fold(), v.query, v.sel, dir)
		if !ok {
			return none
		}
		v.sel = row
		return nil
	}
	body := s.resp.Body
	if s.pieces == nil {
		s.pieces = textPieces(body)
	}
	q := bytes.ToLower([]byte(v.query))
	low := bytes.ToLower(body)
	at := -1
	cur := v.hitAt
	if v.hit < 0 {
		cur = s.pieces[min(max(v.top, 0), len(s.pieces)-1)] - 1
	}
	if dir > 0 {
		if i := bytes.Index(low[cur+1:], q); i >= 0 {
			at = cur + 1 + i
		} else {
			at = bytes.Index(low, q)
		}
	} else {
		if cur < 0 {
			cur = len(low)
		}
		if i := bytes.LastIndex(low[:min(cur, len(low))], q); i >= 0 {
			at = i
		} else {
			at = bytes.LastIndex(low, q)
		}
	}
	if at < 0 {
		return none
	}
	p, _ := slices.BinarySearch(s.pieces, at+1)
	v.top, v.hit, v.hitAt = max(p-1, 0), max(p-1, 0), at
	return nil
}

// copyBody copies the body: the pretty JSON in the tree view, else as
// received (off the UI goroutine: indenting 10MB takes a while).
func (v *respView) copyBody(ctx *ui.Context) tea.Cmd {
	s := v.current()
	if s == nil || s.resp == nil || s.resp.Binary {
		return nil
	}
	body, pretty, ok := s.resp.Body, v.treeShown(s), ctx.T.Apitest.BodyCopied()
	return func() tea.Msg {
		text := string(body)
		if pretty {
			var b bytes.Buffer
			if json.Indent(&b, body, "", "  ") == nil {
				text = b.String()
			}
		}
		return ui.Copy(ctx, text, ok)()
	}
}

// move moves the selection (tree) or scrolls (text) by d.
func (v *respView) move(d int) {
	s := v.current()
	if s != nil && s.tree != nil && !v.raw && v.tab == rtBody {
		v.sel = max(v.sel+d, 0)
		return
	}
	v.top = max(v.top+d, 0)
}

// newRespState checks resp against req's checks and indexes the body: the
// JSON tree (call it off the UI goroutine — the send job does) or nothing.
func newRespState(req *apitool.Request, resp *apitool.Response) *respState {
	s := &respState{resp: resp, method: methodOf(req), url: req.URL, asserts: slices.Clone(req.Asserts)}
	s.checks = apitool.Check(req.Asserts, resp)
	if looksJSON(resp) {
		if tr, err := BuildTree(resp.Body); err == nil {
			s.tree = tr
		}
	}
	s.pieces = textPieces(resp.Body)
	return s
}

// looksJSON: a JSON type, or a text body starting like JSON.
func looksJSON(r *apitool.Response) bool {
	if r.Binary || len(r.Body) == 0 {
		return false
	}
	if strings.Contains(strings.ToLower(r.ContentType), "json") {
		return true
	}
	b := strings.TrimLeft(string(r.Body[:min(len(r.Body), 64)]), " \t\r\n\ufeff")
	return strings.HasPrefix(b, "{") || strings.HasPrefix(b, "[")
}

// showResponse makes resp request key's response (built on the UI goroutine:
// tests and small bodies).
func (t *Tool) showResponse(key string, req *apitool.Request, resp *apitool.Response) {
	t.applyResponse(key, newRespState(req, resp))
}

// applyResponse stores st and the request's last result (✓/✕ in the tree).
func (t *Tool) applyResponse(key string, st *respState) {
	t.rv.set(key, st)
	if st.resp != nil {
		t.results[key] = st.passed() == len(st.checks) && st.resp.RedirectBlocked == nil
	}
}

// ---- save to a file ----

// quotedFilename is a quoted Content-Disposition filename taken as is (a
// backslash is a path separator to strip, not an escape).
var quotedFilename = regexp.MustCompile(`(?i)(?:^|;)\s*filename\s*=\s*"([^"]*)"`)

// saveName is the offered file name of a body: the Content-Disposition
// filename, else the URL's last path segment, cut at both / and \ and
// sanitized; "response.bin" when nothing usable is left.
func saveName(r *apitool.Response, rawURL string) string {
	name := ""
	if cd := r.Header.Get("Content-Disposition"); cd != "" {
		_, params, _ := mime.ParseMediaType(cd)
		if m := quotedFilename.FindStringSubmatch(cd); m != nil && !strings.Contains(strings.ToLower(cd), "filename*") {
			name = m[1]
		} else if params != nil {
			name = params["filename"]
		}
	}
	if name == "" {
		u := rawURL
		if p, err := url.Parse(rawURL); err == nil && p.Host != "" {
			u = p.Path
		} else if i := strings.IndexAny(u, "?#"); i >= 0 {
			u = u[:i]
		}
		name = u
	}
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	// Never a dotfile (".bashrc", ".profile"): the leading dots go.
	name = strings.TrimLeft(strings.TrimSpace(textwidth.Sanitize(name)), ".")
	if strings.TrimSpace(name) == "" {
		return "response.bin"
	}
	return name
}

// saveDir is where a body is saved by default: ~/Downloads, made on the
// first save when missing (never the bare home folder, where a server-named
// file could replace a dotfile).
func saveDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads")
}

// openSaveBody asks where to save the shown body.
func (t *Tool) openSaveBody(ctx *ui.Context) tea.Cmd {
	s := t.rv.current()
	if s == nil || s.resp == nil {
		return nil
	}
	a := ctx.T.Apitest
	def := filepath.Join(saveDir(), saveName(s.resp, s.url))
	return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkSaveBody, data: t.respKey(), title: a.SaveBodyTitle(), prefW: 72,
		body: []string{a.SaveBodyPath()}, input: newLineInput(def)})
}

// saveBody writes the body of request key to path (0600) in a job: a
// spilled body copies the whole spill file. An existing file is replaced
// only after the overwrite question (overwrite = it was answered).
func (t *Tool) saveBody(ctx *ui.Context, key, path string, overwrite bool) tea.Cmd {
	s := t.rv.states[key]
	if s == nil || s.resp == nil {
		return nil
	}
	body, spill, dl := s.resp.Body, s.resp.Spill, saveDir()
	_, cmd := t.startJob(ctx, jobFile, func(context.Context) func(*ui.Context) tea.Cmd {
		var err error
		if filepath.Clean(filepath.Dir(path)) == filepath.Clean(dl) {
			err = os.MkdirAll(dl, 0o700) // the default folder may not exist yet
		}
		if err == nil && spill != "" {
			err = copyPrivate(path, spill, overwrite)
		} else if err == nil {
			err = writePrivate(path, body, overwrite)
		}
		return func(ctx *ui.Context) tea.Cmd {
			if errors.Is(err, fs.ErrExist) && !overwrite {
				return t.askOverwrite(ctx, pendingWrite{kind: dkSaveBody, key: key, path: path})
			}
			a := ctx.T.Apitest
			msg := a.BodySaved(path)
			if err != nil {
				msg = a.BodySaveFailed(describeErr(ctx, err))
			}
			return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkReport, title: a.SaveBodyTitle(), prefW: 72, body: []string{msg, a.ReportClose()}})
		}
	})
	return cmd
}

// copyPrivate copies file src to path (0600, see fillPrivate).
func copyPrivate(path, src string, overwrite bool) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return fillPrivate(path, overwrite, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	})
}
