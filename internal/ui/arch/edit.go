package arch

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/ui"
)

// Grid steps (spec C2.7): node positions snap to a 2×1 grid.
const (
	gridX = 2
	gridY = 1
	// spacing kept around a new node (the arrange pitch's gaps)
	gapX = 4
	gapY = 2
	// a wrapping box's margins around its nodes (archdoc.Arrange's padding)
	wrapPadX    = 2
	wrapPadTop  = 2
	wrapPadBott = 1
	// a new empty box: room for two by two nodes
	newBoxW = 2*(archdoc.NodeW+gapX) + 2*wrapPadX
	newBoxH = 2*(archdoc.NodeH+gapY) + wrapPadTop + wrapPadBott
)

// commit is the one path of every edit of mine: the sink applies it, the undo
// log records it, and the design is analysed again.
func (m *canvasModel) commit(ctx *ui.Context, ch []archdoc.Change) tea.Cmd {
	if len(ch) == 0 || m.sink == nil {
		return nil
	}
	if ro, _ := m.sink.ReadOnly(); ro {
		return ui.Toast(ctx.T.Archcanvas.ReadOnlyCopy(), ui.ToastInfo)
	}
	before := m.sink.Objects()
	cmd := m.sink.Apply(ch)
	m.undo.Record(before, ch)
	m.rebuild(ctx)
	return tea.Batch(cmd, m.settleLater(ctx))
}

// replay applies undo or redo changes (not recorded again).
func (m *canvasModel) replay(ctx *ui.Context, ch []archdoc.Change) tea.Cmd {
	if len(ch) == 0 || m.sink == nil {
		return nil
	}
	cmd := m.sink.Apply(ch)
	m.rebuild(ctx)
	return tea.Batch(cmd, m.settleLater(ctx))
}

func (m *canvasModel) undoStep(ctx *ui.Context, redo bool) tea.Cmd {
	if ro, _ := m.sink.ReadOnly(); ro {
		return ui.Toast(ctx.T.Archcanvas.ReadOnlyCopy(), ui.ToastInfo)
	}
	if redo {
		return m.replay(ctx, m.undo.Redo(m.sink.Objects()))
	}
	return m.replay(ctx, m.undo.Undo(m.sink.Objects()))
}

// edit runs fn on a copy of the design and commits the difference.
func (m *canvasModel) edit(ctx *ui.Context, fn func(d *archdoc.Design)) tea.Cmd {
	before := m.design.ToObjects()
	d, _ := archdoc.FromObjects(before, nil)
	if d == nil {
		return nil
	}
	fn(d)
	return m.commit(ctx, archdoc.DiffObjects(before, d.ToObjects()))
}

// readOnly is the toast for an edit key on a design that cannot be edited.
func (m *canvasModel) readOnly(ctx *ui.Context) (bool, tea.Cmd) {
	if m.sink == nil {
		return true, nil
	}
	if ro, _ := m.sink.ReadOnly(); ro {
		return true, ui.Toast(ctx.T.Archcanvas.ReadOnlyCopy(), ui.ToastInfo)
	}
	return false, nil
}

func findNode(d *archdoc.Design, id string) *archdoc.Node {
	for i := range d.Nodes {
		if d.Nodes[i].ID == id {
			return &d.Nodes[i]
		}
	}
	return nil
}

func findBox(d *archdoc.Design, id string) *archdoc.Box {
	for i := range d.Boxes {
		if d.Boxes[i].ID == id {
			return &d.Boxes[i]
		}
	}
	return nil
}

// boxDepth is 1 for a top-level box (parent cycles stop at the box count).
func boxDepth(d *archdoc.Design, b *archdoc.Box) int {
	depth := 1
	for b.Parent != "" && depth <= len(d.Boxes) {
		p := findBox(d, b.Parent)
		if p == nil {
			break
		}
		depth++
		b = p
	}
	return depth
}

