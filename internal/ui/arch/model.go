package arch

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/archexport"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// canvasModel is one design on the tool surface (a RouteTool instance).
type canvasModel struct {
	route ui.Route
	sink  docSink
	// what the design is drawn from (rebuilt after every change)
	design  *archdoc.Design
	scene   *archdraw.Scene
	hidden  map[archdraw.Zoom]*archdraw.Scene // the scene without overlapping nodes, per zoom
	oldHide map[archdraw.Zoom]*archdraw.Scene // hidden before the last rebuild (route reuse)
	objs    archdoc.Objects                   // the design as objects (the panel reads its fields)
	result  *archsim.Result
	badges  map[string]archdraw.NodeBadge
	labels  map[string]string // node and box id → sanitized label
	loadErr string            // the file that could not be opened ("" = none)

	sel     string // selected node, box or edge id ("" = none)
	vp      archdraw.Viewport
	zoomSet bool // the user picked the zoom (else it follows the canvas width)
	placed  bool // the viewport was fitted to the design once
	drawer  drawerState
	size    ui.Size
	layMode layout.Mode
	regs    regions
	listSel int // the selected row of the list view

	undo archdoc.UndoLog // my own edits (P3 Task 16)

	// keyboard editing (keys.go)
	mode           editMode
	multi          map[string]bool // v: the multi-selection (nil = off)
	cursor         string          // the connect target (c) or the multi cursor (v)
	moveDX, moveDY int             // m: the ghost's offset in world cells
	pick           int             // a / g: the palette cursor (paletteItems index)
	pickBoxes      bool            // g: box kinds only
	pickOpened     bool            // the palette drawer was pulled out for the pick
	find           string          // /: the query typed so far

	// the fault menu and the timed run (faults.go, playback.go); runGen
	// counts design changes so an old run's result is dropped
	faults *faultMenu
	pb     playback
	runGen int

	// the settings panel (panel.go); panelGone: its node was deleted by
	// someone else in the last rebuild (refresh turns it into a note)
	panel     panelState
	panelGone bool

	// mouse (mouse.go, contextmenu.go)
	gest *gesture // the drag in progress
	menu *ctxMenu // the open right-click menu

	// a room document (shared.go, presence.go): the drag shared as presence
	// (dragSentAt = the last frame) and the open share dialog
	dragShared bool
	dragSentAt time.Time
	shareDlg   *shareDialog
	sendDlg    *sendDialog     // the open arch.send_card dialog (senddialog.go)
	verDlg     *versionsDialog // the open versions dialog (versions.go)

	cmt commentsState // a room document's comments (comments.go)
}

var (
	_ ui.Component    = (*canvasModel)(nil)
	_ ui.Titled       = (*canvasModel)(nil)
	_ ui.Helper       = (*canvasModel)(nil)
	_ ui.ToolStatuser = (*canvasModel)(nil)
	_ ui.Closer       = (*canvasModel)(nil)
)

// CloseTool writes a personal design's pending edits before the instance goes
// (hub x, ui.CloseTool); a failed write says so in a toast.
func (m *canvasModel) CloseTool(ctx *ui.Context) tea.Cmd {
	m.cancelGesture()
	if sh, ok := m.sink.(*sharedSink); ok {
		sh.close()
	}
	if ls, ok := m.sink.(*localSink); ok {
		if err := ls.flush(); err != nil {
			return ui.Toast(ctx.T.Archcanvas.SaveFailed(), ui.ToastError)
		}
	}
	return nil
}

// newCanvas is the tool's ui.ToolSpec.New: the sink is chosen in Init.
func newCanvas(_ *ui.Context, r ui.Route) ui.Component {
	return &canvasModel{route: r, hidden: map[archdraw.Zoom]*archdraw.Scene{}, cmt: commentsState{curY: -1}}
}

// storeOf opens the personal design folder.
func storeOf(ctx *ui.Context) (*archdoc.Store, error) {
	if ctx == nil || ctx.Paths.Dir == "" {
		return nil, errors.New("arch: no config folder")
	}
	return archdoc.OpenStore(filepath.Join(ctx.Paths.Dir, "arch"))
}

