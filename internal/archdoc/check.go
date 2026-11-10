package archdoc

import (
	"cmp"
	"slices"
)

// CheckCode names one structural warning (spec C2.8 "설계 점검", JC7). Checks warn
// and never block.
type CheckCode string

const (
	CheckSPOFSingleInstance   CheckCode = "spof_single_instance"    // one instance and no LB in front
	CheckSPOFSingleLB         CheckCode = "spof_single_lb"          // a load balancer with one instance
	CheckPrimaryReplicaSameAZ CheckCode = "primary_replica_same_az" // primary and replica in one AZ
	CheckSingleAZService      CheckCode = "single_az_service"       // every node of a kind sits in one AZ
	CheckUnconnected          CheckCode = "unconnected"             // a node without edges
	CheckCycle                CheckCode = "cycle"                   // nodes on a directed cycle
	CheckAZOutsideVPC         CheckCode = "az_outside_vpc"
	CheckVPCOutsideRegion     CheckCode = "vpc_outside_region"
)

// Finding is one warning about Targets (node or box IDs, sorted).
type Finding struct {
	Code    CheckCode
	Targets []string
}

func compareFinding(a, b Finding) int {
	return cmp.Or(cmp.Compare(a.Code, b.Code), slices.Compare(a.Targets, b.Targets))
}

// graph is a tolerant view of a design: only edges whose ends exist, box parents
// with dangling references dropped and cycles cut (FromObjects rules).
type graph struct {
	d      *Design
	nodes  map[string]*Node
	ids    []string            // node IDs, sorted
	out    map[string][]string // sorted successor IDs (duplicates removed)
	in     map[string][]string // sorted predecessor IDs
	parent map[string]string   // box ID → effective parent box ID
	boxes  map[string]*Box
}

func newGraph(d *Design) *graph {
	nn, nb := len(d.Nodes), len(d.Boxes)
	g := &graph{d: d, nodes: make(map[string]*Node, nn), out: make(map[string][]string, nn), in: make(map[string][]string, nn),
		parent: make(map[string]string, nb), boxes: make(map[string]*Box, nb), ids: make([]string, 0, nn)}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if _, dup := g.nodes[n.ID]; !dup {
			g.nodes[n.ID] = n
			g.ids = append(g.ids, n.ID)
		}
	}
	slices.Sort(g.ids)
	for _, e := range d.Edges {
		if g.nodes[e.From] == nil || g.nodes[e.To] == nil {
			continue
		}
		g.out[e.From] = append(g.out[e.From], e.To)
		g.in[e.To] = append(g.in[e.To], e.From)
	}
	for _, m := range []map[string][]string{g.out, g.in} {
		for k, v := range m {
			slices.Sort(v)
			m[k] = slices.Compact(v)
		}
	}
	boxes := slices.Clone(d.Boxes)
	sortByIDs(boxes)
	boxes = slices.CompactFunc(boxes, func(a, b Box) bool { return a.ID == b.ID })
	repairBoxes(boxes, func(string, IssueCode) {})
	for i := range boxes {
		g.boxes[boxes[i].ID] = &boxes[i]
		g.parent[boxes[i].ID] = boxes[i].Parent
	}
	return g
}

// boxOf is the node's effective parent box ("" = top level or dangling).
func (g *graph) boxOf(n *Node) string {
	if g.boxes[n.Parent] == nil {
		return ""
	}
	return n.Parent
}

// enclosing returns the nearest box of kind k around node n ("" = none).
func (g *graph) enclosing(n *Node, k BoxKind) string {
	for b := g.boxOf(n); b != ""; b = g.parent[b] {
		if g.boxes[b].Kind == k {
			return b
		}
	}
	return ""
}

// sccs returns the strongly connected components with a cycle (size > 1 or a
// self loop), each sorted (Tarjan, nodes and successors in ID order).
func (g *graph) sccs() [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string
	next := 0
	var visit func(v string)
	visit = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range g.out[v] {
			if _, seen := index[w]; !seen {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] != index[v] {
			return
		}
		var comp []string
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			comp = append(comp, w)
			if w == v {
				break
			}
		}
		if len(comp) > 1 || slices.Contains(g.out[v], v) {
			slices.Sort(comp)
			out = append(out, comp)
		}
	}
	for _, v := range g.ids {
		if _, seen := index[v]; !seen {
			visit(v)
		}
	}
	return out
}

// Check returns the design's structural warnings sorted by Code then Targets.
// It accepts unvalidated designs. Client, CDN and external nodes outside boxes
// are normal and never flagged for it.
func Check(d *Design) []Finding {
	g := newGraph(d)
	var out []Finding
	add := func(code CheckCode, targets ...string) {
		slices.Sort(targets)
		out = append(out, Finding{Code: code, Targets: targets})
	}
	behindLB := func(id string) bool {
		for _, p := range g.in[id] {
			if g.nodes[p].Kind == KindLB {
				return true
			}
		}
		return false
	}
	byKind := map[NodeKind][]string{}
	for _, id := range g.ids {
		n := g.nodes[id]
		single := n.Instances <= 1 && !(n.Autoscale.Enabled && n.Autoscale.Min >= 2)
		switch n.Kind {
		case KindLB:
			if single {
				add(CheckSPOFSingleLB, id)
			}
		case KindAPI, KindWorker, KindCache, KindDBReplica, KindQueue, KindSearch, KindDBPrimary:
			failover := n.Kind == KindDBPrimary && n.Params.FailoverTo != "" && n.Params.FailoverTo != id && g.nodes[n.Params.FailoverTo] != nil
			if single && !behindLB(id) && !failover {
				add(CheckSPOFSingleInstance, id)
			}
		}
		if len(g.out[id]) == 0 && len(g.in[id]) == 0 {
			add(CheckUnconnected, id)
		}
		switch n.Kind {
		case KindClient, KindCDN, KindExternal, KindDBPrimary, KindDBReplica:
		default:
			byKind[n.Kind] = append(byKind[n.Kind], id)
		}
	}
	for _, id := range g.ids {
		p := g.nodes[id]
		if p.Kind != KindDBPrimary {
			continue
		}
		az := g.enclosing(p, BoxAZ)
		if az == "" {
			continue
		}
		for _, rid := range g.ids {
			if r := g.nodes[rid]; r.Kind == KindDBReplica && g.enclosing(r, BoxAZ) == az {
				add(CheckPrimaryReplicaSameAZ, id, rid)
			}
		}
	}
	for _, ids := range byKind {
		az := g.enclosing(g.nodes[ids[0]], BoxAZ)
		same := az != ""
		for _, id := range ids[1:] {
			same = same && g.enclosing(g.nodes[id], BoxAZ) == az
		}
		if same {
			add(CheckSingleAZService, slices.Clone(ids)...)
		}
	}
	for _, comp := range g.sccs() {
		add(CheckCycle, comp...)
	}
	for _, id := range sortedKeys(g.boxes) {
		b := g.boxes[id]
		parent := g.boxes[g.parent[id]]
		switch {
		case b.Kind == BoxAZ && (parent == nil || parent.Kind != BoxVPC):
			add(CheckAZOutsideVPC, id)
		case b.Kind == BoxVPC && (parent == nil || parent.Kind != BoxRegion):
			add(CheckVPCOutsideRegion, id)
		}
	}
	slices.SortFunc(out, compareFinding)
	return out
}
