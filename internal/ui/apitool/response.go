package apitool

import (
	"fmt"
	"os"
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
}

// respKeep is how many requests keep their last response in memory.
const respKeep = 6

func newRespView() *respView {
	return &respView{states: map[string]*respState{}, folds: map[string]FoldState{}}
}

// show switches to the response of request key.
func (v *respView) show(key string) {
	if v.key != key {
		v.top, v.sel = 0, 0
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
		v.top, v.sel = 0, 0
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
	v.w, v.h, v.cur = w, h, nil
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
	return textwidth.Fit(append(out, body...), w, h)
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
		for _, l := range textwidth.Wrap(textwidth.Sanitize(piece), w) {
			if len(out) >= h {
				break
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

// handleKey is the response pane's keys (tabs, scrolling).
func (v *respView) handleKey(ctx *ui.Context, t *Tool, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch k := msg.String(); k {
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
	case "enter":
		return true, t.send(ctx)
	default:
		return false, nil
	}
	return true, nil
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
