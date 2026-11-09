package archdraw

import (
	"fmt"
	"math/rand/v2"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/archdoc"
)

func TestRouteAvoidsNodesAndLabels(t *testing.T) {
	d := loadDesign(t, "three_tier")
	sc := Build(d)
	for _, z := range Zooms() {
		var nodeRs []rect
		for i := range sc.nodes {
			nodeRs = append(nodeRs, sc.nodeRect(&sc.nodes[i], z))
		}
		type span struct{ x0, n, y int }
		var labels []span
		for i, r := range sc.boxRects(z) {
			if x0, n := labelSpan(r, sc.boxes[i].label); n > 0 {
				labels = append(labels, span{x0, n, r.y})
			}
		}
		for _, e := range d.Edges {
			pts := sc.Route(e, z)
			if len(pts) < 2 {
				t.Errorf("zoom %d edge %s: no route %v", z, e.ID, pts)
				continue
			}
			for i := 1; i < len(pts); i++ {
				if pts[i-1].X != pts[i].X && pts[i-1].Y != pts[i].Y {
					t.Errorf("zoom %d edge %s: diagonal step %v→%v", z, e.ID, pts[i-1], pts[i])
				}
			}
			for _, p := range cells(pts) {
				for j, r := range nodeRs {
					if r.contains(p.X, p.Y) {
						t.Errorf("zoom %d edge %s: cell %v inside node %s", z, e.ID, p, sc.nodes[j].id)
					}
				}
				for _, l := range labels {
					if p.Y == l.y && p.X >= l.x0 && p.X < l.x0+l.n {
						t.Errorf("zoom %d edge %s: cell %v on a box label", z, e.ID, p)
					}
				}
			}
		}
	}
}

func TestRouteDeterministic(t *testing.T) {
	d := loadDesign(t, "three_tier")
	want := map[Zoom]string{}
	for _, z := range Zooms() {
		want[z] = strings.Join(Build(d).Render(Viewport{X: -4, Y: -2, W: 160, H: 80, Zoom: z}, Overlay{}).Plain(), "\n")
	}
	r := rand.New(rand.NewPCG(1, 2))
	for range 5 {
		d2 := *d
		d2.Edges = slices.Clone(d.Edges)
		r.Shuffle(len(d2.Edges), func(i, j int) { d2.Edges[i], d2.Edges[j] = d2.Edges[j], d2.Edges[i] })
		for _, z := range Zooms() {
			got := strings.Join(Build(&d2).Render(Viewport{X: -4, Y: -2, W: 160, H: 80, Zoom: z}, Overlay{}).Plain(), "\n")
			if got != want[z] {
				t.Fatalf("zoom %d: shuffled edges draw differently:\n%s\n---\n%s", z, got, want[z])
			}
		}
	}
}

func TestCrossingGlyph(t *testing.T) {
	d := archdoc.New("x")
	add := func(id string, x, y int) {
		n := archdoc.Defaults(archdoc.KindAPI)
		n.ID, n.Pos = id, [2]int{x, y}
		d.Nodes = append(d.Nodes, n)
	}
	add("w", 0, 10)  // left
	add("e", 40, 10) // right
	add("n", 20, 0)  // top
	add("s", 20, 22) // bottom
	d.Edges = []archdoc.Edge{
		{ID: "h", From: "w", To: "e", Protocol: archdoc.ProtoHTTP1, LatencyMicros: -1},
		{ID: "v", From: "n", To: "s", Protocol: archdoc.ProtoHTTP1, LatencyMicros: -1},
	}
	g := Build(d).Render(Viewport{W: 60, H: 30, Zoom: ZoomNormal}, Overlay{})
	s := strings.Join(g.Plain(), "\n")
	if !strings.Contains(s, "┼") {
		t.Errorf("no ┼ where the edges cross:\n%s", s)
	}
	for _, glyph := range []string{"▶", "▼"} {
		if !strings.Contains(s, glyph) {
			t.Errorf("no %s arrow:\n%s", glyph, s)
		}
	}
}