// boxAround is the innermost box whose inside holds a node at (x, y) whole
// ("" = none): the deepest one, then the smallest, then the lowest ID.
func boxAround(d *archdoc.Design, x, y int) string {
	best, bestDepth, bestArea := "", 0, 0
	for i := range d.Boxes {
		b := &d.Boxes[i]
		r := b.Rect
		if x < r[0]+1 || y < r[1]+1 || x+archdoc.NodeW > r[0]+r[2]-1 || y+archdoc.NodeH > r[1]+r[3]-1 {
			continue
		}
		depth, area := boxDepth(d, b), r[2]*r[3]
		if best == "" || depth > bestDepth || depth == bestDepth && (area < bestArea || area == bestArea && b.ID < best) {
			best, bestDepth, bestArea = b.ID, depth, area
		}
	}
	return best
}

// descendants are box id and every box inside it.
func descendants(d *archdoc.Design, id string) map[string]bool {
	in := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, b := range d.Boxes {
			if !in[b.ID] && b.Parent != "" && in[b.Parent] {
				in[b.ID] = true
				changed = true
			}
		}
	}
	return in
}

// moveItems moves the nodes and boxes ids by (dx, dy): a box takes everything
// inside it along; a moved node lands in the innermost box around it.
func moveItems(d *archdoc.Design, ids []string, dx, dy int) {
	boxes := map[string]bool{}
	nodes := map[string]bool{}
	for _, id := range ids {
		if findBox(d, id) != nil {
			for b := range descendants(d, id) {
				boxes[b] = true
			}
		} else if findNode(d, id) != nil {
			nodes[id] = true
		}
	}
	for i := range d.Boxes {
		if boxes[d.Boxes[i].ID] {
			d.Boxes[i].Rect[0] += dx
			d.Boxes[i].Rect[1] += dy
		}
	}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		switch {
		case n.Parent != "" && boxes[n.Parent]:
			n.Pos[0] += dx
			n.Pos[1] += dy
		case nodes[n.ID]:
			n.Pos[0] += dx
			n.Pos[1] += dy
			n.Parent = boxAround(d, n.Pos[0], n.Pos[1])
		}
	}
}

// free reports whether a node at (x, y) keeps the gap to every other node.
func free(d *archdoc.Design, x, y int, skip string) bool {
	for _, n := range d.Nodes {
		if n.ID == skip {
			continue
		}
		if x < n.Pos[0]+archdoc.NodeW+gapX && n.Pos[0] < x+archdoc.NodeW+gapX &&
			y < n.Pos[1]+archdoc.NodeH+gapY && n.Pos[1] < y+archdoc.NodeH+gapY {
			return false
		}
	}
	return true
}

func snapX(x int) int {
	if x%gridX != 0 {
		x -= ((x % gridX) + gridX) % gridX
	}
	return x
}

// freeSpot is the empty place nearest to (x, y) for a new node: to the right,
// below, left, above, then rings further out (deterministic).
func freeSpot(d *archdoc.Design, x, y int) (int, int) {
	x = snapX(x)
	if free(d, x, y, "") {
		return x, y
	}
	px, py := archdoc.NodeW+gapX, archdoc.NodeH+gapY
	for ring := 1; ring <= 64; ring++ {
		for _, o := range ringOffsets(ring) {
			cx, cy := x+o[0]*px, y+o[1]*py
			if free(d, cx, cy, "") {
				return cx, cy
			}
		}
	}
	return x, y
}

// ringOffsets are the cells of the square ring r around the origin, starting
// right, then below, left and above.
func ringOffsets(r int) [][2]int {
	out := [][2]int{{r, 0}, {0, r}, {-r, 0}, {0, -r}}
	for i := -r; i <= r; i++ {
		for _, o := range [][2]int{{r, i}, {i, r}, {-r, i}, {i, -r}} {
			if !slices.Contains(out, o) {
				out = append(out, o)
			}
		}
	}
	return out
}

// anchor is where a new part goes: next to the selected node, else at the top
// left of what the canvas shows.
func (m *canvasModel) anchor() (int, int) {
	if n := m.node(m.sel); n != nil {
		return n.Pos[0] + archdoc.NodeW + gapX, n.Pos[1]
	}
	if b := m.box(m.sel); b != nil {
		return b.Rect[0] + wrapPadX, b.Rect[1] + wrapPadTop
	}
	x, y := m.vp.ToWorld(2, 1)
	return x, y
}

