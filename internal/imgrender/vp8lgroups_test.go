package imgrender_test

import (
	"bytes"
	"testing"

	"golang.org/x/image/webp"

	"github.com/patrache/masil/internal/imgrender"
)

// Pending item 13, VP8L Huffman groups: golang.org/x/image/vp8l keeps one
// group of five Huffman trees per distinct entry of the meta Huffman image,
// up to 2600 groups, each tree holding 2n-1 nodes for its n coded symbols
// (the green alphabet is 280 + the colour cache size, up to 2328). A small
// image whose every 4x4 tile names its own group of full trees makes the
// decoder allocate well over a hundred MiB that its pixel count does not
// suggest; the pre-decode estimate has to read those groups.

// bitWriter writes a VP8L bitstream: values LSB first, Huffman codes one bit
// at a time from their most significant bit (the order vp8l's tree walk reads).
type bitWriter struct {
	b   []byte
	acc uint64
	n   uint
}

func (w *bitWriter) put(v uint32, n uint) {
	w.acc |= uint64(v) << w.n
	w.n += n
	for w.n >= 8 {
		w.b = append(w.b, byte(w.acc))
		w.acc >>= 8
		w.n -= 8
	}
}

func (w *bitWriter) code(c uint32, l uint) {
	for i := int(l) - 1; i >= 0; i-- {
		w.put(c>>uint(i)&1, 1)
	}
}

func (w *bitWriter) bytes() []byte {
	out := append([]byte{}, w.b...)
	if w.n > 0 {
		out = append(out, byte(w.acc))
	}
	return out
}

// simpleTree is a one-symbol tree (symbol 0) that costs no bits per symbol.
func (w *bitWriter) simpleTree() {
	w.put(1, 1) // simple
	w.put(0, 1) // one symbol
	w.put(0, 1) // 1-bit symbol
	w.put(0, 1) // symbol 0
}

// run is count consecutive symbols of code length length.
type run struct{ count, length int }

// canonical returns the canonical Huffman codes of lengths (by symbol).
func canonical(lengths map[int]int) map[int]uint32 {
	codes := map[int]uint32{}
	code := uint32(0)
	for l := 1; l <= 15; l++ {
		for s := 0; s < 19; s++ {
			if lengths[s] == l {
				codes[s] = code
				code++
			}
		}
		code <<= 1
	}
	return codes
}

// fullTree is a normal tree giving symbols 0, 1, ... the code lengths of runs
// in order and every later symbol none. vp8l only accepts complete trees, so
// the runs must fill the code space exactly. The code-length code holds the
// runs' lengths and the repeat code 16.
func (w *bitWriter) fullTree(runs ...run) {
	clLen := map[int]int{16: 1}
	switch {
	case len(runs) == 1:
		clLen[runs[0].length] = 1
	case len(runs) == 2 && runs[0].length != runs[1].length:
		clLen[runs[0].length], clLen[runs[1].length] = 2, 2
	default:
		panic("fullTree: one run, or two of different lengths")
	}
	clCode := canonical(clLen)
	w.put(0, 1)  // normal
	w.put(15, 4) // 19 code-length code lengths follow
	for _, s := range [19]int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15} {
		w.put(uint32(clLen[s]), 3)
	}
	// Each run: its length once, then "repeat previous" (16) for 3..6 more.
	type op struct{ sym, extra int }
	var ops []op
	for _, r := range runs {
		ops = append(ops, op{r.length, 0})
		for rem := r.count - 1; rem > 0; {
			if rem < 3 {
				ops = append(ops, op{r.length, 0})
				rem--
				continue
			}
			k := min(rem, 6)
			if rem-k == 1 || rem-k == 2 { // leave a run of 3 rather than 1 or 2
				k -= 3
			}
			ops = append(ops, op{16, k - 3})
			rem -= k
		}
	}
	w.put(1, 1) // max_symbol: read only len(ops) codes
	w.put(7, 3) // in 16 bits
	w.put(uint32(len(ops)-2), 16)
	for _, o := range ops {
		w.code(clCode[o.sym], uint(clLen[o.sym]))
		if o.sym == 16 {
			w.put(uint32(o.extra), 2)
		}
	}
}

// manyGroupsVP8L is a w x h VP8L bitstream (header included unless
// headerless) with a 2048-entry colour cache whose meta Huffman image gives
// each 4x4 tile its own group, groups of full trees, up to 2600 groups; the
// pixels are all zero.
func manyGroupsVP8L(w, h int, headerless bool) []byte {
	var bw bitWriter
	if !headerless {
		bw.put(0x2f, 8)
		bw.put(uint32(w-1), 14)
		bw.put(uint32(h-1), 14)
		bw.put(0, 1)
		bw.put(0, 3)
	}
	bw.put(0, 1)  // no transform
	bw.put(1, 1)  // colour cache
	bw.put(11, 4) // of 2048 entries
	bw.put(1, 1)  // meta Huffman image
	bw.put(0, 3)  // 4x4 tiles
	tw, th := (w+3)/4, (h+3)/4
	groups := min(tw*th, 2600)
	// The meta image: no colour cache, one group, pixel = group index.
	bw.put(0, 1)
	bw.fullTree(run{256, 8}) // green: low byte of the index
	bw.fullTree(run{16, 4})  // red: high byte
	bw.simpleTree()          // blue
	bw.simpleTree()          // alpha
	bw.simpleTree()          // distance
	for t := range tw * th {
		g := uint32(t % groups)
		bw.code(g&0xff, 8)
		bw.code(g>>8, 4)
	}
	for range groups {
		bw.fullTree(run{1768, 11}, run{560, 12}) // all 280 + 2048 green symbols
		bw.fullTree(run{256, 8})
		bw.fullTree(run{256, 8})
		bw.fullTree(run{256, 8})
		bw.fullTree(run{24, 5}, run{16, 6}) // all 40 distance symbols
	}
	for range w * h { // literal pixel 0,0,0,0: code 0 in every tree
		bw.put(0, 11)
		bw.put(0, 24)
	}
	return bw.bytes()
}

