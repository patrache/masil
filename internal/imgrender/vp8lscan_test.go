package imgrender_test

import (
	"bytes"
	"errors"
	"testing"

	"golang.org/x/image/webp"

	"github.com/patrache/masil/internal/imgrender"
)

// Pending item 0-d: the pre-decode scan of a lossless WebP decodes its
// sub-images (transform data, the meta Huffman image) for real. It must
// neither size them from a VP8L header the decoder would refuse (a VP8X
// canvas smaller than the VP8L header: x/image/webp rejects the mismatch
// before decoding) nor allocate a sub-image the byte limit has no room left
// for.

// subImage is a sub-image of five one-symbol trees and no colour cache:
// its pixels cost no bits, however many tiles it has.
func (w *bitWriter) subImage() {
	w.put(0, 1) // no colour cache
	for range 5 {
		w.simpleTree()
	}
}

// subImagesVP8L is a w x h VP8L bitstream with a predictor and a
// cross-colour transform and a meta Huffman image, each one pixel per 4x4
// tile, then one group of one-symbol trees. The top-level pixels are left out.
func subImagesVP8L(w, h int) []byte {
	var bw bitWriter
	bw.put(0x2f, 8)
	bw.put(uint32(w-1), 14)
	bw.put(uint32(h-1), 14)
	bw.put(0, 4)                         // alpha hint, version
	for _, typ := range []uint32{0, 1} { // predictor, cross-colour
		bw.put(1, 1)   // a transform
		bw.put(typ, 2) // of this type
		bw.put(0, 3)   // 4x4 tiles
		bw.subImage()
	}
	bw.put(0, 1) // no more transforms
	bw.put(0, 1) // no colour cache
	bw.put(1, 1) // meta Huffman image
	bw.put(0, 3) // 4x4 tiles
	bw.subImage()
	for range 5 {
		bw.simpleTree()
	}
	return append(bw.bytes(), make([]byte, 16)...)
}

// TestVP8LHeaderLargerThanVP8XCanvasIsRefused: a 1x1 VP8X canvas over a
// 16384x16384 VP8L header passes the pixel limit, so the scan must refuse
// the mismatch instead of decoding three 4096x4096 sub-images (192 MiB).
func TestVP8LHeaderLargerThanVP8XCanvasIsRefused(t *testing.T) {
	for _, c := range []struct {
		name       string
		canvas, ll [2]int
	}{
		{"1x1 canvas, 16384x16384 VP8L", [2]int{1, 1}, [2]int{16384, 16384}},
		{"64x64 canvas, 4000x3000 VP8L", [2]int{64, 64}, [2]int{4000, 3000}},
		{"4000x3000 canvas, 64x64 VP8L", [2]int{4000, 3000}, [2]int{64, 64}},
	} {
		t.Run(c.name, func(t *testing.T) {
			data := buildWebP(vp8xChunk(0, c.canvas[0], c.canvas[1]), riffChunk{"VP8L", subImagesVP8L(c.ll[0], c.ll[1])})
			if _, err := webp.Decode(bytes.NewReader(data)); err == nil {
				t.Fatal("the decoder accepts the mismatch; the fixture shows nothing")
			}
			for _, display := range []bool{true, false} {
				var err error
				got := allocated(func() { err = imgrender.CheckHeader(data, ctWebP, display) })
				if err == nil {
					t.Errorf("display=%v: accepted", display)
				}
				if got > 1<<20 {
					t.Errorf("display=%v: the check allocated %d bytes before refusing", display, got)
				}
			}
		})
	}
	// The same bitstream under a canvas of its own size passes.
	ok := buildWebP(vp8xChunk(0, 64, 64), riffChunk{"VP8L", subImagesVP8L(64, 64)})
	if err := imgrender.CheckHeader(ok, ctWebP, true); err != nil {
		t.Fatalf("matching VP8X + VP8L refused: %v", err)
	}
}

// TestVP8LSubImageOverByteBudgetIsRefused: a lossless image whose pixel
// buffers just fit the display byte limit leaves no room for a 4x4-tile
// sub-image (here 1025x1006 tiles, about 4 MB each). The scan must refuse
// it before decoding the sub-image, not after decoding all three.
func TestVP8LSubImageOverByteBudgetIsRefused(t *testing.T) {
	const w, h = 4100, 4024 // 16.5 MP: under the display pixel limit
	data := buildWebP(riffChunk{"VP8L", subImagesVP8L(w, h)})
	var err error
	got := allocated(func() { err = imgrender.CheckHeader(data, ctWebP, true) })
	if !errors.Is(err, imgrender.ErrTooLarge) {
		t.Fatalf("display check: err = %v, want ErrTooLarge", err)
	}
	if got > 2<<20 {
		t.Fatalf("the check allocated %d bytes before refusing (one sub-image is %d)", got, 4*((w+3)/4)*((h+3)/4))
	}
	// The upload limit has room for them: the same file passes there.
	if err := imgrender.CheckHeader(data, ctWebP, false); err != nil {
		t.Fatalf("upload check: %v", err)
	}
}