// addPart adds palette item it next to the selection and selects it.
func (m *canvasModel) addPart(ctx *ui.Context, it paletteItem) tea.Cmd {
	if ro, cmd := m.readOnly(ctx); ro {
		return cmd
	}
	ax, ay := m.anchor()
	var id string
	cmd := m.edit(ctx, func(d *archdoc.Design) {
		if it.box != "" {
			id = archdoc.NewID('b')
			x, y := ax, ay
			if len(d.Nodes)+len(d.Boxes) > 0 {
				bx, by, bw, _ := m.scene.Bounds()
				x, y = bx+bw+gapX, by
			}
			d.Boxes = append(d.Boxes, archdoc.Box{ID: id, Kind: it.box, Label: boxKindName(ctx.T, it.box), Rect: [4]int{snapX(x), y, newBoxW, newBoxH}})
			return
		}
		n := archdoc.Defaults(it.node)
		id = archdoc.NewID('n')
		n.ID, n.Label = id, kindName(ctx.T, it.node)
		n.Pos[0], n.Pos[1] = freeSpot(d, ax, ay)
		n.Parent = boxAround(d, n.Pos[0], n.Pos[1])
		d.Nodes = append(d.Nodes, n)
	})
	if m.exists(id) {
		m.sel = id
		m.reveal(id)
	}
	return cmd
}

// connect adds an edge from → to (once).
func (m *canvasModel) connect(ctx *ui.Context, from, to string) tea.Cmd {
	if from == "" || to == "" || from == to || m.node(from) == nil || m.node(to) == nil {
		return nil
	}
	for _, e := range m.design.Edges {
		if e.From == from && e.To == to {
			return ui.Toast(ctx.T.Archcanvas.ConnectExists(), ui.ToastInfo)
		}
	}
	return m.edit(ctx, func(d *archdoc.Design) {
		e := archdoc.DefaultEdge()
		e.ID, e.From, e.To = archdoc.NewID('e'), from, to
		d.Edges = append(d.Edges, e)
	})
}

// wrap puts nodes ids in a new box of kind k and selects the box.
func (m *canvasModel) wrap(ctx *ui.Context, ids []string, k archdoc.BoxKind) tea.Cmd {
	if len(ids) == 0 {
		return nil
	}
	id := archdoc.NewID('b')
	cmd := m.edit(ctx, func(d *archdoc.Design) {
		x0, y0, x1, y1 := 0, 0, 0, 0
		parent, first := "", true
		var in []*archdoc.Node
		for _, nid := range ids {
			n := findNode(d, nid)
			if n == nil {
				continue
			}
			in = append(in, n)
			if first {
				x0, y0, x1, y1, parent, first = n.Pos[0], n.Pos[1], n.Pos[0]+archdoc.NodeW, n.Pos[1]+archdoc.NodeH, n.Parent, false
				continue
			}
			x0, y0 = min(x0, n.Pos[0]), min(y0, n.Pos[1])
			x1, y1 = max(x1, n.Pos[0]+archdoc.NodeW), max(y1, n.Pos[1]+archdoc.NodeH)
			if n.Parent != parent {
				parent = ""
			}
		}
		if len(in) == 0 {
			return
		}
		if p := findBox(d, parent); p == nil || !nestOK(k, p.Kind) {
			parent = ""
		}
		x := snapX(x0 - wrapPadX)
		d.Boxes = append(d.Boxes, archdoc.Box{ID: id, Kind: k, Label: boxKindName(ctx.T, k), Parent: parent,
			Rect: [4]int{x, y0 - wrapPadTop, x1 + wrapPadX - x, y1 - y0 + wrapPadTop + wrapPadBott}})
		for _, n := range in {
			n.Parent = id
		}
	})
	if m.exists(id) {
		m.sel = id
	}
	return cmd
}

// nestOK is archdoc's nesting rule (AZ ⊂ VPC ⊂ region).
func nestOK(child, parent archdoc.BoxKind) bool {
	switch child {
	case archdoc.BoxVPC:
		return parent == archdoc.BoxRegion
	case archdoc.BoxAZ:
		return parent == archdoc.BoxVPC
	}
	return false
}

