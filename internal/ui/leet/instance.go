package leet

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/leetcode/htmldoc"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/termpane"
)

// One leet instance: "leet:today" (today's problem) or "leet:<date>/<slug>"
// (a problem opened from a #daily card). It shows the first-run notice the
// first time, asks masil for my streaks (the hub remembers them), loads the
// problem and lays it out for the mode (A10.4): problem | editor side by
// side, sub-tabs in the widget mode, a few lines in the tiny mode.

// meTag is the ui.Do tag of GET /v1/leet/me.
const meTag = "leet.me"

// Widget sub-tabs.
const (
	tabProblem = iota
	tabCode
	tabResult
	tabCount
)

type instance struct {
	id     string
	target string // "" = today, else "<date>/<slug>"
	size   ui.Size
	zoomed bool

	phase phase
	err   error              // phaseFailed
	q     *leetcode.Question // nil while loading or failed
	date  string             // the problem's UTC date
	doc   *docView
	// loadDay is the clock's UTC day when today's problem was last asked
	// for: the first tick of a later day asks once more.
	loadDay string
	ticking bool // a countdown tick is armed

	scroll  int  // problem lines scrolled off the top
	tab     int  // widget sub-tab
	reading bool // tiny: the problem's paged view
	page    int  // tiny reading page

	ed      codeEditor // the editor slot (nil = empty)
	edFocus bool       // keys go to the editor
	// where the last draw put the editor (Cursor)
	edX, edY int
	edShown  bool

	runReqs, submitReqs int // run / submit requests taken (tests)

	judge     *judgeState // the last run or submit (nil = none yet)
	judgeSeq  int
	pending   judgeKind // the run / submit waiting for a login
	waitUntil time.Time // the pacer said wait until then (zero = no)

	accepted *acceptedMsg // the last Accepted submit (c leaves its card)
	statusOn bool         // this instance holds the auto "solving" status
}

var (
	_ ui.Component     = (*instance)(nil)
	_ ui.KeyHandler    = (*instance)(nil)
	_ ui.Titled        = (*instance)(nil)
	_ ui.KeyGrabber    = (*instance)(nil)
	_ ui.InputCapturer = (*instance)(nil)
	_ ui.Cursorer      = (*instance)(nil)
	_ ui.StatusTexter  = (*instance)(nil)
	_ ui.Closer        = (*instance)(nil)
	_ ui.ToolStatuser  = (*instance)(nil)
)

func newInstance(_ *ui.Context, r ui.Route) ui.Component {
	in := &instance{id: r.ID}
	if r.ID != todayInstance {
		in.target = r.Query[queryTarget]
		if in.target == "" {
			in.target = strings.TrimPrefix(r.ID, instancePrefix)
		}
	}
	return in
}

func (in *instance) Init(ctx *ui.Context) tea.Cmd {
	var cmds []tea.Cmd
	if !loadState(ctx).FirstRunDone {
		cmds = append(cmds, ui.OpenRegisteredOverlay(OverlayFirstRun, ""))
	}
	cmds = append(cmds, fetchMe(ctx), in.load(ctx), in.arm(ctx))
	return tea.Batch(cmds...)
}

// load asks for the instance's problem.
func (in *instance) load(ctx *ui.Context) tea.Cmd {
	in.phase, in.err = phaseLoading, nil
	if in.target == "" {
		in.loadDay = leetcode.UTCDate(nowFunc(ctx)())
	}
	return loadProblem(ctx, in.id, in.target)
}

// arm schedules the next countdown tick (one at a time).
func (in *instance) arm(ctx *ui.Context) tea.Cmd {
	if in.ticking {
		return nil
	}
	in.ticking = true
	return ui.After(ctx, nextTick(nowFunc(ctx)()), tickMsg{id: in.id})
}

// fetchMe asks masil for my streaks, recent solves and #daily spaces.
func fetchMe(ctx *ui.Context) tea.Cmd {
	return ui.Do(ctx, meTag, func(c context.Context, a *api.Client) (*api.LeetMe, error) {
		if a == nil {
			return nil, nil
		}
		return a.LeetMe(c)
	})
}

