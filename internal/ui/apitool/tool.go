package apitool

import (
	"cmp"
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"

	bkey "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/secretsui"
)

// pane is a part of the tool screen: the tree, the request editor, the
// response. In the tabbed modes it is also the shown tab.
type pane int

const (
	paneTree pane = iota
	paneRequest
	paneResponse
)

// viewMode is how the tool is laid out for its size (C3.6).
type viewMode int

const (
	vmNone  viewMode = iota // too small: nothing is drawn
	vmTiny                  // three lines
	vmTabs                  // [목록] [요청] [응답]
	vmSplit                 // tree | request / response
)

// Layout thresholds (C3.6): the split view needs the standard mode's width
// (a 120-column window), the tabbed view takes the rest down to 44 columns.
const (
	splitMinW   = 100
	splitMinH   = 16
	treeW       = 26 // the left column of the split view
	splitMin    = 5  // rows the request and the response keep at least
	splitDef    = 45 // request share of the right column, percent
	historyRows = 8  // history rows under the tree at most
	narrowW     = 76 // below this the JSON tree indents by 1 and the status line is short
)

// savedState is the tool's section of tools-state.json (A8.5): what to show
// next time, read by the hub status line too.
type savedState struct {
	Collection string `json:"collection,omitempty"`
	Request    string `json:"request,omitempty"`
	Env        string `json:"env,omitempty"`
	Split      int    `json:"split,omitempty"`
}

// apiConfig is [tools.api] (C3.7 history settings).
type apiConfig struct {
	History     bool `toml:"history"`
	HistoryMax  int  `toml:"history_max"`
	HistoryDays int  `toml:"history_days"`
}

// Tool is one API tool instance on the tool surface.
type Tool struct {
	route ui.Route
	size  ui.Size

	local   *apitool.LocalStore
	hist    *apitool.History
	sender  *apitool.Sender
	recent  []apitool.HistoryEntry
	names   []string                       // personal collections, sorted
	cols    map[string]*apitool.Collection // loaded personal collections
	envs    []string
	env     *apitool.Env
	globals []apitool.Var

	colName string           // the collection of the edited request ("" = not saved yet)
	req     *apitool.Request // the edited request (a copy), nil = none
	results map[string]bool  // "<col>/<req>" -> the last send passed

	focus pane
	split int
	drag  bool

	tree *treeView
	ed   *editor
	rv   *respView

	jobs   map[jobKind]*job
	jobSeq int
	cursor *tea.Cursor

	foreign        map[string]foreignInfo // request id -> last changed by someone else
	privateOK      map[string]bool        // privKey -> a private target was allowed
	fileChanged    bool                   // the collection changed outside masil
	pendingSecret  *pendingSecret
	pendingPuts    []pendingPut
	pendingHost    *apitool.HostNotAllowedError
	pendingPrivate string
}

var (
	_ ui.Component     = (*Tool)(nil)
	_ ui.KeyHandler    = (*Tool)(nil)
	_ ui.MouseHandler  = (*Tool)(nil)
	_ ui.DragHandler   = (*Tool)(nil)
	_ ui.InputCapturer = (*Tool)(nil)
	_ ui.Cursorer      = (*Tool)(nil)
	_ ui.Titled        = (*Tool)(nil)
	_ ui.Helper        = (*Tool)(nil)
	_ ui.Completer     = (*Tool)(nil)
	_ ui.Closer        = (*Tool)(nil)
	_ ui.ToolStatuser  = (*Tool)(nil)
)

