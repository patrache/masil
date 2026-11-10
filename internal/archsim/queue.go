package archsim

import "math"

// erlangCExactMax is the most servers erlangC runs the exact recurrence for;
// its cost is O(servers), and validated designs reach 448,000.
const erlangCExactMax = 1024

// erlangC is the probability that an arrival waits in an M/M/c queue with c
// servers and offered load a = λ/μ. It uses the Erlang B recurrence
// B(k) = a·B(k−1) / (k + a·B(k−1)), which stays in [0, 1] for any c, then
// C = B / (1 − ρ(1 − B)) with ρ = a/c. No servers or ρ ≥ 1 → 1; no load → 0.
//
// Above erlangCExactMax servers the queue is mapped onto erlangCExactMax
// servers with the same Halfin–Whitt β = (c − a)/√a, on which C depends in
// the many-server limit: within 0.004 of the exact value (slightly high), and
// the cost no longer grows with c.
func erlangC(c int, a float64) float64 {
	if !(a > 0) {
		return 0
	}
	if c <= 0 || a >= float64(c) {
		return 1
	}
	if c > erlangCExactMax {
		beta := (float64(c) - a) / math.Sqrt(a)
		t := float64(erlangCExactMax)
		s := (math.Sqrt(float64(beta*beta)+float64(4*t)) - beta) / 2 // √a' with (t − a')/√a' = β
		c, a = erlangCExactMax, float64(s*s)
		if !(a > 0) {
			return 0
		}
		if a >= t {
			return 1
		}
	}
	b := 1.0
	for k := 1; k <= c && b != 0; k++ { // once B underflows to 0 it stays 0
		ab := float64(a * b)
		b = ab / (float64(k) + ab)
	}
	rho := a / float64(c)
	return b / (1 - float64(rho*(1-b)))
}

// station is an M/M/c service point: c servers of rate mu (requests/s each),
// arrivals at lambda (requests/s).
type station struct {
	c          int
	mu, lambda float64
}

// capacity is c·μ, 0 when there are no servers or no speed.
func (s station) capacity() float64 {
	if s.c <= 0 || !(s.mu > 0) {
		return 0
	}
	return float64(float64(s.c) * s.mu)
}

// rho is the utilisation λ/(cμ): 0 without load, +Inf with load and no capacity.
func (s station) rho() float64 {
	if !(s.lambda > 0) {
		return 0
	}
	c := s.capacity()
	if c == 0 {
		return math.Inf(1)
	}
	return s.lambda / c
}

// waitProb is the probability of waiting (Erlang C).
func (s station) waitProb() float64 {
	if !(s.lambda > 0) {
		return 0
	}
	if s.rho() >= 1 {
		return 1
	}
	return erlangC(s.c, s.lambda/s.mu)
}

// meanWait is the mean queueing time C/(cμ − λ) in seconds; +Inf when ρ ≥ 1.
func (s station) meanWait() float64 {
	if !(s.lambda > 0) {
		return 0
	}
	if s.rho() >= 1 {
		return math.Inf(1)
	}
	return s.waitProb() / (s.capacity() - s.lambda)
}

// sample draws one request's queueing and service time in seconds: it waits
// with probability C for Exp(cμ − λ), service is Exp(μ). ρ ≥ 1 waits +Inf.
func (s station) sample(r *rng) (wait, service float64) {
	return s.sampleP(r, s.waitProb())
}

// sampleP is sample with the wait probability computed once by the caller. It
// always uses three draws so streams stay aligned whatever the load.
func (s station) sampleP(r *rng, pWait float64) (wait, service float64) {
	u := r.f64()
	uw := r.f64() // the wait draw; its logarithm only when the request waits
	service = r.exp(s.mu)
	switch {
	case !(s.lambda > 0):
		wait = 0
	case s.rho() >= 1:
		wait = math.Inf(1)
	case u < pWait:
		wait = expOf(uw, s.capacity()-s.lambda)
	}
	return wait, service
}
