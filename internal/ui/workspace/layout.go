package workspace

import (
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Cells of the workspace surface (ctrl+\ 1..4).
const (
	PaneTree   = 1
	PaneCode   = 2
	PaneTerm   = 3
	PanePeople = 4
)

// Plan is where each cell of the surface goes (spec B6.7). Zero rects are not
// drawn. Rects tile the surface; each cell draws its own separators.
type Plan struct {
	Band, Tree, Code, Term, People, SubTabs layout.Rect
	Single                                  int  // widget: the one cell shown
	Tiny                                    bool // tiny: only the summary
}

const (
	treeWide   = 26
	treeNormal = 22
	peopleW    = 24
)

// planLayout places the cells for a tool surface of size in mode. zoomed is
// the tool's wide view, sharing adds the people cell, guest is the
// participant screen (same arrangement), focus picks the widget's cell.
func planLayout(mode layout.Mode, size ui.Size, zoomed, sharing, guest bool, focus int) Plan {
	_ = guest // the participant screen uses the same arrangement (B6.7)
	if mode <= layout.ModeTiny || size.W < 20 || size.H < 6 {
		return Plan{Tiny: true}
	}
	var p Plan
	w, h := size.W, size.H
	p.Band = layout.Rect{X: 0, Y: 0, W: w, H: 1}
	body := layout.Rect{X: 0, Y: 1, W: w, H: h - 1}
	if mode == layout.ModeWidget {
		p.SubTabs = layout.Rect{X: 0, Y: 1, W: w, H: 1}
		cell := layout.Rect{X: 0, Y: 2, W: w, H: h - 2}
		if focus < PaneTree || focus > PanePeople || (focus == PanePeople && !sharing) {
			focus = PaneCode
		}
		p.Single = focus
		p.set(focus, cell)
		return p
	}
	right := body
	if sharing && mode == layout.ModeCommand {
		pw := min(peopleW, w/3)
		p.People = layout.Rect{X: w - pw, Y: body.Y, W: pw, H: body.H}
		right.W -= pw
	}
	tw := treeNormal
	if zoomed && right.W >= 100 {
		tw = treeWide
	}
	if right.W < 60 {
		tw = max(right.W/3, 8)
	}
	tree := layout.Rect{X: 0, Y: body.Y, W: tw, H: body.H}
	rest := layout.Rect{X: tw, Y: body.Y, W: right.W - tw, H: body.H}
	if sharing && mode != layout.ModeCommand && focus == PanePeople {
		p.People = tree // the people list takes the tree's place until focus moves
	} else {
		p.Tree = tree
	}
	if mode == layout.ModeCommand && zoomed && rest.W >= 120 {
		cw := rest.W * 55 / 100
		p.Code = layout.Rect{X: rest.X, Y: rest.Y, W: cw, H: rest.H}
		p.Term = layout.Rect{X: rest.X + cw, Y: rest.Y, W: rest.W - cw, H: rest.H}
		return p
	}
	ch := max(rest.H*55/100, 3)
	if ch >= rest.H {
		ch = rest.H - 1
	}
	p.Code = layout.Rect{X: rest.X, Y: rest.Y, W: rest.W, H: ch}
	p.Term = layout.Rect{X: rest.X, Y: rest.Y + ch, W: rest.W, H: rest.H - ch}
	return p
}

func (p *Plan) set(cell int, r layout.Rect) {
	switch cell {
	case PaneTree:
		p.Tree = r
	case PaneCode:
		p.Code = r
	case PaneTerm:
		p.Term = r
	case PanePeople:
		p.People = r
	}
}

// rectOf is the cell's rect in the plan.
func (p Plan) rectOf(cell int) layout.Rect {
	switch cell {
	case PaneTree:
		return p.Tree
	case PaneCode:
		return p.Code
	case PaneTerm:
		return p.Term
	case PanePeople:
		return p.People
	}
	return layout.Rect{}
}

// zoomCell is the plan with one cell over the whole body (ctrl+\ z).
func zoomCell(p Plan, cell int) Plan {
	if p.Tiny || p.Single != 0 {
		return p
	}
	r := layout.Rect{X: 0, Y: p.Band.Y + p.Band.H, W: p.Band.W, H: 0}
	for _, c := range []layout.Rect{p.Tree, p.Code, p.Term, p.People} {
		if c.W > 0 {
			r.H = max(r.H, c.Y+c.H-r.Y)
		}
	}
	q := Plan{Band: p.Band}
	q.set(cell, r)
	return q
}
