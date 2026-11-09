package arch

import (
	"slices"
	"strings"

	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// listRow is one row of the list view: a box or a node (id), or a heading.
type listRow struct {
	id    string
	depth int
	box   bool
}

// listRows is the outline: boxes nested region > VPC > AZ with their nodes,
// then the nodes outside every box. Boxes on a parent cycle or with a missing
// parent count as top level; every box and node appears once.
func (m *canvasModel) listRows() []listRow {
	boxIDs := map[string]bool{}
	for _, b := range m.design.Boxes {
		boxIDs[b.ID] = true
	}
	children := map[string][]string{} // box id ("" = top) → child box ids
	for _, b := range m.design.Boxes {
		p := b.Parent
		if !boxIDs[p] || p == b.ID {
			p = ""
		}
		children[p] = append(children[p], b.ID)
	}
	nodesIn := map[string][]string{}
	for _, n := range m.design.Nodes {
		p := n.Parent
		if !boxIDs[p] {
			p = ""
		}
		nodesIn[p] = append(nodesIn[p], n.ID)
	}
	for _, v := range children {
		slices.Sort(v)
	}
	for _, v := range nodesIn {
		slices.Sort(v)
	}
	var out []listRow
	seen := map[string]bool{}
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, listRow{id: id, depth: depth, box: true})
		for _, n := range nodesIn[id] {
			out = append(out, listRow{id: n, depth: depth + 1})
		}
		for _, c := range children[id] {
			walk(c, depth+1)
		}
	}
	for _, c := range children[""] {
		walk(c, 0)
	}
	// boxes only reachable through a parent cycle
	for _, b := range m.design.Boxes {
		walk(b.ID, 0)
	}
	if top := nodesIn[""]; len(top) > 0 {
		out = append(out, listRow{depth: 0}) // heading: outside boxes
		for _, n := range top {
			out = append(out, listRow{id: n, depth: 1})
		}
	}
	return out
}

// listText is one row's text: "▾ label (kind)" or "label · 81% · p99 140ms ⚠".
func (m *canvasModel) listText(ctx *ui.Context, r listRow) (string, sty) {
	t := ctx.T.Archcanvas
	pad := strings.Repeat("  ", r.depth)
	if r.id == "" {
		return pad + t.ListOutside(), sDim
	}
	if r.box {
		if b := m.box(r.id); b != nil {
			return pad + "▾ " + m.label(r.id) + " (" + boxKindName(ctx.T, b.Kind) + ")", sDim
		}
	}
	s := pad + m.label(r.id)
	st := sBase
	if _, ok := m.badges[r.id]; ok {
		bd := m.badgeOf(r.id)
		s += bd.Text + " · " + pctText(bd.BarPermille) + " · p99 " + archdraw.FormatMs(bd.P99Ms)
		switch {
		case bd.Danger:
			s, st = s+" ✕", sErr
		case bd.Warn:
			s, st = s+" ⚠", sWarn
		}
	}
	return s, st
}

func pctText(permille int) string { return strconvItoa(max(permille, 0)/10) + "%" }

// drawList draws the list view in r.
func (m *canvasModel) drawList(ctx *ui.Context, b *buf, r layout.Rect, focused bool) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	b.fill(r, sBase)
	rows := m.listRows()
	if len(rows) == 0 {
		b.line(r.X+1, r.Y, r.W-1, ctx.T.Archcanvas.ListEmpty(), sDim)
		return
	}
	m.listSel = min(max(m.listSel, 0), len(rows)-1)
	off := 0
	if m.listSel >= r.H {
		off = m.listSel - r.H + 1
	}
	for i := 0; i < r.H && off+i < len(rows); i++ {
		row := rows[off+i]
		s, st := m.listText(ctx, row)
		if off+i == m.listSel && focused {
			st = sSel
		}
		b.line(r.X+1, r.Y+i, r.W-1, s, st)
	}
}
