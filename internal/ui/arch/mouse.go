package arch

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/ui"
)

var _ ui.DragHandler = (*canvasModel)(nil)

// gestureKind is what a left-button drag does.
type gestureKind int

const (
	gestureMove        gestureKind = iota // a node: ghost, drop = move (into the box under it)
	gestureBoxMove                        // a box: with everything inside it
	gestureConnect                        // from a ◆ handle: drop on a node = edge
	gesturePan                            // empty canvas: scroll
	gesturePaletteDrop                    // a palette row: drop on the canvas = add there
)

// gesture is the drag in progress (pane cells).
type gesture struct {
	kind           gestureKind
	id             string      // the node or box dragged / connected from
	item           paletteItem // gesturePaletteDrop
	startX, startY int
	lastX, lastY   int
	moved          bool
	blocked        bool // narrow canvas: the toast was shown, nothing happens
}

// wheel step in screen rows.
const wheelRows = 3

// cancelGesture drops the drag without changing anything.
func (m *canvasModel) cancelGesture() { m.gest = nil }

// ClearDrag is the app cutting the gesture short (an overlay, a layout change).
func (m *canvasModel) ClearDrag() { m.cancelGesture() }

// inCanvas reports whether pane cell p is on the canvas and its canvas cell.
func (m *canvasModel) inCanvas(p ui.Point) (int, int, bool) {
	c := m.regs.Canvas
	return p.X - c.X, p.Y - c.Y, m.regs.View == viewCanvas && c.Contains(p.X, p.Y)
}

// HandleMouse: presses, the wheel and right clicks (spec C2.7 mouse table).
func (m *canvasModel) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, p ui.Point) tea.Cmd {
	if m.design == nil || m.sink == nil || m.regs.View == viewSummary {
		return nil
	}
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		return m.wheel(msg.Button, p)
	case tea.MouseClickMsg:
		switch msg.Button {
		case tea.MouseLeft:
			if m.faults != nil {
				m.faults = nil // a click leaves the fault menu
				return nil
			}
			if m.menu != nil {
				return m.menuClick(ctx, p)
			}
			return m.press(ctx, p)
		case tea.MouseRight:
			m.menu = nil
			return m.rightClick(p)
		}
	}
	return nil
}

func (m *canvasModel) wheel(b tea.MouseButton, p ui.Point) tea.Cmd {
	step := 0
	switch b {
	case tea.MouseWheelUp:
		step = -1
	case tea.MouseWheelDown:
		step = 1
	default:
		return nil
	}
	if m.regs.View == viewList {
		if m.regs.Canvas.Contains(p.X, p.Y) {
			m.listSel = max(m.listSel+step, 0)
		}
		return nil
	}
	if _, _, ok := m.inCanvas(p); ok {
		m.panCells(0, step*wheelRows)
	}
	return nil
}

// panCells scrolls the viewport by (sx, sy) screen cells.
func (m *canvasModel) panCells(sx, sy int) {
	x0, y0 := m.vp.ToWorld(0, 0)
	x1, y1 := m.vp.ToWorld(sx, sy)
	m.vp.X += x1 - x0
	m.vp.Y += y1 - y0
}

// paletteAt is the palette item drawn at pane cell p (ok=false: none).
func (m *canvasModel) paletteAt(ctx *ui.Context, p ui.Point) (paletteItem, bool) {
	r := m.regs.Palette
	if r.W == 0 || !r.Contains(p.X, p.Y) {
		return paletteItem{}, false
	}
	_, items := paletteRows(ctx.T)
	sel := -1
	if m.mode == modePick {
		sel = m.pick
	}
	off := 0
	for i, it := range items {
		if it == sel && sel >= 0 && i >= r.H {
			off = i - r.H + 1
		}
	}
	i := p.Y - r.Y + off
	if i < 0 || i >= len(items) || items[i] < 0 {
		return paletteItem{}, false
	}
	return paletteItems()[items[i]], true
}

