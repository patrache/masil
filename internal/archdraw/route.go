package archdraw

import (
	"strconv"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/textwidth"
)

// Point is a cell in a zoom's absolute cell space (world coordinates scaled by
// the zoom; equal to world coordinates at ZoomDetail).
type Point struct{ X, Y int }

// Routing constants: the search area around the two ends, the wider area tried
// when that fails, and the extra cost of a bend.
const (
	routeMargin     = 6
	routeWideMargin = 24
	bendCost        = 4
	bucketW         = 16
	bucketH         = 8
)

// obstacles are the cells an edge may not use at one zoom: node rectangles and
// box label spans, bucketed for fast lookup.
type obstacles struct {
	buckets map[[2]int][]rect
}

func (o *obstacles) add(r rect) {
	if r.w <= 0 || r.h <= 0 {
		return
	}
	for bx := floorDiv(r.x, bucketW); bx <= floorDiv(r.x+r.w-1, bucketW); bx++ {
		for by := floorDiv(r.y, bucketH); by <= floorDiv(r.y+r.h-1, bucketH); by++ {
			k := [2]int{bx, by}
			o.buckets[k] = append(o.buckets[k], r)
		}
	}
}

func (o *obstacles) blocked(x, y int) bool {
	for _, r := range o.buckets[[2]int{floorDiv(x, bucketW), floorDiv(y, bucketH)}] {
		if r.contains(x, y) {
			return true
		}
	}
	return false
}

// obstaclesAt builds (once per zoom) the blocked cells. Call with s.mu held.
func (s *Scene) obstaclesAt(z Zoom) *obstacles {
	if s.blocks[z] != nil {
		return s.blocks[z]
	}
	o := &obstacles{buckets: map[[2]int][]rect{}}
	for i := range s.nodes {
		o.add(s.nodeRect(&s.nodes[i], z))
	}
	for i, r := range s.boxRects(z) {
		if x0, n := labelSpan(r, s.boxes[i].label); n > 0 {
			o.add(rect{x0, r.y, n, 1})
		}
	}
	s.blocks[z] = o
	return o
}

// direction indexes: 0 left, 1 right, 2 up, 3 down (the fixed tie order).
var dirDX, dirDY = [4]int{-1, 1, 0, 0}, [4]int{0, 0, -1, 1}

const (
	bitL uint8 = 1 << iota
	bitR
	bitU
	bitD
)

var dirBit = [4]uint8{bitL, bitR, bitU, bitD}

// ports picks where an edge leaves its source and enters its target: the start
// cell next to the source, the end cell next to the target, the direction the
// line leaves the source with and the direction it arrives with.
func ports(a, b rect) (start, end Point, out, in int) {
	switch {
	case b.x >= a.x+a.w: // target to the right
		return Point{a.x + a.w, a.y + a.h/2}, Point{b.x - 1, b.y + b.h/2}, 1, 1
	case b.x+b.w <= a.x: // to the left
		return Point{a.x - 1, a.y + a.h/2}, Point{b.x + b.w, b.y + b.h/2}, 0, 0
	case b.y >= a.y: // below
		return Point{a.x + a.w/2, a.y + a.h}, Point{b.x + b.w/2, b.y - 1}, 3, 3
	}
	return Point{a.x + a.w/2, a.y - 1}, Point{b.x + b.w/2, b.y + b.h}, 2, 2
}

// routeArea is the rectangle an edge's route can use at most (with the wide
// fallback margin), for culling before routing.
func routeArea(a, b rect) rect {
	x0, y0 := min(a.x, b.x)-routeWideMargin, min(a.y, b.y)-routeWideMargin
	x1, y1 := max(a.x+a.w, b.x+b.w)+routeWideMargin, max(a.y+a.h, b.y+b.h)+routeWideMargin
	return rect{x0, y0, x1 - x0, y1 - y0}
}

// Route returns edge e's path at zoom z as corner points in the zoom's cell
// space (first = the cell next to the source, last = the arrow cell next to the
// target); nil when an end is missing. Paths are orthogonal, never cross a node
// or a box label, and depend only on the edge's ends — not on other edges or
// their order. Results are cached per scene.
func (s *Scene) Route(e archdoc.Edge, z Zoom) []Point {
	z = z.valid()
	ia, okA := s.nodeIdx[e.From]
	ib, okB := s.nodeIdx[e.To]
	if !okA || !okB || ia == ib {
		return nil
	}
	key := e.From + "\x00" + e.To
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.routes[z] == nil {
		s.routes[z] = map[string][]Point{}
	}
	if p, ok := s.routes[z][key]; ok {
		return p
	}
	a, b := s.nodeRect(&s.nodes[ia], z), s.nodeRect(&s.nodes[ib], z)
	start, end, out, in := ports(a, b)
	o := s.obstaclesAt(z)
	path, capped := s.rt.astar(o, start, end, out, in, routeMargin)
	if path == nil && !capped { // walled in nearby: look further around
		path, _ = s.rt.astar(o, start, end, out, in, routeWideMargin)
	}
	if path == nil { // boxed in: a plain elbow
		path = []Point{start, {end.X, start.Y}, end}
		if start.Y == end.Y || start.X == end.X {
			path = []Point{start, end}
		}
	}
	s.routes[z][key] = path
	return path
}