// New is the RouteTool factory of the API tool (ToolSpec.New). Query "col"
// and "req" pick what to open; without them the last collection opens.
func New(ctx *ui.Context, r ui.Route) ui.Component {
	t := &Tool{
		route:   r,
		local:   apitool.OpenLocal(ctx.Paths),
		cols:    map[string]*apitool.Collection{},
		results: map[string]bool{},
		split:   splitDef,
		focus:   paneRequest,
		jobs:    map[jobKind]*job{},

		foreign:   map[string]foreignInfo{},
		privateOK: map[string]bool{},
	}
	cfg := apiConfig{History: true}
	if ctx.Config != nil {
		if err := ctx.Config.ToolSection(ui.ToolAPI, &cfg); err != nil {
			slog.Warn("api tool config", "err", err)
		}
	}
	ho := apitool.DefaultHistoryOptions()
	ho.Enabled = cfg.History
	if cfg.HistoryMax > 0 {
		ho.Max = cfg.HistoryMax
	}
	if cfg.HistoryDays > 0 {
		ho.MaxAge = time.Duration(cfg.HistoryDays) * 24 * time.Hour
	}
	now := ctx.Now
	if now == nil {
		now = time.Now
	}
	t.hist = apitool.OpenHistory(filepath.Join(t.local.Dir, "history"), ho, now)
	t.sender = apitool.NewSender(filepath.Join(ctx.Paths.Cache, "api"), slog.Default())
	t.tree = newTreeView()
	t.ed = newEditor()
	t.rv = newRespView()

	var st savedState
	ctx.ToolState.Section(ui.ToolAPI, &st)
	if st.Split >= 10 && st.Split <= 90 {
		t.split = st.Split
	}
	t.reload(ctx)
	col, req := st.Collection, st.Request
	if c := r.Query["col"]; c != "" {
		col, req = c, r.Query["req"]
	}
	t.selectEnv(ctx, st.Env)
	t.openFirst(col, req)
	t.tree.follow(ctx, t)
	return t
}

// reload reads the personal collection list, the environments and globals
// (small files), keeping the loaded collections that still exist.
func (t *Tool) reload(ctx *ui.Context) {
	names, err := t.local.ListCollections()
	if err != nil {
		slog.Warn("api: list collections", "err", err)
	}
	t.names = names
	for n := range t.cols {
		if !slices.Contains(names, n) {
			delete(t.cols, n)
		}
	}
	t.envs, _ = t.local.ListEnvs()
	t.globals, _ = t.local.LoadGlobals()
}

// collection returns the loaded collection name (loading it on first use).
func (t *Tool) collection(name string) *apitool.Collection {
	if name == "" {
		return nil
	}
	if c, ok := t.cols[name]; ok {
		return c
	}
	c, err := t.local.LoadCollection(name)
	if err != nil {
		slog.Warn("api: load collection", "err", err)
		return nil
	}
	t.cols[name] = c
	return c
}

// openFirst selects request req of collection col, else that collection's
// first request, else the first collection's.
func (t *Tool) openFirst(col, req string) {
	if !slices.Contains(t.names, col) {
		col, req = "", ""
		if len(t.names) > 0 {
			col = t.names[0]
		}
	}
	c := t.collection(col)
	if c == nil {
		return
	}
	t.tree.expand(col, true)
	if _, ok := c.Requests[req]; !ok {
		req = ""
		if ids := c.Root.RequestIDs(); len(ids) > 0 {
			req = ids[0]
		}
	}
	if req != "" {
		t.selectRequest(col, req)
	}
}

// selectRequest makes request id of collection col the edited one.
func (t *Tool) selectRequest(col, id string) {
	c := t.collection(col)
	if c == nil || c.Requests[id] == nil {
		return
	}
	r := cloneRequest(c.Requests[id])
	t.colName, t.req = col, r
	t.ed.load(r)
	t.rv.show(t.respKey())
}

// selectEnv picks the personal environment name ("" or unknown = none).
func (t *Tool) selectEnv(ctx *ui.Context, name string) {
	t.env = nil
	if name == "" || !slices.Contains(t.envs, name) {
		return
	}
	e, err := t.local.LoadEnv(name)
	if err != nil {
		slog.Warn("api: load env", "err", err)
		return
	}
	t.env = e
}

// respKey names the edited request for per-request state (responses, folds).
func (t *Tool) respKey() string {
	if t.req == nil {
		return ""
	}
	return t.colName + "/" + t.req.ID
}

