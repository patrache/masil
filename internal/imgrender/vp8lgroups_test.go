package imgrender_test

import (
	"bytes"
	"errors"
	"math/bits"
	"testing"

	"golang.org/x/image/webp"

	"github.com/patrache/masil/internal/imgrender"
)

// Pending item 13, VP8L: golang.org/x/image/vp8l keeps every meta-Huffman
// group of the image in memory (up to 2600 groups of 5 trees, ~48 KB each
// with a full colour-cache alphabet), whatever the image size. A 208x200
// lossless image can therefore make the decoder allocate well over 100 MB.
// The bomb check must count the groups the bitstream really declares.

// vlBits writes a VP8L bitstream (least significant bit first).
type vlBits struct {
	b   []byte
	acc uint64
	n   uint
}

func (w *vlBits) put(v uint32, n uint) {
	w.acc |= uint64(v) << w.n
	w.n += n
	for w.n >= 8 {
		w.b = append(w.b, byte(w.acc))
		w.acc >>= 8
		w.n -= 8
	}
}

// code writes a Huffman code of length n, most significant bit first (the
// order vp8l's tree walk consumes it).
func (w *vlBits) code(c uint32, n uint) {
	for i := int(n) - 1; i >= 0; i-- {
		w.put(c>>uint(i)&1, 1)
	}
}

func (w *vlBits) bytes() []byte {
	if w.n > 0 {
		return append(w.b, byte(w.acc))
	}
	return w.b
}