// remove deletes ids in one step: a node takes its edges along (and the faults
// aimed at them); a box leaves what is in it to its own parent.
func (m *canvasModel) remove(ctx *ui.Context, ids []string) tea.Cmd {
	if len(ids) == 0 {
		return nil
	}
	gone := map[string]bool{}
	for _, id := range ids {
		gone[id] = true
	}
	cmd := m.edit(ctx, func(d *archdoc.Design) {
		lift := map[string]string{} // removed box → its parent
		for _, b := range d.Boxes {
			if gone[b.ID] {
				lift[b.ID] = b.Parent
			}
		}
		up := func(p string) string { // the nearest parent that stays
			for range len(lift) + 1 {
				q, ok := lift[p]
				if !ok {
					return p
				}
				p = q
			}
			return ""
		}
		d.Boxes = slices.DeleteFunc(d.Boxes, func(b archdoc.Box) bool { return gone[b.ID] })
		for i := range d.Boxes {
			d.Boxes[i].Parent = up(d.Boxes[i].Parent)
		}
		d.Nodes = slices.DeleteFunc(d.Nodes, func(n archdoc.Node) bool { return gone[n.ID] })
		for i := range d.Nodes {
			n := &d.Nodes[i]
			n.Parent = up(n.Parent)
			if gone[n.Params.FailoverTo] {
				n.Params.FailoverTo = ""
			}
		}
		d.Edges = slices.DeleteFunc(d.Edges, func(e archdoc.Edge) bool {
			if gone[e.From] || gone[e.To] {
				gone[e.ID] = true
			}
			return gone[e.ID]
		})
		d.Faults = slices.DeleteFunc(d.Faults, func(f archdoc.Fault) bool { return gone[f.Target] })
	})
	if !m.exists(m.sel) {
		m.sel = ""
	}
	return cmd
}

// move moves ids by (dx, dy) world cells in one step.
func (m *canvasModel) move(ctx *ui.Context, ids []string, dx, dy int) tea.Cmd {
	if len(ids) == 0 || dx == 0 && dy == 0 {
		return nil
	}
	return m.edit(ctx, func(d *archdoc.Design) { moveItems(d, ids, dx, dy) })
}

// arrange lays the whole design out again (archdoc.Arrange).
func (m *canvasModel) arrange(ctx *ui.Context) tea.Cmd {
	if ro, cmd := m.readOnly(ctx); ro {
		return cmd
	}
	cmd := m.commit(ctx, archdoc.Arrange(m.design))
	m.fit()
	return cmd
}

// reveal scrolls the viewport so the node or box id is on the canvas
// (centred when it was off it).
func (m *canvasModel) reveal(id string) {
	x, y, w, h := 0, 0, archdoc.NodeW, archdoc.NodeH
	switch {
	case m.node(id) != nil:
		n := m.node(id)
		x, y = n.Pos[0], n.Pos[1]
	case m.box(id) != nil:
		b := m.box(id)
		x, y, w, h = b.Rect[0], b.Rect[1], b.Rect[2], b.Rect[3]
	default:
		return
	}
	v := m.vp
	sx, sy := v.ToScreen(x, y)
	ex, ey := v.ToScreen(x+w, y+h)
	if sx >= 0 && sy >= 0 && ex <= v.W && ey <= v.H {
		return
	}
	// centre: the world point at the canvas middle becomes the item's middle
	cx, cy := v.ToWorld(v.W/2, v.H/2)
	m.vp.X += x + w/2 - cx
	m.vp.Y += y + h/2 - cy
}

// selection is what an edit key acts on: the multi-selection, else the
// selected node or box.
func (m *canvasModel) selection() []string {
	if len(m.multi) > 0 {
		out := make([]string, 0, len(m.multi))
		for id := range m.multi {
			out = append(out, id)
		}
		slices.Sort(out)
		return out
	}
	if m.sel != "" {
		return []string{m.sel}
	}
	return nil
}
