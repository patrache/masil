package archsim

import (
	"cmp"
	"math"
	"slices"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim/pricing"
)

// Request classes (JC9: reads and writes only).
const (
	clsRead = iota
	clsWrite
)

const (
	sameAZMicros    = 200
	crossAZMicros   = 1_000
	outsideMicros   = 20_000 // a client, CDN or external service outside every box
	maxRetries      = 10
	maxFixedPointIt = 20
	fixedPointEps   = 1e-9
)

func mul(a, b float64) float64 { return float64(a * b) }

// route is one share of a node's outgoing traffic.
type route struct {
	e    int
	frac float64
}

type mnode struct {
	n       *archdoc.Node
	region  string
	az      string
	boxes   []string // enclosing boxes, innermost first
	boxed   bool
	routes  [2][]route
	errFrac [2]float64 // share with nowhere valid to go (writes to a replica without a primary)
	failTo  int        // failover target node, -1 = none
}

type medge struct {
	e          *archdoc.Edge
	from, to   int
	back       bool // closes a cycle: carries nothing (archdoc.Check warns)
	async      bool
	baseMicros float64
	mbps       float64 // the edge's own bandwidth, 0 = none
	pathMbps   float64 // bandwidth for transfer time: own or the endpoints' NICs
}

// model is a tolerant, sorted view of a design: duplicate IDs keep the first in
// ID order, edges to missing nodes are ignored, box parent cycles are cut and
// cycle edges carry no flow. Every walk is in ID order.
type model struct {
	d       *archdoc.Design
	nodes   []mnode
	idx     map[string]int
	edges   []medge
	eidx    map[string]int
	order   []int // topological, clients first
	clients []int
	boxes   map[string]*archdoc.Box
}

func sortedUnique[T any](s []T, id func(*T) string) []*T {
	out := make([]*T, 0, len(s))
	for i := range s {
		out = append(out, &s[i])
	}
	slices.SortStableFunc(out, func(a, b *T) int { return cmp.Compare(id(a), id(b)) })
	return slices.CompactFunc(out, func(a, b *T) bool { return id(a) == id(b) })
}

func newModel(d *archdoc.Design) *model {
	m := &model{d: d, idx: map[string]int{}, eidx: map[string]int{}, boxes: map[string]*archdoc.Box{}}
	for _, b := range sortedUnique(d.Boxes, func(b *archdoc.Box) string { return b.ID }) {
		m.boxes[b.ID] = b
	}
	for _, n := range sortedUnique(d.Nodes, func(n *archdoc.Node) string { return n.ID }) {
		m.idx[n.ID] = len(m.nodes)
		mn := mnode{n: n, failTo: -1, boxes: m.chain(n.Parent)}
		mn.boxed = len(mn.boxes) > 0
		mn.region = n.Region
		for _, b := range mn.boxes {
			bx := m.boxes[b]
			if mn.region == "" && bx.Kind == archdoc.BoxRegion {
				mn.region = bx.RegionCode
			}
			if mn.az == "" && bx.Kind == archdoc.BoxAZ {
				mn.az = b
			}
		}
		m.nodes = append(m.nodes, mn)
		if n.Kind == archdoc.KindClient && n.Traffic != nil {
			m.clients = append(m.clients, len(m.nodes)-1)
		}
	}
	for _, e := range sortedUnique(d.Edges, func(e *archdoc.Edge) string { return e.ID }) {
		from, ok1 := m.idx[e.From]
		to, ok2 := m.idx[e.To]
		if !ok1 || !ok2 {
			continue
		}
		m.eidx[e.ID] = len(m.edges)
		m.edges = append(m.edges, medge{e: e, from: from, to: to, async: e.Protocol == archdoc.ProtoAsync, mbps: float64(max(e.Mbps, 0))})
	}
	for i := range m.nodes {
		n := m.nodes[i].n
		if n.Kind != archdoc.KindDBPrimary {
			continue
		}
		if t, ok := m.idx[n.Params.FailoverTo]; ok && t != i {
			if k := m.nodes[t].n.Kind; k == archdoc.KindDBReplica || k == archdoc.KindDBPrimary {
				m.nodes[i].failTo = t
			}
		}
	}
	m.sortTopo()
	for i := range m.edges {
		m.edges[i].baseMicros = m.defaultLatency(&m.edges[i])
		m.edges[i].pathMbps = m.edges[i].mbps
		if m.edges[i].pathMbps == 0 {
			a, b := m.nodes[m.edges[i].from].n, m.nodes[m.edges[i].to].n
			m.edges[i].pathMbps = math.Min(mul(float64(max(a.NICMbps, 0)), float64(max(a.Instances, 1))),
				mul(float64(max(b.NICMbps, 0)), float64(max(b.Instances, 1))))
		}
	}
	out := make([][]int, len(m.nodes))
	for i := range m.edges {
		if !m.edges[i].back {
			out[m.edges[i].from] = append(out[m.edges[i].from], i)
		}
	}
	for i := range m.nodes {
		m.buildRoutes(i, out[i])
	}
	return m
}