// save remembers what is shown (tools-state.json).
func (t *Tool) save(ctx *ui.Context) {
	st := savedState{Collection: t.colName, Split: t.split}
	if t.req != nil && t.colName != "" {
		st.Request = t.req.ID
	}
	if t.env != nil {
		st.Env = t.env.Name
	}
	if err := ctx.ToolState.SetSection(ui.ToolAPI, st); err != nil {
		slog.Warn("api: save tool state", "err", err)
	}
}

// historyMsg carries the recent history rows of tool t.
type historyMsg struct {
	t    *Tool
	rows []apitool.HistoryEntry
}

// loadHistory reads the recent history rows off the UI goroutine.
func (t *Tool) loadHistory() tea.Cmd {
	h := t.hist
	return func() tea.Msg {
		rows, err := h.List(historyRows)
		if err != nil {
			slog.Warn("api: history", "err", err)
		}
		return historyMsg{t: t, rows: rows}
	}
}

// Init implements ui.Component: the history loads and a command the route
// carries runs.
func (t *Tool) Init(ctx *ui.Context) tea.Cmd {
	return tea.Batch(t.loadHistory(), t.command(ctx, t.route.Query[queryCmd]))
}

// Update implements ui.Component.
func (t *Tool) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case jobTickMsg:
		if m.t == t {
			return t.onJobTick(ctx, m)
		}
	case historyMsg:
		if m.t == t {
			t.recent = m.rows
		}
	case ui.RouteUpdatedMsg:
		if m.Route.ID == t.route.ID {
			return t.onRoute(ctx, m.Route)
		}
	case historyClearedMsg:
		t.recent = nil
	case tea.PasteMsg:
		return t.onPaste(ctx, m)
	case dialogMsg:
		if m.Inst == t.route.ID {
			return t.onDialog(ctx, m)
		}
	case secretPutMsg:
		if m.t == t {
			return t.onSecretPut(ctx, m)
		}
	case secretsui.ConsentMsg:
		if m.Tool != ui.ToolAPI || len(t.pendingPuts) == 0 {
			return nil
		}
		if m.Choice == secrets.FallbackUnset {
			t.pendingPuts = nil
			return nil
		}
		return t.flushPuts(ctx)
	case saveConfirmedMsg:
		if m.t == t && t.req != nil {
			return t.writeRequest(ctx)
		}
	case ui.ToolSwitchedMsg:
		if m.Route.ID == t.route.ID {
			return t.checkFile()
		}
	case ui.FocusChangedMsg:
		if m.RouteKey == t.route.Key() {
			return t.checkFile()
		}
	case fileCheckMsg:
		if m.t == t && m.name == t.colName {
			t.fileChanged = m.changed
		}
		return nil
	}
	return t.update(ctx, msg)
}

// onRoute follows a reused instance's new query (a palette command, a card).
func (t *Tool) onRoute(ctx *ui.Context, r ui.Route) tea.Cmd {
	t.route = r
	if c := r.Query["col"]; c != "" {
		t.reload(ctx)
		t.openFirst(c, r.Query["req"])
		t.tree.follow(ctx, t)
	}
	return t.command(ctx, r.Query[queryCmd])
}

// Resize implements ui.Component.
func (t *Tool) Resize(_ *ui.Context, s ui.Size) { t.size = s }

// mode is the layout for the current size.
func (t *Tool) mode(ctx *ui.Context) viewMode {
	switch {
	case ctx.Mode == layout.ModeTooSmall || t.size.W <= 0 || t.size.H <= 0:
		return vmNone
	case ctx.Mode == layout.ModeTiny || t.size.H < 6:
		return vmTiny
	case t.size.W >= splitMinW && t.size.H >= splitMinH:
		return vmSplit
	}
	return vmTabs
}

