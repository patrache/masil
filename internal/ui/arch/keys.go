package arch

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// editMode is what the canvas keys do right now.
type editMode int

const (
	modeNormal  editMode = iota
	modeMove             // m: arrows move a ghost, enter commits, esc cancels
	modeConnect          // c: hjkl picks the target, enter connects
	modeMulti            // v: hjkl adds to the selection, space toggles
	modeFind             // /: typing a node name, enter jumps
	modePick             // a (part) or g (box kind): the palette cursor
	modeHelp             // ?: the tool's help over the canvas
	modePanel            // e: the settings panel has the keys (P3 Task 18)
)

var (
	_ ui.KeyHandler    = (*canvasModel)(nil)
	_ ui.InputCapturer = (*canvasModel)(nil)
)

// pan step in screen cells (ctrl+arrows).
const panX, panY = 8, 3

// dirOf maps h j k l and the arrows to a direction.
func dirOf(s string) (archdraw.Dir, bool) {
	switch s {
	case "h", "left":
		return archdraw.DirLeft, true
	case "j", "down":
		return archdraw.DirDown, true
	case "k", "up":
		return archdraw.DirUp, true
	case "l", "right":
		return archdraw.DirRight, true
	}
	return 0, false
}

// gridStep is one grid cell in direction d (world cells).
func gridStep(d archdraw.Dir) (int, int) {
	switch d {
	case archdraw.DirLeft:
		return -gridX, 0
	case archdraw.DirRight:
		return gridX, 0
	case archdraw.DirUp:
		return 0, -gridY
	}
	return 0, gridY
}

// CapturesInput: typing a find query never reaches the soft globals.
func (m *canvasModel) CapturesInput() bool {
	return m.mode == modeFind || m.faults != nil || (m.mode == modePanel && (m.panel.editing || m.panel.pick != nil))
}

// HandleKey is the canvas keyboard (spec C2.7). Keys it does not use
// (ctrl+k, tab, ctrl+t, [ ], the last esc …) go on to the app's globals.
func (m *canvasModel) HandleKey(ctx *ui.Context, k tea.KeyPressMsg) (bool, tea.Cmd) {
	handled, cmd := m.handleKey(ctx, k)
	m.syncPresence()
	return handled, cmd
}

func (m *canvasModel) handleKey(ctx *ui.Context, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.design == nil || m.sink == nil {
		return false, nil
	}
	s := k.String()
	if sh := m.shared(); sh != nil && sh.preview != nil && s == "esc" && m.mode == modeNormal && m.menu == nil && m.faults == nil {
		return true, m.showVersion(ctx, sh, nil) // the preview ends: back to now
	}
	if m.regs.View == viewSummary {
		return m.tinyKey(ctx, s)
	}
	if m.faults != nil {
		return m.faultKey(ctx, k, s)
	}
	if m.menu != nil {
		return m.menuKey(ctx, s)
	}
	switch m.mode {
	case modeFind:
		return m.findKey(ctx, k, s)
	case modePick:
		return m.pickKey(ctx, s)
	case modeMove:
		return m.moveKey(ctx, s)
	case modeConnect:
		return m.connectKey(ctx, s)
	case modeHelp:
		if s == "esc" || s == "?" || s == "q" {
			m.mode = modeNormal
		}
		return !reservedGlobal(s), nil
	case modePanel:
		if handled, cmd, ok := m.panelKey(ctx, k, s); ok {
			return handled, cmd
		}
		if s == "d" || s == "delete" {
			// the form's delete leaves the form (its item goes)
			m.closePanel()
		}
	case modeMulti:
		if handled, cmd, ok := m.multiKey(ctx, s); ok {
			return handled, cmd
		}
	}
	return m.normalKey(ctx, s)
}

// reservedGlobal are keys the canvas never takes, whatever its mode.
func reservedGlobal(s string) bool {
	switch s {
	case "ctrl+k", "ctrl+t", "ctrl+c", "tab", "shift+tab", "[", "]", "ctrl+w", "ctrl+f", "ctrl+n":
		return true
	}
	return false
}

// editKeys are the keys that change the design (the tiny summary refuses them).
var editKeys = []string{"a", "c", "d", "g", "m", "x", "e", "p", "v", "u", "ctrl+r", "=", "H", "J", "K", "L", "/"}

func (m *canvasModel) tinyKey(ctx *ui.Context, s string) (bool, tea.Cmd) {
	switch {
	case s == "space":
		return true, m.toggleRun(ctx)
	case s == "<" || s == ">":
		return true, m.speed(s == ">")
	case slices.Contains(editKeys, s):
		return true, ui.Toast(ctx.T.Archcanvas.TooSmallEdit(), ui.ToastInfo)
	}
	return false, nil
}