func (m *canvasModel) Init(ctx *ui.Context) tea.Cmd {
	if m.sink != nil {
		return nil
	}
	q := m.route.Query
	var dialog tea.Cmd
	if q[queryDialog] == "open" {
		dialog = ui.OpenOverlay(newFilesDialog())
	}
	return tea.Batch(dialog, m.initSink(ctx))
}

// initSink picks the design source from the instance query.
func (m *canvasModel) initSink(ctx *ui.Context) tea.Cmd {
	q := m.route.Query
	switch {
	case q[queryFile] != "":
		name := q[queryFile]
		st, err := storeOf(ctx)
		var d *archdoc.Design
		if err == nil {
			d, err = st.Load(name)
		}
		if err != nil {
			m.loadErr = name
			if errors.Is(err, fs.ErrNotExist) {
				m.loadErr = ""
				d = archdoc.New(name) // a name not saved yet: start it
				m.setSink(ctx, &localSink{instance: m.route.ID, store: st, name: name, objs: d.ToObjects(), timers: ctx.Timers})
				return nil
			}
			m.setSink(ctx, &copySink{objs: archdoc.New("").ToObjects(), reason: roBroken})
			return nil
		}
		m.setSink(ctx, &localSink{instance: m.route.ID, store: st, name: name, objs: d.ToObjects(), timers: ctx.Timers})
	case q[queryDoc] != "":
		cmd := m.openShared(ctx, q[queryDoc])
		if q[queryComments] == "1" {
			return tea.Batch(cmd, m.openComments(ctx)) // a mention alert
		}
		return tea.Batch(cmd, m.loadComments(ctx, q[queryDoc]))
	case q[queryMsg] != "":
		// a chat card's copy: at hand when the card opened it, else (a
		// restart) loaded from the message
		if d := getCopy(q[queryMsg]); d != nil {
			m.setSink(ctx, &copySink{objs: d.ToObjects()})
			return nil
		}
		m.setSink(ctx, &copySink{objs: archdoc.New("").ToObjects()})
		return fetchCopy(ctx, m.route.ID, q[queryMsg])
	default:
		m.setSink(ctx, &localSink{instance: m.route.ID, objs: archdoc.New("").ToObjects(), timers: ctx.Timers})
	}
	return nil
}

// setSink switches the design source and redraws from it.
func (m *canvasModel) setSink(ctx *ui.Context, s docSink) {
	m.sink = s
	m.placed = false
	m.rebuild(ctx)
}

// rebuild re-reads the design from the sink and re-runs the instant analysis.
func (m *canvasModel) rebuild(ctx *ui.Context) {
	if m.sink == nil {
		return
	}
	d, _ := archdoc.FromObjects(m.sink.Objects(), nil)
	if d == nil {
		d = archdoc.New("")
	}
	m.design = d
	m.objs = d.ToObjects()
	// the routes nothing touched carry over: the first frame after an edit
	// routes only the edges near the change (spec C2.7 frame budget)
	m.scene = archdraw.BuildFrom(m.scene, d)
	m.oldHide, m.hidden = m.hidden, map[archdraw.Zoom]*archdraw.Scene{}
	m.result = archsim.Analyze(d, archsim.Options{})
	m.badges = archexport.Badges(d, m.result)
	m.labels = map[string]string{}
	for _, n := range d.Nodes {
		m.labels[n.ID] = cleanText(n.Label)
		if m.labels[n.ID] == "" {
			m.labels[n.ID] = kindName(ctx.T, n.Kind)
		}
	}
	for _, b := range d.Boxes {
		m.labels[b.ID] = cleanText(b.Label)
		if m.labels[b.ID] == "" {
			m.labels[b.ID] = boxKindName(ctx.T, b.Kind)
		}
	}
	if m.mode == modePanel && m.panel.target != "" && !m.exists(m.panel.target) {
		m.closePanel()
		m.panelGone = true
	}
	if m.sel != "" && !m.exists(m.sel) {
		m.sel = ""
	}
	m.gestureGone()
	if m.faults != nil {
		for _, o := range m.faults.opts {
			if !m.exists(o.target) {
				m.faults = nil
				break
			}
		}
	}
	if !m.placed {
		m.fit()
	}
}

