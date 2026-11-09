package apitool

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The layouts per mode (C3.6):
//
//	split (>= 100 columns): tree (26) │ request / response, the split line
//	   draggable (each side at least 5 rows)
//	tabs (44..99): [목록] [요청] [응답]; the response tab keeps the URL and
//	   status lines on top; below 76 columns the status line is short and
//	   the JSON tree indents by 1
//	tiny: three lines — request / short status / "⏎ 다시 보내기 · r 실행"
//	too small: nothing

// splitGeometry is where the split view puts things.
type splitGeometry struct {
	rightX, rightW int
	reqH           int // request rows; the split line is row reqH
	respY, respH   int
	treeH, histH   int
}

func (t *Tool) splitGeom() splitGeometry {
	w, h := t.size.W, t.size.H
	g := splitGeometry{rightX: treeW + 1, rightW: max(w-treeW-1, 0)}
	g.reqH = h * t.split / 100
	g.reqH = min(max(g.reqH, splitMin), max(h-splitMin-1, splitMin))
	g.respY = g.reqH + 1
	g.respH = max(h-g.respY, 0)
	g.histH = min(historyRows+1, h/3)
	g.treeH = max(h-g.histH, 0)
	return g
}

// View implements ui.Component: exactly size.H lines of size.W cells.
func (t *Tool) View(ctx *ui.Context, focused bool) string {
	t.cursor = nil
	w, h := t.size.W, t.size.H
	var lines []string
	switch t.mode(ctx) {
	case vmNone:
		return strings.Join(textwidth.Fit(nil, w, h), "\n")
	case vmTiny:
		lines = t.viewTiny(ctx)
	case vmTabs:
		lines = t.viewTabs(ctx, focused)
	default:
		lines = t.viewSplit(ctx, focused)
	}
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

// viewSplit draws tree │ request / response.
func (t *Tool) viewSplit(ctx *ui.Context, focused bool) []string {
	th := ctx.Theme
	g := t.splitGeom()
	left := append(t.tree.view(ctx, t, treeW, g.treeH, focused && t.focus == paneTree),
		t.historyView(ctx, treeW, g.histH)...)
	req := t.ed.view(ctx, t, g.rightW, g.reqH, focused && t.focus == paneRequest)
	resp := t.rv.view(ctx, t, g.rightW, g.respH, focused && t.focus == paneResponse, false)
	if c := t.ed.cursor(); c != nil && t.focus == paneRequest {
		t.cursor = tea.NewCursor(c.X+g.rightX, c.Y)
	}
	if c := t.rv.cursorAt(); c != nil && t.focus == paneResponse {
		t.cursor = tea.NewCursor(c.X+g.rightX, c.Y+g.respY)
	}
	sep := dim(th, "│")
	line := dim(th, strings.Repeat("─", g.rightW))
	out := make([]string, t.size.H)
	for y := range out {
		var right string
		switch {
		case y < g.reqH:
			right = req[y]
		case y == g.reqH:
			right = line
		default:
			right = resp[y-g.respY]
		}
		out[y] = left[y] + sep + right
	}
	return out
}

// viewTabs draws the tab bar and the shown tab.
func (t *Tool) viewTabs(ctx *ui.Context, focused bool) []string {
	w, h := t.size.W, t.size.H
	out := []string{tabBar(ctx, t.focus, w)}
	bodyH := max(h-1, 0)
	switch t.focus {
	case paneTree:
		histH := min(historyRows+1, bodyH/3)
		out = append(out, t.tree.view(ctx, t, w, bodyH-histH, focused)...)
		out = append(out, t.historyView(ctx, w, histH)...)
	case paneRequest:
		out = append(out, t.ed.view(ctx, t, w, bodyH, focused)...)
		if c := t.ed.cursor(); c != nil {
			t.cursor = tea.NewCursor(c.X, c.Y+1)
		}
	default:
		top := t.ed.urlLine(ctx, t, w)
		out = append(out, top)
		out = append(out, t.rv.view(ctx, t, w, max(bodyH-1, 0), focused, true)...)
		if c := t.rv.cursorAt(); c != nil {
			t.cursor = tea.NewCursor(c.X, c.Y+2)
		}
	}
	return out
}

// viewTiny is the three-line tiny view (C3.6).
func (t *Tool) viewTiny(ctx *ui.Context) []string {
	w := t.size.W
	a := ctx.T.Apitest
	first := ""
	if t.req != nil {
		first = t.req.Method + " " + shortPath(t.req.URL)
		if t.req.Settings.Insecure {
			first = "⚠ " + first
		}
	}
	second := t.tinyStatus(ctx)
	return []string{
		textwidth.Truncate(textwidth.Sanitize(first), w, "…"),
		textwidth.Truncate(second, w, "…"),
		dim(ctx.Theme, textwidth.Truncate(a.TinyHint(), w, "…")),
	}
}

// tabs are the tab labels of the tabbed view.
func tabLabels(ctx *ui.Context) []string {
	a := ctx.T.Apitest
	return []string{a.TabList(), a.TabRequest(), a.TabResponse()}
}

// tabBar is "[목록] [요청] [응답]" with the shown tab highlighted.
func tabBar(ctx *ui.Context, cur pane, w int) string {
	var b strings.Builder
	for i, l := range tabLabels(ctx) {
		if i > 0 {
			b.WriteString(" ")
		}
		s := "[" + l + "]"
		if pane(i) == cur {
			b.WriteString(style(ctx.Theme, func(s theme.Styles) lipgloss.Style { return s.TabActive }, s))
		} else {
			b.WriteString(dim(ctx.Theme, s))
		}
	}
	return textwidth.Truncate(b.String(), w, "…")
}

// tabHit is the tab under column x of the tab bar.
func tabHit(ctx *ui.Context, x int) (int, bool) {
	at := 0
	for i, l := range tabLabels(ctx) {
		cw := textwidth.Width("[" + l + "]")
		if x >= at && x < at+cw {
			return i, true
		}
		at += cw + 1
	}
	return 0, false
}

// shortPath is the URL template without its scheme and host or its leading
// {{variable}} ("{{base_url}}/v1/payments" -> "/v1/payments").
func shortPath(u string) string {
	s := strings.TrimSpace(u)
	if strings.HasPrefix(s, "{{") {
		if i := strings.Index(s, "}}"); i >= 0 {
			s = s[i+2:]
		}
	} else if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.IndexByte(s, '/'); j >= 0 {
			s = s[j:]
		} else {
			s = "/"
		}
	}
	if s == "" {
		s = "/"
	}
	return s
}