func (in *instance) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case runRequestedMsg:
		if m.id == in.id {
			return in.runRequested(ctx)
		}
		return nil
	case submitRequestedMsg:
		if m.id == in.id {
			return in.submitRequested(ctx)
		}
		return nil
	case credsLoadedMsg:
		if m.id == in.id {
			return in.credsLoaded(ctx, m)
		}
		return nil
	case judgeSentMsg:
		if m.id == in.id {
			return in.sent(ctx, m)
		}
		return nil
	case checkTickMsg:
		if m.id == in.id {
			return in.tick(ctx, m)
		}
		return nil
	case checkedMsg:
		if m.id == in.id {
			return in.checked(ctx, m)
		}
		return nil
	case loginDoneMsg:
		return in.loginDone(ctx)
	case acceptedMsg:
		if m.id != in.id {
			return nil
		}
		in.accepted = &m
		putAccepted(m)
		return tea.Batch(recordSolve(ctx, m), in.restoreStatus(ctx))
	case ui.ResultMsg[*api.LeetSolveResult]:
		if m.Tag != recordTag || in.accepted == nil {
			return nil
		}
		if m.Err != nil {
			return ui.Toast(ctx.T.Leetcode.RecordFailed(), ui.ToastError)
		}
		return fetchMe(ctx) // the hub's 풂 and 🔥
	case ui.ResultMsg[*api.UserStatus], ui.ResultMsg[struct{}]:
		statusResult(ctx, msg)
		return nil
	case termpane.ExitedMsg:
		switch m.InstanceID {
		case in.id + execSuffix: // E ended: the slot shows what it wrote
			if in.ed != nil {
				if err := in.ed.Reload(); err != nil {
					return ui.Toast(ctx.T.Leetcode.FileFailed(), ui.ToastError)
				}
			}
			return nil
		case in.id: // nvim in the pane ended
			if p, ok := in.ed.(*paneEditor); ok {
				cmd := p.Update(ctx, msg)
				in.ed, in.edFocus = nil, false
				return cmd
			}
			return nil
		}
	case langChangedMsg, firstRunDoneMsg:
		if in.ed != nil && in.canEdit() {
			if p, err := leetcode.SolutionPath(solutionDir(ctx), in.date, in.q.FrontendID, in.q.Slug, currentLang(ctx)); err != nil || p != in.ed.Path() {
				return in.dropEditor()
			}
			if g, ok := in.ed.(*goEditor); ok { // the same file (python3 / python): its keywords
				g.setLang(currentLang(ctx).Slug)
			}
		}
		return nil
	case ui.ResultMsg[*api.LeetMe]:
		if m.Tag == meTag && m.Err == nil {
			rememberMe(ctx, m.Value)
		}
	case ui.ResultMsg[*loaded]:
		if m.Tag == loadTag(in.id) && m.Value != nil {
			return in.loaded(ctx, m.Value)
		}
	case ui.ResultMsg[*api.LeetDailyReport]:
		if in.q != nil && m.Tag == reportTag(in.date) {
			reported(ctx, in.date, in.q.Slug, m)
		}
	case tickMsg:
		if m.id != in.id {
			return nil
		}
		in.ticking = false
		cmds := []tea.Cmd{in.arm(ctx)}
		if day := leetcode.UTCDate(nowFunc(ctx)()); in.target == "" && in.phase != phaseLoading && day > in.loadDay {
			cmds = append(cmds, in.load(ctx)) // the next problem: asked for once
		}
		return tea.Batch(cmds...)
	case ui.ToolSwitchedMsg:
		if m.Route.ID == in.id {
			in.zoomed = m.Zoomed
		}
	case imageLoadedMsg:
		if m.err == nil {
			in.doc.invalidate() // drawn on the next View (any instance with it)
		}
		return nil
	case openFailedMsg:
		if m.id == in.id {
			return ui.Copy(ctx, m.url, ctx.T.Leetcode.LinkCopied())
		}
	}
	if in.ed != nil {
		return in.ed.Update(ctx, msg)
	}
	return nil
}