// chain lists the boxes around a box ID, innermost first, stopping at a missing
// parent or a parent cycle.
func (m *model) chain(id string) []string {
	var out []string
	for id != "" && m.boxes[id] != nil && !slices.Contains(out, id) && len(out) <= len(m.boxes) {
		out = append(out, id)
		id = m.boxes[id].Parent
	}
	return out
}

// sortTopo orders nodes by a depth-first search from the clients, then every
// other node, in ID order. Edges to a node still on the stack close a cycle and
// are marked back (their flow is dropped). A failover link counts as an edge so
// a promoted replica always comes after its primary; one that would close a
// cycle is dropped.
func (m *model) sortTopo() {
	adj := make([][]int, len(m.nodes))
	for i := range m.edges {
		adj[m.edges[i].from] = append(adj[m.edges[i].from], i)
	}
	state := make([]int8, len(m.nodes))
	post := make([]int, 0, len(m.nodes))
	var visit func(u int)
	visit = func(u int) {
		state[u] = 1
		for _, ei := range adj[u] {
			switch v := m.edges[ei].to; state[v] {
			case 1:
				m.edges[ei].back = true
			case 0:
				visit(v)
			}
		}
		if f := m.nodes[u].failTo; f >= 0 {
			switch state[f] {
			case 1:
				m.nodes[u].failTo = -1
			case 0:
				visit(f)
			}
		}
		state[u] = 2
		post = append(post, u)
	}
	for _, c := range m.clients {
		if state[c] == 0 {
			visit(c)
		}
	}
	for u := range m.nodes {
		if state[u] == 0 {
			visit(u)
		}
	}
	slices.Reverse(post)
	m.order = post
}

// defaultLatency is the edge's base latency in µs (C2.8 연결 설정).
func (m *model) defaultLatency(e *medge) float64 {
	if e.e.LatencyMicros >= 0 {
		return float64(e.e.LatencyMicros)
	}
	a, b := &m.nodes[e.from], &m.nodes[e.to]
	outside := func(n *mnode) bool {
		k := n.n.Kind
		return !n.boxed && (k == archdoc.KindClient || k == archdoc.KindCDN || k == archdoc.KindExternal)
	}
	switch {
	case outside(a) || outside(b):
		return outsideMicros
	case a.region != "" && b.region != "" && a.region != b.region:
		return float64(pricing.RegionLatencyMicros(a.region, b.region))
	case a.az == b.az:
		return sameAZMicros
	default:
		return crossAZMicros
	}
}

func permille(e *archdoc.Edge, c int) float64 {
	v := e.ReadPermille
	if c == clsWrite {
		v = e.WritePermille
	}
	return float64(max(v, 0))
}

