package ot

import (
	"math/rand"
)

// alphabet used by the property tests; 👩‍💻 is three runes (woman, ZWJ, laptop).
var alphabet = []rune("ab한👩‍💻\n é")

func randString(r *rand.Rand, max int) string {
	n := r.Intn(max + 1)
	out := make([]rune, n)
	for i := range out {
		out[i] = alphabet[r.Intn(len(alphabet))]
	}
	return string(out)
}

func randDoc(r *rand.Rand) []rune { return []rune(randString(r, 30)) }

// randOp makes a random operation over a document of baseLen runes.
func randOp(r *rand.Rand, baseLen int) Op {
	var b builder
	left := baseLen
	for left > 0 {
		n := 1 + r.Intn(min(left, 6))
		switch r.Intn(4) {
		case 0:
			b.insert(randString(r, 4))
		case 1:
			b.delete(n)
			left -= n
		default:
			b.retain(n)
			left -= n
		}
	}
	if r.Intn(2) == 0 {
		b.insert(randString(r, 4))
	}
	return b.op
}

func newRand(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }
