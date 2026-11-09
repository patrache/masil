package archsim

import (
	"math"
	"slices"
	"strconv"
	"testing"
)

func TestSplitMixKnownSequence(t *testing.T) {
	r := newRNG(0)
	if got := r.u64(); got != 0xe220a8397b1dcdaf {
		t.Fatalf("first = %#x", got)
	}
	if got := r.u64(); got != 0x6e789e6aa1b965f4 {
		t.Fatalf("second = %#x", got)
	}
	for i := 0; i < 1000; i++ {
		if f := r.f64(); f < 0 || f >= 1 {
			t.Fatalf("f64 = %v", f)
		}
	}
	// split streams differ from each other and from the parent, and do not advance it.
	p := newRNG(42)
	a, b := p.split(1), p.split(2)
	if a.u64() == b.u64() {
		t.Fatal("split streams collide")
	}
	if q := newRNG(42); p.u64() != q.u64() {
		t.Fatal("split advanced the parent")
	}
	if p.exp(0) != math.Inf(1) || newRNG(3).exp(-1) != math.Inf(1) {
		t.Fatal("exp with rate <= 0 must be +Inf")
	}
}

func ulpDiff(a, b float64) uint64 {
	ia, ib := math.Float64bits(a), math.Float64bits(b)
	if ia > ib {
		return ia - ib
	}
	return ib - ia
}

func TestDetLogClose(t *testing.T) {
	if detLog(1) != 0 {
		t.Fatalf("detLog(1) = %v", detLog(1))
	}
	r := newRNG(1)
	for i := 0; i < 10_000; i++ {
		x := r.f64() * 1e6
		if x == 0 {
			continue
		}
		if d := ulpDiff(detLog(x), math.Log(x)); d > 2 {
			t.Fatalf("detLog(%v) = %v, math.Log = %v (%d ulp)", x, detLog(x), math.Log(x), d)
		}
	}
	for _, x := range []float64{1e-300, 0.5, 2, math.E, 1e300} {
		if d := ulpDiff(detLog(x), math.Log(x)); d > 2 {
			t.Fatalf("detLog(%v) %d ulp off", x, d)
		}
	}
	// Subnormal input: amd64's assembly math.Log answers -709.08…; detLog is exact
	// on every architecture.
	if got := detLog(math.SmallestNonzeroFloat64); got != -744.4400719213812 {
		t.Fatalf("detLog(min subnormal) = %v", got)
	}
	if !math.IsInf(detLog(0), -1) || !math.IsNaN(detLog(-1)) || !math.IsInf(detLog(math.Inf(1)), 1) {
		t.Fatal("special cases")
	}
}

func TestErlangCKnown(t *testing.T) {
	if got := erlangC(1, 0.5); math.Abs(got-0.5) > 1e-12 {
		t.Fatalf("erlangC(1, 0.5) = %v", got)
	}
	if got := erlangC(2, 1); math.Abs(got-1.0/3) > 1e-12 {
		t.Fatalf("erlangC(2, 1) = %v", got)
	}
	if got := erlangC(500, 450); !(got > 0 && got < 1) {
		t.Fatalf("erlangC(500, 450) = %v", got)
	}
	if got := erlangC(100_000, 99_000); !(got >= 0 && got < 1) || math.IsNaN(got) {
		t.Fatalf("erlangC(100000, 99000) = %v", got)
	}
	// Degenerate: no servers or overload means everyone waits; no load means nobody does.
	for _, tc := range []struct {
		c    int
		a    float64
		want float64
	}{{0, 1, 1}, {-3, 1, 1}, {2, 2, 1}, {2, 5, 1}, {3, 0, 0}, {3, -1, 0}} {
		if got := erlangC(tc.c, tc.a); got != tc.want {
			t.Fatalf("erlangC(%d, %v) = %v, want %v", tc.c, tc.a, got, tc.want)
		}
	}
}

func sampleMeanWait(st station, n int, seed uint64) float64 {
	r := newRNG(seed)
	sum := 0.0
	for i := 0; i < n; i++ {
		w, _ := st.sample(r)
		sum += w
	}
	return sum / float64(n)
}

func TestMM1MeanWait(t *testing.T) {
	st := station{c: 1, mu: 100, lambda: 80}
	if math.Abs(st.rho()-0.8) > 1e-12 || math.Abs(st.meanWait()-0.04) > 1e-12 {
		t.Fatalf("rho %v meanWait %v", st.rho(), st.meanWait())
	}
	if got := sampleMeanWait(st, 200_000, 1); math.Abs(got/0.04-1) > 0.01 {
		t.Fatalf("sample mean %v vs 0.04", got)
	}
	// Service times have mean 1/μ.
	r := newRNG(1)
	sum := 0.0
	for i := 0; i < 200_000; i++ {
		_, s := st.sample(r)
		sum += s
	}
	if mean := sum / 200_000; math.Abs(mean/0.01-1) > 0.01 {
		t.Fatalf("service mean %v", mean)
	}
}

func TestMMcMeanWait(t *testing.T) {
	st := station{c: 4, mu: 100, lambda: 320}
	want := st.meanWait()
	if !(want > 0) {
		t.Fatalf("meanWait = %v", want)
	}
	if got := sampleMeanWait(st, 200_000, 1); math.Abs(got/want-1) > 0.01 {
		t.Fatalf("sample mean %v vs %v", got, want)
	}
}

