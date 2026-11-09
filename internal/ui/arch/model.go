package arch

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

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
	running        bool            // space: the timed run (P3 Task 19)
	speedX         int             // < >: the timed run's speed

	// mouse (mouse.go, contextmenu.go)
	gest *gesture // the drag in progress
	menu *ctxMenu // the open right-click menu
}

var (
	_ ui.Component    = (*canvasModel)(nil)
	_ ui.Titled       = (*canvasModel)(nil)
	_ ui.Helper       = (*canvasModel)(nil)
	_ ui.ToolStatuser = (*canvasModel)(nil)
)

// newCanvas is the tool's ui.ToolSpec.New: the sink is chosen in Init.
func newCanvas(_ *ui.Context, r ui.Route) ui.Component {
	return &canvasModel{route: r, hidden: map[archdraw.Zoom]*archdraw.Scene{}}
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
	default:
		// a new design; card copies (msg) and room documents (doc) arrive with
		// P3 Tasks 22 and 26 and start empty until then
		ro := q[queryMsg] != "" || q[queryDoc] != ""
		objs := archdoc.New("").ToObjects()
		if ro {
			m.setSink(ctx, &copySink{objs: objs})
		} else {
			m.setSink(ctx, &localSink{instance: m.route.ID, objs: objs, timers: ctx.Timers})
		}
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
	m.scene = archdraw.Build(d)
	m.hidden = map[archdraw.Zoom]*archdraw.Scene{}
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
	if m.sel != "" && !m.exists(m.sel) {
		m.sel = ""
	}
	m.gestureGone()
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
	case ui.LocaleChangedMsg:
		m.rebuild(ctx)
	case ui.ModeChangedMsg:
		m.cancelGesture()
		m.menu = nil
	case ui.FocusChangedMsg:
		if msg.RouteKey != m.route.Key() {
			m.cancelGesture()
		}
	}
	return nil
}

// runCommand runs an arch.* palette command on this canvas (the later tasks
// fill in share, export, versions, send_card and run).
func (m *canvasModel) runCommand(*ui.Context, string) tea.Cmd { return nil }

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