// Title implements ui.Titled: "API 시험 · <collection> · <env>".
func (t *Tool) Title(ctx *ui.Context) (string, ui.Badge) {
	parts := []string{ctx.T.Apitest.ToolName()}
	if t.colName != "" {
		parts = append(parts, textwidth.Sanitize(t.colName))
	}
	if t.env != nil {
		parts = append(parts, textwidth.Sanitize(t.env.Name))
	}
	return strings.Join(parts, " · "), ui.Badge{}
}

// ToolStatus implements ui.ToolStatuser (the hub's open list).
func (t *Tool) ToolStatus(ctx *ui.Context) string {
	if t.running(jobSend) {
		return ctx.T.Apitest.Sending()
	}
	return ""
}

// CloseTool implements ui.Closer: running work stops.
func (t *Tool) CloseTool(*ui.Context) tea.Cmd {
	for _, j := range t.jobs {
		j.cancel()
	}
	t.rv.release()
	return nil
}

// Help implements ui.Helper (the status line's first hints).
func (t *Tool) Help(ctx *ui.Context) []bkey.Binding {
	a := ctx.T.Apitest
	b := []bkey.Binding{
		bkey.NewBinding(bkey.WithKeys("enter"), bkey.WithHelp("⏎", a.HelpSend())),
		bkey.NewBinding(bkey.WithKeys("r"), bkey.WithHelp("r", a.HelpRunner())),
		bkey.NewBinding(bkey.WithKeys("e"), bkey.WithHelp("e", a.HelpEnv())),
	}
	if t.mode(ctx) == vmTabs {
		b = append(b, bkey.NewBinding(bkey.WithKeys("[", "]"), bkey.WithHelp("[ ]", a.HelpTabs())))
	}
	return append(b, bkey.NewBinding(bkey.WithKeys("?"), bkey.WithHelp("?", a.HelpHelp())))
}

// CapturesInput implements ui.InputCapturer: a field is being edited.
func (t *Tool) CapturesInput() bool { return t.ed.capturing() }

// Completing implements ui.Completer: while a table cell is edited, tab moves
// between its key and value instead of the shell's focus.
func (t *Tool) Completing() bool { return t.ed.capturing() }

// Cursor implements ui.Cursorer.
func (t *Tool) Cursor() *tea.Cursor { return t.cursor }

// HandleKey implements ui.KeyHandler.
func (t *Tool) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	mode := t.mode(ctx)
	if mode == vmNone {
		return false, nil
	}
	if t.ed.capturing() {
		return t.ed.handleKey(ctx, t, msg)
	}
	k := msg.String()
	switch k {
	case "?":
		return true, ui.OpenOverlay(newHelpDialog(ctx))
	case "[", "]":
		if mode == vmTiny {
			return false, nil
		}
		d := 1
		if k == "[" {
			d = -1
		}
		t.focus = pane((int(t.focus) + 3 + d) % 3)
		return true, nil
	}
	if mode == vmTiny {
		return t.tinyKey(ctx, msg)
	}
	if handled, cmd := t.globalKey(ctx, msg); handled {
		return true, cmd
	}
	switch t.focus {
	case paneTree:
		return t.tree.handleKey(ctx, t, msg)
	case paneRequest:
		return t.ed.handleKey(ctx, t, msg)
	default:
		return t.rv.handleKey(ctx, t, msg)
	}
}

// HandleMouse implements ui.MouseHandler: a click focuses the pane under
// it; a press on the split line starts resizing.
func (t *Tool) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, p ui.Point) tea.Cmd {
	if _, ok := msg.(tea.MouseClickMsg); !ok {
		return nil
	}
	switch t.mode(ctx) {
	case vmSplit:
		g := t.splitGeom()
		switch {
		case p.X < treeW:
			t.focus = paneTree
			t.tree.click(ctx, t, p.Y)
		case p.Y == g.reqH:
			t.drag = true
		case p.Y < g.reqH:
			t.focus = paneRequest
		default:
			t.focus = paneResponse
		}
	case vmTabs:
		if p.Y == 0 {
			if i, ok := tabHit(ctx, p.X); ok {
				t.focus = pane(i)
			}
		} else if t.focus == paneTree {
			t.tree.click(ctx, t, p.Y-1)
		}
	}
	return nil
}