func TestStationDegenerate(t *testing.T) {
	r := newRNG(9)
	for _, st := range []station{{0, 100, 10}, {2, 0, 10}, {2, 100, 0}, {0, 0, 0}, {2, 100, 500}, {-1, -5, -3}} {
		rho, mw := st.rho(), st.meanWait()
		w, s := st.sample(r)
		for _, v := range []float64{rho, mw, w, s} {
			if math.IsNaN(v) || v < 0 {
				t.Fatalf("%+v: rho %v wait %v sample %v %v", st, rho, mw, w, s)
			}
		}
	}
	// Every sample draws the same amount of randomness, so streams stay aligned.
	a, b := newRNG(5), newRNG(5)
	station{c: 1, mu: 100, lambda: 0}.sample(a)
	station{c: 1, mu: 100, lambda: 99}.sample(b)
	if a.u64() != b.u64() {
		t.Fatal("sample consumed a varying number of draws")
	}
}

// TestSplitStreamsDoNotOverlap: a child stream must not be a shifted copy of
// its parent or of a sibling, which a raw state XOR gives whenever the seed and
// stream·golden share no set bits (seed 0, small seeds — P3 review).
func TestSplitStreamsDoNotOverlap(t *testing.T) {
	const n, streams = 256, 64
	for _, seed := range []uint64{0, 1, 2, 42, 1 << 52, ^uint64(0)} {
		seen := map[uint64]string{}
		p := newRNG(seed)
		for range n {
			seen[p.u64()] = "parent"
		}
		parent := newRNG(seed)
		for k := uint64(0); k <= streams; k++ {
			c := parent.split(k)
			for i := range n {
				v := c.u64()
				if who, ok := seen[v]; ok {
					t.Fatalf("seed %d: split(%d) output %d repeats a value of %s", seed, k, i, who)
				}
				seen[v] = "split(" + strconv.FormatUint(k, 10) + ")"
			}
		}
	}
}

// erlangCRecurrence is the plain Erlang B recurrence over every server: the
// reference for erlangC's large-c path.
func erlangCRecurrence(c int, a float64) float64 {
	b := 1.0
	for k := 1; k <= c && b != 0; k++ {
		ab := float64(a * b)
		b = ab / (float64(k) + ab)
	}
	rho := a / float64(c)
	return b / (1 - float64(rho*(1-b)))
}

// Above erlangCExactMax servers erlangC maps the design onto erlangCExactMax
// servers with the same Halfin–Whitt β, so its cost does not grow with c. It
// stays within 0.01 of the exact value and never below it by more than noise.
func TestErlangCLargeServersClose(t *testing.T) {
	for _, c := range []int{erlangCExactMax + 1, 2000, 5000, 20_000, 100_000, 448_000} {
		for _, rho := range []float64{0.1, 0.3, 0.5, 0.8, 0.9, 0.95, 0.98, 0.99, 0.995, 0.999, 0.9999} {
			a := float64(rho * float64(c))
			want, got := erlangCRecurrence(c, a), erlangC(c, a)
			if math.IsNaN(got) || got < 0 || got > 1 || math.Abs(got-want) > 0.01 {
				t.Fatalf("erlangC(%d, %v) = %v, exact %v", c, a, got, want)
			}
		}
	}
	// At or below the threshold the exact recurrence answers, bit for bit.
	for _, c := range []int{1, 7, 64, erlangCExactMax} {
		a := float64(0.97 * float64(c))
		if got, want := erlangC(c, a), erlangCRecurrence(c, a); got != want {
			t.Fatalf("erlangC(%d, %v) = %v, want %v", c, a, got, want)
		}
	}
}

// p50p99 picks the same two order statistics as sorting and percentile does,
// for any length and with many ties (timeouts, zero-latency paths).
func TestP50P99MatchesSort(t *testing.T) {
	r := newRNG(7)
	for n := range 300 {
		for _, levels := range []int{0, 1, 3, 50} {
			xs := make([]float64, n)
			for i := range xs {
				if levels == 0 {
					xs[i] = r.exp(1)
				} else {
					xs[i] = float64(int(float64(r.f64() * float64(levels))))
				}
			}
			if n%7 == 3 && n > 0 {
				xs[n/2] = math.Inf(1)
			}
			sorted := slices.Clone(xs)
			slices.Sort(sorted)
			p50, p99 := p50p99(xs)
			if p50 != percentile(sorted, 50) || p99 != percentile(sorted, 99) {
				t.Fatalf("n=%d levels=%d: p50p99 = %v, %v; sort gives %v, %v", n, levels, p50, p99, percentile(sorted, 50), percentile(sorted, 99))
			}
		}
	}
	// 20,000 samples, the steady-state size, and already sorted / reversed input.
	xs := make([]float64, defaultSamples)
	for i := range xs {
		xs[i] = float64(i % 977)
	}
	for _, in := range [][]float64{xs, slices.Sorted(slices.Values(xs)), func() []float64 { s := slices.Sorted(slices.Values(xs)); slices.Reverse(s); return s }()} {
		sorted := slices.Sorted(slices.Values(in))
		p50, p99 := p50p99(slices.Clone(in))
		if p50 != percentile(sorted, 50) || p99 != percentile(sorted, 99) {
			t.Fatalf("p50p99 = %v, %v; sort gives %v, %v", p50, p99, percentile(sorted, 50), percentile(sorted, 99))
		}
	}
}