// buildRoutes splits a node's traffic over its outgoing edges by the edges'
// read and write shares (normalized when they add up past 1000; the rest ends
// at the node). Writes never go to a replica: from a database node that share
// just ends (replication), otherwise it moves to the node's primary edges or,
// with none, fails.
func (m *model) buildRoutes(u int, out []int) {
	mn := &m.nodes[u]
	src := mn.n.Kind
	for c := range 2 {
		var rs []route
		sum := 0.0
		for _, i := range out {
			s := permille(m.edges[i].e, c)
			rs = append(rs, route{e: i, frac: s})
			sum += s
		}
		norm := math.Max(sum, 1000)
		for i := range rs {
			rs[i].frac /= norm
		}
		if c == clsWrite {
			moved, primary := 0.0, 0.0
			var prim []int
			for i := range rs {
				switch m.nodes[m.edges[rs[i].e].to].n.Kind {
				case archdoc.KindDBReplica:
					moved += rs[i].frac
					rs[i].frac = 0
				case archdoc.KindDBPrimary:
					prim = append(prim, i)
					primary += rs[i].frac
				}
			}
			isDB := src == archdoc.KindDBPrimary || src == archdoc.KindDBReplica
			switch {
			case moved == 0 || isDB:
			case len(prim) == 0:
				mn.errFrac[c] = moved
			default:
				for _, i := range prim {
					if primary > 0 {
						rs[i].frac += mul(moved, rs[i].frac/primary)
					} else {
						rs[i].frac += moved / float64(len(prim))
					}
				}
			}
		}
		mn.routes[c] = slices.DeleteFunc(rs, func(r route) bool { return !(r.frac > 0) })
	}
}

// passInput is the state one flow pass reads.
type passInput struct {
	rps  []float64 // per node: client requests/s
	p    []float64 // per node: failure probability of one attempt (last pass)
	q    []float64 // per edge: share an overfull link loses (last pass)
	thru []bool    // per node: failed-over primary forwarding to its target
	// excluded reports a load balancer target taken out of rotation (nil = none).
	excluded func(lb, target int) bool
}

// flowState is what one pass computes.
type flowState struct {
	l     [][2]float64 // per node: attempts arriving, per class
	k     [][2]float64 // per node: attempts × KB per request
	o     [][2]float64 // per node: original requests still alive arriving
	ea    []float64    // per edge: attempts
	ek    []float64    // per edge: attempts × KB
	err   float64      // original requests failing per second
	total float64      // original requests per second
	buf   []route
}

func newFlowState(m *model) *flowState {
	return &flowState{
		l: make([][2]float64, len(m.nodes)), k: make([][2]float64, len(m.nodes)), o: make([][2]float64, len(m.nodes)),
		ea: make([]float64, len(m.edges)), ek: make([]float64, len(m.edges)),
	}
}

func (fs *flowState) reset() {
	clear(fs.l)
	clear(fs.k)
	clear(fs.o)
	clear(fs.ea)
	clear(fs.ek)
	fs.err, fs.total = 0, 0
}

// evaluator decides how much of a node's arrivals it serves this pass.
type evaluator interface {
	// serve returns the attempts per second the node passes on, the class mix
	// and KB per request of what it passes on, and the read hit share that ends
	// at the node.
	serve(u int, l, k [2]float64) (served float64, mix, kb [2]float64, hit float64)
}

// trafficOf is a client's traffic model with out-of-range values clamped.
func trafficOf(n *archdoc.Node) (read, kb float64) {
	t := n.Traffic
	read = float64(min(max(t.ReadPermille, 0), 1000)) / 1000
	kb = float64(max(t.ReqKB, 0)) + float64(max(t.RespKB, 0))
	return read, kb
}

func retriesOf(n *archdoc.Node) int { return min(max(n.Retries, 0), maxRetries) }

// retryTerms gives 1 + P + … + P^R (attempts per request) and P^(R+1) (the
// share that still fails after every retry).
func retryTerms(p float64, r int) (attempts, fail float64) {
	pk := 1.0
	for range r + 1 {
		attempts += pk
		pk = mul(pk, p)
	}
	return attempts, pk
}

// routesAt is a node's routes for class c with load balancer targets out of
// rotation removed (their share spread over the rest; with none left it fails).
func (m *model) routesAt(fs *flowState, u, c int, in *passInput) ([]route, float64) {
	mn := &m.nodes[u]
	rs, ef := mn.routes[c], mn.errFrac[c]
	if in.excluded == nil || mn.n.Kind != archdoc.KindLB {
		return rs, ef
	}
	out, live := 0.0, 0.0
	for _, r := range rs {
		if in.excluded(u, m.edges[r.e].to) {
			out += r.frac
		} else {
			live += r.frac
		}
	}
	if out == 0 {
		return rs, ef
	}
	fs.buf = fs.buf[:0]
	if live == 0 {
		return fs.buf, ef + out
	}
	scale := (live + out) / live
	for _, r := range rs {
		if !in.excluded(u, m.edges[r.e].to) {
			fs.buf = append(fs.buf, route{e: r.e, frac: mul(r.frac, scale)})
		}
	}
	return fs.buf, ef
}