// runRequested / submitRequested: r, ctrl+\ r, :Run (and s…).
func (in *instance) runRequested(ctx *ui.Context) tea.Cmd {
	in.runReqs++
	return in.startJudge(ctx, kindRun)
}

func (in *instance) submitRequested(ctx *ui.Context) tea.Cmd {
	in.submitReqs++
	return in.startJudge(ctx, kindSubmit)
}

// loaded takes a load's outcome.
func (in *instance) loaded(ctx *ui.Context, l *loaded) tea.Cmd {
	var cmd tea.Cmd // the old problem's editor, closed
	switch {
	case l.q != nil && (l.err == nil || errors.Is(l.err, leetcode.ErrPremiumOnly)):
		if in.q != nil && (in.date != l.date || in.q.Slug != l.q.Slug) {
			cmd = in.problemChanged()
		}
		in.q, in.date, in.err = l.q, l.date, nil
		in.scroll, in.page = 0, 0
		if l.q.IsPaidOnly || l.err != nil {
			in.phase, in.doc = phasePremium, nil
		} else {
			blocks := htmldoc.Convert(l.q.ContentHTML)
			in.phase, in.doc = phaseReady, newDocView(blocks)
			cmd = tea.Batch(cmd, requestImages(ctx, blocks))
		}
	default:
		in.phase, in.err = phaseFailed, l.err
		in.q, in.doc = nil, nil
		return nil
	}
	status := in.startStatus(ctx)
	cmd = tea.Batch(cmd, in.ensureFile(ctx))
	if in.target != "" {
		return tea.Batch(cmd, status)
	}
	rememberDay(ctx, in.date, in.q, true)
	return tea.Batch(cmd, report(ctx, in.date, in.q), status)
}

// ensureFile makes the solution file as the problem opens (Task 20), with
// LeetCode's starting code, so an outside editor can open it before e; a
// file already there is left alone. A failure is told when e, E, r or s
// need the file.
func (in *instance) ensureFile(ctx *ui.Context) tea.Cmd {
	if !in.canEdit() {
		return nil
	}
	path, err := leetcode.SolutionPath(solutionDir(ctx), in.date, in.q.FrontendID, in.q.Slug, currentLang(ctx))
	if err != nil {
		return nil
	}
	snippet := in.q.Snippets[currentLang(ctx).Slug]
	return func() tea.Msg {
		_, _ = leetcode.EnsureSolutionFile(path, snippet)
		return nil
	}
}

// problemChanged forgets what belonged to the problem shown so far (today's
// problem rolled over at UTC midnight): its editor (r / s would send the new
// problem's file while the old one is on screen), its finished result and
// its Accepted card. A run or submit still under way keeps its own problem.
func (in *instance) problemChanged() tea.Cmd {
	if !in.judge.busy() {
		in.judge = nil
	}
	in.accepted, in.pending, in.waitUntil = nil, kindNone, time.Time{}
	if in.ed == nil {
		return nil
	}
	return in.dropEditor()
}

func (in *instance) Resize(ctx *ui.Context, size ui.Size) {
	in.size = size
	in.layoutEditor(ctx)
}

// GrabsKeys: nvim in the pane has the keys (ctrl+c and tab included).
func (in *instance) GrabsKeys() bool {
	p, ok := in.ed.(*paneEditor)
	return ok && in.edFocus && p.GrabsKeys()
}

// Completing implements ui.Completer: tab accepts the built-in editor's
// open completion menu instead of moving the focus.
func (in *instance) Completing() bool {
	g, ok := in.ed.(*goEditor)
	return ok && in.edFocus && g.m.CompletionMenuVisible()
}