// exists reports whether id is a node, box or edge of the design.
func (m *canvasModel) exists(id string) bool {
	for _, n := range m.design.Nodes {
		if n.ID == id {
			return true
		}
	}
	for _, b := range m.design.Boxes {
		if b.ID == id {
			return true
		}
	}
	for _, e := range m.design.Edges {
		if e.ID == id {
			return true
		}
	}
	return false
}

// fit puts the design's top-left corner near the canvas's.
func (m *canvasModel) fit() {
	if m.scene == nil {
		return
	}
	x, y, _, _ := m.scene.Bounds()
	m.vp.X, m.vp.Y = x-2, y-1
	m.placed = true
}

// node returns node id of the design.
func (m *canvasModel) node(id string) *archdoc.Node {
	for i := range m.design.Nodes {
		if m.design.Nodes[i].ID == id {
			return &m.design.Nodes[i]
		}
	}
	return nil
}

func (m *canvasModel) box(id string) *archdoc.Box {
	for i := range m.design.Boxes {
		if m.design.Boxes[i].ID == id {
			return &m.design.Boxes[i]
		}
	}
	return nil
}

func (m *canvasModel) edge(id string) *archdoc.Edge {
	for i := range m.design.Edges {
		if m.design.Edges[i].ID == id {
			return &m.design.Edges[i]
		}
	}
	return nil
}

func (m *canvasModel) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := m.commentMsg(ctx, msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case saveDueMsg:
		if ls, ok := m.sink.(*localSink); ok && msg.instance == m.route.ID {
			return ls.due(msg.gen)
		}
	case savedMsg:
		if ls, ok := m.sink.(*localSink); ok && msg.instance == m.route.ID {
			ls.done(msg)
		}
	case commandMsg:
		if msg.instance == m.route.ID {
			return m.runCommand(ctx, msg.id)
		}
	case cardCopyMsg:
		if _, ok := m.sink.(*copySink); ok && msg.instance == m.route.ID {
			if msg.d == nil {
				m.setSink(ctx, &copySink{objs: archdoc.New("").ToObjects(), reason: roBroken})
				return ui.Toast(ctx.T.Archcanvas.CardCopyFailed(), ui.ToastError)
			}
			m.setSink(ctx, &copySink{objs: msg.d.ToObjects()})
		}
	case collabEventMsg:
		if msg.instance == m.route.ID {
			return m.onCollab(ctx, msg.E)
		}
	case versionPreviewMsg:
		if sh := m.shared(); sh != nil && msg.instance == m.route.ID {
			return m.showVersion(ctx, sh, &versionPreview{label: msg.label, objs: msg.objs})
		}
	case docMetaMsg:
		if sh := m.shared(); sh != nil && msg.instance == m.route.ID && msg.err == nil && msg.doc != nil {
			m.setMeta(ctx, sh, msg.doc)
		}
	case ui.EventMsg:
		return m.onDocEvent(ctx, msg.Event)
	case timedDoneMsg:
		if msg.instance == m.route.ID {
			return m.runDone(ctx, msg)
		}
	case playTickMsg:
		if msg.instance == m.route.ID {
			return m.playStep(ctx, msg)
		}
	case settleMsg:
		if msg.instance == m.route.ID && m.scene != nil {
			if sc, _ := m.visibleScene(); !sc.Settle(m.vp, archdraw.FrameRouteLimit) {
				return m.settleLater(ctx)
			}
		}
	case ui.LocaleChangedMsg:
		m.rebuild(ctx)
	case ui.ModeChangedMsg:
		m.cancelGesture()
		m.menu = nil
	case ui.FocusChangedMsg:
		if msg.RouteKey != m.route.Key() {
			m.cancelGesture()
			if m.mode == modePanel && m.panel.editing {
				// leaving the box commits the value (a bad one is dropped)
				cmd := m.commitInput(ctx)
				m.panel.stopInput()
				return cmd
			}
		}
	}
	return nil
}

