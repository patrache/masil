// Package archsim is the deterministic architecture simulation engine (spec
// C2.9, JC8): steady-state analysis and timed runs over an archdoc.Design. It
// is pure computation — no UI, network or files — and gives the same integers
// on every platform: its own random generator, a portable logarithm, no
// architecture-specific math functions, every float product rounded explicitly
// so no multiply-add is fused (TestNoFusedMultiplyAdd), and maps walked in
// sorted ID order only.
package archsim

import "math"

// golden is 2^64 / φ, the SplitMix64 increment and the stream multiplier.
const golden = 0x9e3779b97f4a7c15

// rng is SplitMix64 (Steele, Lea, Flood 2014): a fixed algorithm, so the
// sequence never depends on the Go version.
type rng struct{ s uint64 }

func newRNG(seed uint64) *rng { return &rng{s: seed} }

func (r *rng) u64() uint64 {
	r.s += golden
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// f64 is uniform in [0, 1) with 53 random bits.
func (r *rng) f64() float64 {
	return float64(float64(r.u64()>>11) * 0x1p-53)
}

// exp draws from the exponential distribution with the given rate (mean
// 1/rate). It always uses one draw; rate <= 0 gives +Inf.
func (r *rng) exp(rate float64) float64 {
	u := r.f64()
	if !(rate > 0) {
		return math.Inf(1)
	}
	v := -detLog(1-u) / rate
	if v == 0 {
		return 0 // not -0
	}
	return v
}

// split returns an independent stream (per node, per frame) without advancing
// r: state ^ stream·golden.
func (r *rng) split(stream uint64) *rng {
	return &rng{s: r.s ^ stream*golden}
}