var vlCodeLengthOrder = [19]int{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// simpleTree writes a one-symbol tree: the symbol costs no bits.
func (w *vlBits) simpleTree(sym uint32) {
	w.put(1, 1) // simple code
	w.put(0, 1) // one symbol
	if sym < 2 {
		w.put(0, 1)
		w.put(sym, 1)
	} else {
		w.put(1, 1)
		w.put(sym, 8)
	}
}

// flatTree writes a complete tree giving symbols 0..2^l-1 code length l (the
// canonical code of symbol s is s) and the rest of the alphabet none. The
// code lengths themselves use a one-symbol code, so they cost no bits.
func (w *vlBits) flatTree(l, alphabet int) {
	n := 1 << l
	pos := 0
	for vlCodeLengthOrder[pos] != l {
		pos++
	}
	nCodes := max(4, pos+1)
	w.put(0, 1) // normal code
	w.put(uint32(nCodes-4), 4)
	for i := range nCodes {
		v := uint32(0)
		if vlCodeLengthOrder[i] == l {
			v = 1
		}
		w.put(v, 3)
	}
	if n == alphabet {
		w.put(0, 1)
		return
	}
	w.put(1, 1) // max_symbol: read only n code lengths
	ms := uint32(n - 2)
	k := 0
	for 2+2*k < bits.Len32(ms) {
		k++
	}
	w.put(uint32(k), 3)
	w.put(ms, uint(2+2*k))
}

// vp8lGroupsStream is a w x h VP8L bitstream (with its 5-byte header unless
// headerless, the ALPH form) whose meta-Huffman image (4x4 tiles) names
// groups 0..groups-1 and whose groups carry full trees: a 2048-symbol green
// tree under an 11-bit colour cache, 256-symbol red/blue/alpha trees and a
// 32-symbol distance tree. The stream stops where pixel data would begin:
// the decoder has allocated everything this test is about by then.
func vp8lGroupsStream(w, h, groups int, headerless bool) []byte {
	var b vlBits
	if !headerless {
		b.put(0x2f, 8)
		b.put(uint32(w-1), 14)
		b.put(uint32(h-1), 14)
		b.put(0, 1)
		b.put(0, 3)
	}
	b.put(0, 1)  // no transform
	b.put(1, 1)  // colour cache
	b.put(11, 4) // 2048 entries: green alphabet 280 + 2048
	b.put(1, 1)  // meta-Huffman image
	b.put(0, 3)  // 4x4 tiles
	tw, th := (w+3)/4, (h+3)/4
	// The meta image: no colour cache, one group of 8-bit green and red
	// codes (the group index is red<<8 | green), zero-bit blue/alpha/distance.
	b.put(0, 1)
	b.flatTree(8, 280)
	b.flatTree(8, 256)
	b.simpleTree(0)
	b.simpleTree(0)
	b.simpleTree(0)
	for k := range tw * th {
		idx := uint32(0)
		if k < groups {
			idx = uint32(k)
		}
		b.code(idx&0xff, 8) // green
		b.code(idx>>8, 8)   // red
	}
	for range groups {
		b.flatTree(11, 280+2048)
		b.flatTree(8, 256)
		b.flatTree(8, 256)
		b.flatTree(8, 256)
		b.flatTree(5, 40)
	}
	return b.bytes()
}

func TestDecodeCostCountsVP8LHuffmanGroups(t *testing.T) {
	const w, h = 208, 200 // 52 x 50 = 2600 tiles
	lossless := buildWebP(riffChunk{"VP8L", vp8lGroupsStream(w, h, 2600, false)})
	alpha := buildWebP(vp8xChunk(vp8xAlpha, w, h),
		riffChunk{"ALPH", append([]byte{1}, vp8lGroupsStream(w, h, 2600, true)...)}, vp8Chunk(w, h))
	for name, data := range map[string][]byte{"VP8L": lossless, "VP8X + ALPH + VP8": alpha} {
		t.Run(name, func(t *testing.T) {
			cost, err := imgrender.DecodeCost(data, ctWebP)
			if err != nil {
				t.Fatalf("DecodeCost: %v", err)
			}
			got := allocated(func() { _, err = webp.Decode(bytes.NewReader(data)) })
			if err == nil {
				t.Fatal("the crafted stream decoded; it should stop before its pixels")
			}
			t.Logf("decoder allocated %d before failing (%v), estimate %d", got, err, cost)
			if got < 100<<20 {
				t.Fatalf("decoder allocated only %d: the fixture no longer exercises the Huffman groups", got)
			}
			if uint64(cost) < got {
				t.Fatalf("estimate %d < %d bytes the decoder allocated", cost, got)
			}
			if err := imgrender.CheckHeader(data, ctWebP, true); !errors.Is(err, imgrender.ErrTooLarge) {
				t.Fatalf("display check: err = %v, want ErrTooLarge", err)
			}
		})
	}
}

// The groups are counted from the bitstream, not assumed at the worst case:
// a lossless image of ordinary size with a few groups stays displayable.
func TestDecodeCostVP8LFewGroupsStaysSmall(t *testing.T) {
	const w, h = 2000, 1500
	for _, groups := range []int{1, 40} {
		data := buildWebP(riffChunk{"VP8L", vp8lGroupsStream(w, h, groups, false)})
		cost, err := imgrender.DecodeCost(data, ctWebP)
		if err != nil {
			t.Fatalf("DecodeCost: %v", err)
		}
		var derr error
		got := allocated(func() { _, derr = webp.Decode(bytes.NewReader(data)) })
		t.Logf("%d groups: decoder allocated %d (%v), estimate %d", groups, got, derr, cost)
		if uint64(cost) < got {
			t.Fatalf("%d groups: estimate %d < %d bytes the decoder allocated", groups, cost, got)
		}
		if err := imgrender.CheckHeader(data, ctWebP, true); err != nil {
			t.Fatalf("%d groups: display check: %v", groups, err)
		}
	}
}

// vp8lPredictorStream is a w x h VP8L bitstream that opens with a predictor
// transform over 4x4 tiles: reading it means decoding a (w/4) x (h/4)
// sub-image first. It stops there (the sub-image data is missing).
func vp8lPredictorStream(w, h int) []byte {
	var b vlBits
	b.put(0x2f, 8)
	b.put(uint32(w-1), 14)
	b.put(uint32(h-1), 14)
	b.put(0, 1)
	b.put(0, 3)
	b.put(1, 1) // a transform
	b.put(0, 2) // predictor
	b.put(0, 3) // 4x4 tiles
	b.put(0, 1) // sub-image: no colour cache
	for range 5 {
		b.simpleTree(0) // every pixel costs no bits: the sub-image decodes in full
	}
	return b.bytes()
}

// The walk itself must not allocate what the check is there to refuse: it
// reads the bitstream only at the size the decoder will decode (a VP8X canvas
// that disagrees with the VP8L header is refused by x/image/webp before any
// allocation), and only when the rest of the estimate is within the limit.
func TestDecodeCostWalkStaysWithinTheCheck(t *testing.T) {
	// A 64x64 canvas carrying a VP8L header for 16384x16384 (a 16 MiB transform image).
	mismatch := buildWebP(vp8xChunk(0, 64, 64), riffChunk{"VP8L", vp8lPredictorStream(16384, 16384)})
	// 7000x7000 is within the upload pixel limit, but its pixels alone are over the byte limit.
	huge := buildWebP(riffChunk{"VP8L", vp8lPredictorStream(7000, 7000)})
	for name, c := range map[string]struct {
		data    []byte
		display bool
		ok      bool
	}{
		"VP8X canvas smaller than the VP8L header": {mismatch, true, true},
		"VP8L whose pixels are over the limit":     {huge, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			var err error
			got := allocated(func() { err = imgrender.CheckHeader(c.data, ctWebP, c.display) })
			if c.ok && err != nil {
				t.Fatalf("CheckHeader: %v", err)
			}
			if !c.ok && !errors.Is(err, imgrender.ErrTooLarge) {
				t.Fatalf("CheckHeader: err = %v, want ErrTooLarge", err)
			}
			if got > 1<<20 {
				t.Fatalf("the check allocated %d bytes", got)
			}
		})
	}
}