func TestEdgeLabelsOnlyDetailOrSelected(t *testing.T) {
	d := archdoc.New("lbl")
	for i, id := range []string{"a", "b"} {
		n := archdoc.Defaults(archdoc.KindAPI)
		n.ID, n.Pos = id, [2]int{i * 40, 0}
		d.Nodes = append(d.Nodes, n)
	}
	d.Edges = []archdoc.Edge{{ID: "e1", From: "a", To: "b", Protocol: archdoc.ProtoGRPC, LatencyMicros: -1}}
	sc := Build(d)
	render := func(z Zoom, sel bool) string {
		return strings.Join(sc.Render(Viewport{W: 70, H: 8, Zoom: z}, Overlay{Selected: map[string]bool{"e1": sel}}).Plain(), "\n")
	}
	if s := render(ZoomDetail, false); !strings.Contains(s, "gRPC") {
		t.Errorf("detail: no label:\n%s", s)
	}
	if s := render(ZoomNormal, false); strings.Contains(s, "gRPC") {
		t.Errorf("normal, not selected: label drawn:\n%s", s)
	}
	if s := render(ZoomNormal, true); !strings.Contains(s, "gRPC") {
		t.Errorf("normal, selected: no label:\n%s", s)
	}
	if h := sc.HitTest(Viewport{W: 70, H: 8, Zoom: ZoomNormal}, 20, 1); h.Kind != HitEdge || h.ID != "e1" {
		t.Errorf("hit on the edge line: %+v", h)
	}
}

// bigDesign is 300 nodes in a 20-column grid and 1,000 edges between them.
func bigDesign() *archdoc.Design {
	d := archdoc.New("big")
	for i := range 300 {
		n := archdoc.Defaults(archdoc.KindAPI)
		n.ID = fmt.Sprintf("n%03d", i)
		n.Pos = [2]int{(i % 20) * 16, (i / 20) * 6}
		d.Nodes = append(d.Nodes, n)
	}
	r := rand.New(rand.NewPCG(7, 7))
	for i := range 1000 {
		a := r.IntN(300)
		b := (a + 1 + r.IntN(40)) % 300
		d.Edges = append(d.Edges, archdoc.Edge{ID: fmt.Sprintf("e%04d", i), From: d.Nodes[a].ID, To: d.Nodes[b].ID, LatencyMicros: -1})
	}
	return d
}

func BenchmarkRender300x1000(b *testing.B) {
	sc := Build(bigDesign())
	v := Viewport{W: 200, H: 60, Zoom: ZoomDetail}
	for b.Loop() {
		sc.Render(v, Overlay{})
	}
}

func median(ds []time.Duration) time.Duration {
	slices.Sort(ds)
	return ds[len(ds)/2]
}

// movedOne is d with node id moved right by dx world cells.
func movedOne(d *archdoc.Design, id string, dx int) *archdoc.Design {
	d2 := *d
	d2.Nodes = slices.Clone(d.Nodes)
	for i := range d2.Nodes {
		if d2.Nodes[i].ID == id {
			d2.Nodes[i].Pos[0] += dx
		}
	}
	return &d2
}

// TestRouteReuseAcrossBuilds: a scene built from the previous one keeps the
// routes nothing touched — the first frame after a one-node edit routes only
// the edges near that node — and draws exactly what a fresh Build draws. With
// a RouteLimit the frame routes at most that many and Settle finishes the rest.
func TestRouteReuseAcrossBuilds(t *testing.T) {
	d := bigDesign()
	prev := Build(d)
	for _, z := range Zooms() {
		prev.Render(Viewport{W: 200, H: 60, Zoom: z}, Overlay{})
	}
	d2 := movedOne(d, "n150", 2)
	next, fresh, limited := BuildFrom(prev, d2), Build(d2), BuildFrom(prev, d2)
	for _, z := range Zooms() {
		v := Viewport{W: 200, H: 60, Zoom: z}
		want := strings.Join(fresh.Render(v, Overlay{}).Plain(), "\n")
		got := strings.Join(next.Render(v, Overlay{}).Plain(), "\n")
		if got != want {
			t.Errorf("zoom %d: reused routes draw differently from a fresh build:\n%s\n---\n%s", z, got, want)
		}
		if n, all := next.searches[z], fresh.searches[z]; all == 0 || n*4 > all {
			t.Errorf("zoom %d: %d searches after a one-node edit (a fresh build: %d)", z, n, all)
		}
		if !next.Settle(v, 0) {
			t.Errorf("zoom %d: a full render left stale routes", z)
		}
		// the limited frame (the moved node's own edges always route), then
		// Settle in steps of the same size
		own := 0
		for _, e := range d2.Edges {
			if e.From == "n150" || e.To == "n150" {
				own++
			}
		}
		limited.Render(v, Overlay{RouteLimit: 8})
		if n := limited.searches[z]; n > 8+own {
			t.Errorf("zoom %d: %d searches in a frame limited to 8 (+%d own edges)", z, n, own)
		}
		steps := 0
		for !limited.Settle(v, 8) {
			if steps++; steps > 100 {
				t.Fatalf("zoom %d: Settle never finishes", z)
			}
		}
		if got := strings.Join(limited.Render(v, Overlay{RouteLimit: 8}).Plain(), "\n"); got != want {
			t.Errorf("zoom %d: settled routes draw differently from a fresh build:\n%s\n---\n%s", z, got, want)
		}
	}
	// prev == nil is a plain Build
	if s := BuildFrom(nil, d2); len(s.nodes) != 300 {
		t.Errorf("BuildFrom(nil): %d nodes", len(s.nodes))
	}
}