// press is a left press: select what is under it and start its gesture.
func (m *canvasModel) press(ctx *ui.Context, p ui.Point) tea.Cmd {
	m.gest = nil
	if it, ok := m.paletteAt(ctx, p); ok {
		if m.mode == modePick {
			m.endPick()
		}
		m.gest = &gesture{kind: gesturePaletteDrop, item: it, startX: p.X, startY: p.Y, lastX: p.X, lastY: p.Y}
		return nil
	}
	if m.regs.View == viewList && m.regs.Canvas.Contains(p.X, p.Y) {
		rows := m.listRows()
		off := 0
		if m.listSel >= m.regs.Canvas.H {
			off = m.listSel - m.regs.Canvas.H + 1
		}
		if i := p.Y - m.regs.Canvas.Y + off; i >= 0 && i < len(rows) && rows[i].id != "" {
			m.listSel, m.sel = i, rows[i].id
		}
		return nil
	}
	sx, sy, ok := m.inCanvas(p)
	if !ok {
		return nil
	}
	if m.mode != modeNormal && m.mode != modePanel {
		m.mode, m.cursor, m.moveDX, m.moveDY = modeNormal, "", 0, 0
		m.multi = nil
	}
	sc, _ := m.visibleScene()
	hit := sc.HitTest(m.vp, sx, sy)
	g := &gesture{id: hit.ID, startX: p.X, startY: p.Y, lastX: p.X, lastY: p.Y}
	switch hit.Kind {
	case archdraw.HitZoomIn, archdraw.HitZoomOut:
		m.zoom(hit.Kind == archdraw.HitZoomIn)
		return nil
	case archdraw.HitHandle:
		m.sel, g.kind = hit.ID, gestureConnect
	case archdraw.HitNode:
		m.sel, g.kind = hit.ID, gestureMove
	case archdraw.HitBoxBorder:
		m.sel, g.kind = hit.ID, gestureBoxMove
	case archdraw.HitBox:
		m.sel, g.kind = hit.ID, gesturePan
	case archdraw.HitEdge:
		m.sel, g.kind = hit.ID, gesturePan
	default:
		m.sel, g.kind = "", gesturePan
	}
	m.gest = g
	return nil
}

// rightClick opens the menu of what is under p.
func (m *canvasModel) rightClick(p ui.Point) tea.Cmd {
	m.cancelGesture()
	sx, sy, ok := m.inCanvas(p)
	if !ok {
		return nil
	}
	sc, _ := m.visibleScene()
	switch hit := sc.HitTest(m.vp, sx, sy); hit.Kind {
	case archdraw.HitNode, archdraw.HitHandle, archdraw.HitBox, archdraw.HitBoxBorder, archdraw.HitEdge:
		m.openMenu(hit.ID, p.X, p.Y)
	}
	return nil
}

// editing reports whether gesture kind k changes the design.
func editing(k gestureKind) bool { return k != gesturePan }

// HandleDrag: the motions and the release of the gesture a press started.
func (m *canvasModel) HandleDrag(ctx *ui.Context, msg tea.MouseMsg, p ui.Point) tea.Cmd {
	g := m.gest
	if g == nil {
		return nil
	}
	_, release := msg.(tea.MouseReleaseMsg)
	if p.X != g.lastX || p.Y != g.lastY {
		var cmd tea.Cmd
		if !g.moved && !g.blocked && editing(g.kind) {
			if ro, c := m.readOnly(ctx); ro {
				g.blocked, cmd = true, c
			} else if m.regs.Canvas.W < dragMinW {
				g.blocked, cmd = true, ui.Toast(ctx.T.Archcanvas.NarrowNoDrag(), ui.ToastInfo)
			}
		}
		if g.kind == gesturePan {
			m.panCells(g.lastX-p.X, g.lastY-p.Y)
		}
		g.moved = true
		g.lastX, g.lastY = p.X, p.Y
		if !release {
			return cmd
		}
	}
	if !release {
		return nil
	}
	m.gest = nil
	if g.blocked {
		return nil
	}
	if !g.moved {
		if g.kind == gesturePaletteDrop { // a click on a part adds it next to the selection
			return m.addPart(ctx, g.item)
		}
		return nil
	}
	return m.drop(ctx, g, p)
}

// dragDelta is the gesture's offset in world cells, on the 2×1 grid.
func (m *canvasModel) dragDelta(g *gesture) (int, int) {
	x0, y0 := m.vp.ToWorld(g.startX-m.regs.Canvas.X, g.startY-m.regs.Canvas.Y)
	x1, y1 := m.vp.ToWorld(g.lastX-m.regs.Canvas.X, g.lastY-m.regs.Canvas.Y)
	return snapX(x1 - x0), y1 - y0
}

// drop commits the finished gesture as one change.
func (m *canvasModel) drop(ctx *ui.Context, g *gesture, p ui.Point) tea.Cmd {
	switch g.kind {
	case gestureMove, gestureBoxMove:
		if !m.exists(g.id) {
			return nil
		}
		dx, dy := m.dragDelta(g)
		return m.move(ctx, []string{g.id}, dx, dy)
	case gestureConnect:
		sx, sy, ok := m.inCanvas(p)
		if !ok {
			return nil
		}
		sc, _ := m.visibleScene()
		if hit := sc.HitTest(m.vp, sx, sy); hit.Kind == archdraw.HitNode || hit.Kind == archdraw.HitHandle {
			return m.connect(ctx, g.id, hit.ID)
		}
	case gesturePaletteDrop:
		sx, sy, ok := m.inCanvas(p)
		if !ok {
			return nil
		}
		wx, wy := m.vp.ToWorld(sx, sy)
		return m.addPartAt(ctx, g.item, snapX(wx), wy)
	}
	return nil
}