func (m *canvasModel) normalKey(ctx *ui.Context, s string) (bool, tea.Cmd) {
	if (s == "left" || s == "right") && m.pb.tl != nil && !m.pb.running {
		// a paused run: the arrows move along its time bar (h l still select)
		if s == "left" {
			m.stepTime(-1)
		} else {
			m.stepTime(1)
		}
		return true, nil
	}
	if d, ok := dirOf(s); ok {
		m.stepSelection(d)
		return true, nil
	}
	switch s {
	case "H", "J", "K", "L":
		d, _ := dirOf(strings.ToLower(s))
		if ro, cmd := m.readOnly(ctx); ro {
			return true, cmd
		}
		dx, dy := gridStep(d)
		return true, m.move(ctx, m.selection(), dx, dy)
	case "m":
		if ro, cmd := m.readOnly(ctx); ro {
			return true, cmd
		}
		if len(m.selection()) > 0 && m.movable() {
			m.mode, m.moveDX, m.moveDY = modeMove, 0, 0
		}
		return true, nil
	case "a":
		if ro, cmd := m.readOnly(ctx); ro {
			return true, cmd
		}
		m.startPick(false)
		return true, nil
	case "g":
		if ro, cmd := m.readOnly(ctx); ro {
			return true, cmd
		}
		if len(m.selectedNodes()) > 0 {
			m.startPick(true)
		}
		return true, nil
	case "c":
		if ro, cmd := m.readOnly(ctx); ro {
			return true, cmd
		}
		if m.node(m.sel) != nil {
			m.mode, m.cursor = modeConnect, m.sel
		}
		return true, nil
	case "v":
		if m.regs.View == viewList || m.node(m.sel) == nil {
			// the list view, or no node selected: v switches the canvas and list views
			m.toggleView()
			return true, nil
		}
		m.mode = modeMulti
		m.multi = map[string]bool{}
		if m.sel != "" {
			m.multi[m.sel] = true
		}
		m.cursor = m.sel
		return true, nil
	case "d", "delete", "backspace":
		if ro, cmd := m.readOnly(ctx); ro {
			return true, cmd
		}
		ids := m.selection()
		m.leaveMulti()
		return true, m.remove(ctx, ids)
	case "u":
		return true, m.undoStep(ctx, false)
	case "ctrl+r":
		return true, m.undoStep(ctx, true)
	case "e", "p":
		// e opens the settings panel; p toggles it (a drawer pushes back in)
		if s == "p" && m.mode == modePanel {
			m.mode = modeNormal
			if m.regs.PanelDrawer {
				m.drawer.panel = false
				m.layout()
			}
			return true, nil
		}
		m.openPanel()
		return true, nil
	case "enter":
		if m.regs.View == viewList && m.sel != "" {
			// the list view edits with a stacked form (the canvas keeps enter global)
			m.openPanel()
			return true, nil
		}
		return false, nil
	case "x":
		if m.sel == "" {
			return true, ui.Toast(ctx.T.Archcanvas.FaultNone(), ui.ToastInfo)
		}
		return true, m.openFaults(ctx, m.sel)
	case "ctrl+left", "ctrl+right", "ctrl+up", "ctrl+down":
		d, _ := dirOf(strings.TrimPrefix(s, "ctrl+"))
		m.pan(d)
		return true, nil
	case "0":
		if m.sel != "" {
			m.centre(m.sel)
		} else {
			m.fit()
		}
		return true, nil
	case "+", "-":
		m.zoom(s == "+")
		return true, nil
	case "=":
		return true, m.arrange(ctx)
	case "/":
		m.mode, m.find = modeFind, ""
		return true, nil
	case "space":
		return true, m.toggleRun(ctx)
	case "<", ">":
		return true, m.speed(s == ">")
	case "!":
		m.drawer.checks = !m.drawer.checks
		m.layout()
		return true, nil
	case "?":
		m.mode = modeHelp
		return true, nil
	}
	return false, nil
}

// toggleView switches between the canvas and the list view.
func (m *canvasModel) toggleView() {
	m.drawer.viewSet = true
	if m.regs.View == viewList {
		m.drawer.view = viewCanvas
	} else {
		m.drawer.view = viewList
	}
	m.layout()
}

// movable: a node or box selection (an edge does not move).
func (m *canvasModel) movable() bool {
	for _, id := range m.selection() {
		if m.node(id) == nil && m.box(id) == nil {
			return false
		}
	}
	return true
}

