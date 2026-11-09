package archsim

import (
	"context"
	"math"
	"slices"

	"github.com/patrache/masil/internal/archdoc"
)

// TimedOptions tunes RunTimed.
type TimedOptions struct {
	DurationS       int // 0 = the design's duration_s (300 if unset), at most 600
	FrameEveryTicks int // 0 = 10 (one frame per simulated second)
	SamplesPerFrame int // 0 = 2,000
}

// EventKind is something that happened in a timed run. Events carry no text:
// the UI words them from the catalog.
type EventKind string

const (
	EventFaultStart    EventKind = "fault_start"    // Target = fault target, Value = count or added ms
	EventFaultEnd      EventKind = "fault_end"      // Target = fault target
	EventLBReroute     EventKind = "lb_reroute"     // Target = LB, Value = targets left in rotation
	EventFailoverStart EventKind = "failover_start" // Target = primary, Value = failover seconds
	EventFailoverDone  EventKind = "failover_done"  // Target = promoted node
	EventWritesFailing EventKind = "writes_failing" // Target = primary with no live failover target
	EventScaleOut      EventKind = "scale_out"      // Target = node, Value = instances after the order
	EventScaleIn       EventKind = "scale_in"       // Target = node, Value = instances left
	EventQueueDrop     EventKind = "queue_drop"     // Target = node, Value = requests dropped in that tick
)

// Event is one timed-run event at T10 tenths of a second.
type Event struct {
	T10    int       `json:"t10"`
	Kind   EventKind `json:"kind"`
	Target string    `json:"target"`
	Value  int       `json:"value"`
}

// Frame is the state at T10 (the end of a frame); RPS and ErrPermille cover
// the frame's ticks, the rest is the last tick.
type Frame struct {
	T10         int                  `json:"t10"`
	RPS         int                  `json:"rps"`
	P50Ms       int                  `json:"p50_ms"`
	P99Ms       int                  `json:"p99_ms"`
	ErrPermille int                  `json:"err_permille"`
	Nodes       map[string]FrameNode `json:"nodes"`
}

// FrameNode is one node in a frame.
type FrameNode struct {
	UtilPermille int  `json:"util_permille"`
	Instances    int  `json:"instances"`
	QueueLen     int  `json:"queue_len"`
	Down         bool `json:"down"`
}

// Timeline is a timed run. Summary covers the whole run: averages for loads,
// instances and cost, peaks for utilization, latencies from Samples (20,000)
// requests spread over the frames.
type Timeline struct {
	EngineVersion int     `json:"engine_version"`
	Frames        []Frame `json:"frames"`
	Events        []Event `json:"events"`
	Summary       *Result `json:"summary"`
}

const (
	tickS           = 0.1
	defaultDuration = 300
	maxDuration     = 600
	defaultFrameTks = 10
	defaultPerFrame = 2_000
	cancelEvery     = 100 // ticks between context checks
)

// failover is one primary's failover progress.
type failover struct {
	active bool // down, failover clock running
	since  int
	given  bool // the target was dead at the deadline: no recovery
}

// timedEval serves one tick: arrivals join each node's queue, the node works
// off up to capacity × 0.1s and drops what overflows QueueCap; a queue wait
// past the timeout fails everything served.
type timedEval struct {
	m        *model
	inst     []float64
	addMs    []float64
	q        []float64 // queue length per node
	ns       []nodeEval
	sn       []nodeEval // copy for the sampler (deterministic queue wait)
	waitS    []float64
	mix, kb  [][2]float64
	drops    []float64
	served   []float64
	dropping []bool
	tick     int
	emit     func(Event)
}

