package archsim

import (
	"math"

	"github.com/patrache/masil/internal/archdoc"
)

// waveTable is the "하루 파도" curve: 24 permille points of the peak RPS
// spread evenly over the run and joined by straight lines (no trigonometry).
var waveTable = [24]int{
	420, 360, 310, 280, 270, 300, 380, 520, 680, 820, 920, 980,
	1000, 970, 930, 900, 910, 950, 1000, 960, 860, 720, 580, 480,
}

// curveRPS is a client's requests/s at time t seconds of a run of durS
// seconds (C2.9 시간 곡선). Unknown curves are constant.
func curveRPS(tr *archdoc.Traffic, t, durS float64) float64 {
	rps := float64(max(tr.RPS, 0))
	switch tr.Curve {
	case archdoc.CurveRamp:
		if durS > 0 {
			return mul(rps, math.Min(math.Max(t/durS, 0), 1))
		}
	case archdoc.CurveSpike:
		at, dur := float64(tr.SpikeAtS), float64(max(tr.SpikeDurS, 0))
		if t >= at && t < at+dur {
			return mul(rps, float64(max(tr.SpikeMult, 0)))
		}
	case archdoc.CurveWave:
		if !(durS > 0) {
			return mul(rps, float64(waveTable[0])/1000)
		}
		x := mul(math.Min(math.Max(t/durS, 0), 1), float64(len(waveTable)))
		i := min(int(x), len(waveTable)-1)
		frac := x - float64(i)
		a, b := float64(waveTable[i]), float64(waveTable[(i+1)%len(waveTable)])
		return mul(rps, (a+mul(b-a, frac))/1000)
	}
	return rps
}