// pass pushes one round of traffic through the graph in topological order.
// Load (attempts, retries included) and original requests are tracked apart: a
// node passes on what it served, an edge multiplies attempts by the caller's
// retries against the last pass's failure probability of the link and the
// target, and an original request fails on an edge with that probability to
// the power retries+1. Below an async edge only load continues.
func (m *model) pass(fs *flowState, in *passInput, ev evaluator) {
	fs.reset()
	for _, c := range m.clients {
		rps := in.rps[c]
		if !(rps > 0) {
			continue
		}
		read, kb := trafficOf(m.nodes[c].n)
		lr := mul(rps, read)
		lw := rps - lr
		fs.l[c] = [2]float64{lr, lw}
		fs.k[c] = [2]float64{mul(lr, kb), mul(lw, kb)}
		fs.o[c] = fs.l[c]
		fs.total += rps
	}
	for _, u := range m.order {
		mn := &m.nodes[u]
		l, k, o := fs.l[u], fs.k[u], fs.o[u]
		if in.thru != nil && in.thru[u] && mn.failTo >= 0 {
			f := mn.failTo
			for c := range 2 {
				fs.l[f][c] += l[c]
				fs.k[f][c] += k[c]
				fs.o[f][c] += o[c]
			}
			continue
		}
		var served, hit float64
		var mix, kb [2]float64
		if mn.n.Kind == archdoc.KindClient {
			served = l[0] + l[1]
			mix, kb = mixOf(l, k)
		} else {
			served, mix, kb, hit = ev.serve(u, l, k)
		}
		fwd := [2]float64{mul(served, mix[0]), mul(served, mix[1])}
		fwd[0] = mul(fwd[0], 1-hit)
		oc := [2]float64{mul(o[0], 1-hit), o[1]}
		r := retriesOf(mn.n)
		for c := range 2 {
			rs, ef := m.routesAt(fs, u, c, in)
			for _, rt := range rs {
				e := &m.edges[rt.e]
				v := e.to
				pe := 1 - mul(1-in.q[rt.e], 1-in.p[v])
				att, failAll := retryTerms(pe, r)
				a := mul(mul(fwd[c], rt.frac), att)
				ka := mul(a, kb[c])
				fs.ea[rt.e] += a
				fs.ek[rt.e] += ka
				deliver := 1 - in.q[rt.e]
				fs.l[v][c] += mul(a, deliver)
				fs.k[v][c] += mul(ka, deliver)
				orig := mul(oc[c], rt.frac)
				fail := mul(orig, failAll)
				fs.err += fail
				if !e.async {
					fs.o[v][c] += orig - fail
				}
			}
			fs.err += mul(oc[c], ef)
		}
	}
}

// mixOf is the class mix and KB per request of arrivals (reads only when empty).
func mixOf(l, k [2]float64) (mix, kb [2]float64) {
	lam := l[0] + l[1]
	if !(lam > 0) {
		return [2]float64{1, 0}, kb
	}
	mix = [2]float64{l[0] / lam, l[1] / lam}
	for c := range 2 {
		if l[c] > 0 {
			kb[c] = k[c] / l[c]
		}
	}
	return mix, kb
}

// linkLoss is the share an edge with its own bandwidth loses when overfull, and
// its utilization.
func (m *model) linkLoss(fs *flowState, e int) (loss, util float64) {
	me := &m.edges[e]
	if !(me.mbps > 0) {
		return 0, 0
	}
	util = mul(fs.ek[e], 8192) / mul(me.mbps, 1e6)
	if util > 1 {
		return 1 - 1/util, util
	}
	return 0, util
}

// transferMicros is the time to move one average request over an edge.
func (m *model) transferMicros(fs *flowState, e int) float64 {
	me := &m.edges[e]
	if !(fs.ea[e] > 0) || !(me.pathMbps > 0) {
		return 0
	}
	kb := fs.ek[e] / fs.ea[e]
	return mul(kb, 8192) / me.pathMbps
}