func TestRenderBudget(t *testing.T) {
	if os.Getenv("MASIL_PERF") != "1" {
		t.Skip("MASIL_PERF=1 runs the frame budget")
	}
	sc := Build(bigDesign())
	v := Viewport{W: 200, H: 60, Zoom: ZoomDetail}
	var ds []time.Duration
	for range 20 {
		t0 := time.Now()
		sc.Render(v, Overlay{})
		ds = append(ds, time.Since(t0))
	}
	if m := median(ds); m > 8*time.Millisecond {
		t.Errorf("median frame %v > 8ms", m)
	}
	// the first frame after an edit (a new scene from the previous one), per zoom
	for _, z := range Zooms() {
		v := Viewport{W: 200, H: 60, Zoom: z}
		d := bigDesign()
		prev := Build(d)
		prev.Render(v, Overlay{})
		ds = ds[:0]
		var settle []time.Duration
		for i := range 20 {
			d = movedOne(d, fmt.Sprintf("n%03d", 100+i), 2)
			t0 := time.Now()
			next := BuildFrom(prev, d)
			next.Render(v, Overlay{RouteLimit: FrameRouteLimit})
			ds = append(ds, time.Since(t0))
			for {
				t0 = time.Now()
				done := next.Settle(v, FrameRouteLimit)
				settle = append(settle, time.Since(t0))
				if done {
					break
				}
			}
			prev = next
		}
		t.Logf("zoom %d: median first frame after an edit %v, slowest Settle step %v", z, median(ds), slices.Max(settle))
		if m := median(ds); m > 8*time.Millisecond {
			t.Errorf("zoom %d: median first frame after an edit %v > 8ms", z, m)
		}
		if m := slices.Max(settle); m > 16*time.Millisecond {
			t.Errorf("zoom %d: slowest Settle step %v > 16ms", z, m)
		}
	}
	// a drag frame on a 50-node design: only the ghost moves
	d := bigDesign()
	d.Nodes = d.Nodes[:50]
	small := Build(d)
	ds = ds[:0]
	for i := range 20 {
		t0 := time.Now()
		small.Render(v, Overlay{Ghost: &Ghost{ID: "n000", X: i * 2, Y: i}})
		ds = append(ds, time.Since(t0))
	}
	if m := median(ds); m > 8*time.Millisecond {
		t.Errorf("median drag frame %v > 8ms", m)
	}
}

// farDesign is 300 valid nodes, half at x=-99000 and half at x=99000, with
// 1,000 edges across, and one huge box: every edge and the box cross the screen
// at x≈0 while almost none of their cells are on it.
func farDesign() *archdoc.Design {
	d := archdoc.New("far")
	for i := range 300 {
		n := archdoc.Defaults(archdoc.KindAPI)
		n.ID = fmt.Sprintf("n%03d", i)
		x := -99000
		if i%2 == 1 {
			x = 99000
		}
		n.Pos = [2]int{x, (i / 2) * 6}
		d.Nodes = append(d.Nodes, n)
	}
	for i := range 1000 {
		a := (i * 2) % 300
		b := (i*2+1+(i/150)*2)%300 | 1
		d.Edges = append(d.Edges, archdoc.Edge{ID: fmt.Sprintf("e%04d", i), From: d.Nodes[a].ID, To: d.Nodes[b].ID, Protocol: archdoc.ProtoHTTP1, LatencyMicros: -1})
	}
	d.Boxes = []archdoc.Box{{ID: "huge", Kind: archdoc.BoxRegion, Label: "r", Rect: [4]int{-99990, -50000, 99990, 99990}}}
	return d
}

func TestFarEdgesCheap(t *testing.T) {
	d := farDesign()
	if issues := d.Validate(); len(issues) != 0 {
		t.Fatalf("fixture invalid: %v", issues)
	}
	sc := Build(d)
	for _, z := range Zooms() {
		v := Viewport{X: -60, Y: 0, W: 120, H: 40, Zoom: z}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		g := sc.Render(v, Overlay{Ghost: &Ghost{ID: "huge", X: -99960, Y: -50000}, ConnectFrom: "n000", ConnectTo: [2]int{0, 5}})
		sc.HitTest(v, 60, 3)
		runtime.ReadMemStats(&after)
		checkGrid(t, g)
		if s := strings.Join(g.Plain(), "\n"); !strings.Contains(s, "━") {
			t.Errorf("zoom %d: the crossing edges are not drawn:\n%s", z, s)
		}
		if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 64 {
			t.Errorf("zoom %d: one frame and one hit test allocated %d MB", z, mb)
		}
	}
}