// HandleDrag implements ui.DragHandler: dragging the split line moves it.
func (t *Tool) HandleDrag(ctx *ui.Context, msg tea.MouseMsg, p ui.Point) tea.Cmd {
	if !t.drag {
		return nil
	}
	if _, rel := msg.(tea.MouseReleaseMsg); rel {
		t.drag = false
		t.save(ctx)
		return nil
	}
	h := t.size.H
	if h > 2*splitMin+1 {
		y := min(max(p.Y, splitMin), h-splitMin-1)
		t.split = min(max(y*100/h, 10), 90)
	}
	return nil
}

// ClearDrag implements ui.DragHandler.
func (t *Tool) ClearDrag() { t.drag = false }

// globalKey handles the keys that work from every pane.
func (t *Tool) globalKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		return true, t.saveRequest(ctx)
	case "n":
		if t.focus != paneResponse {
			return true, t.newRequest(ctx)
		}
	case "e":
		return true, t.openEnv(ctx)
	case "r":
		if t.focus != paneResponse || t.rv.current() == nil {
			return true, t.openRunner(ctx)
		}
	case "i":
		return true, t.openImport(ctx)
	case "x":
		return true, t.openExport(ctx)
	case "esc":
		if t.running(jobSend) {
			t.cancelJob(jobSend)
			return true, nil
		}
	case "R":
		if t.fileChanged {
			return true, t.reloadFile(ctx)
		}
	}
	return false, nil
}

// tinyKey is the tiny mode: resend and the runner only.
func (t *Tool) tinyKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return true, t.send(ctx)
	case "r":
		return true, t.runQuick(ctx)
	case "esc":
		if t.running(jobSend) || t.running(jobRun) {
			t.cancelJob(jobSend)
			t.cancelJob(jobRun)
			return true, nil
		}
		return false, nil
	}
	if msg.Text != "" || msg.String() == "ctrl+s" {
		return true, ui.Toast(ctx.T.Apitest.NeedBigger(), ui.ToastInfo)
	}
	return false, nil
}

// cloneRequest deep-copies r (the edited copy never aliases the collection).
func cloneRequest(r *apitool.Request) *apitool.Request {
	c := *r
	c.Query = slices.Clone(r.Query)
	c.Headers = slices.Clone(r.Headers)
	c.Body.Form = slices.Clone(r.Body.Form)
	c.Asserts = slices.Clone(r.Asserts)
	c.Extracts = slices.Clone(r.Extracts)
	return &c
}

// sortedNames is names sorted (personal collections, environments).
func sortedNames(names []string) []string {
	out := slices.Clone(names)
	slices.SortFunc(out, cmp.Compare[string])
	return out
}

// background is the context for store calls made off the UI goroutine.
func background() context.Context { return context.Background() }

// command runs a palette command the route carried.
func (t *Tool) command(ctx *ui.Context, c string) tea.Cmd {
	switch c {
	case "new":
		return t.newRequest(ctx)
	case "import":
		return t.openImport(ctx)
	case "env":
		return t.openEnv(ctx)
	case "runner":
		return t.openRunner(ctx)
	case "send":
		return t.send(ctx)
	}
	return nil
}

// update handles the non-input messages of the panes and dialogs.
func (t *Tool) update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	for _, tb := range []*KVTable{t.ed.query, t.ed.headers, t.ed.asserts, t.ed.extracts, t.ed.form} {
		if tb.HandleMsg(msg) {
			return nil
		}
	}
	return nil
}

// newRequest starts an unsaved request ("새 요청", GET) and edits its URL.
func (t *Tool) newRequest(ctx *ui.Context) tea.Cmd {
	r := &apitool.Request{Name: ctx.T.Apitest.NewRequest(), Method: "GET"}
	t.colName, t.req = "", r
	t.ed.load(r)
	t.ed.url = newLineInput("")
	t.rv.show("")
	t.focus = paneRequest
	return nil
}