// settleMsg asks instance to route the edges its last frames left stale
// (archdraw.Overlay.RouteLimit) — a few per tick, so no frame blows its budget.
type settleMsg struct{ instance string }

// settleDelay is the pause between settle steps (about two frames).
const settleDelay = 30 * time.Millisecond

func (m *canvasModel) settleLater(ctx *ui.Context) tea.Cmd {
	if ctx == nil || ctx.Timers == nil {
		return nil
	}
	return ctx.Timers.After(settleDelay, settleMsg{instance: m.route.ID})
}

// runCommand runs an arch.* palette command on this canvas (the later tasks
// fill in share, export, versions, send_card and run).
func (m *canvasModel) runCommand(ctx *ui.Context, id string) tea.Cmd {
	switch id {
	case "arch.run":
		return m.toggleRun(ctx)
	case "arch.share":
		if m.shared() != nil {
			return ui.Toast(ctx.T.Archcanvas.ShareAlready(), ui.ToastInfo)
		}
		if m.design == nil {
			return nil
		}
		m.cancelGesture()
		m.menu = nil
		m.shareDlg = m.newShareDialog(ctx)
		return ui.OpenOverlay(m.shareDlg)
	case "arch.versions":
		sh := m.shared()
		if sh == nil {
			return ui.Toast(ctx.T.Archcanvas.VersionsPersonal(), ui.ToastInfo)
		}
		m.cancelGesture()
		m.menu = nil
		m.verDlg = m.newVersionsDialog(sh)
		return ui.OpenOverlay(m.verDlg)
	case "arch.save_copy":
		return m.saveCopy(ctx)
	case "arch.send_card":
		if m.design == nil {
			return nil
		}
		m.cancelGesture()
		m.menu = nil
		m.sendDlg = m.newSendDialog(ctx)
		return ui.OpenOverlay(m.sendDlg)
	case "arch.export":
		if m.design == nil {
			return nil
		}
		m.cancelGesture()
		m.menu = nil
		return ui.OpenOverlay(m.newExportDialog(ctx))
	}
	return nil
}

func (m *canvasModel) Resize(ctx *ui.Context, size ui.Size) {
	m.size, m.layMode = size, ctx.Mode
	m.cancelGesture()
	m.menu = nil
	m.layout()
}

// layout recomputes the regions and the viewport size.
func (m *canvasModel) layout() {
	m.regs = computeRegions(m.layMode, m.size.W, m.size.H, m.drawer)
	m.vp.W, m.vp.H = m.regs.Canvas.W, m.regs.Canvas.H
	m.showComments()
	if !m.zoomSet {
		m.vp.Zoom = archdraw.ZoomDetail
		if m.layMode == layout.ModeWidget || m.regs.Canvas.W < listBelow {
			m.vp.Zoom = archdraw.ZoomNormal
		}
	}
}

// title is the design's sanitized title ("제목 없는 설계" when empty).
func (m *canvasModel) title(ctx *ui.Context) string {
	if m.design != nil {
		if t := cleanText(m.design.Title); t != "" {
			return t
		}
	}
	if n := m.route.Query[queryFile]; n != "" {
		return cleanText(n)
	}
	return ctx.T.Archcanvas.Untitled()
}

func (m *canvasModel) Title(ctx *ui.Context) (string, ui.Badge) {
	return m.title(ctx), ui.Badge{}
}

func (m *canvasModel) ToolStatus(ctx *ui.Context) string {
	t := ctx.T.Archcanvas
	switch {
	case m.sink == nil:
		return t.StatusNew()
	case m.shared() != nil:
		return t.StatusRoom(m.title(ctx), 1+len(m.shared().peers))
	case m.sink.Status() == saveCopy:
		return t.StatusCopy(m.title(ctx))
	case m.sink.Status() == saveUnsaved:
		return t.StatusNew()
	}
	return t.StatusPersonal(m.title(ctx))
}

// cleanText makes user text one safe line.
func cleanText(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ").Replace(s)
	return strings.TrimSpace(textwidth.Sanitize(s))
}