// CompletionKeys implements ui.CodeCompleter: ctrl+n / ctrl+p are the
// built-in editor's while it types with completion on.
func (in *instance) CompletionKeys() bool {
	g, ok := in.ed.(*goEditor)
	return ok && in.edFocus && g.completionKeys()
}

// CapturesInput: the editor has the printable keys.
func (in *instance) CapturesInput() bool {
	return in.ed != nil && in.edFocus && in.ed.CapturesInput()
}

// Cursor is the editor's cursor, moved to its place in the instance.
func (in *instance) Cursor() *tea.Cursor {
	if in.ed == nil || !in.edFocus {
		return nil
	}
	c := in.ed.Cursor()
	if c == nil {
		return nil
	}
	if !in.edShown {
		return nil
	}
	cc := *c
	cc.X += in.edX
	cc.Y += in.edY
	return &cc
}

// StatusText is the pane's "typing into nvim" line while it has the keys.
func (in *instance) StatusText(ctx *ui.Context) (string, bool) {
	if p, ok := in.ed.(*paneEditor); ok && in.edFocus {
		return p.StatusText(ctx)
	}
	return "", false
}

// CloseTool ends nvim in the pane.
// CloseTool ends nvim in the pane and puts my status back.
func (in *instance) CloseTool(ctx *ui.Context) tea.Cmd {
	return tea.Batch(in.dropEditor(), in.restoreStatus(ctx))
}

// Title is the tool and the problem's number ("오늘의 문제 · 1606") once the
// problem is known, so two open problems are told apart (A8.2, A8.3).
func (in *instance) Title(ctx *ui.Context) (string, ui.Badge) {
	if in.q == nil {
		return ctx.T.Leetcode.Name(), ui.Badge{}
	}
	return textwidth.Sanitize(ctx.T.Leetcode.InstanceTitle(in.q.FrontendID)), ui.Badge{}
}

// ToolStatus is the hub's open-list status: a run or submit under way, else
// the editor the solution is open in ("" = none yet).
func (in *instance) ToolStatus(ctx *ui.Context) string {
	t := ctx.T.Leetcode
	switch {
	case in.judge.busy() && in.judge.kind == kindSubmit:
		return t.Submitting()
	case in.judge.busy():
		return t.Running()
	}
	switch in.ed.(type) {
	case *paneEditor:
		return t.StatusPane()
	case *goEditor:
		return t.StatusGoeditor()
	}
	return ""
}

func (in *instance) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if in.edActive(ctx) {
		handled, cmd := in.ed.HandleKey(ctx, msg)
		if g, ok := in.ed.(*goEditor); ok && g.takeLeave() {
			in.edFocus = false
			return true, cmd
		}
		if handled {
			return true, cmd
		}
		if _, ok := in.ed.(*goEditor); ok {
			return false, cmd // ctrl+c: the app's
		}
		// nvim left with ctrl+\ esc: the tool's keys
	}
	sp := splitFor(ctx.Mode, in.zoomed, in.size.W)
	if sp.Tiny && in.reading {
		switch k {
		case "esc", "q", "enter":
			in.reading = false
		case "down", "j", "pgdown", "space":
			in.page++
		case "up", "k", "pgup":
			in.page = max(0, in.page-1)
		case "o":
			return true, in.openBrowser()
		default:
			return false, nil
		}
		return true, nil
	}
	if sp.Tabbed {
		switch k {
		case "right":
			in.tab = (in.tab + 1) % tabCount
			return true, nil
		case "left":
			in.tab = (in.tab + tabCount - 1) % tabCount
			return true, nil
		case "1", "2", "3":
			in.tab = int(k[0] - '1')
			return true, nil
		}
	}
	switch k {
	case "o":
		return true, in.openBrowser()
	case "e":
		return true, in.openEditor(ctx)
	case "E":
		return true, in.execEdit(ctx)
	case "r":
		return true, emit(runRequestedMsg{id: in.id})
	case "s":
		return true, emit(submitRequestedMsg{id: in.id})
	case "l":
		return true, in.cycleLang(ctx)
	case "c":
		if in.accepted != nil {
			return true, ui.OpenRegisteredOverlay(OverlayShare, in.id)
		}
		return true, nil
	case "v":
		if in.judge != nil {
			in.judge.toggled = !in.judge.toggled
		}
		return true, nil
	case "enter":
		if sp.Tiny {
			in.reading, in.page = true, 0
			return true, nil
		}
	case "down", "j":
		in.scroll++
		return true, nil
	case "up", "k":
		in.scroll = max(0, in.scroll-1)
		return true, nil
	case "pgdown", "space":
		in.scroll += max(1, in.size.H-3)
		return true, nil
	case "pgup":
		in.scroll = max(0, in.scroll-max(1, in.size.H-3))
		return true, nil
	case "home", "g":
		in.scroll = 0
		return true, nil
	}
	return false, nil
}

