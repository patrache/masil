package archdoc

import (
	"cmp"
	"slices"
)

// Layout constants for Arrange (world cells; all even so x stays on the even grid).
const (
	arrangeColGap  = 4 // column pitch NodeW+4
	arrangeRowGap  = 2 // row pitch NodeH+2
	arrangePadX    = 2 // box inner margin left and right
	arrangePadTop  = 2 // box title line + margin
	arrangePadBott = 1
)

// layers returns each node's column: the longest path from a client, with back
// edges (found by a DFS that starts from clients, then the rest, in ID order)
// ignored so cycles cannot loop.
func (g *graph) layers() map[string]int {
	const (
		unseen = iota
		active
		done
	)
	state := map[string]int{}
	back := map[[2]string]bool{}
	var dfs func(v string)
	dfs = func(v string) {
		state[v] = active
		for _, w := range g.out[v] {
			switch state[w] {
			case unseen:
				dfs(w)
			case active:
				back[[2]string{v, w}] = true
			}
		}
		state[v] = done
	}
	starts := slices.Clone(g.ids)
	slices.SortStableFunc(starts, func(a, b string) int {
		ca, cb := g.nodes[a].Kind == KindClient, g.nodes[b].Kind == KindClient
		switch {
		case ca && !cb:
			return -1
		case cb && !ca:
			return 1
		}
		return 0
	})
	var order []string // reverse post-order = topological order of the DAG
	var post func(v string)
	seen := map[string]bool{}
	post = func(v string) {
		seen[v] = true
		for _, w := range g.out[v] {
			if !back[[2]string{v, w}] && !seen[w] {
				post(w)
			}
		}
		order = append(order, v)
	}
	for _, v := range starts {
		if state[v] == unseen {
			dfs(v)
		}
	}
	for _, v := range starts {
		if !seen[v] {
			post(v)
		}
	}
	slices.Reverse(order)
	col := make(map[string]int, len(order))
	for _, v := range order {
		for _, w := range g.out[v] {
			if !back[[2]string{v, w}] {
				col[w] = max(col[w], col[v]+1)
			}
		}
	}
	return col
}

type arrangeItem struct {
	id    string
	box   bool
	key   int // column key: the smallest column of the nodes inside
	w, h  int
	items []*arrangeItem // box contents
}

// Arrange lays the design out in columns by longest path from the clients
// (column pitch NodeW+4, row pitch NodeH+2, ID order inside a column). Nodes in a
// box are laid out inside that box and boxes are resized to fit; no two node
// rectangles overlap. It returns Set changes of "pos" and "rect" for every value
// that moves, sorted by object key. It accepts unvalidated designs.
func Arrange(d *Design) []Change {
	g := newGraph(d)
	col := g.layers()
	children := map[string][]*arrangeItem{} // container box ID ("" = root) → items
	byBox := map[string]*arrangeItem{}
	for _, id := range sortedKeys(g.boxes) {
		it := &arrangeItem{id: id, box: true, key: -1}
		byBox[id] = it
		children[g.parent[id]] = append(children[g.parent[id]], it)
	}
	for _, id := range g.ids {
		parent := g.boxOf(g.nodes[id])
		children[parent] = append(children[parent], &arrangeItem{id: id, key: col[id], w: NodeW, h: NodeH})
	}
	for id, it := range byBox {
		it.items = children[id]
	}
	var size func(it *arrangeItem)
	size = func(it *arrangeItem) {
		if !it.box {
			return
		}
		for _, c := range it.items {
			size(c)
			if c.key >= 0 && (it.key < 0 || c.key < it.key) {
				it.key = c.key
			}
		}
		w, h := contentSize(it.items)
		it.w = max(w, NodeW) + 2*arrangePadX
		it.h = max(h, NodeH) + arrangePadTop + arrangePadBott
	}
	root := children[""]
	for _, it := range root {
		size(it)
	}

	pos := map[string][2]int{}
	rect := map[string][4]int{}
	var place func(items []*arrangeItem, x0, y0 int)
	place = func(items []*arrangeItem, x0, y0 int) {
		x := x0
		for _, column := range columns(items) {
			y, cw := y0, 0
			for _, it := range column {
				if it.box {
					rect[it.id] = [4]int{x, y, it.w, it.h}
					place(it.items, x+arrangePadX, y+arrangePadTop)
				} else {
					pos[it.id] = [2]int{x, y}
				}
				y += it.h + arrangeRowGap
				cw = max(cw, it.w)
			}
			x += cw + arrangeColGap
		}
	}
	place(root, 0, 0)

	var out []Change
	for _, id := range g.ids {
		if p := pos[id]; p != g.nodes[id].Pos {
			out = append(out, Change{Kind: ChangeSet, Obj: ObjKey('n', id), Field: "pos", Value: marshalField(&p)})
		}
	}
	for _, id := range sortedKeys(g.boxes) {
		if r := rect[id]; r != g.boxes[id].Rect {
			out = append(out, Change{Kind: ChangeSet, Obj: ObjKey('b', id), Field: "rect", Value: marshalField(&r)})
		}
	}
	slices.SortFunc(out, func(a, b Change) int { return cmp.Compare(a.Obj, b.Obj) })
	return out
}

// columns groups items by column key (an empty box counts as column 0), each
// column in ID order (a node before a box of the same ID).
func columns(items []*arrangeItem) [][]*arrangeItem {
	sorted := slices.Clone(items)
	slices.SortStableFunc(sorted, func(a, b *arrangeItem) int {
		ka, kb := max(a.key, 0), max(b.key, 0)
		if c := cmp.Or(cmp.Compare(ka, kb), cmp.Compare(a.id, b.id)); c != 0 {
			return c
		}
		switch {
		case a.box == b.box:
			return 0
		case b.box:
			return -1
		}
		return 1
	})
	var out [][]*arrangeItem
	for i, it := range sorted {
		if i == 0 || max(it.key, 0) != max(sorted[i-1].key, 0) {
			out = append(out, nil)
		}
		out[len(out)-1] = append(out[len(out)-1], it)
	}
	return out
}

func contentSize(items []*arrangeItem) (w, h int) {
	cols := columns(items)
	for i, column := range cols {
		cw, ch := 0, 0
		for j, it := range column {
			cw = max(cw, it.w)
			ch += it.h
			if j > 0 {
				ch += arrangeRowGap
			}
		}
		w += cw
		if i > 0 {
			w += arrangeColGap
		}
		h = max(h, ch)
	}
	return w, h
}