// selectedNodes are the selected node ids.
func (m *canvasModel) selectedNodes() []string {
	var out []string
	for _, id := range m.selection() {
		if m.node(id) != nil {
			out = append(out, id)
		}
	}
	return out
}

// next is the node (canvas) or row (list) from id in direction d.
func (m *canvasModel) next(id string, d archdraw.Dir) string {
	if m.regs.View == viewList {
		rows := m.listRows()
		i := slices.IndexFunc(rows, func(r listRow) bool { return r.id == id && id != "" })
		step := 0
		switch d {
		case archdraw.DirDown:
			step = 1
		case archdraw.DirUp:
			step = -1
		}
		if step == 0 {
			return id
		}
		if i < 0 {
			i = m.listSel - step
		}
		for j := i + step; j >= 0 && j < len(rows); j += step {
			if rows[j].id != "" {
				m.listSel = j
				return rows[j].id
			}
		}
		return id
	}
	if m.node(id) == nil {
		// nothing (or a box or edge) selected: start at the top-left node
		ids := m.scene.NodeIDs()
		best := ""
		for _, n := range ids {
			x, y, _ := m.scene.NodeAt(n)
			if best == "" {
				best = n
				continue
			}
			bx, by, _ := m.scene.NodeAt(best)
			if y < by || y == by && x < bx {
				best = n
			}
		}
		return best
	}
	if n := m.scene.Nearest(id, d); n != "" {
		return n
	}
	return id
}

func (m *canvasModel) stepSelection(d archdraw.Dir) {
	if n := m.next(m.sel, d); n != "" {
		m.sel = n
		m.reveal(n)
	}
}

// startPick opens the palette cursor: parts (a) or box kinds (g).
func (m *canvasModel) startPick(boxes bool) {
	m.mode, m.pickBoxes, m.pick = modePick, boxes, 0
	if boxes {
		m.pick = len(archdoc.NodeKinds())
	}
	m.pickOpened = false
	if m.regs.PaletteDrawer && !m.drawer.palette {
		m.drawer.palette, m.pickOpened = true, true
		m.layout()
	}
}

func (m *canvasModel) endPick() {
	m.mode = modeNormal
	if m.pickOpened {
		m.drawer.palette, m.pickOpened = false, false
		m.layout()
	}
	if m.multi != nil {
		m.mode = modeMulti
	}
}

func (m *canvasModel) pickKey(ctx *ui.Context, s string) (bool, tea.Cmd) {
	items := paletteItems()
	lo, hi := 0, len(items)-1
	if m.pickBoxes {
		lo = len(archdoc.NodeKinds())
	}
	switch s {
	case "j", "down":
		m.pick = min(m.pick+1, hi)
	case "k", "up":
		m.pick = max(m.pick-1, lo)
	case "esc":
		m.endPick()
	case "enter":
		it := items[min(max(m.pick, lo), hi)]
		boxes := m.pickBoxes
		ids := m.selectedNodes()
		m.endPick()
		if boxes {
			m.leaveMulti()
			return true, m.wrap(ctx, ids, it.box)
		}
		return true, m.addPart(ctx, it)
	default:
		return !reservedGlobal(s), nil
	}
	return true, nil
}

func (m *canvasModel) moveKey(ctx *ui.Context, s string) (bool, tea.Cmd) {
	if d, ok := dirOf(strings.ToLower(s)); ok {
		dx, dy := gridStep(d)
		m.moveDX += dx
		m.moveDY += dy
		return true, nil
	}
	switch s {
	case "esc":
		m.moveDX, m.moveDY = 0, 0
		m.mode = modeNormal
		if m.multi != nil {
			m.mode = modeMulti
		}
	case "enter":
		ids, dx, dy := m.selection(), m.moveDX, m.moveDY
		m.moveDX, m.moveDY = 0, 0
		m.leaveMulti()
		m.mode = modeNormal
		return true, m.move(ctx, ids, dx, dy)
	default:
		return !reservedGlobal(s), nil
	}
	return true, nil
}

func (m *canvasModel) connectKey(ctx *ui.Context, s string) (bool, tea.Cmd) {
	if d, ok := dirOf(s); ok {
		m.cursor = m.next(m.cursor, d)
		return true, nil
	}
	switch s {
	case "esc":
		m.mode, m.cursor = modeNormal, ""
	case "enter":
		from, to := m.sel, m.cursor
		m.mode, m.cursor = modeNormal, ""
		return true, m.connect(ctx, from, to)
	default:
		return !reservedGlobal(s), nil
	}
	return true, nil
}