// gestureOverlay adds the drag in progress to ov.
func (m *canvasModel) gestureOverlay(ov *archdraw.Overlay) {
	g := m.gest
	if g == nil || !g.moved || g.blocked {
		return
	}
	switch g.kind {
	case gestureMove:
		if n := m.node(g.id); n != nil {
			dx, dy := m.dragDelta(g)
			x, y := n.Pos[0]+dx, n.Pos[1]+dy
			ov.Ghost = &archdraw.Ghost{ID: n.ID, X: x, Y: y, IntoBox: m.boxUnder(n.ID, x, y)}
		}
	case gestureBoxMove:
		if b := m.box(g.id); b != nil {
			dx, dy := m.dragDelta(g)
			ov.Ghost = &archdraw.Ghost{ID: b.ID, X: b.Rect[0] + dx, Y: b.Rect[1] + dy}
		}
	case gestureConnect:
		if m.node(g.id) != nil {
			wx, wy := m.vp.ToWorld(g.lastX-m.regs.Canvas.X, g.lastY-m.regs.Canvas.Y)
			ov.ConnectFrom, ov.ConnectTo = g.id, [2]int{wx, wy}
			sc, _ := m.visibleScene()
			if hit := sc.HitTest(m.vp, g.lastX-m.regs.Canvas.X, g.lastY-m.regs.Canvas.Y); hit.Kind == archdraw.HitNode && hit.ID != g.id {
				ov.Selected[hit.ID] = true
			}
		}
	}
}

// drawPaletteGhost draws the part being dragged from the palette at the pointer.
func (m *canvasModel) drawPaletteGhost(ctx *ui.Context, b *buf) {
	g := m.gest
	if g == nil || g.kind != gesturePaletteDrop || !g.moved || g.blocked {
		return
	}
	if _, _, ok := m.inCanvas(ui.Point{X: g.lastX, Y: g.lastY}); !ok {
		return
	}
	w, h := archdraw.NodeSize(m.vp.Zoom)
	c := m.regs.Canvas
	x0, y0 := g.lastX, g.lastY
	for y := y0; y < y0+h && y < c.Y+c.H; y++ {
		for x := x0; x < x0+w && x < c.X+c.W; x++ {
			ch := " "
			switch {
			case y == y0 || y == y0+h-1:
				ch = "╌"
			case x == x0 || x == x0+w-1:
				ch = "╎"
			}
			b.set(x, y, ch, 1, sAccBold)
		}
	}
	b.text(x0+1, y0, g.item.name(ctx.T), min(w-2, c.X+c.W-x0-1), sAccBold)
}

// addPartAt adds palette item it with its top-left corner at world (x, y).
func (m *canvasModel) addPartAt(ctx *ui.Context, it paletteItem, x, y int) tea.Cmd {
	if ro, cmd := m.readOnly(ctx); ro {
		return cmd
	}
	var id string
	cmd := m.edit(ctx, func(d *archdoc.Design) {
		if it.box != "" {
			id = archdoc.NewID('b')
			d.Boxes = append(d.Boxes, archdoc.Box{ID: id, Kind: it.box, Label: boxKindName(ctx.T, it.box), Rect: [4]int{x, y, newBoxW, newBoxH}})
			return
		}
		n := archdoc.Defaults(it.node)
		id = archdoc.NewID('n')
		n.ID, n.Label, n.Pos = id, kindName(ctx.T, it.node), [2]int{x, y}
		n.Parent = boxAround(d, x, y)
		d.Nodes = append(d.Nodes, n)
	})
	if m.exists(id) {
		m.sel = id
	}
	return cmd
}

// gestureGone cancels the gesture and the menu when their item left the design
// (someone else deleted it).
func (m *canvasModel) gestureGone() {
	if g := m.gest; g != nil && g.id != "" && g.kind != gesturePan && !m.exists(g.id) {
		m.gest = nil
	}
	if m.menu != nil && !m.exists(m.menu.target) {
		m.menu = nil
	}
	if m.multi != nil {
		for id := range m.multi {
			if !m.exists(id) {
				delete(m.multi, id)
			}
		}
	}
	if m.cursor != "" && !m.exists(m.cursor) {
		m.cursor = ""
		if m.mode == modeConnect {
			m.mode = modeNormal
		}
	}
	if m.mode == modeMove && !slices.ContainsFunc(m.selection(), m.exists) {
		m.mode, m.moveDX, m.moveDY = modeNormal, 0, 0
	}
}
