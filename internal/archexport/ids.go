package archexport

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/patrache/masil/internal/archdoc"
)

// tree is a tolerant, sorted view of a design for export: the first of
// duplicate IDs wins, a box that reuses a node's ID is dropped (the node keeps
// it), a box parent cycle is cut at its smallest ID, children of missing boxes
// sit at the top and edges need both ends.
type tree struct {
	nodes      []*archdoc.Node
	boxes      []*archdoc.Box
	edges      []*archdoc.Edge
	ids        map[string]string // design ID → n1, b1, …
	node       map[string]*archdoc.Node
	box        map[string]*archdoc.Box
	parent     map[string]string // effective box parent of a box
	childBoxes map[string][]*archdoc.Box
	childNodes map[string][]*archdoc.Node
}

func sorted[T any](s []T, id func(*T) string) []*T {
	out := make([]*T, 0, len(s))
	for i := range s {
		out = append(out, &s[i])
	}
	slices.SortStableFunc(out, func(a, b *T) int { return cmp.Compare(id(a), id(b)) })
	return slices.CompactFunc(out, func(a, b *T) bool { return id(a) == id(b) })
}

func newTree(d *archdoc.Design) *tree {
	t := &tree{ids: map[string]string{}, node: map[string]*archdoc.Node{}, box: map[string]*archdoc.Box{},
		parent: map[string]string{}, childBoxes: map[string][]*archdoc.Box{}, childNodes: map[string][]*archdoc.Node{}}
	for _, n := range sorted(d.Nodes, func(n *archdoc.Node) string { return n.ID }) {
		t.nodes = append(t.nodes, n)
		t.node[n.ID] = n
		t.ids[n.ID] = "n" + strconv.Itoa(len(t.nodes))
	}
	for _, b := range sorted(d.Boxes, func(b *archdoc.Box) string { return b.ID }) {
		if t.node[b.ID] != nil {
			continue
		}
		t.boxes = append(t.boxes, b)
		t.box[b.ID] = b
		t.ids[b.ID] = "b" + strconv.Itoa(len(t.boxes))
	}
	for _, b := range t.boxes {
		t.parent[b.ID] = b.Parent
		if t.box[b.Parent] == nil {
			t.parent[b.ID] = ""
		}
	}
	for _, b := range t.boxes { // ID order: a cycle breaks at its smallest ID
		for p, steps := t.parent[b.ID], 0; p != "" && steps <= len(t.boxes); p, steps = t.parent[p], steps+1 {
			if p == b.ID {
				t.parent[b.ID] = ""
				break
			}
		}
	}
	for _, b := range t.boxes {
		t.childBoxes[t.parent[b.ID]] = append(t.childBoxes[t.parent[b.ID]], b)
	}
	for _, n := range t.nodes {
		p := n.Parent
		if t.box[p] == nil {
			p = ""
		}
		t.childNodes[p] = append(t.childNodes[p], n)
	}
	for _, e := range sorted(d.Edges, func(e *archdoc.Edge) string { return e.ID }) {
		if t.node[e.From] != nil && t.node[e.To] != nil {
			t.edges = append(t.edges, e)
		}
	}
	return t
}

// boxPath lists the box IDs from the top down to (and including) id.
func (t *tree) boxPath(id string) []string {
	var out []string
	for b, steps := id, 0; b != "" && steps <= len(t.boxes); b, steps = t.parent[b], steps+1 {
		out = append(out, b)
	}
	slices.Reverse(out)
	return out
}

// nodeBox is the node's effective box ("" = top level).
func (t *tree) nodeBox(n *archdoc.Node) string {
	if t.box[n.Parent] == nil {
		return ""
	}
	return n.Parent
}

// safeIDs maps node and box IDs to generated identifiers in ID order (n1, n2,
// … for nodes, b1, b2, … for boxes) so no user text ever becomes an identifier.
func safeIDs(d *archdoc.Design) map[string]string { return newTree(d).ids }
