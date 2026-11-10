package apitool

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The collection runner (C3.7, flow ④): r opens the setup (environment,
// iterations, stop on failure, delay), the run goes in a job (one request at
// a time, from a snapshot) and the progress table fills as the job's poll
// drains the runner's events; a failed line's ⏎ opens its response. A
// shared collection first shows the hosts this run sends to (JC17). In the
// tiny view r runs the collection once and the second line is the progress.

// dkRunHosts is the shared run's host list question.
const dkRunHosts dialogKind = "run_hosts"

// runRow is one finished request of a run.
type runRow struct {
	iter          int
	reqID, name   string
	method        string
	status        int
	took          time.Duration
	passed, total int
	failed        bool
	reason        string
	st            *respState // a failed line's response, indexed in the run job (nil = not kept)
}

// kept reports whether the line kept its response.
func (r runRow) kept() bool { return r.st != nil }

// A run keeps the responses of its first runKeep failed lines only, and
// runKeepBytes of their bodies at most (1,000 iterations of a failing 2MB
// response must not stay in memory); later failed lines keep their status
// and reason.
const (
	runKeep      = 6
	runKeepBytes = 20 << 20
)

// runItem is a runner event handed to the UI goroutine with its response
// already indexed.
type runItem struct {
	ev apitool.RunEvent
	st *respState
}

// runState is the last run of the tool.
type runState struct {
	col   string
	n     int // planned requests
	rows  []runRow
	sel   int
	ok    int
	fail  int
	done  bool
	tiny  bool                        // started from the tiny view (its second line shows it)
	reqs  map[string]*apitool.Request // the snapshot's requests (to open a failed line)
	sum   *apitool.RunSummary         // set when done (the api.run card)
	doc   string                      // the shared collection run ("" = personal)
	runMu sync.Mutex
	queue []runItem // events the job goroutine handed over
}

// pendingRun is a prepared run (a shared one waits for its host list answer).
type pendingRun struct {
	snap   apitool.RunSnapshot
	opts   apitool.RunOptions
	n      int
	tiny   bool
	shared bool
	hosts  []string
}

// runTarget is what r runs: the tree's selected folder or collection,
// else the edited request's collection.
func (t *Tool) runTarget(ctx *ui.Context) (col, folder string) {
	if t.focus == paneTree {
		rows := t.tree.rows(ctx, t)
		if t.tree.cur < len(rows) {
			switch r := rows[t.tree.cur]; r.kind {
			case rowFolder:
				return r.col, r.folder
			case rowCollection, rowRequest:
				return r.col, ""
			}
		}
	}
	return t.colName, ""
}

// openRunner opens the runner setup.
func (t *Tool) openRunner(ctx *ui.Context) tea.Cmd {
	col, folder := t.runTarget(ctx)
	if t.collection(col) == nil {
		return ui.Toast(ctx.T.Apitest.SaveFirst(), ui.ToastInfo)
	}
	return ui.OpenOverlay(&runSetup{t: t, col: col, folder: folder, iters: newLineInput("1"), delay: newLineInput("0")})
}

// runQuick is the tiny view's r: the edited request's collection, once (a
// shared collection asks about its hosts first).
func (t *Tool) runQuick(ctx *ui.Context) tea.Cmd {
	if t.running(jobRun) {
		return nil
	}
	pr, cmd := t.prepareRun(ctx, t.colName, "", apitool.RunOptions{Iterations: 1})
	if pr == nil {
		return cmd
	}
	pr.tiny = true
	if pr.shared {
		t.pendingRun = pr
		return ui.OpenOverlay(t.hostsDialog(ctx, pr))
	}
	_, cmd = t.launch(ctx, pr, nil)
	return cmd
}

// folderRequests are the request ids under folder id of c ("" = all).
func folderRequests(c *apitool.Collection, id string) []string {
	if id == "" {
		return c.Root.RequestIDs()
	}
	var find func(f *apitool.Folder) *apitool.Folder
	find = func(f *apitool.Folder) *apitool.Folder {
		for _, it := range f.Items {
			if it.Folder == nil {
				continue
			}
			if it.Folder.ID == id {
				return it.Folder
			}
			if g := find(it.Folder); g != nil {
				return g
			}
		}
		return nil
	}
	if f := find(&c.Root); f != nil {
		return f.RequestIDs()
	}
	return nil
}

// prepareRun snapshots the target (nil and a toast when it cannot run).
func (t *Tool) prepareRun(ctx *ui.Context, col, folder string, o apitool.RunOptions) (*pendingRun, tea.Cmd) {
	a := ctx.T.Apitest
	if err := o.Validate(); err != nil {
		return nil, ui.Toast(a.RunBadIterations(), ui.ToastError)
	}
	c := t.collection(col)
	if c == nil {
		return nil, ui.Toast(a.SaveFirst(), ui.ToastInfo)
	}
	sc := t.snapshot(ctx)
	sc.Collection, sc.CollectionID = cloneVars(c.Vars), c.ID
	pr := &pendingRun{snap: apitool.Snapshot(c, folder, sc), opts: o, n: len(folderRequests(c, folder)) * o.Iterations, shared: t.shared[col]}
	if pr.shared {
		// A request someone else turned the certificate check off for runs
		// with the check unless I allowed it (a send's question).
		doc, _ := docOfKey(col)
		for _, r := range pr.snap.Collection.Requests {
			if _, ask := t.peerInsecure(doc, r); ask {
				r.Settings.Insecure = false
			}
		}
		hosts, err := apitool.RunHosts(pr.snap, dyn(ctx))
		if err != nil {
			slog.Debug("api: run hosts", "err", err)
		}
		pr.hosts = hosts
	}
	return pr, nil
}

// hostsBody is the shared run's host list text.
func hostsBody(ctx *ui.Context, pr *pendingRun) []string {
	body := []string{ctx.T.Apitest.RunHostsBody()}
	for _, h := range pr.hosts {
		body = append(body, "· "+textwidth.Sanitize(h))
	}
	return body
}

// hostsDialog asks before a shared run (the tiny view's r).
func (t *Tool) hostsDialog(ctx *ui.Context, pr *pendingRun) *dialog {
	a := ctx.T.Apitest
	return &dialog{inst: t.route.ID, kind: dkRunHosts, title: a.RunHostsTitle(), prefW: 64,
		body: hostsBody(ctx, pr), choices: []string{a.RunStart(), a.Cancel()}}
}

// runHostsAnswer starts the previewed run; its hosts may be private.
func (t *Tool) runHostsAnswer(ctx *ui.Context, m dialogMsg) tea.Cmd {
	p := t.pendingRun
	t.pendingRun = nil
	if p == nil || m.Choice != 0 {
		return nil
	}
	_, cmd := t.launch(ctx, p, hostSet(p.hosts))
	return cmd
}

func hostSet(hosts []string) map[string]bool {
	ok := map[string]bool{}
	for _, h := range hosts {
		ok[h] = true
	}
	return ok
}

// launch runs pr in a job; the poll moves the runner's events into the
// table on the UI goroutine.
func (t *Tool) launch(ctx *ui.Context, pr *pendingRun, privOK map[string]bool) (*runState, tea.Cmd) {
	snap, o := pr.snap, pr.opts
	foreign := map[string]foreignInfo{}
	for k, v := range t.foreign {
		foreign[k] = v
	}
	hist, spillDir := t.hist, t.spillDir
	runner := &apitool.Runner{
		Send: t.sender.Send,
		Dyn:  dyn(ctx),
		Foreign: func(id string) (string, bool) {
			fi, ok := foreign[id]
			return fi.By, ok && fi.foreign()
		},
		PrivateOK: privOK,
	}
	st := &runState{col: snap.Collection.Name, n: pr.n, tiny: pr.tiny, reqs: snap.Collection.Requests}
	if snap.Scope != nil {
		st.doc = snap.Scope.DocID
	}
	t.run = st
	j, cmd := t.startJob(ctx, jobRun, func(c context.Context) func(*ui.Context) tea.Cmd {
		if err := os.MkdirAll(spillDir, 0o700); err != nil {
			slog.Warn("api: spill dir", "err", err)
		}
		var sum *apitool.RunSummary
		kept, keptBytes := 0, 0
		for ev := range runner.Start(c, snap, o) {
			it := runItem{ev: ev}
			if r := ev.Response; ev.Failed && r != nil && kept < runKeep && keptBytes+len(r.Body) <= runKeepBytes {
				// BuildTree of a big body takes a while: here, not in Update.
				it.st = newRespState(reqOr(snap.Collection.Requests[ev.RequestID]), r)
				kept++
				keptBytes += len(r.Body)
			}
			it.ev.Response = nil // only the indexed state is kept
			st.runMu.Lock()
			st.queue = append(st.queue, it)
			st.runMu.Unlock()
			if ev.Done {
				sum = ev.Summary
			}
		}
		if sum != nil {
			if err := hist.AddRun(*sum); err != nil {
				slog.Warn("api: history run", "err", err)
			}
		}
		return func(*ui.Context) tea.Cmd {
			st.drain()
			st.done = true
			st.sum = sum
			if i := st.firstFailed(); i >= 0 {
				st.sel = i
			}
			return t.loadHistory()
		}
	})
	j.poll = st.drain
	return st, cmd
}

// drain moves the queued events into the table (UI goroutine).
func (st *runState) drain() {
	st.runMu.Lock()
	evs := st.queue
	st.queue = nil
	st.runMu.Unlock()
	for _, it := range evs {
		ev := it.ev
		if ev.Done {
			continue
		}
		st.rows = append(st.rows, runRow{
			iter: ev.Iter, reqID: ev.RequestID, name: ev.Name, method: methodOf(reqOr(st.reqs[ev.RequestID])),
			status: ev.Status, took: ev.Time, passed: ev.Passed, total: ev.Total,
			failed: ev.Failed, reason: ev.Reason, st: it.st,
		})
		if ev.Failed {
			st.fail++
		} else {
			st.ok++
		}
	}
}

func reqOr(r *apitool.Request) *apitool.Request {
	if r == nil {
		return &apitool.Request{}
	}
	return r
}

// firstFailed is the first failed row (-1 none).
func (st *runState) firstFailed() int {
	for i, r := range st.rows {
		if r.failed {
			return i
		}
	}
	return -1
}

// openRunRow shows a failed run line's request with its response (indexed
// in the run job; a line past runKeep says its response was not kept).
func (t *Tool) openRunRow(ctx *ui.Context, r runRow) tea.Cmd {
	st := t.run
	if st == nil {
		return nil
	}
	if !r.kept() {
		return ui.Toast(ctx.T.Apitest.RunRespNotKept(runKeep), ui.ToastInfo)
	}
	t.selectRequest(st.col, r.reqID)
	if t.req == nil || t.req.ID != r.reqID {
		return nil
	}
	t.applyResponse(t.respKey(), r.st)
	t.focus = paneResponse
	t.tree.follow(ctx, t)
	t.save(ctx)
	return func() tea.Msg { return ui.CloseOverlayMsg{} }
}

// tinyStatus is the tiny view's second line: the run's progress while (or
// after) a tiny run, else the last response's short status.
func (t *Tool) tinyStatus(ctx *ui.Context) string {
	if st := t.run; st != nil && st.tiny {
		return ctx.T.Apitest.RunProgressShort(len(st.rows), st.n, st.ok, st.fail)
	}
	if s := t.rv.current(); s != nil && s.resp != nil {
		return statusText(ctx, s, t.size.W, true)
	}
	if s := t.rv.current(); s != nil {
		return textwidth.Sanitize(s.errText)
	}
	return ""
}

// ---- the setup overlay ----

// runSetup rows.
const (
	rsEnv = iota
	rsIters
	rsStop
	rsDelay
	rsStart
	rsRows
)

// runSetup is the runner's setup dialog.
//
// The same overlay goes on to ask about a shared run's hosts and then shows
// the progress (one overlay: no close-then-open ordering between Cmds).
type runSetup struct {
	t           *Tool
	col, folder string
	row         int
	iters       *lineInput
	delay       *lineInput
	stop        bool
	size        ui.Size

	ask        *pendingRun // a shared run's host list is shown
	askBody    []string
	askChoices []string
	sel        int      // its choice (0 run, 1 cancel)
	prog       *runView // the run started: the progress table
}

var (
	_ ui.Overlay    = (*runSetup)(nil)
	_ ui.KeyHandler = (*runSetup)(nil)
	_ ui.Titled     = (*runSetup)(nil)
)

func (d *runSetup) Init(*ui.Context) tea.Cmd            { return nil }
func (d *runSetup) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (d *runSetup) Resize(_ *ui.Context, s ui.Size) {
	d.size = s
	if d.prog != nil {
		d.prog.size = s
	}
}
func (d *runSetup) Modal() bool { return true }
func (d *runSetup) Title(ctx *ui.Context) (string, ui.Badge) {
	switch {
	case d.prog != nil:
		return d.prog.Title(ctx)
	case d.ask != nil:
		return ctx.T.Apitest.RunHostsTitle(), ui.Badge{}
	}
	return ctx.T.Apitest.RunTitle(), ui.Badge{}
}

func (d *runSetup) Bounds(body layout.Rect) layout.Rect {
	if d.prog != nil {
		return d.prog.Bounds(body)
	}
	w, h := min(body.W, 56), min(body.H, rsRows+5)
	if d.ask != nil {
		w = min(body.W, 64)
		h = min(body.H, len(d.askLines(max(w-4, 1)))+2)
	}
	return layout.Rect{X: body.X + (body.W-w)/2, Y: body.Y + (body.H-h)/2, W: w, H: h}
}

// askLines is the host list question wrapped to w.
func (d *runSetup) askLines(w int) []string {
	var out []string
	for i, p := range d.askBody {
		if i == 1 {
			out = append(out, "")
		}
		out = append(out, textwidth.Wrap(p, w)...)
	}
	out = append(out, "")
	for i, c := range d.askChoices {
		mark := "  "
		if i == d.sel {
			mark = "› "
		}
		out = append(out, mark+c)
	}
	return out
}

// start runs the setup's options (a shared run asks first).
func (d *runSetup) start(ctx *ui.Context, pr *pendingRun, privOK map[string]bool) tea.Cmd {
	st, cmd := d.t.launch(ctx, pr, privOK)
	d.ask, d.prog = nil, &runView{t: d.t, st: st, size: d.size}
	return cmd
}

func (d *runSetup) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if d.prog != nil {
		return d.prog.HandleKey(ctx, msg)
	}
	if d.ask != nil {
		switch k {
		case "ctrl+c":
			return false, nil
		case "up", "k", "shift+tab":
			d.sel = 0
		case "down", "j", "tab":
			d.sel = 1
		case "esc":
			return true, func() tea.Msg { return ui.CloseOverlayMsg{} }
		case "enter":
			if d.sel != 0 {
				return true, func() tea.Msg { return ui.CloseOverlayMsg{} }
			}
			return true, d.start(ctx, d.ask, hostSet(d.ask.hosts))
		}
		return true, nil
	}
	switch k {
	case "ctrl+c":
		return false, nil
	case "esc":
		return true, func() tea.Msg { return ui.CloseOverlayMsg{} }
	case "j", "down", "tab":
		d.row = min(d.row+1, rsRows-1)
	case "k", "up", "shift+tab":
		d.row = max(d.row-1, 0)
	case "h", "left", "l", "right":
		if d.row == rsEnv {
			dir := 1
			if k == "h" || k == "left" {
				dir = -1
			}
			return true, d.t.switchEnv(ctx, dir)
		}
	case "space":
		if d.row == rsStop {
			d.stop = !d.stop
		}
	case "enter":
		iters, _ := strconv.Atoi(d.iters.text())
		ms, _ := strconv.Atoi(d.delay.text())
		o := apitool.RunOptions{Iterations: iters, Delay: time.Duration(max(ms, 0)) * time.Millisecond, StopOnFail: d.stop}
		if o.Validate() != nil {
			return true, ui.Toast(ctx.T.Apitest.RunBadIterations(), ui.ToastError)
		}
		pr, cmd := d.t.prepareRun(ctx, d.col, d.folder, o)
		switch {
		case pr == nil:
			return true, cmd
		case pr.shared:
			d.ask, d.sel = pr, 0
			d.askBody = hostsBody(ctx, pr)
			d.askChoices = []string{ctx.T.Apitest.RunStart(), ctx.T.Apitest.Cancel()}
			return true, nil
		}
		return true, d.start(ctx, pr, nil)
	case "backspace":
		if f := d.field(); f != nil {
			f.key(msg)
		}
	default:
		if f := d.field(); f != nil && len(msg.Text) == 1 && msg.Text >= "0" && msg.Text <= "9" {
			f.key(msg)
		}
	}
	return true, nil
}

