// Package archdraw draws an architecture design onto a grid of terminal cells
// (spec C2.7): three zoom levels, viewport culling, deterministic orthogonal
// edge routes, hit testing and directional neighbours. It has no styles of its
// own (cells carry a Style the UI maps to colours), no catalog text and no I/O.
// It accepts unvalidated designs: dangling references and box parent cycles
// are tolerated, never a panic.
package archdraw

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/textwidth"
)

// Zoom is a drawing scale. World coordinates never change with the zoom; the
// zoom scales them and the node size.
type Zoom int

// Zoom levels, most detailed first: ZoomDetail draws a node as 4 lines (name×n /
// bar / p99 / warning) at world scale, ZoomNormal as 3 lines at 3/4 scale and
// ZoomDot as one "[api×4 81%]" line at 2/3 × 1/3 scale.
const (
	ZoomDetail Zoom = iota
	ZoomNormal
	ZoomDot
)

// Zooms lists the zoom levels, most detailed first.
func Zooms() []Zoom { return []Zoom{ZoomDetail, ZoomNormal, ZoomDot} }

// In is the next more detailed zoom (ZoomDetail stays).
func (z Zoom) In() Zoom { return max(ZoomDetail, z.valid()-1) }

// Out is the next less detailed zoom (ZoomDot stays).
func (z Zoom) Out() Zoom { return min(ZoomDot, z.valid()+1) }

func (z Zoom) valid() Zoom {
	if z < ZoomDetail || z > ZoomDot {
		return ZoomDetail
	}
	return z
}

// scale returns the zoom's x and y scale as fractions nx/dx and ny/dy.
func (z Zoom) scale() (nx, dx, ny, dy int) {
	switch z.valid() {
	case ZoomNormal:
		return 3, 4, 3, 4
	case ZoomDot:
		return 2, 3, 1, 3
	}
	return 1, 1, 1, 1
}