// hnode is one A* frontier entry.
type hnode struct {
	f, g int32
	x, y int32
	dir  int8
}

func hless(a, b hnode) bool {
	if a.f != b.f {
		return a.f < b.f
	}
	if a.g != b.g {
		return a.g > b.g // deeper first: fewer re-expansions
	}
	if a.y != b.y {
		return a.y < b.y
	}
	if a.x != b.x {
		return a.x < b.x
	}
	return a.dir < b.dir
}

// router holds reusable search buffers (one per scene, used under its lock).
type router struct {
	heap    []hnode
	best    []int32
	prev    []int32
	stamp   []uint32
	gen     uint32
	blocked []bool
}

func (r *router) push(n hnode) {
	r.heap = append(r.heap, n)
	i := len(r.heap) - 1
	for i > 0 {
		p := (i - 1) / 2
		if !hless(r.heap[i], r.heap[p]) {
			break
		}
		r.heap[i], r.heap[p] = r.heap[p], r.heap[i]
		i = p
	}
}

func (r *router) pop() hnode {
	h := r.heap
	top := h[0]
	last := len(h) - 1
	h[0] = h[last]
	h = h[:last]
	i := 0
	for {
		l, m := 2*i+1, i
		if l < len(h) && hless(h[l], h[m]) {
			m = l
		}
		if l+1 < len(h) && hless(h[l+1], h[m]) {
			m = l + 1
		}
		if m == i {
			break
		}
		h[i], h[m] = h[m], h[i]
		i = m
	}
	r.heap = h
	return top
}

// maxExpand bounds one search; a design too dense to route in that many steps
// gets the plain elbow instead of stalling the frame.
const maxExpand = 1500