func (te *timedEval) serve(u int, l, k [2]float64) (float64, [2]float64, [2]float64, float64) {
	n := te.m.nodes[u].n
	ne := te.m.assess(u, te.inst[u], te.addMs[u], l, k)
	lam := ne.lam
	if lam > 0 {
		te.mix[u], te.kb[u] = mixOf(l, k)
	} else if te.mix[u] == ([2]float64{}) {
		te.mix[u] = [2]float64{1, 0}
	}
	te.drops[u], te.waitS[u] = 0, 0
	if ne.down {
		te.q[u], te.served[u] = 0, 0
		te.ns[u], te.sn[u] = ne, ne
		return 0, te.mix[u], te.kb[u], 0
	}
	arrive := mul(lam, tickS)
	queue := te.q[u] + arrive
	var processed float64
	switch {
	case math.IsInf(ne.cap, 1):
		processed, queue = queue, 0
	default:
		processed = math.Min(queue, mul(ne.cap, tickS))
		queue -= processed
	}
	drop := math.Max(queue-float64(max(n.QueueCap, 0)), 0)
	queue -= drop
	te.q[u] = queue
	served := processed / tickS
	te.served[u] = served
	pDrop := 0.0
	if arrive > 0 {
		pDrop = math.Min(drop/arrive, 1)
	}
	if drop > 0 {
		te.drops[u] = drop
		if !te.dropping[u] {
			te.emit(Event{T10: te.tick, Kind: EventQueueDrop, Target: n.ID, Value: toInt(drop)})
		}
	}
	te.dropping[u] = drop > 0
	pTimeout := ne.pTimeout
	if queue > 0 && ne.cap > 0 {
		te.waitS[u] = queue / ne.cap
		pTimeout = 0
		if ne.timeoutS > 0 && te.waitS[u] > ne.timeoutS {
			pTimeout = 1
		}
	}
	ne.p = 1 - mul(mul(1-pDrop, 1-pTimeout), 1-ne.pErr)
	if ne.cap > 0 && !math.IsInf(ne.cap, 1) {
		ne.util = math.Max(ne.util, served/ne.cap)
	}
	te.ns[u] = ne
	sn := ne
	sn.over = lam > 0 && ne.cap == 0 || pTimeout == 1
	if ne.util >= 1 { // the backlog is the deterministic wait; no random wait on top
		sn.st.lambda, sn.pWait = 0, 0
	}
	te.sn[u] = sn
	return served, te.mix[u], te.kb[u], ne.hit
}

// nodeAcc accumulates one node over the run for the summary.
type nodeAcc struct {
	lam, err, inst, capSum float64
	capTicks               int
	peak                   float64
	peakEval               nodeEval
	over, down             bool
	lat                    []float64
}