// openBrowser opens the problem's page (also from a failed or premium view).
func (in *instance) openBrowser() tea.Cmd {
	slug := ""
	switch {
	case in.q != nil:
		slug = in.q.Slug
	case in.target != "":
		_, slug, _ = splitTarget(in.target)
	}
	return openInBrowser(in.id, slug)
}

// ---- drawing ----

func (in *instance) View(ctx *ui.Context, _ bool) string {
	w, h := in.size.W, in.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	in.edShown = false
	th := themeOf(ctx)
	sp := splitFor(ctx.Mode, in.zoomed, w)
	var rows []string
	switch {
	case sp.Tiny:
		rows = in.viewTiny(ctx, th, w, h)
	case sp.Tabbed:
		rows = in.viewTabbed(ctx, th, w, h)
	default:
		rows = in.viewSplit(ctx, th, sp, w, h)
	}
	out := make([]string, h)
	for i := range out {
		l := ""
		if i < len(rows) {
			l = rows[i]
		}
		out[i] = fitStyled(th, l, w)
	}
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}

// header is the full head line: id, difficulty, date and the countdown.
func (in *instance) header(ctx *ui.Context, th *theme.Theme) string {
	if in.q == nil {
		return th.S.Title.Render(hubIcon + " " + textwidth.Sanitize(ctx.T.Leetcode.Name()))
	}
	return th.S.Title.Render(textwidth.Sanitize(ctx.T.Leetcode.Header(in.q.FrontendID, in.q.Difficulty, in.date, countdown(nowFunc(ctx)()))))
}

// problemLines is the left column / problem tab at width w: the title, the
// tags, then the body (or the state line).
func (in *instance) problemLines(ctx *ui.Context, th *theme.Theme, w int) []string {
	if w <= 0 {
		return nil
	}
	t := ctx.T
	var out []string
	switch in.phase {
	case phaseLoading:
		return []string{th.S.Dim.Render(textwidth.Truncate(t.Leetcode.Loading(), w, "…"))}
	case phaseFailed:
		for _, l := range textwidth.Wrap(loadErrorText(ctx, in.err), w) {
			out = append(out, th.S.Warning.Render(l))
		}
		return out
	}
	for _, l := range textwidth.Wrap(textwidth.Sanitize(in.q.Title), w) {
		out = append(out, th.S.Bold.Render(l))
	}
	if len(in.q.Tags) > 0 {
		tags := make([]string, len(in.q.Tags))
		for i, tg := range in.q.Tags {
			tags[i] = textwidth.Sanitize(tg)
		}
		out = append(out, th.S.Dim.Render(textwidth.Truncate(textwidth.Sanitize(t.Leetcode.Tags(strings.Join(tags, " · "))), w, "…")))
	}
	out = append(out, "")
	if in.phase == phasePremium {
		for _, l := range textwidth.Wrap(t.Leetcode.PremiumLocked(), w) {
			out = append(out, th.S.Warning.Render(l))
		}
		return out
	}
	return append(out, in.doc.Lines(th, w, textwidth.Sanitize(t.Leetcode.ImageLink()))...)
}

