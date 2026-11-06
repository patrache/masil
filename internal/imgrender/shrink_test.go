package imgrender_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/patrache/masil/internal/imgrender"
)

// The shrink to the display size must not need more than the output plus
// small row buffers: x/image/draw's kernel scaler keeps a dst-width x
// src-height float buffer (32 B per entry), ~270 MB for a 4096x4096 source,
// which defeats the pre-decode byte limit.
func TestDisplayImageShrinkMemoryIsBounded(t *testing.T) {
	const n = 4096
	src := image.NewGray(image.Rect(0, 0, n, n))
	for i := range src.Pix {
		src.Pix[i] = 0x80
	}
	side := imgrender.MaxDisplaySide
	var out image.Image
	got := allocated(func() { out = imgrender.DisplayImage(src) })
	if s := out.Bounds().Size(); s != image.Pt(side, side) {
		t.Fatalf("size = %v, want %dx%d", s, side, side)
	}
	limit := uint64(4*side*side) + 4<<20
	if got > limit {
		t.Fatalf("shrink allocated %d bytes, want <= %d (output + 4 MiB)", got, limit)
	}
	for _, p := range []image.Point{{0, 0}, {side / 2, side / 3}, {side - 1, side - 1}} {
		if c := color.GrayModel.Convert(out.At(p.X, p.Y)).(color.Gray); c.Y != 0x80 {
			t.Fatalf("pixel %v = %v, want 0x80", p, c)
		}
	}
}

func TestDisplayImageShrinkKeepsAlphaAndAverages(t *testing.T) {
	const w, h = 4100, 10
	src := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{255, 0, 0, 255}
			if x >= w/2 {
				c = color.NRGBA{0, 0, 255, 0} // transparent right half
			}
			if x < w/2 && x%2 == 1 {
				c = color.NRGBA{0, 0, 0, 255} // left half: red/black stripes
			}
			src.SetNRGBA(x, y, c)
		}
	}
	out := imgrender.DisplayImage(src)
	b := out.Bounds()
	if b.Dx() != imgrender.MaxDisplaySide {
		t.Fatalf("width = %d", b.Dx())
	}
	r, g, bl, a := out.At(b.Dx()/4, b.Dy()/2).RGBA()
	if a>>8 != 255 || r>>8 < 0x7c || r>>8 > 0x84 || g != 0 || bl != 0 {
		t.Fatalf("left quarter = %d %d %d %d, want ~50%% red, opaque", r>>8, g>>8, bl>>8, a>>8)
	}
	if _, _, _, a := out.At(b.Dx()*3/4, b.Dy()/2).RGBA(); a != 0 {
		t.Fatalf("transparent half has alpha %d", a>>8)
	}
}

// downscaleToFit shrinks the user's own file the same way; a 3000x3000 noise
// image takes a few attempts, each of which may hold one RGBA frame and its
// JPEG, never a dst x src float buffer.
func TestDownscaleToFitMemoryIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("encodes a 3000x3000 noise image several times")
	}
	const n = 3000
	src := image.NewRGBA(image.Rect(0, 0, n, n))
	x := uint32(1)
	for i := range src.Pix {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		src.Pix[i] = uint8(x)
	}
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	var err error
	got := allocated(func() { _, err = imgrender.DownscaleToFit(src) })
	if err != nil {
		t.Fatal(err)
	}
	// Generous: four full-size RGBA frames and their JPEG buffers.
	limit := uint64(4 * (4*n*n + 16<<20))
	if got > limit {
		t.Fatalf("downscaleToFit allocated %d bytes, want <= %d", got, limit)
	}
}