// NodeSize is a node's size in screen cells at zoom z.
func NodeSize(z Zoom) (w, h int) {
	switch z.valid() {
	case ZoomNormal:
		return 10, 3
	case ZoomDot:
		return 10, 1
	}
	return archdoc.NodeW, archdoc.NodeH
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func ceilDiv(a, b int) int { return -floorDiv(-a, b) }

// sx and sy scale a world coordinate into the zoom's absolute cell space.
func (z Zoom) sx(wx int) int {
	nx, dx, _, _ := z.scale()
	return floorDiv(wx*nx, dx)
}

func (z Zoom) sy(wy int) int {
	_, _, ny, dy := z.scale()
	return floorDiv(wy*ny, dy)
}

// Viewport is the part of the world shown: X, Y the world point at the screen's
// top-left cell, W, H the screen size in cells.
type Viewport struct {
	X, Y, W, H int
	Zoom       Zoom
}

// ToScreen maps a world point to screen cells (may lie outside 0..W, 0..H).
func (v Viewport) ToScreen(wx, wy int) (int, int) {
	z := v.Zoom.valid()
	return z.sx(wx) - z.sx(v.X), z.sy(wy) - z.sy(v.Y)
}

// ToWorld maps a screen cell to the smallest world point drawn in it, so
// ToScreen(ToWorld(sx, sy)) == (sx, sy).
func (v Viewport) ToWorld(sx, sy int) (int, int) {
	z := v.Zoom.valid()
	nx, dx, ny, dy := z.scale()
	return ceilDiv((sx+z.sx(v.X))*dx, nx), ceilDiv((sy+z.sy(v.Y))*dy, ny)
}

// Dir is a direction for Nearest.
type Dir int

// Directions.
const (
	DirLeft Dir = iota
	DirDown
	DirUp
	DirRight
)

type sceneNode struct {
	id, label string
	kind      archdoc.NodeKind
	x, y      int // world
}

type sceneBox struct {
	id, label  string
	kind       archdoc.BoxKind
	x, y, w, h int // world
	parent     string
	depth      int // 0 = top level (a parent cycle or dangling parent counts as top level)
}

// Scene is a design prepared for drawing: labels sanitized, boxes in drawing
// order, edges whose two ends exist. Build one per design change; Render, Route
// and HitTest may be called many times on it. A Scene is safe for concurrent use.
type Scene struct {
	nodes   []sceneNode // by ID
	nodeIdx map[string]int
	boxes   []sceneBox // drawing order: depth, then ID
	boxIdx  map[string]int
	edges   []archdoc.Edge // by ID; both ends exist, not a self loop

	mu     sync.Mutex
	routes [3]map[string]*cachedRoute // per zoom, filled lazily
	blocks [3]*obstacles
	// BuildFrom: the previous scene's routes, its shape (nodes and boxes only,
	// so scenes never chain) and, per zoom, the obstacles that changed since
	prev        [3]map[string]*cachedRoute
	prevShape   *Scene
	changed     [3][]rect
	changedDone [3]bool
	rt          router
	// searches counts the A* routings per zoom (tests check route reuse)
	searches [3]int
}

// cleanLabel makes user text one safe line for drawing.
func cleanLabel(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ").Replace(s)
	return strings.TrimSpace(textwidth.Sanitize(s))
}

// Build prepares d for drawing. d is not kept.
func Build(d *archdoc.Design) *Scene {
	s := &Scene{nodeIdx: map[string]int{}, boxIdx: map[string]int{}}
	if d == nil {
		return s
	}
	for _, n := range d.Nodes {
		if _, dup := s.nodeIdx[n.ID]; dup || n.ID == "" {
			continue
		}
		label := cleanLabel(n.Label)
		if label == "" {
			label = cleanLabel(string(n.Kind))
		}
		s.nodeIdx[n.ID] = len(s.nodes)
		s.nodes = append(s.nodes, sceneNode{id: n.ID, label: label, kind: n.Kind, x: n.Pos[0], y: n.Pos[1]})
	}
	slices.SortFunc(s.nodes, func(a, b sceneNode) int { return cmp.Compare(a.id, b.id) })
	for i, n := range s.nodes {
		s.nodeIdx[n.id] = i
	}
	raw := map[string]archdoc.Box{}
	for _, b := range d.Boxes {
		if _, dup := raw[b.ID]; dup || b.ID == "" {
			continue
		}
		raw[b.ID] = b
	}
	for id, b := range raw {
		label := cleanLabel(b.Label)
		if label == "" {
			label = cleanLabel(string(b.Kind))
		}
		s.boxes = append(s.boxes, sceneBox{
			id: id, label: label, kind: b.Kind, parent: b.Parent,
			x: b.Rect[0], y: b.Rect[1], w: max(b.Rect[2], 0), h: max(b.Rect[3], 0),
			depth: boxDepth(raw, id),
		})
	}
	slices.SortFunc(s.boxes, func(a, b sceneBox) int {
		return cmp.Or(cmp.Compare(a.depth, b.depth), cmp.Compare(a.id, b.id))
	})
	for i, b := range s.boxes {
		s.boxIdx[b.id] = i
	}
	for _, e := range d.Edges {
		_, okF := s.nodeIdx[e.From]
		_, okT := s.nodeIdx[e.To]
		if !okF || !okT || e.From == e.To || e.ID == "" {
			continue
		}
		s.edges = append(s.edges, e)
	}
	slices.SortFunc(s.edges, func(a, b archdoc.Edge) int { return cmp.Compare(a.ID, b.ID) })
	s.edges = slices.CompactFunc(s.edges, func(a, b archdoc.Edge) bool { return a.ID == b.ID })
	return s
}

// BuildFrom is Build for a changed design: the new scene reuses prev's route
// for every edge whose two ends and whose nearby obstacles (everything its
// search could see) did not change, so the first frame after an edit routes
// only the edges near what moved. The result is the same as Build(d); prev is
// only read and may be nil.
func BuildFrom(prev *Scene, d *archdoc.Design) *Scene {
	s := Build(d)
	if prev == nil {
		return s
	}
	prev.mu.Lock()
	for z := range s.prev {
		s.prev[z] = maps.Clone(prev.routes[z])
	}
	prev.mu.Unlock()
	s.prevShape = &Scene{nodes: prev.nodes, boxes: prev.boxes, boxIdx: prev.boxIdx}
	return s
}

// boxDepth counts a box's ancestors; a dangling parent ends the chain and a
// parent cycle makes every box on it top level.
func boxDepth(boxes map[string]archdoc.Box, id string) int {
	seen := map[string]bool{id: true}
	depth := 0
	for cur := boxes[id].Parent; cur != ""; cur = boxes[cur].Parent {
		if seen[cur] {
			return 0
		}
		if _, ok := boxes[cur]; !ok {
			break
		}
		seen[cur] = true
		depth++
	}
	return depth
}

// rect is a rectangle in a zoom's absolute cell space.
type rect struct{ x, y, w, h int }

func (r rect) contains(x, y int) bool { return x >= r.x && y >= r.y && x < r.x+r.w && y < r.y+r.h }

func (r rect) intersects(o rect) bool {
	return r.x < o.x+o.w && o.x < r.x+r.w && r.y < o.y+o.h && o.y < r.y+r.h
}

func (s *Scene) nodeRect(n *sceneNode, z Zoom) rect {
	w, h := NodeSize(z)
	return rect{z.sx(n.x), z.sy(n.y), w, h}
}

func nodeRectAt(wx, wy int, z Zoom) rect {
	w, h := NodeSize(z)
	return rect{z.sx(wx), z.sy(wy), w, h}
}

func (s *Scene) boxRect(b *sceneBox, z Zoom) rect {
	x0, y0 := z.sx(b.x), z.sy(b.y)
	return rect{x0, y0, z.sx(b.x+b.w) - x0, z.sy(b.y+b.h) - y0}
}

// boxRects are the boxes' rectangles at zoom z, in drawing order. A nested box
// is kept inside its parent's border so scaling never makes the two outlines
// share a row or column.
func (s *Scene) boxRects(z Zoom) []rect {
	out := make([]rect, len(s.boxes))
	for i := range s.boxes {
		b := &s.boxes[i]
		r := s.boxRect(b, z)
		if pi, ok := s.boxIdx[b.parent]; ok && b.depth > 0 && pi < i {
			p := out[pi]
			x0, y0 := max(r.x, p.x+1), max(r.y, p.y+1)
			x1, y1 := min(r.x+r.w, p.x+p.w-1), min(r.y+r.h, p.y+p.h-1)
			r = rect{x0, y0, max(x1-x0, 0), max(y1-y0, 0)}
		}
		out[i] = r
	}
	return out
}

// NodeIDs lists the scene's nodes by ID.
func (s *Scene) NodeIDs() []string {
	out := make([]string, len(s.nodes))
	for i, n := range s.nodes {
		out[i] = n.id
	}
	return out
}

// Bounds is the world rectangle (x, y, w, h) covering every node and box; all
// zero for an empty scene.
func (s *Scene) Bounds() (x, y, w, h int) {
	first := true
	var x0, y0, x1, y1 int
	add := func(ax, ay, bx, by int) {
		if first {
			x0, y0, x1, y1, first = ax, ay, bx, by, false
			return
		}
		x0, y0, x1, y1 = min(x0, ax), min(y0, ay), max(x1, bx), max(y1, by)
	}
	for _, n := range s.nodes {
		add(n.x, n.y, n.x+archdoc.NodeW, n.y+archdoc.NodeH)
	}
	for _, b := range s.boxes {
		add(b.x, b.y, b.x+b.w, b.y+b.h)
	}
	return x0, y0, x1 - x0, y1 - y0
}

// NodeAt returns the world position of node id.
func (s *Scene) NodeAt(id string) (x, y int, ok bool) {
	i, ok := s.nodeIdx[id]
	if !ok {
		return 0, 0, false
	}
	return s.nodes[i].x, s.nodes[i].y, true
}

// Overlaps lists the nodes (by ID) whose rectangles overlap another node's at
// zoom z — the UI then says "zoom in" instead of drawing the overlap.
func (s *Scene) Overlaps(z Zoom) []string {
	hit := map[string]bool{}
	rs := make([]rect, len(s.nodes))
	order := make([]int, len(s.nodes))
	for i := range s.nodes {
		rs[i] = s.nodeRect(&s.nodes[i], z)
		order[i] = i
	}
	// sweep by x: only nodes whose x ranges overlap are compared
	slices.SortFunc(order, func(a, b int) int { return cmp.Or(cmp.Compare(rs[a].x, rs[b].x), cmp.Compare(a, b)) })
	for i, a := range order {
		for _, b := range order[i+1:] {
			if rs[b].x >= rs[a].x+rs[a].w {
				break
			}
			if rs[a].intersects(rs[b]) {
				hit[s.nodes[a].id], hit[s.nodes[b].id] = true, true
			}
		}
	}
	out := make([]string, 0, len(hit))
	for id := range hit {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Nearest returns the node closest to node from in direction dir ("" = none):
// only nodes whose centre lies strictly that way count, the distance along dir
// plus twice the sideways offset decides, ties go to the smaller ID.
func (s *Scene) Nearest(from string, dir Dir) string {
	i, ok := s.nodeIdx[from]
	if !ok {
		return ""
	}
	cx, cy := 2*s.nodes[i].x+archdoc.NodeW, 2*s.nodes[i].y+archdoc.NodeH // doubled centres
	best, bestD := "", 0
	for j := range s.nodes {
		if j == i {
			continue
		}
		dx := 2*s.nodes[j].x + archdoc.NodeW - cx
		dy := 2*s.nodes[j].y + archdoc.NodeH - cy
		var along, side int
		switch dir {
		case DirLeft:
			along, side = -dx, dy
		case DirRight:
			along, side = dx, dy
		case DirUp:
			along, side = -dy, dx
		default:
			along, side = dy, dx
		}
		if along <= 0 {
			continue
		}
		// rows are about 3x taller than columns are wide on screen
		if dir == DirUp || dir == DirDown {
			along *= 3
		} else {
			side *= 3
		}
		d := along + 2*abs(side)
		if best == "" || d < bestD || (d == bestD && s.nodes[j].id < best) {
			best, bestD = s.nodes[j].id, d
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