// field is the number field of the selected row (nil for the others).
func (d *runSetup) field() *lineInput {
	switch d.row {
	case rsIters:
		return d.iters
	case rsDelay:
		return d.delay
	}
	return nil
}

func (d *runSetup) View(ctx *ui.Context, focused bool) string {
	a := ctx.T.Apitest
	w, h := d.size.W, d.size.H
	if d.prog != nil {
		return d.prog.View(ctx, focused)
	}
	if d.ask != nil {
		var out []string
		for _, l := range d.askLines(max(w-2, 1)) {
			out = append(out, " "+l)
		}
		return strings.Join(textwidth.Fit(out, w, h), "\n")
	}
	cw := max(w-2, 1)
	env := a.EnvNone()
	if d.t.env != nil {
		env = textwidth.Sanitize(d.t.env.Name)
	}
	state := a.StateOff()
	if d.stop {
		state = a.StateOn()
	}
	target := d.col
	if d.folder != "" {
		if c := d.t.collection(d.col); c != nil {
			target += " / " + folderName(&c.Root, d.folder)
		}
	}
	rows := []string{
		a.RunEnv(env),
		a.RunIterations(d.iters.text()),
		a.RunStopOnFail(state),
		a.RunDelay(d.delay.text()),
		a.RunStart(),
	}
	out := []string{" " + textwidth.Truncate(a.RunTarget(textwidth.Sanitize(target)), cw, "…"), ""}
	for i, r := range rows {
		line := "  " + textwidth.Truncate(r, cw-2, "…")
		if i == d.row {
			line = " " + selected(ctx.Theme, textwidth.PadRight("› "+textwidth.Truncate(r, cw-2, "…"), cw))
		}
		out = append(out, line)
	}
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}