// astar finds the cheapest orthogonal path (1 per step, bendCost per bend) from
// start to end inside their bounding box grown by margin, and returns its
// corner points (nil = none). The path leaves start heading startDir and
// reaches end heading endDir.
func (r *router) astar(o *obstacles, start, end Point, startDir, endDir, margin int) (_ []Point, capped bool) {
	x0, y0 := min(start.X, end.X)-margin, min(start.Y, end.Y)-margin
	w, h := abs(start.X-end.X)+2*margin+1, abs(start.Y-end.Y)+2*margin+1
	if w*h > 200_000 {
		return nil, true
	}
	n := w * h
	if len(r.blocked) < n {
		r.blocked = make([]bool, n)
		r.best = make([]int32, n*4)
		r.prev = make([]int32, n*4)
		r.stamp = make([]uint32, n*4)
		r.gen = 0
	}
	r.gen++
	if r.gen == 0 { // wrapped: forget every stamp
		clear(r.stamp)
		r.gen = 1
	}
	// rasterize the obstacles of the area once
	blocked := r.blocked[:n]
	clear(blocked)
	area := rect{x0, y0, w, h}
	seen := map[rect]bool{}
	for bx := floorDiv(x0, bucketW); bx <= floorDiv(x0+w-1, bucketW); bx++ {
		for by := floorDiv(y0, bucketH); by <= floorDiv(y0+h-1, bucketH); by++ {
			for _, ob := range o.buckets[[2]int{bx, by}] {
				if seen[ob] || !ob.intersects(area) {
					continue
				}
				seen[ob] = true
				for y := max(ob.y, y0); y < min(ob.y+ob.h, y0+h); y++ {
					row := (y - y0) * w
					for x := max(ob.x, x0); x < min(ob.x+ob.w, x0+w); x++ {
						blocked[row+x-x0] = true
					}
				}
			}
		}
	}
	blocked[(start.Y-y0)*w+start.X-x0] = false
	blocked[(end.Y-y0)*w+end.X-x0] = false
	idx := func(x, y, d int) int { return ((y-y0)*w+(x-x0))*4 + d }
	get := func(i int) int32 {
		if r.stamp[i] != r.gen {
			return -1
		}
		return r.best[i]
	}
	set := func(i int, g, p int32) { r.stamp[i], r.best[i], r.prev[i] = r.gen, g, p }
	// Manhattan distance plus the bends still needed at the least: two when
	// heading away from the target, one when it is off the current line.
	hfn := func(x, y, d int) int32 {
		dx, dy := end.X-x, end.Y-y
		h := abs(dx) + abs(dy)
		along, side := dx*dirDX[d]+dy*dirDY[d], dx*dirDY[d]+dy*dirDX[d]
		switch {
		case along < 0:
			h += 2 * bendCost
		case side != 0:
			h += bendCost
		}
		return int32(h)
	}
	r.heap = r.heap[:0]
	si := idx(start.X, start.Y, startDir)
	set(si, 0, -1)
	r.push(hnode{f: hfn(start.X, start.Y, startDir), x: int32(start.X), y: int32(start.Y), dir: int8(startDir)})
	goal, expanded := -1, 0
	for ; len(r.heap) > 0 && expanded < maxExpand; expanded++ {
		c := r.pop()
		cx, cy, cd := int(c.x), int(c.y), int(c.dir)
		ci := idx(cx, cy, cd)
		if get(ci) != c.g {
			continue
		}
		if cx == end.X && cy == end.Y && (cd == endDir || start == end) {
			goal = ci
			break
		}
		for d := range 4 {
			if d^1 == cd { // no turning back
				continue
			}
			nx, ny := cx+dirDX[d], cy+dirDY[d]
			if nx < x0 || ny < y0 || nx >= x0+w || ny >= y0+h || blocked[(ny-y0)*w+nx-x0] {
				continue
			}
			g := c.g + 1
			if d != cd {
				g += bendCost
			}
			ni := idx(nx, ny, d)
			if b := get(ni); b >= 0 && b <= g {
				continue
			}
			set(ni, g, int32(ci))
			r.push(hnode{f: g + hfn(nx, ny, d), g: g, x: int32(nx), y: int32(ny), dir: int8(d)})
		}
	}
	if goal < 0 {
		return nil, expanded >= maxExpand
	}
	var rev []Point
	for i := goal; ; i = int(r.prev[i]) {
		c := i / 4
		rev = append(rev, Point{x0 + c%w, y0 + c/w})
		if i == si || r.prev[i] < 0 {
			break
		}
	}
	pts := make([]Point, 0, 6)
	for i := len(rev) - 1; i >= 0; i-- {
		p := rev[i]
		if k := len(pts); k >= 2 {
			a, b := pts[k-2], pts[k-1]
			if (a.X == b.X && b.X == p.X) || (a.Y == b.Y && b.Y == p.Y) {
				pts[k-1] = p
				continue
			}
		}
		pts = append(pts, p)
	}
	return pts, false
}

// cells expands a route's corner points into every cell it passes.
func cells(pts []Point) []Point {
	if len(pts) == 0 {
		return nil
	}
	out := []Point{pts[0]}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		for a != b {
			switch {
			case a.X < b.X:
				a.X++
			case a.X > b.X:
				a.X--
			case a.Y < b.Y:
				a.Y++
			default:
				a.Y--
			}
			out = append(out, a)
		}
	}
	return out
}

// lineGlyph maps a cell's connections to a heavy line glyph.
func lineGlyph(bits uint8) string {
	switch bits {
	case bitL | bitR, bitL, bitR:
		return "━"
	case bitU | bitD, bitU, bitD:
		return "┃"
	case bitR | bitD:
		return "┏"
	case bitL | bitD:
		return "┓"
	case bitR | bitU:
		return "┗"
	case bitL | bitU:
		return "┛"
	case bitL | bitR | bitD:
		return "┳"
	case bitL | bitR | bitU:
		return "┻"
	case bitU | bitD | bitR:
		return "┣"
	case bitU | bitD | bitL:
		return "┫"
	}
	return "┼"
}

var arrows = [4]string{"◀", "▶", "▲", "▼"}

var protocolNames = map[archdoc.Protocol]string{
	archdoc.ProtoHTTP1: "HTTP/1.1", archdoc.ProtoHTTP2: "HTTP/2", archdoc.ProtoGRPC: "gRPC",
	archdoc.ProtoTCP: "TCP", archdoc.ProtoAsync: "async",
}

// EdgeLabel is an edge's short label: "HTTP/2 · 2ms" (protocol, then the
// latency when it is not the topology default).
func EdgeLabel(e archdoc.Edge) string {
	p, ok := protocolNames[e.Protocol]
	if !ok {
		p = cleanLabel(string(e.Protocol))
	}
	if e.LatencyMicros < 0 {
		return p
	}
	tenths := (e.LatencyMicros + 50) / 100
	ms := strconv.Itoa(tenths/10) + "ms"
	if tenths%10 != 0 {
		ms = strconv.Itoa(tenths/10) + "." + strconv.Itoa(tenths%10) + "ms"
	}
	if p == "" {
		return ms
	}
	return p + " · " + ms
}