// RunTimed is the tick simulation (C2.9 "시간 실행"): 0.1s ticks for the
// run's duration with the traffic curve, scheduled faults, LB health checks,
// primary failover, autoscaling with warm-up, queues that fill and drain, and
// retries feeding back one tick later. It records one frame per
// FrameEveryTicks ticks and returns ctx.Err() when cancelled.
func RunTimed(ctx context.Context, d *archdoc.Design, opt TimedOptions) (*Timeline, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d == nil {
		d = &archdoc.Design{}
	}
	dur := opt.DurationS
	if dur <= 0 {
		dur = d.DurationS
	}
	if dur <= 0 {
		dur = defaultDuration
	}
	dur = min(dur, maxDuration)
	every := opt.FrameEveryTicks
	if every <= 0 {
		every = defaultFrameTks
	}
	perFrame := opt.SamplesPerFrame
	if perFrame <= 0 {
		perFrame = defaultPerFrame
	}
	m := newModel(d)
	N, E := len(m.nodes), len(m.edges)
	ticks := dur * 10
	frames := ticks / every
	tl := &Timeline{EngineVersion: EngineVersion, Frames: make([]Frame, 0, frames)}
	emit := func(e Event) { tl.Events = append(tl.Events, e) }

	faults := m.resolveFaults()
	fx := newFaultEffects(m)
	scalers := make([]scaler, N)
	for u := range m.nodes {
		scalers[u] = newScaler(m.nodes[u].n)
	}
	in := &passInput{rps: make([]float64, N), p: make([]float64, N), q: make([]float64, E), thru: make([]bool, N)}
	down := make([]bool, N)
	downSince := make([]int, N)
	fo := make([]failover, N)
	lbTargets := map[int][]int{}
	lbOut := map[[2]int]bool{}
	for u := range m.nodes {
		if m.nodes[u].n.Kind != archdoc.KindLB {
			continue
		}
		var ts []int
		for c := range 2 {
			for _, r := range m.nodes[u].routes[c] {
				ts = append(ts, m.edges[r.e].to)
			}
		}
		slices.Sort(ts)
		lbTargets[u] = slices.Compact(ts)
	}
	tick := 0
	healthTicks := func(lb int) int { return max(m.nodes[lb].n.Params.HealthIntervalS, 0) * 10 }
	in.excluded = func(lb, v int) bool { return down[v] && tick-downSince[v] >= healthTicks(lb) }

	te := &timedEval{m: m, inst: make([]float64, N), addMs: fx.addMs, q: make([]float64, N), ns: make([]nodeEval, N),
		sn: make([]nodeEval, N), waitS: make([]float64, N), mix: make([][2]float64, N), kb: make([][2]float64, N),
		drops: make([]float64, N), served: make([]float64, N), dropping: make([]bool, N), emit: emit}
	fs := newFlowState(m)
	avg := newFlowState(m) // run totals of fs (divided by ticks at the end)
	acc := make([]nodeAcc, N)
	edgePeak := make([]float64, E)
	edgeAdd := make([]float64, E)
	var clientSum = make([]float64, N)
	var frameReq, frameErr, runReq, runErr float64
	root := newRNG(d.Seed)
	summaryLat := make([]float64, 0, defaultSamples)

	for ; tick < ticks; tick++ {
		if tick%cancelEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		applyFaults(faults, tick, fx, emit)
		for u := range m.nodes {
			s := &scalers[u]
			s.join(tick)
			n := m.nodes[u].n
			inst := float64(max(s.active-fx.instOff[u], 0))
			isDown := n.Kind != archdoc.KindClient && (fx.down[u] || !(inst > 0))
			if fx.down[u] {
				inst = 0
			}
			if isDown && !down[u] {
				downSince[u] = tick
			}
			down[u] = isDown
			te.inst[u] = inst
		}
		m.stepFailover(tick, down, fo, in.thru, emit)
		for lb, ts := range sortedLBs(lbTargets) {
			changed, live := false, 0
			for _, v := range ts {
				out := in.excluded(lb, v)
				if out != lbOut[[2]int{lb, v}] {
					lbOut[[2]int{lb, v}] = out
					changed = changed || out
				}
				if !out {
					live++
				}
			}
			if changed {
				emit(Event{T10: tick, Kind: EventLBReroute, Target: m.nodes[lb].n.ID, Value: live})
			}
		}
		if tick > 0 && tick%10 == 0 {
			for u := range m.nodes {
				if !down[u] {
					id := m.nodes[u].n.ID
					scalers[u].decide(tick, te.ns[u].cpuUtil, func(k EventKind, v int) { emit(Event{T10: tick, Kind: k, Target: id, Value: v}) })
				}
			}
		}
		t := float64(tick) / 10
		for _, c := range m.clients {
			in.rps[c] = 0
			if !fx.down[c] {
				in.rps[c] = curveRPS(m.nodes[c].n.Traffic, t, float64(dur))
			}
			clientSum[c] += in.rps[c]
		}
		for u := range m.nodes {
			switch {
			case in.thru[u] && m.nodes[u].failTo >= 0:
				in.p[u] = in.p[m.nodes[u].failTo]
			case down[u]:
				in.p[u] = 1
			}
		}
		te.tick = tick
		clear(te.ns)
		m.pass(fs, in, te)
		for u := range m.nodes {
			switch {
			case m.nodes[u].n.Kind == archdoc.KindClient:
				in.p[u] = 0
			case !in.thru[u]:
				in.p[u] = te.ns[u].p
			}
		}
		for e := range m.edges {
			loss, util := m.linkLoss(fs, e)
			in.q[e] = loss
			edgePeak[e] = math.Max(edgePeak[e], util)
			edgeAdd[e] += fx.edgeAddMs[e]
		}
		addFlow(avg, fs)
		frameReq += mul(fs.total, tickS)
		frameErr += mul(fs.err, tickS)
		for u := range m.nodes {
			a, ne := &acc[u], &te.ns[u]
			a.lam += ne.lam
			a.err += mul(ne.lam, ne.p)
			a.inst += te.inst[u]
			if !math.IsInf(ne.cap, 1) && !ne.down && m.nodes[u].n.Kind != archdoc.KindClient {
				a.capSum += ne.cap
				a.capTicks++
			}
			if ne.util > a.peak && ne.limit != "" && !ne.down {
				a.peak, a.peakEval = ne.util, *ne
			}
			a.over = a.over || ne.over
			a.down = a.down || down[u]
		}
		if (tick+1)%every != 0 {
			continue
		}
		fi := len(tl.Frames)
		ss := &sampleState{ns: te.sn, waitS: te.waitS, in: in, fs: fs, edgeS: m.edgeSeconds(fs, fx.edgeAddMs)}
		lat := m.sample(root.split(2).split(uint64(fi)), perFrame, ss, nil)
		slices.Sort(lat)
		share := defaultSamples / max(frames, 1)
		if fi < defaultSamples%max(frames, 1) {
			share++
		}
		ss.visitMs = func(u int, ms float64) { acc[u].lat = append(acc[u].lat, ms) }
		summaryLat = m.sample(root.split(3).split(uint64(fi)), share, ss, summaryLat)
		f := Frame{T10: tick + 1, RPS: toInt(frameReq / mul(float64(every), tickS)), P50Ms: toInt(percentile(lat, 50)),
			P99Ms: toInt(percentile(lat, 99)), Nodes: make(map[string]FrameNode, N)}
		if frameReq > 0 {
			f.ErrPermille = min(permilleOf(frameErr/frameReq), 1000)
		}
		for u := range m.nodes {
			inst := toInt(te.inst[u])
			f.Nodes[m.nodes[u].n.ID] = FrameNode{UtilPermille: permilleOf(te.ns[u].util), Instances: inst, QueueLen: toInt(te.q[u]), Down: down[u]}
		}
		tl.Frames = append(tl.Frames, f)
		runReq += frameReq
		runErr += frameErr
		frameReq, frameErr = 0, 0
	}
	slices.Sort(summaryLat)
	tl.Summary = m.summary(d, ticks, avg, acc, edgePeak, edgeAdd, clientSum, runReq, runErr, summaryLat)
	return tl, nil
}