// folderName is the name of folder id under f ("" when missing).
func folderName(f *apitool.Folder, id string) string {
	for _, it := range f.Items {
		if it.Folder == nil {
			continue
		}
		if it.Folder.ID == id {
			return textwidth.Sanitize(it.Folder.Name)
		}
		if n := folderName(it.Folder, id); n != "" {
			return n
		}
	}
	return ""
}

// ---- the progress overlay ----

// runView is the progress table of run st.
type runView struct {
	t    *Tool
	st   *runState
	top  int
	size ui.Size
}

var (
	_ ui.Overlay    = (*runView)(nil)
	_ ui.KeyHandler = (*runView)(nil)
	_ ui.Titled     = (*runView)(nil)
)

func (v *runView) Init(*ui.Context) tea.Cmd            { return nil }
func (v *runView) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (v *runView) Resize(_ *ui.Context, s ui.Size)     { v.size = s }
func (v *runView) Modal() bool                         { return true }
func (v *runView) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Apitest.RunTitle() + " · " + textwidth.Sanitize(v.st.col), ui.Badge{}
}

func (v *runView) Bounds(body layout.Rect) layout.Rect {
	w, h := min(body.W, 96), min(body.H, 30)
	return layout.Rect{X: body.X + (body.W-w)/2, Y: body.Y + (body.H-h)/2, W: w, H: h}
}

