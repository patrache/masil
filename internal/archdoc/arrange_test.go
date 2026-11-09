package archdoc

import (
	"math/rand/v2"
	"reflect"
	"testing"
)

func arranged(t *testing.T, d *Design) *Design {
	t.Helper()
	got, is := FromObjects(d.ToObjects().Apply(Arrange(d)), nil)
	if len(is) != 0 {
		t.Fatalf("issues after arrange: %v", is)
	}
	return got
}

func overlap(a, b [4]int) bool {
	return a[0] < b[0]+b[2] && b[0] < a[0]+a[2] && a[1] < b[1]+b[3] && b[1] < a[1]+a[3]
}

func inside(inner, outer [4]int) bool {
	return inner[0] >= outer[0] && inner[1] >= outer[1] && inner[0]+inner[2] <= outer[0]+outer[2] && inner[1]+inner[3] <= outer[1]+outer[3]
}

func nodeRect(n Node) [4]int { return [4]int{n.Pos[0], n.Pos[1], NodeW, NodeH} }

func checkLayout(t *testing.T, name string, d *Design) {
	t.Helper()
	boxes := map[string]Box{}
	for _, b := range d.Boxes {
		boxes[b.ID] = b
	}
	for i, a := range d.Nodes {
		if a.Pos[0]%2 != 0 {
			t.Fatalf("%s: node %s at odd x %d", name, a.ID, a.Pos[0])
		}
		for _, b := range d.Nodes[i+1:] {
			if overlap(nodeRect(a), nodeRect(b)) {
				t.Fatalf("%s: nodes %s %v and %s %v overlap", name, a.ID, a.Pos, b.ID, b.Pos)
			}
		}
		if p, ok := boxes[a.Parent]; ok && !inside(nodeRect(a), p.Rect) {
			t.Fatalf("%s: node %s %v outside its box %s %v", name, a.ID, a.Pos, p.ID, p.Rect)
		}
	}
	for _, b := range d.Boxes {
		if p, ok := boxes[b.Parent]; ok && !inside(b.Rect, p.Rect) {
			t.Fatalf("%s: box %s %v outside its box %s %v", name, b.ID, b.Rect, p.ID, p.Rect)
		}
		for _, n := range d.Nodes {
			if n.Parent != b.ID && overlap(nodeRect(n), b.Rect) && !isAncestor(boxes, b.ID, n.Parent) {
				t.Fatalf("%s: node %s overlaps unrelated box %s", name, n.ID, b.ID)
			}
		}
	}
}

func isAncestor(boxes map[string]Box, anc, id string) bool {
	for i := 0; id != "" && i <= len(boxes); i++ {
		if id == anc {
			return true
		}
		id = boxes[id].Parent
	}
	return false
}

func TestArrangeNoOverlap(t *testing.T) {
	for name, b := range fixtures(t) {
		checkLayout(t, name, arranged(t, mustDecode(t, b)))
	}
	checkLayout(t, "small", arranged(t, small()))
	// Columns follow the longest path from the client.
	d := arranged(t, small())
	x := map[string]int{}
	for _, n := range d.Nodes {
		x[n.ID] = n.Pos[0]
	}
	if !(x["n_client"] < x["n_lb"] && x["n_lb"] < x["n_api"] && x["n_api"] < x["n_db"]) {
		t.Fatalf("columns = %v", x)
	}
	// Arranging an arranged design changes nothing.
	if ch := Arrange(d); len(ch) != 0 {
		t.Fatalf("second Arrange = %v", ch)
	}
}

func TestArrangeDeterministic(t *testing.T) {
	for name, b := range fixtures(t) {
		d := mustDecode(t, b)
		want := Arrange(d)
		for seed := uint64(1); seed <= 5; seed++ {
			c := mustDecode(t, b)
			r := rand.New(rand.NewPCG(seed, 0))
			r.Shuffle(len(c.Nodes), func(i, j int) { c.Nodes[i], c.Nodes[j] = c.Nodes[j], c.Nodes[i] })
			r.Shuffle(len(c.Boxes), func(i, j int) { c.Boxes[i], c.Boxes[j] = c.Boxes[j], c.Boxes[i] })
			r.Shuffle(len(c.Edges), func(i, j int) { c.Edges[i], c.Edges[j] = c.Edges[j], c.Edges[i] })
			if got := Arrange(c); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s seed %d: Arrange depends on slice order", name, seed)
			}
		}
	}
}

func TestArrangeTolerant(t *testing.T) {
	b := newBuilder().node("a", KindAPI, "x", 2).node("b", KindAPI, "y", 2).box("x", BoxAZ, "y").box("y", BoxVPC, "x").edge("a", "b").edge("b", "a")
	b.d.Nodes = append(b.d.Nodes, Node{ID: "c", Kind: KindCache, Parent: "gone"})
	ch := Arrange(b.d) // parent cycle, edge cycle, dangling parent: no panic, finite result
	if len(ch) == 0 {
		t.Fatal("no changes")
	}
}
