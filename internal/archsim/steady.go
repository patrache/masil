package archsim

import (
	"math"
	"slices"

	"github.com/patrache/masil/internal/archdoc"
)

// maxInt bounds every reported integer so a hostile design cannot overflow.
const maxInt = 1_000_000_000_000_000

// toInt rounds once; NaN and negatives become 0, huge values maxInt.
func toInt(x float64) int {
	switch {
	case !(x > 0):
		return 0
	case x >= maxInt:
		return maxInt
	}
	return int(math.Round(x))
}

func permilleOf(x float64) int { return toInt(mul(x, 1000)) }

// nodeEval is one node's state under some load.
type nodeEval struct {
	lam      float64 // attempts arriving per second
	inst     float64 // instances serving
	cap      float64 // requests/s at the first limit (+Inf when nothing limits)
	util     float64 // lam / cap
	limit    LimitKind
	demand   float64 // bottleneck numbers (see Bottleneck)
	perUnit  float64
	cpuUtil  float64
	over     bool
	down     bool
	p        float64 // failure probability of one attempt
	hasSt    bool
	st       station // CPU servers with the arrival rate scaled to util
	pWait    float64
	hit      float64 // read share that ends here (CDN, cache)
	timeoutS float64
	fixedS   float64 // fixed latency (external service, object storage) + injected
}

func (ne *nodeEval) served() float64 {
	switch {
	case ne.down:
		return 0
	case ne.over:
		return ne.cap
	}
	return ne.lam
}