// window is lines[scroll : scroll+h] with the scroll clamped.
func (in *instance) window(lines []string, h int) []string {
	if h <= 0 {
		return nil
	}
	in.scroll = min(in.scroll, max(0, len(lines)-h))
	in.scroll = max(in.scroll, 0)
	return lines[in.scroll:min(len(lines), in.scroll+h)]
}

// editorLines is the editor slot: the editor, or how to open one.
func (in *instance) editorLines(ctx *ui.Context, th *theme.Theme, w, h int) []string {
	if w <= 0 || h <= 0 {
		return nil
	}
	if in.ed != nil {
		x, y, _, _, ok := in.edRect(ctx)
		in.edX, in.edY, in.edShown = x, y, ok
		return strings.Split(in.ed.View(ctx, in.edFocus), "\n")
	}
	return []string{th.S.Dim.Render(textwidth.Truncate(ctx.T.Leetcode.CodePlaceholder(), w, "…"))}
}

// hints is the bottom key line: the editor's while it has the keys.
func (in *instance) hints(ctx *ui.Context, th *theme.Theme, wide string, w int) string {
	t := ctx.T
	if in.ed != nil && in.edFocus {
		switch e := in.ed.(type) {
		case *goEditor:
			wide = t.Leetcode.RunUsesSavedGoeditor()
		case *paneEditor:
			if e.GrabsKeys() {
				wide = t.Leetcode.RunUsesSaved()
			}
		}
	}
	return th.S.Dim.Render(textwidth.Truncate(wide, w, "…"))
}

// resultLines is the result slot: the last run or submit, or what to press.
func (in *instance) resultLines(ctx *ui.Context, th *theme.Theme, w, h int) []string {
	if w <= 0 || h <= 0 {
		return nil
	}
	if b := in.resultBlock(ctx, th, w, false); len(b) > 0 {
		return b
	}
	return []string{th.S.Dim.Render(textwidth.Truncate(ctx.T.Leetcode.ResultPlaceholder(), w, "…"))}
}

// viewSplit: the head line, problem | editor, the key hints.
func (in *instance) viewSplit(ctx *ui.Context, th *theme.Theme, sp split, w, h int) []string {
	rows := []string{in.header(ctx, th)}
	if h < 3 {
		return rows
	}
	foot := in.foot(ctx, th, ctx.T.Leetcode.HintsWide(), w)
	foot = foot[:min(len(foot), h-2)]
	bodyH := h - 1 - len(foot)
	if in.phase == phaseLoading || in.phase == phaseFailed { // no problem: the line gets the width
		for _, l := range in.window(in.problemLines(ctx, th, w), bodyH) {
			rows = append(rows, l)
		}
		for len(rows) < 1+bodyH {
			rows = append(rows, "")
		}
		return append(rows, foot...)
	}
	// the result sits under the problem: "─ 결과 ─" and up to half the height
	var res []string
	if b := in.resultBlock(ctx, th, sp.Left, true); len(b) > 0 && bodyH >= 4 {
		res = append([]string{th.S.Dim.Render(textwidth.Truncate("─ "+textwidth.Sanitize(ctx.T.Leetcode.TabResult())+" "+strings.Repeat("─", sp.Left), sp.Left, ""))}, b...)
		res = res[:min(len(res), max(2, bodyH/2))]
	}
	left := in.window(in.problemLines(ctx, th, sp.Left), bodyH-len(res))
	for len(left) < bodyH-len(res) {
		left = append(left, "")
	}
	left = append(left, res...)
	right := in.editorLines(ctx, th, sp.Right, bodyH)
	rule := th.S.Dim.Render("│")
	for i := range bodyH {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		rows = append(rows, fitStyled(th, l, sp.Left)+rule+fitStyled(th, r, sp.Right))
	}
	return append(rows, foot...)
}

