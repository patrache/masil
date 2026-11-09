package archsim

import "math"

// detLog is the natural logarithm with the same result on every architecture.
// It is the pure-Go algorithm of Go's math/log.go (Copyright The Go Authors,
// BSD-style license; itself from FreeBSD's
// /usr/src/lib/msun/src/e_log.c, Copyright (C) 1993 Sun Microsystems, "Permission
// to use, copy, modify, and distribute this software is freely granted, provided
// that this notice is preserved"), with every product wrapped in float64() so the
// compiler cannot fuse it into a multiply-add (math.Log is assembly on amd64 and
// fused on arm64).
func detLog(x float64) float64 {
	const (
		ln2Hi = 6.93147180369123816490e-01 /* 3fe62e42 fee00000 */
		ln2Lo = 1.90821492927058770002e-10 /* 3dea39ef 35793c76 */
		l1    = 6.666666666666735130e-01   /* 3FE55555 55555593 */
		l2    = 3.999999999940941908e-01   /* 3FD99999 9997FA04 */
		l3    = 2.857142874366239149e-01   /* 3FD24924 94229359 */
		l4    = 2.222219843214978396e-01   /* 3FCC71C5 1D8E78AF */
		l5    = 1.818357216161805012e-01   /* 3FC74664 96CB03DE */
		l6    = 1.531383769920937332e-01   /* 3FC39A09 D078C69F */
		l7    = 1.479819860511658591e-01   /* 3FC2F112 DF3E5244 */
	)
	switch {
	case math.IsNaN(x) || math.IsInf(x, 1):
		return x
	case x < 0:
		return math.NaN()
	case x == 0:
		return math.Inf(-1)
	}
	// reduce
	f1, ki := math.Frexp(x)
	if f1 < math.Sqrt2/2 {
		f1 = float64(f1 * 2)
		ki--
	}
	f := f1 - 1
	k := float64(ki)
	// compute
	s := f / (2 + f)
	s2 := float64(s * s)
	s4 := float64(s2 * s2)
	t1 := float64(s2 * (l1 + float64(s4*(l3+float64(s4*(l5+float64(s4*l7)))))))
	t2 := float64(s4 * (l2 + float64(s4*(l4+float64(s4*l6)))))
	r := t1 + t2
	hfsq := float64(0.5 * f * f)
	return float64(k*ln2Hi) - ((hfsq - (float64(s*(hfsq+r)) + float64(k*ln2Lo))) - f)
}

// detExp is e**x with the same result on every architecture: the pure-Go
// algorithm of Go's math/exp.go (same origin and notices as detLog), every
// product wrapped in float64() so it cannot fuse.
func detExp(x float64) float64 {
	const (
		ln2Hi     = 6.93147180369123816490e-01
		ln2Lo     = 1.90821492927058770002e-10
		log2e     = 1.44269504088896338700e+00
		overflow  = 7.09782712893383973096e+02
		underflow = -7.45133219101941108420e+02
		nearZero  = 1.0 / (1 << 28)
		p1        = 1.66666666666666657415e-01  /* 0x3FC55555; 0x55555555 */
		p2        = -2.77777777770155933842e-03 /* 0xBF66C16C; 0x16BEBD93 */
		p3        = 6.61375632143793436117e-05  /* 0x3F11566A; 0xAF25DE2C */
		p4        = -1.65339022054652515390e-06 /* 0xBEBBBD41; 0xC5D26BF1 */
		p5        = 4.13813679705723846039e-08  /* 0x3E663769; 0x72BEA4D0 */
	)
	switch {
	case math.IsNaN(x):
		return x
	case x > overflow:
		return math.Inf(1)
	case x < underflow:
		return 0
	case -nearZero < x && x < nearZero:
		return 1 + x
	}
	var k int
	switch {
	case x < 0:
		k = int(float64(log2e*x) - 0.5)
	case x > 0:
		k = int(float64(log2e*x) + 0.5)
	}
	hi := x - float64(float64(k)*ln2Hi)
	lo := float64(float64(k) * ln2Lo)
	r := hi - lo
	t := float64(r * r)
	c := r - float64(t*(p1+float64(t*(p2+float64(t*(p3+float64(t*(p4+float64(t*p5)))))))))
	y := 1 - ((lo - float64(r*c)/(2-c)) - hi)
	return math.Ldexp(y, k)
}