// assess computes a node's limits (C2.9): CPU (instances × cores servers of
// speed 1e6/CPU µs), disk IOPS, NIC, connection pool (Little's law), and the
// fixed request limit of external services, object storage and queues. The
// first limit to fill sets the utilization; ties go to that order.
func (m *model) assess(u int, inst, addMs float64, l, k [2]float64) nodeEval {
	n := m.nodes[u].n
	lam := l[0] + l[1]
	ne := nodeEval{lam: lam, inst: inst, cap: math.Inf(1), timeoutS: float64(max(n.TimeoutMs, 0)) / 1000}
	ne.fixedS = (float64(max(n.Params.FixedLatencyMs, 0)) + math.Max(addMs, 0)) / 1000
	memTot := mul(float64(max(n.MemoryMiB, 0)), math.Max(inst, 0))
	ws := float64(max(n.Params.WorkingSetMiB, 0))
	switch n.Kind {
	case archdoc.KindCDN, archdoc.KindCache:
		ne.hit = float64(min(max(n.Params.HitPermille, 0), 1000)) / 1000
		if n.Kind == archdoc.KindCache && ws > memTot {
			ne.hit = mul(ne.hit, memTot/ws)
		}
	}
	if !(inst > 0) {
		ne.down, ne.p, ne.hit = true, 1, 0
		return ne
	}
	consider := func(kind LimitKind, c, demand, perUnit float64) {
		if c < ne.cap {
			ne.cap, ne.limit, ne.demand, ne.perUnit = c, kind, demand, perUnit
		}
	}
	cpu := float64(max(n.CPUMicrosPerReq, 0))
	servers := mul(float64(max(n.CPUCores, 0)), inst)
	cpuCap := math.Inf(1)
	if cpu > 0 {
		cpuCap = mul(servers, 1e6) / cpu
		consider(LimitCPU, cpuCap, lam, cpu)
	}
	if lam > 0 && (n.Params.ReadIOPSMilli > 0 || n.Params.WriteIOPSMilli > 0) {
		rIO := float64(max(n.Params.ReadIOPSMilli, 0)) / 1000
		if (n.Kind == archdoc.KindDBPrimary || n.Kind == archdoc.KindDBReplica) && ws > memTot && memTot > 0 {
			rIO = mul(rIO, ws/memTot)
		}
		wIO := float64(max(n.Params.WriteIOPSMilli, 0)) / 1000
		rd, wd := mul(l[0], rIO), mul(l[1], wIO)
		if dem := rd + wd; dem > 0 {
			iops := mul(float64(max(n.Disk.IOPS, 0)), inst)
			demand, per := l[1], mul(wIO, 1000)
			if rd > wd {
				demand, per = l[0], mul(rIO, 1000)
			}
			consider(LimitIOPS, mul(lam, iops)/dem, demand, per)
		}
	}
	if lam > 0 {
		if kb := (k[0] + k[1]) / lam; kb > 0 {
			consider(LimitNIC, mul(mul(float64(max(n.NICMbps, 0)), inst), 1e6)/mul(kb, 8192), lam, mul(kb, 1000))
		}
	}
	if n.Params.PoolMax > 0 && cpu > 0 {
		consider(LimitPool, mul(float64(n.Params.PoolMax), 1e6)/cpu, lam, cpu)
	}
	if n.Params.MaxRPS > 0 {
		switch n.Kind {
		case archdoc.KindExternal, archdoc.KindObjectStorage:
			consider(LimitExt, float64(n.Params.MaxRPS), lam, 1000)
		case archdoc.KindQueue:
			consider(LimitQueue, float64(n.Params.MaxRPS), lam, 1000)
		}
	}
	switch {
	case !(lam > 0):
	case ne.cap == 0:
		ne.util = math.Inf(1)
	case !math.IsInf(ne.cap, 1):
		ne.util = lam / ne.cap
	}
	if lam > 0 && cpuCap > 0 && !math.IsInf(cpuCap, 1) {
		ne.cpuUtil = lam / cpuCap
	} else if lam > 0 && cpuCap == 0 {
		ne.cpuUtil = math.Inf(1)
	}
	ne.over = ne.util >= 1
	pOver, pTimeout := 0.0, 0.0
	if ne.over {
		pOver = 1 - ne.cap/lam
	}
	if cpu > 0 && servers >= 1 {
		c := int(math.Min(servers, 1e6))
		mu := 1e6 / cpu
		ne.hasSt = true
		ne.st = station{c: c, mu: mu, lambda: mul(mul(math.Min(ne.util, 1), float64(c)), mu)}
		if !ne.over {
			ne.pWait = ne.st.waitProb()
			if ne.timeoutS > 0 {
				theta := ne.st.capacity() - ne.st.lambda
				pTimeout = mul(ne.pWait, detExp(-mul(theta, ne.timeoutS)))
			}
		}
	}
	pErr := float64(min(max(n.Params.ErrorPermille, 0), 1000)) / 1000
	ne.p = 1 - mul(mul(1-pOver, 1-pTimeout), 1-pErr)
	return ne
}

// autoscaled is the steady-state instance count of an autoscaled node: the
// fewest in Min..Max whose CPU utilization stays at the target.
func autoscaled(n *archdoc.Node, lam float64) float64 {
	a := n.Autoscale
	lo := max(a.Min, 0)
	hi := max(a.Max, lo)
	cpu, cores := float64(max(n.CPUMicrosPerReq, 0)), float64(max(n.CPUCores, 0))
	target := float64(min(max(a.TargetCPUPermille, 1), 1000)) / 1000
	if !(cpu > 0) || !(cores > 0) || !(lam > 0) {
		return float64(lo)
	}
	need := math.Ceil(mul(lam, cpu)/mul(mul(cores, 1e6), target) - 1e-9)
	return math.Min(math.Max(need, float64(lo)), float64(hi))
}

func baseInstances(n *archdoc.Node) float64 {
	if n.Autoscale.Enabled {
		lo := max(n.Autoscale.Min, 0)
		return float64(min(max(n.Instances, lo), max(n.Autoscale.Max, lo)))
	}
	return float64(max(n.Instances, 0))
}

// steadyEval serves every node at its steady-state capacity.
type steadyEval struct {
	m  *model
	ns []nodeEval
}

