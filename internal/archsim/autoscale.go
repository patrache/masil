package archsim

import "github.com/patrache/masil/internal/archdoc"

// scaler is one node's instance count in a timed run (C2.8 자동 확장).
type scaler struct {
	enabled   bool
	lo, hi    int
	active    int
	pending   []int // ticks at which ordered instances join, ascending
	lastScale int   // tick of the last join or removal
	target    float64
	warmup    int // ticks
	cooldown  int // ticks
}

func newScaler(n *archdoc.Node) scaler {
	s := scaler{active: max(n.Instances, 0), lastScale: -1 << 30}
	if !n.Autoscale.Enabled || n.Kind == archdoc.KindClient {
		return s
	}
	a := n.Autoscale
	s.enabled = true
	s.lo = max(a.Min, 0)
	s.hi = max(a.Max, s.lo)
	s.active = min(max(s.active, s.lo), s.hi)
	s.target = float64(min(max(a.TargetCPUPermille, 1), 1000)) / 1000
	s.warmup = max(a.WarmupS, 0) * 10
	s.cooldown = max(a.CooldownS, 0) * 10
	return s
}

// join brings in the instances whose warm-up ends by tick.
func (s *scaler) join(tick int) {
	for len(s.pending) > 0 && s.pending[0] <= tick {
		s.pending = s.pending[1:]
		s.active++
		s.lastScale = tick
	}
}

// decide runs once a second: CPU utilization above the target orders one
// instance (it joins after the warm-up); well below it, with nothing on order
// and the cooldown over, one instance leaves. Counts stay in Min..Max.
func (s *scaler) decide(tick int, cpuUtil float64, emit func(kind EventKind, value int)) {
	if !s.enabled {
		return
	}
	switch {
	case cpuUtil > s.target && s.active+len(s.pending) < s.hi:
		s.pending = append(s.pending, tick+s.warmup)
		emit(EventScaleOut, s.active+len(s.pending))
	case len(s.pending) == 0 && s.active > max(s.lo, 1) && tick-s.lastScale >= s.cooldown &&
		mul(cpuUtil, float64(s.active))/float64(s.active-1) < s.target:
		s.active--
		s.lastScale = tick
		emit(EventScaleIn, s.active)
	}
}