// multiKey handles the multi-selection's own keys; ok=false passes the key
// to the normal keys (they act on the whole selection).
func (m *canvasModel) multiKey(_ *ui.Context, s string) (bool, tea.Cmd, bool) {
	if d, ok := dirOf(s); ok {
		if n := m.next(m.cursor, d); n != "" {
			m.cursor = n
			m.multi[n] = true
			m.sel = n
			m.reveal(n)
		}
		return true, nil, true
	}
	switch s {
	case "space":
		if m.cursor != "" {
			if m.multi[m.cursor] {
				delete(m.multi, m.cursor)
			} else {
				m.multi[m.cursor] = true
			}
		}
		return true, nil, true
	case "esc", "v":
		m.leaveMulti()
		return true, nil, true
	}
	return false, nil, false
}

func (m *canvasModel) leaveMulti() {
	m.multi = nil
	if m.mode == modeMulti {
		m.mode = modeNormal
	}
}

func (m *canvasModel) findKey(ctx *ui.Context, k tea.KeyPressMsg, s string) (bool, tea.Cmd) {
	switch s {
	case "esc":
		m.mode, m.find = modeNormal, ""
		return true, nil
	case "enter":
		q := m.find
		m.mode, m.find = modeNormal, ""
		return true, m.findNode(ctx, q)
	case "backspace":
		if r := []rune(m.find); len(r) > 0 {
			m.find = string(r[:len(r)-1])
		}
		return true, nil
	}
	if reservedGlobal(s) {
		return false, nil
	}
	if k.Text != "" && textwidth.Width(m.find) < 40 {
		m.find += textwidth.Sanitize(k.Text)
	}
	return true, nil
}

// findNode selects the first node (then box) whose label or kind holds q and
// scrolls to it.
func (m *canvasModel) findNode(ctx *ui.Context, q string) tea.Cmd {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	match := func(label, kind string) bool {
		return strings.Contains(strings.ToLower(label), q) || strings.Contains(strings.ToLower(kind), q)
	}
	ids := m.scene.NodeIDs()
	for _, id := range ids {
		n := m.node(id)
		if n != nil && match(m.label(id), string(n.Kind)+" "+kindName(ctx.T, n.Kind)) {
			m.sel = id
			m.centre(id)
			return nil
		}
	}
	boxes := make([]string, 0, len(m.design.Boxes))
	for _, b := range m.design.Boxes {
		boxes = append(boxes, b.ID)
	}
	slices.Sort(boxes)
	for _, id := range boxes {
		if b := m.box(id); match(m.label(id), string(b.Kind)) {
			m.sel = id
			m.centre(id)
			return nil
		}
	}
	return ui.Toast(ctx.T.Archcanvas.FindNone(textwidth.Truncate(q, 20, "…")), ui.ToastInfo)
}

// centre scrolls so id is in the middle of the canvas.
func (m *canvasModel) centre(id string) {
	m.vp.X, m.vp.Y = 1<<20, 1<<20 // off screen: reveal centres
	m.reveal(id)
}

// pan moves the viewport by a screen step in direction d.
func (m *canvasModel) pan(d archdraw.Dir) {
	sx, sy := 0, 0
	switch d {
	case archdraw.DirLeft:
		sx = -panX
	case archdraw.DirRight:
		sx = panX
	case archdraw.DirUp:
		sy = -panY
	default:
		sy = panY
	}
	x0, y0 := m.vp.ToWorld(0, 0)
	x1, y1 := m.vp.ToWorld(sx, sy)
	m.vp.X += x1 - x0
	m.vp.Y += y1 - y0
}

// zoom steps the zoom in (more detail) or out, keeping the canvas middle.
func (m *canvasModel) zoom(in bool) {
	cx, cy := m.vp.ToWorld(m.vp.W/2, m.vp.H/2)
	if in {
		m.vp.Zoom = m.vp.Zoom.In()
	} else {
		m.vp.Zoom = m.vp.Zoom.Out()
	}
	m.zoomSet = true
	nx, ny := m.vp.ToWorld(m.vp.W/2, m.vp.H/2)
	m.vp.X += cx - nx
	m.vp.Y += cy - ny
}

// speeds are the playback speeds < > step through.
var speeds = []int{1, 2, 4, 8, 16}

func (m *canvasModel) speed(up bool) tea.Cmd {
	i := slices.Index(speeds, m.speedX)
	if i < 0 {
		i = 0
	}
	if up {
		i = min(i+1, len(speeds)-1)
	} else {
		i = max(i-1, 0)
	}
	m.speedX = speeds[i]
	return nil
}
