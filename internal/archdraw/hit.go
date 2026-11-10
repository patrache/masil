package archdraw

import "github.com/patrache/masil/internal/archdoc"

// HitKind is what a screen cell belongs to.
type HitKind int

// Hit kinds.
const (
	HitNone      HitKind = iota
	HitNode              // a node's body or border
	HitHandle            // a node's ◆ connection handle
	HitBox               // inside a box (the innermost one)
	HitBoxBorder         // a box's outline or label
	HitEdge              // a drawn edge cell
	HitComment           // a node's comment mark (reserved: HitTest has no comment counts; the comments panel resolves it)
	HitZoomIn            // the ＋ button
	HitZoomOut           // the － button
)

// Hit is the result of HitTest: the kind and the node, box or edge ID.
type Hit struct {
	Kind HitKind
	ID   string
}

// HitTest says what is drawn at screen cell (sx, sy) of viewport v: the zoom
// buttons (the top-right four cells, "＋－"), then nodes (handle first), edges, box outlines and box insides, innermost first.
func (s *Scene) HitTest(v Viewport, sx, sy int) Hit {
	if sx < 0 || sy < 0 || sx >= v.W || sy >= v.H {
		return Hit{}
	}
	if sy == 0 && v.W >= 4 && sx >= v.W-4 {
		if sx < v.W-2 {
			return Hit{Kind: HitZoomIn}
		}
		return Hit{Kind: HitZoomOut}
	}
	z := v.Zoom.valid()
	px, py := sx+z.sx(v.X), sy+z.sy(v.Y)     // absolute zoom cells
	for i := len(s.nodes) - 1; i >= 0; i-- { // drawn last = on top
		n := &s.nodes[i]
		r := s.nodeRect(n, z)
		if !r.contains(px, py) {
			continue
		}
		if z != ZoomDot && px == r.x+r.w-1 && py == r.y+r.h/2 {
			return Hit{Kind: HitHandle, ID: n.id}
		}
		return Hit{Kind: HitNode, ID: n.id}
	}
	if id := s.edgeAt(z, px, py); id != "" {
		return Hit{Kind: HitEdge, ID: id}
	}
	brs := s.boxRects(z)
	for i := len(s.boxes) - 1; i >= 0; i-- { // deepest first
		b := &s.boxes[i]
		r := brs[i]
		if r.w < 2 || r.h < 1 || !r.contains(px, py) {
			continue
		}
		if px == r.x || px == r.x+r.w-1 || py == r.y || py == r.y+r.h-1 {
			return Hit{Kind: HitBoxBorder, ID: b.id}
		}
		return Hit{Kind: HitBox, ID: b.id}
	}
	return Hit{}
}

// BoxAt returns the innermost box whose inside contains world point (wx, wy)
// ("" = none) — where a node dropped there would go.
func (s *Scene) BoxAt(wx, wy int) string {
	for i := len(s.boxes) - 1; i >= 0; i-- {
		b := &s.boxes[i]
		if wx > b.x && wy > b.y && wx < b.x+b.w-1 && wy < b.y+b.h-1 {
			return b.id
		}
	}
	return ""
}

// BoxAtRect returns the innermost box whose inside fully holds a node placed at
// world (wx, wy) ("" = none).
func (s *Scene) BoxAtRect(wx, wy int) string {
	for i := len(s.boxes) - 1; i >= 0; i-- {
		b := &s.boxes[i]
		if wx > b.x && wy > b.y && wx+archdoc.NodeW <= b.x+b.w-1 && wy+archdoc.NodeH <= b.y+b.h-1 {
			return b.id
		}
	}
	return ""
}