func TestVP8LHuffmanGroupsAreCosted(t *testing.T) {
	if testing.Short() {
		t.Skip("decodes an image that allocates ~200 MiB")
	}
	const w, h = 256, 164 // 64 x 41 = 2624 tiles
	lossless := buildWebP(riffChunk{"VP8L", manyGroupsVP8L(w, h, false)})
	alphStream := append([]byte{0x01}, manyGroupsVP8L(w, h, true)...) // compression 1: VP8L
	withAlpha := buildWebP(vp8xChunk(vp8xAlpha, w, h), riffChunk{"ALPH", alphStream}, vp8Chunk(w, h))
	if len(lossless) > imgrender.MaxUploadBytes {
		t.Fatalf("fixture is %d bytes, over the upload limit", len(lossless))
	}

	for _, c := range []struct {
		name   string
		data   []byte
		decode bool // the whole file decodes (the ALPH case has a fake VP8 frame)
	}{
		{"VP8L", lossless, true},
		{"ALPH", withAlpha, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			cost, err := imgrender.DecodeCost(c.data, ctWebP)
			if err != nil {
				t.Fatalf("DecodeCost: %v", err)
			}
			var img any
			got := allocated(func() { img, err = webp.Decode(bytes.NewReader(c.data)) })
			if c.decode && err != nil {
				t.Fatalf("decode: %v (the fixture must be a valid image)", err)
			}
			t.Logf("%dx%d: decoder allocated %d, estimate %d (decoded %T, err %v)", w, h, got, cost, img, err)
			if got < 100<<20 {
				t.Fatalf("decoder allocated only %d bytes: the fixture no longer shows the gap", got)
			}
			if uint64(cost) < got {
				t.Fatalf("estimate %d < %d bytes the decoder allocated", cost, got)
			}
			if err := imgrender.CheckHeader(c.data, ctWebP, true); err == nil {
				t.Fatal("display check accepted an image whose Huffman groups exceed the display limit")
			}
		})
	}

	// An ordinary lossless image with the same pixel count is still fine.
	small := readFixture(t, "simple-lossless.webp")
	if err := imgrender.CheckHeader(small, ctWebP, true); err != nil {
		t.Fatalf("ordinary lossless image refused: %v", err)
	}
}

// TestVP8LUnreadableHuffmanCodeIsRefused: the check reads the bitstream the
// way the decoder does; one that the decoder cannot read up to its pixels
// (here an all-zero tree: no symbol has a code) is refused before decoding,
// for the VP8L chunk and a VP8L-compressed ALPH plane alike.
func TestVP8LUnreadableHuffmanCodeIsRefused(t *testing.T) {
	broken := func(headerless bool) []byte {
		var bw bitWriter
		if !headerless {
			bw.put(0x2f, 8)
			bw.put(63, 14)
			bw.put(63, 14)
			bw.put(0, 4)
		}
		bw.put(0, 3)                                   // no transform, no colour cache, no meta Huffman image
		return append(bw.bytes(), make([]byte, 32)...) // a normal tree of zero lengths
	}
	for name, data := range map[string][]byte{
		"VP8L":                                 buildWebP(riffChunk{"VP8L", broken(false)}),
		"ALPH":                                 buildWebP(vp8xChunk(vp8xAlpha, 64, 64), riffChunk{"ALPH", append([]byte{1}, broken(true)...)}, vp8Chunk(64, 64)),
		"VP8L truncated in its Huffman groups": buildWebP(riffChunk{"VP8L", manyGroupsVP8L(64, 64, false)[:200]}),
	} {
		if _, err := webp.Decode(bytes.NewReader(data)); err == nil {
			t.Fatalf("%s: the decoder reads it; the fixture shows nothing", name)
		}
		for _, display := range []bool{true, false} {
			if err := imgrender.CheckHeader(data, ctWebP, display); err == nil {
				t.Errorf("%s display=%v: accepted", name, display)
			}
		}
	}
	// The same start with readable trees passes (vp8lStart: one-symbol trees).
	if err := imgrender.CheckHeader(buildWebP(vp8lChunk(64, 64)), ctWebP, true); err != nil {
		t.Fatalf("readable start refused: %v", err)
	}
}