func (se *steadyEval) serve(u int, l, k [2]float64) (float64, [2]float64, [2]float64, float64) {
	n := se.m.nodes[u].n
	inst := baseInstances(n)
	if n.Autoscale.Enabled {
		inst = autoscaled(n, l[0]+l[1])
	}
	ne := se.m.assess(u, inst, 0, l, k)
	se.ns[u] = ne
	mix, kb := mixOf(l, k)
	return ne.served(), mix, kb, ne.hit
}

// survival is P(T > t) for one M/M/c response time T = W + S: with
// probability 1−C no wait, else an Exp(θ) wait, θ = cμ − λ.
func (ne *nodeEval) survival(t float64) float64 {
	mu, c := ne.st.mu, ne.pWait
	es := detExp(-mul(mu, t))
	if !(c > 0) {
		return es
	}
	theta := ne.st.capacity() - ne.st.lambda
	var h float64
	if d := theta - mu; math.Abs(d) <= mul(1e-9, mu) {
		h = mul(1+mul(mu, t), es)
	} else {
		h = (mul(theta, es) - mul(mu, detExp(-mul(theta, t)))) / d
	}
	return mul(1-c, es) + mul(c, h)
}

// quantileMs is the node's own latency quantile q (wait + service + fixed),
// capped at the timeout: bisection on the exact M/M/c survival function.
func (ne *nodeEval) quantileMs(q float64) float64 {
	switch {
	case ne.down:
		return 0
	case ne.over:
		return mul(ne.timeoutS, 1000)
	case !ne.hasSt:
		return mul(ne.fixedS, 1000)
	}
	target := 1 - q
	lo, hi := 0.0, 1/ne.st.mu
	for i := 0; i < 200 && ne.survival(hi) > target; i++ {
		hi = mul(hi, 2)
	}
	for i := 0; i < 60 && hi-lo > 1e-7; i++ { // 0.1µs: far below the 1ms rounding
		mid := (lo + hi) / 2
		if ne.survival(mid) > target {
			lo = mid
		} else {
			hi = mid
		}
	}
	t := (lo + hi) / 2
	if ne.timeoutS > 0 {
		t = math.Min(t, ne.timeoutS)
	}
	return mul(t+ne.fixedS, 1000)
}

// percentile is the ⌈n·pct/100⌉-th smallest of sorted.
func percentile(sorted []float64, pct int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := (len(sorted)*pct + 99) / 100
	return sorted[max(i-1, 0)]
}

// sampleState is what the end-to-end sampler reads per node.
type sampleState struct {
	ns      []nodeEval
	waitS   []float64 // extra deterministic queue wait (timed runs)
	edgeS   []float64 // per edge: latency in seconds
	in      *passInput
	fs      *flowState
	visitMs func(u int, ms float64)
}

// sample draws n end-to-end requests (JC8): each picks a client by its share
// of traffic, a class by the read share, then walks the routes adding edge
// latency and the node's sampled wait and service. It ends where the request
// ends (no route, a cache or CDN hit, an async edge) or fails (a down node, an
// overloaded node or a wait past the timeout, which costs the timeout).
func (m *model) sample(r *rng, n int, s *sampleState, out []float64) []float64 {
	total := 0.0
	for _, c := range m.clients {
		total += math.Max(s.in.rps[c], 0)
	}
	if !(total > 0) || n <= 0 {
		return out
	}
	for range n {
		x := mul(r.f64(), total)
		u := m.clients[len(m.clients)-1]
		for _, c := range m.clients {
			if x < math.Max(s.in.rps[c], 0) {
				u = c
				break
			}
			x -= math.Max(s.in.rps[c], 0)
		}
		read, _ := trafficOf(m.nodes[u].n)
		cls := clsWrite
		if r.f64() < read {
			cls = clsRead
		}
		t := 0.0
	walk:
		for range len(m.nodes) + 1 {
			rs, _ := m.routesAt(s.fs, u, cls, s.in)
			x := r.f64()
			next := -1
			for _, rt := range rs {
				if x < rt.frac {
					next = rt.e
					break
				}
				x -= rt.frac
			}
			if next < 0 || m.edges[next].async {
				break
			}
			t += s.edgeS[next]
			v := m.edges[next].to
			for i := 0; i < len(m.nodes) && s.in.thru != nil && s.in.thru[v] && m.nodes[v].failTo >= 0; i++ {
				v = m.nodes[v].failTo
			}
			ne := &s.ns[v]
			nodeT := ne.fixedS
			switch {
			case ne.down:
				break walk
			case ne.over:
				t += ne.timeoutS
				s.visit(v, ne.timeoutS)
				break walk
			case ne.hasSt:
				w, sv := ne.st.sampleP(r, ne.pWait)
				if s.waitS != nil {
					w += s.waitS[v]
				}
				if ne.timeoutS > 0 && w+sv > ne.timeoutS {
					t += ne.timeoutS
					s.visit(v, ne.timeoutS)
					break walk
				}
				nodeT += w + sv
			}
			t += nodeT
			s.visit(v, nodeT)
			if cls == clsRead && ne.hit > 0 && r.f64() < ne.hit {
				break
			}
			u = v
		}
		out = append(out, mul(t, 1000))
	}
	return out
}