// visibleRoutes returns the routes of the edges that may show on screen.
func (s *Scene) visibleRoutes(z Zoom, screen rect) ([]archdoc.Edge, [][]Point) {
	var es []archdoc.Edge
	var rs [][]Point
	for _, e := range s.edges {
		a := s.nodeRect(&s.nodes[s.nodeIdx[e.From]], z)
		b := s.nodeRect(&s.nodes[s.nodeIdx[e.To]], z)
		if !routeArea(a, b).intersects(screen) {
			continue
		}
		p := s.Route(e, z)
		if len(p) == 0 {
			continue
		}
		es = append(es, e)
		rs = append(rs, p)
	}
	return es, rs
}

// drawEdges draws every visible edge: heavy lines merged per cell (so crossings
// become ┼), an arrow next to the target, and protocol labels at ZoomDetail
// (or for selected edges at any zoom).
func (s *Scene) drawEdges(g *Grid, z Zoom, ox, oy int, ov Overlay) {
	if g.W == 0 || g.H == 0 {
		return
	}
	screen := rect{ox, oy, g.W, g.H}
	es, rs := s.visibleRoutes(z, screen)
	bits := make([]uint8, g.W*g.H)
	sel := make([]bool, g.W*g.H)
	type arrow struct {
		p   Point
		dir int
		sel bool
	}
	var heads []arrow
	mark := func(x, y int, b uint8, selected bool) {
		x, y = x-ox, y-oy
		if x < 0 || y < 0 || x >= g.W || y >= g.H {
			return
		}
		bits[y*g.W+x] |= b
		sel[y*g.W+x] = sel[y*g.W+x] || selected
	}
	for i, e := range es {
		selected := ov.Selected[e.ID]
		cs := cells(rs[i])
		a := s.nodeRect(&s.nodes[s.nodeIdx[e.From]], z)
		b := s.nodeRect(&s.nodes[s.nodeIdx[e.To]], z)
		_, _, out, in := ports(a, b)
		mark(cs[0].X, cs[0].Y, dirBit[out^1], selected) // joins the source's border
		for j := 1; j < len(cs); j++ {
			p, c := cs[j-1], cs[j]
			for d := range 4 {
				if p.X+dirDX[d] == c.X && p.Y+dirDY[d] == c.Y {
					mark(p.X, p.Y, dirBit[d], selected)
					mark(c.X, c.Y, dirBit[d^1], selected)
				}
			}
		}
		heads = append(heads, arrow{cs[len(cs)-1], in, selected})
	}
	for i, b := range bits {
		if b == 0 {
			continue
		}
		st := StyleEdge
		if sel[i] {
			st = StyleEdgeSelected
		}
		g.set(i%g.W, i/g.W, lineGlyph(b), 1, st)
	}
	for _, h := range heads {
		st := StyleEdge
		if h.sel {
			st = StyleEdgeSelected
		}
		g.set(h.p.X-ox, h.p.Y-oy, arrows[h.dir], 1, st)
	}
	for i, e := range es {
		selected := ov.Selected[e.ID]
		if z != ZoomDetail && !selected {
			continue
		}
		label := EdgeLabel(e)
		lw := textwidth.Width(label)
		if lw == 0 {
			continue
		}
		st := StyleEdge
		if selected {
			st = StyleEdgeSelected
		}
		// the longest horizontal segment that holds the label with a cell each side
		bestLen, bx, by := 0, 0, 0
		pts := rs[i]
		for j := 1; j < len(pts); j++ {
			a, b := pts[j-1], pts[j]
			if a.Y != b.Y {
				continue
			}
			l := abs(a.X-b.X) + 1
			if l > bestLen {
				bestLen, bx, by = l, min(a.X, b.X), a.Y
			}
		}
		if bestLen >= lw+4 {
			x := bx + (bestLen-lw)/2
			g.text(x-ox, by-oy, label, lw, st)
		}
	}
}

// edgeAt returns the edge drawn at absolute zoom cell (px, py) ("" = none).
func (s *Scene) edgeAt(z Zoom, px, py int) string {
	es, rs := s.visibleRoutes(z, rect{px, py, 1, 1})
	for i := range es {
		for _, c := range cells(rs[i]) {
			if c.X == px && c.Y == py {
				return es[i].ID
			}
		}
	}
	return ""
}