// style renders s with a theme style (no theme: as is).
func style(th *theme.Theme, pick func(theme.Styles) lipgloss.Style, s string) string {
	if th == nil || s == "" {
		return s
	}
	return pick(th.S).Render(s)
}

func dim(th *theme.Theme, s string) string {
	return style(th, func(s theme.Styles) lipgloss.Style { return s.Dim }, s)
}

func selected(th *theme.Theme, s string) string {
	return style(th, func(s theme.Styles) lipgloss.Style { return s.Selected }, s)
}

func bold(th *theme.Theme, s string) string {
	return style(th, func(s theme.Styles) lipgloss.Style { return s.Bold }, s)
}

// ---- dialogs ----

// dialogKind names what a dialog's answer is for.
type dialogKind string

// dialogMsg is a closed dialog's answer, broadcast to the tool instance inst:
// Choice is the chosen row (-1 = esc), Input the text field.
type dialogMsg struct {
	Inst   string
	Kind   dialogKind
	Data   string
	Input  string
	Choice int
}

// dialog is the tool's modal: a wrapped body that is never cut (no_truncate
// texts scroll instead), an optional one-line field and a list of choices.
type dialog struct {
	inst, title string
	kind        dialogKind
	data        string
	body        []string // paragraphs, wrapped at draw time
	input       *lineInput
	choices     []string
	sel         int
	top         int
	prefW       int
	size        ui.Size
	cursor      *tea.Cursor
}

var (
	_ ui.Overlay    = (*dialog)(nil)
	_ ui.KeyHandler = (*dialog)(nil)
	_ ui.Titled     = (*dialog)(nil)
	_ ui.Cursorer   = (*dialog)(nil)
)

func (d *dialog) Init(*ui.Context) tea.Cmd { return nil }