func (s *sampleState) visit(u int, sec float64) {
	if s.visitMs != nil {
		s.visitMs(u, mul(sec, 1000))
	}
}

// steady runs the fixed point: passes repeat with the last pass's failure
// probabilities (retries feed back into load) until nothing changes by 1e-9 or
// 20 passes.
func (m *model) steady(in *passInput) (*flowState, *steadyEval) {
	fs := newFlowState(m)
	se := &steadyEval{m: m, ns: make([]nodeEval, len(m.nodes))}
	if in.p == nil {
		in.p = make([]float64, len(m.nodes))
	}
	if in.q == nil {
		in.q = make([]float64, len(m.edges))
	}
	for range maxFixedPointIt {
		clear(se.ns)
		m.pass(fs, in, se)
		delta := 0.0
		for u := range m.nodes {
			np := se.ns[u].p
			if m.nodes[u].n.Kind == archdoc.KindClient || in.thru != nil && in.thru[u] {
				np = 0
			}
			delta = math.Max(delta, math.Abs(np-in.p[u]))
			in.p[u] = np
		}
		for e := range m.edges {
			nq, _ := m.linkLoss(fs, e)
			delta = math.Max(delta, math.Abs(nq-in.q[e]))
			in.q[e] = nq
		}
		if !(delta >= fixedPointEps) {
			break
		}
	}
	return fs, se
}

func (m *model) clientRPS() []float64 {
	rps := make([]float64, len(m.nodes))
	for _, c := range m.clients {
		rps[c] = float64(max(m.nodes[c].n.Traffic.RPS, 0))
	}
	return rps
}