// sortedLBs iterates load balancers in node (= ID) order.
func sortedLBs(m map[int][]int) func(func(int, []int) bool) {
	return func(yield func(int, []int) bool) {
		keys := make([]int, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}

// stepFailover moves each down primary through its failover (C2.9): writes
// (and reads) to it fail for FailoverS, then a live target takes over for
// good; a missing or dead target means no recovery. A primary back up before
// the deadline cancels the failover.
func (m *model) stepFailover(tick int, down []bool, fo []failover, thru []bool, emit func(Event)) {
	for u := range m.nodes {
		n := m.nodes[u].n
		if n.Kind != archdoc.KindDBPrimary || thru[u] {
			continue
		}
		f := &fo[u]
		target := m.nodes[u].failTo
		switch {
		case !down[u]:
			*f = failover{}
		case !f.active:
			f.active, f.since = true, tick
			if target >= 0 {
				emit(Event{T10: tick, Kind: EventFailoverStart, Target: n.ID, Value: max(n.Params.FailoverS, 0)})
			} else {
				emit(Event{T10: tick, Kind: EventWritesFailing, Target: n.ID})
			}
		}
		if !f.active || f.given || target < 0 || tick-f.since < max(n.Params.FailoverS, 0)*10 {
			continue
		}
		if down[target] {
			f.given = true
			emit(Event{T10: tick, Kind: EventWritesFailing, Target: n.ID})
			continue
		}
		thru[u] = true
		emit(Event{T10: tick, Kind: EventFailoverDone, Target: m.nodes[target].n.ID})
	}
}

func addFlow(dst, src *flowState) {
	for i := range src.l {
		for c := range 2 {
			dst.l[i][c] += src.l[i][c]
			dst.k[i][c] += src.k[i][c]
			dst.o[i][c] += src.o[i][c]
		}
	}
	for e := range src.ea {
		dst.ea[e] += src.ea[e]
		dst.ek[e] += src.ek[e]
	}
	dst.err += src.err
	dst.total += src.total
}

func scaleFlow(fs *flowState, f float64) {
	for i := range fs.l {
		for c := range 2 {
			fs.l[i][c] = mul(fs.l[i][c], f)
			fs.k[i][c] = mul(fs.k[i][c], f)
			fs.o[i][c] = mul(fs.o[i][c], f)
		}
	}
	for e := range fs.ea {
		fs.ea[e] = mul(fs.ea[e], f)
		fs.ek[e] = mul(fs.ek[e], f)
	}
	fs.err, fs.total = mul(fs.err, f), mul(fs.total, f)
}

// summary folds a run into one Result.
func (m *model) summary(d *archdoc.Design, ticks int, avg *flowState, acc []nodeAcc, edgePeak, edgeAdd, clientSum []float64,
	req, errs float64, lat []float64) *Result {
	inv := 1 / float64(max(ticks, 1))
	scaleFlow(avg, inv)
	r := &Result{EngineVersion: EngineVersion, Nodes: map[string]NodeResult{}, Edges: map[string]EdgeResult{},
		RPS: toInt(avg.total), P50Ms: toInt(percentile(lat, 50)), P99Ms: toInt(percentile(lat, 99)), Findings: archdoc.Check(d)}
	if req > 0 {
		r.ErrorPermille = min(permilleOf(errs/req), 1000)
	}
	inst := make([]float64, len(m.nodes))
	rps := make([]float64, len(m.nodes))
	for u := range m.nodes {
		inst[u] = mul(acc[u].inst, inv)
		rps[u] = mul(clientSum[u], inv)
	}
	r.Cost = m.cost(inst, avg, rps)
	var bn bottleneckPick
	for u := range m.nodes {
		a := &acc[u]
		id := m.nodes[u].n.ID
		nr := NodeResult{InRPS: toInt(mul(a.lam, inv)), AvgInstancesMilli: permilleOf(inst[u]), CostUSDMonth: r.Cost.ByNode[id],
			UtilPermille: permilleOf(a.peak), Limit: a.peakEval.limit, Overloaded: a.over, Down: a.down}
		if m.nodes[u].n.Kind == archdoc.KindClient {
			nr.InRPS = toInt(rps[u])
		}
		if a.capTicks > 0 {
			nr.CapacityRPS = toInt(a.capSum / float64(a.capTicks))
		}
		if a.lam > 0 {
			nr.ErrPermille = min(permilleOf(a.err/a.lam), 1000)
		}
		slices.Sort(a.lat)
		nr.P50Ms, nr.P99Ms = toInt(percentile(a.lat, 50)), toInt(percentile(a.lat, 99))
		r.Nodes[id] = nr
		r.Overloaded = r.Overloaded || a.over
		if a.peak > 0 {
			bn.node(id, &a.peakEval)
		}
	}
	edgeS := m.edgeSeconds(avg, nil)
	for e := range m.edges {
		id := m.edges[e].e.ID
		r.Edges[id] = EdgeResult{RPS: toInt(avg.ea[e]), UtilPermille: permilleOf(edgePeak[e]),
			LatencyMicros: toInt(mul(edgeS[e], 1e6) + mul(mul(edgeAdd[e], inv), 1000))}
		r.Overloaded = r.Overloaded || edgePeak[e] >= 1
		bn.edge(id, edgePeak[e], avg, e)
	}
	r.Bottleneck = bn.result()
	return r
}