// viewTabbed: the short head, the sub-tabs, the tab's body, the key hints.
func (in *instance) viewTabbed(ctx *ui.Context, th *theme.Theme, w, h int) []string {
	t := ctx.T
	head := th.S.Title.Render(hubIcon + " " + textwidth.Sanitize(t.Leetcode.Name()))
	if in.q != nil {
		head = th.S.Title.Render(textwidth.Sanitize(t.Leetcode.HeaderWidget(in.q.FrontendID, in.q.Difficulty)))
	}
	rows := []string{head}
	if h < 2 {
		return rows
	}
	var tabs strings.Builder
	for i, name := range []string{t.Leetcode.TabProblem(), t.Leetcode.TabCode(), t.Leetcode.TabResult()} {
		name = textwidth.Sanitize(name)
		if i == in.tab {
			tabs.WriteString(th.S.TabActive.Render("[" + name + "]"))
		} else {
			tabs.WriteString(th.S.Tab.Render(" " + name + " "))
		}
	}
	rows = append(rows, tabs.String())
	foot := in.foot(ctx, th, t.Leetcode.HintsWidget(), w)
	foot = foot[:min(len(foot), max(1, h-3))]
	bodyH := h - 2 - len(foot)
	if bodyH < 1 {
		return rows
	}
	var body []string
	switch in.tab {
	case tabProblem:
		body = in.window(in.problemLines(ctx, th, w), bodyH)
	case tabCode:
		body = in.editorLines(ctx, th, w, bodyH)
	default:
		body = in.resultLines(ctx, th, w, bodyH)
	}
	for i := range bodyH {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		rows = append(rows, l)
	}
	return append(rows, foot...)
}

// viewTiny: the status line and the keys, or the paged problem.
func (in *instance) viewTiny(ctx *ui.Context, th *theme.Theme, w, h int) []string {
	t := ctx.T
	if in.ed != nil && in.edFocus && h > 1 { // the built-in editor gets the screen
		rows := in.editorLines(ctx, th, w, h-1)
		for len(rows) < h-1 {
			rows = append(rows, "")
		}
		return append(rows[:h-1], in.hints(ctx, th, t.Leetcode.TinyActions(), w))
	}
	if in.reading {
		lines := in.problemLines(ctx, th, w)
		per := max(1, h-1)
		pages := max(1, (len(lines)+per-1)/per)
		in.page = min(in.page, pages-1)
		rows := slicePage(lines, in.page*per, per)
		for len(rows) < per {
			rows = append(rows, "")
		}
		return append(rows, th.S.Dim.Render(textwidth.Truncate(t.Leetcode.ReadingFooter(in.page+1, pages), w, "…")))
	}
	var rows []string
	switch in.phase {
	case phaseLoading, phaseFailed:
		rows = append(rows, in.problemLines(ctx, th, w)...)
	default:
		me := knownMe(ctx)
		solved, streak := t.Leetcode.Unsolved(), 0
		if me != nil {
			streak = me.Current
			if solvedOn(me, in.date) {
				solved = t.Leetcode.Solved()
			}
		}
		rows = append(rows, th.S.Title.Render(textwidth.Truncate(textwidth.Sanitize(t.Leetcode.TinyStatus(in.q.FrontendID, in.q.Difficulty, solved, streak)), w, "…")))
		if in.phase == phasePremium {
			rows = append(rows, th.S.Warning.Render(textwidth.Truncate(t.Leetcode.PremiumLocked(), w, "…")))
		}
	}
	for _, s := range []string{t.Leetcode.TinyRead(), t.Leetcode.TinyEdit(), t.Leetcode.TinyActions()} {
		rows = append(rows, th.S.Dim.Render(textwidth.Truncate(s, w, "…")))
	}
	rows = append(rows, in.resultBlock(ctx, th, w, false)...)
	return rows
}

// slicePage is lines[from : from+n] (clamped).
func slicePage(lines []string, from, n int) []string {
	if from >= len(lines) {
		return nil
	}
	return append([]string(nil), lines[from:min(len(lines), from+n)]...)
}