// Analyze is the steady-state analysis (C2.9 "즉시 분석"): constant client
// traffic, no faults, autoscaled nodes at the size their target needs. It
// accepts unvalidated designs: degenerate values give Down or Overloaded nodes,
// never NaN, negative numbers or a panic.
func Analyze(d *archdoc.Design, opt Options) *Result {
	if d == nil {
		d = &archdoc.Design{}
	}
	m := newModel(d)
	in := &passInput{rps: m.clientRPS()}
	fs, se := m.steady(in)
	samples := opt.Samples
	if samples <= 0 {
		samples = defaultSamples
	}
	ss := &sampleState{ns: se.ns, in: in, fs: fs, edgeS: m.edgeSeconds(fs, nil)}
	lat := m.sample(newRNG(d.Seed).split(1), samples, ss, make([]float64, 0, samples))
	slices.Sort(lat)
	r := &Result{
		EngineVersion: EngineVersion, Nodes: map[string]NodeResult{}, Edges: map[string]EdgeResult{},
		RPS: toInt(fs.total), P50Ms: toInt(percentile(lat, 50)), P99Ms: toInt(percentile(lat, 99)),
		Findings: archdoc.Check(d),
	}
	if fs.total > 0 {
		r.ErrorPermille = min(permilleOf(fs.err/fs.total), 1000)
	}
	inst := make([]float64, len(m.nodes))
	for u := range m.nodes {
		inst[u] = se.ns[u].inst
	}
	cost := m.cost(inst, fs, in.rps)
	r.Cost = cost
	for u := range m.nodes {
		id := m.nodes[u].n.ID
		ne := &se.ns[u]
		nr := NodeResult{AvgInstancesMilli: permilleOf(ne.inst), CostUSDMonth: cost.ByNode[id]}
		if m.nodes[u].n.Kind == archdoc.KindClient {
			nr.InRPS = toInt(in.rps[u])
			nr.AvgInstancesMilli = permilleOf(float64(max(m.nodes[u].n.Instances, 0)))
		} else {
			fillNode(&nr, ne, in.p[u])
		}
		r.Nodes[id] = nr
		r.Overloaded = r.Overloaded || nr.Overloaded
	}
	var bn bottleneckPick
	for u := range m.nodes { // ID order
		bn.node(m.nodes[u].n.ID, &se.ns[u])
	}
	for e := range m.edges {
		_, util := m.linkLoss(fs, e)
		er := EdgeResult{RPS: toInt(fs.ea[e]), UtilPermille: permilleOf(util), LatencyMicros: toInt(mul(ss.edgeS[e], 1e6))}
		r.Edges[m.edges[e].e.ID] = er
		r.Overloaded = r.Overloaded || util >= 1
		bn.edge(m.edges[e].e.ID, util, fs, e)
	}
	r.Bottleneck = bn.result()
	return r
}

func fillNode(nr *NodeResult, ne *nodeEval, p float64) {
	nr.InRPS = toInt(ne.lam)
	if !math.IsInf(ne.cap, 1) {
		nr.CapacityRPS = toInt(ne.cap)
	}
	nr.UtilPermille = permilleOf(ne.util)
	nr.Limit = ne.limit
	nr.P50Ms = toInt(ne.quantileMs(0.5))
	nr.P99Ms = toInt(ne.quantileMs(0.99))
	nr.ErrPermille = min(permilleOf(p), 1000)
	nr.Overloaded, nr.Down = ne.over, ne.down
}

// edgeSeconds is every edge's latency: base + injected + transfer time.
func (m *model) edgeSeconds(fs *flowState, addMs []float64) []float64 {
	out := make([]float64, len(m.edges))
	for e := range m.edges {
		micros := m.edges[e].baseMicros + m.transferMicros(fs, e)
		if addMs != nil {
			micros += mul(math.Max(addMs[e], 0), 1000)
		}
		out[e] = micros / 1e6
	}
	return out
}

// bottleneckPick keeps the highest utilization seen (nodes before edges and
// lower IDs win ties).
type bottleneckPick struct {
	best *Bottleneck
	util float64
}

func (b *bottleneckPick) consider(util float64, bn Bottleneck) {
	if util > b.util {
		b.util = util
		bn.UtilPermille = permilleOf(util)
		b.best = &bn
	}
}

func (b *bottleneckPick) node(id string, ne *nodeEval) {
	if ne.down || ne.limit == "" {
		return
	}
	b.consider(ne.util, Bottleneck{Target: id, Limit: ne.limit, Demand: toInt(ne.demand), PerUnitMilli: toInt(ne.perUnit)})
}

func (b *bottleneckPick) edge(id string, util float64, fs *flowState, e int) {
	if !(util > 0) {
		return
	}
	kb := 0.0
	if fs.ea[e] > 0 {
		kb = fs.ek[e] / fs.ea[e]
	}
	b.consider(util, Bottleneck{Target: id, Limit: LimitLink, Demand: toInt(fs.ea[e]), PerUnitMilli: toInt(mul(kb, 1000))})
}

func (b *bottleneckPick) result() *Bottleneck { return b.best }