func (v *runView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	st := v.st
	switch msg.String() {
	case "ctrl+c":
		return false, nil
	case "esc", "q":
		if !st.done && v.t.running(jobRun) {
			v.t.cancelJob(jobRun)
			return true, nil
		}
		return true, func() tea.Msg { return ui.CloseOverlayMsg{} }
	case "j", "down":
		st.sel = min(st.sel+1, max(len(st.rows)-1, 0))
	case "k", "up":
		st.sel = max(st.sel-1, 0)
	case "enter":
		if st.sel < len(st.rows) && st.rows[st.sel].failed {
			return true, v.t.openRunRow(ctx, st.rows[st.sel])
		}
	case "s":
		if st.done && st.sum != nil {
			return true, v.t.openShareRun(ctx, st)
		}
	}
	return true, nil
}

func (v *runView) View(ctx *ui.Context, _ bool) string {
	a, st := ctx.T.Apitest, v.st
	w, h := v.size.W, v.size.H
	cw := max(w-2, 1)
	out := []string{" " + textwidth.Truncate(a.RunProgress(len(st.rows), st.n, st.ok, st.fail), cw, "…"), ""}
	hint := a.RunRunningHint()
	if st.done {
		hint = a.RunDoneHint()
	}
	bodyH := max(h-len(out)-2, 1)
	if st.sel < v.top {
		v.top = st.sel
	}
	if st.sel >= v.top+bodyH {
		v.top = st.sel - bodyH + 1
	}
	nameW := max(min(cw-30, 32), 6)
	for i := v.top; i < len(st.rows) && i < v.top+bodyH; i++ {
		r := st.rows[i]
		mark, result := "✓", strconv.Itoa(r.status)
		if r.failed {
			mark = "✕"
			if r.status == 0 {
				result = a.RunFailedNet(r.reason)
			}
		}
		line := mark + " " + textwidth.PadRight(r.method, 7) +
			textwidth.PadRight(textwidth.Truncate(textwidth.Sanitize(r.name), nameW, "…"), nameW) + "  " +
			textwidth.PadRight(result, 6) + " " + textwidth.PadRight(strconv.Itoa(int(r.took.Milliseconds()))+"ms", 7) +
			" " + strconv.Itoa(r.passed) + "/" + strconv.Itoa(r.total)
		line = textwidth.Truncate(line, cw, "…")
		if i == st.sel {
			line = selected(ctx.Theme, textwidth.PadRight(line, cw))
		}
		out = append(out, " "+line)
	}
	out = textwidth.Fit(out, w, h-1)
	out = append(out, " "+dim(ctx.Theme, textwidth.Truncate(hint, cw, "…")))
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}