// Update takes pastes into the field.
func (d *dialog) Update(_ *ui.Context, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(tea.PasteMsg); ok && d.input != nil {
		d.input.paste(m.Content)
	}
	return nil
}

func (d *dialog) Resize(_ *ui.Context, s ui.Size) { d.size = s }
func (d *dialog) Modal() bool                     { return true }
func (d *dialog) Cursor() *tea.Cursor             { return d.cursor }

func (d *dialog) Title(*ui.Context) (string, ui.Badge) {
	return textwidth.Sanitize(d.title), ui.Badge{}
}

// Bounds is a centered box tall enough for the wrapped body.
func (d *dialog) Bounds(body layout.Rect) layout.Rect {
	w := min(max(d.prefW, 40), body.W)
	inner := max(w-4, 1)
	h := len(d.lines(inner)) + 2
	h = min(h, body.H)
	return layout.Rect{X: body.X + max(0, (body.W-w)/2), Y: body.Y + max(0, (body.H-h)/2), W: w, H: h}
}

// lines is the dialog's content wrapped to w (choices included).
func (d *dialog) lines(w int) []string {
	var out []string
	for i, p := range d.body {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, textwidth.Wrap(textwidth.Sanitize(p), w)...)
	}
	if d.input != nil {
		out = append(out, "", "")
	}
	if len(d.choices) > 0 {
		out = append(out, "")
		for i, c := range d.choices {
			mark := "  "
			if i == d.sel {
				mark = "› "
			}
			out = append(out, textwidth.Wrap(mark+textwidth.Sanitize(c), w)...)
		}
	}
	return out
}

func (d *dialog) answer(choice int) tea.Cmd {
	msg := dialogMsg{Inst: d.inst, Kind: d.kind, Data: d.data, Choice: choice}
	if d.input != nil {
		msg.Input = d.input.text()
	}
	return tea.Batch(func() tea.Msg { return ui.CloseOverlayMsg{} }, func() tea.Msg { return msg })
}

func (d *dialog) HandleKey(_ *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	switch k {
	case "esc":
		return true, d.answer(-1)
	case "enter":
		if len(d.choices) == 0 {
			return true, d.answer(0)
		}
		return true, d.answer(d.sel)
	case "up", "shift+tab":
		d.sel = max(d.sel-1, 0)
		return true, nil
	case "down", "tab":
		d.sel = min(d.sel+1, max(len(d.choices)-1, 0))
		return true, nil
	case "ctrl+c":
		return false, nil
	}
	if d.input != nil {
		d.input.key(msg)
		return true, nil
	}
	switch k {
	case "j":
		d.top++
	case "k":
		d.top = max(d.top-1, 0)
	}
	return true, nil
}

func (d *dialog) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	pad := 0
	if w >= 12 {
		pad = 1
	}
	cw := max(w-2*pad, 1)
	rows := d.lines(cw)
	inputAt := -1
	if d.input != nil {
		n := 0
		for i, p := range d.body {
			if i > 0 {
				n++
			}
			n += len(textwidth.Wrap(textwidth.Sanitize(p), cw))
		}
		inputAt = n + 1
		rows[inputAt] = d.input.view(cw)
	}
	maxTop := max(len(rows)-h, 0)
	d.top = min(d.top, maxTop)
	if inputAt >= 0 && inputAt-d.top >= h {
		d.top = min(inputAt-h+1, maxTop)
	}
	d.cursor = nil
	if inputAt >= 0 && inputAt >= d.top && inputAt-d.top < h {
		d.cursor = tea.NewCursor(pad+d.input.cursorX(cw), inputAt-d.top)
	}
	var out []string
	for _, r := range rows[d.top:] {
		out = append(out, strings.Repeat(" ", pad)+r)
	}
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}

// newHelpDialog is the tool's help (C3.13: the terms line comes first and is
// never cut).
func newHelpDialog(ctx *ui.Context) *dialog {
	a := ctx.T.Apitest
	return &dialog{
		title: a.HelpTitle(),
		body:  []string{a.HelpTerms(), a.HelpKeys1(), a.HelpKeys2(), a.HelpKeys3(), a.HelpKeys4(), a.HelpKeys5(), a.HelpClose()},
		prefW: 72,
	}
}
