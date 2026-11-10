package archsim

import (
	"cmp"
	"slices"

	"github.com/patrache/masil/internal/archdoc"
)

// fault is one scheduled fault resolved against the model.
type fault struct {
	f          *archdoc.Fault
	start, end int   // ticks; end < 0 = until the run ends
	nodes      []int // affected nodes
	edge       int   // affected edge (latency on an edge), -1 = none
}

// faultEffects is what the active faults do in one tick.
type faultEffects struct {
	down      []bool
	instOff   []int
	addMs     []float64 // per node
	edgeAddMs []float64 // per edge
}

func newFaultEffects(m *model) *faultEffects {
	return &faultEffects{down: make([]bool, len(m.nodes)), instOff: make([]int, len(m.nodes)),
		addMs: make([]float64, len(m.nodes)), edgeAddMs: make([]float64, len(m.edges))}
}

// resolveFaults turns the design's faults into tick windows (C2.9 장애):
// node_off and instances_off hit one node, latency a node or an edge, az_off
// and region_off every node inside the box. Faults whose target does not exist
// are dropped. Order: start tick, then fault ID.
func (m *model) resolveFaults() []fault {
	var out []fault
	for _, f := range sortedUnique(m.d.Faults, func(f *archdoc.Fault) string { return f.ID }) {
		ft := fault{f: f, start: max(f.AtS, 0) * 10, end: -1, edge: -1}
		if f.DurS > 0 {
			ft.end = ft.start + f.DurS*10
		}
		switch f.Kind {
		case archdoc.FaultNodeOff, archdoc.FaultInstancesOff:
			if u, ok := m.idx[f.Target]; ok {
				ft.nodes = []int{u}
			}
		case archdoc.FaultLatency:
			if u, ok := m.idx[f.Target]; ok {
				ft.nodes = []int{u}
			} else if e, ok := m.eidx[f.Target]; ok {
				ft.edge = e
			}
		case archdoc.FaultAZOff, archdoc.FaultRegionOff:
			if m.boxes[f.Target] != nil {
				for u := range m.nodes {
					if slices.Contains(m.nodes[u].boxes, f.Target) {
						ft.nodes = append(ft.nodes, u)
					}
				}
			}
		}
		if len(ft.nodes) > 0 || ft.edge >= 0 {
			out = append(out, ft)
		}
	}
	slices.SortStableFunc(out, func(a, b fault) int { return cmp.Compare(a.start, b.start) })
	return out
}

// apply sets the effects of the faults active at tick and reports starts and
// ends.
func applyFaults(faults []fault, tick int, fx *faultEffects, emit func(Event)) {
	clear(fx.down)
	clear(fx.instOff)
	clear(fx.addMs)
	clear(fx.edgeAddMs)
	for i := range faults {
		ft := &faults[i]
		if tick == ft.start {
			emit(Event{T10: tick, Kind: EventFaultStart, Target: ft.f.Target, Value: max(ft.f.Count, ft.f.AddMs, 0)})
		}
		if tick == ft.end {
			emit(Event{T10: tick, Kind: EventFaultEnd, Target: ft.f.Target})
		}
		if tick < ft.start || ft.end >= 0 && tick >= ft.end {
			continue
		}
		add := float64(max(ft.f.AddMs, 0))
		for _, u := range ft.nodes {
			switch ft.f.Kind {
			case archdoc.FaultInstancesOff:
				fx.instOff[u] += max(ft.f.Count, 0)
			case archdoc.FaultLatency:
				fx.addMs[u] += add
			default:
				fx.down[u] = true
			}
		}
		if ft.edge >= 0 {
			fx.edgeAddMs[ft.edge] += add
		}
	}
}
