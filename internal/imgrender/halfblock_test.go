package imgrender_test

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/imgrender"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

var black = color.RGBA{0, 0, 0, 0xff}

func checkLines(t *testing.T, lines []string, cols, rows int) {
	t.Helper()
	if len(lines) != rows {
		t.Fatalf("len(lines) = %d, want %d", len(lines), rows)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != cols {
			t.Fatalf("line %d StringWidth = %d, want %d: %q", i, w, cols, l)
		}
		if w := ansi.StringWidthWc(l); w != cols {
			t.Fatalf("line %d StringWidthWc = %d, want %d: %q", i, w, cols, l)
		}
		if s := ansi.Strip(l); s != strings.Repeat("▀", cols) {
			t.Fatalf("line %d stripped = %q, want %d half blocks", i, s, cols)
		}
	}
}

func rows(w int, cs ...color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, len(cs)))
	for y, c := range cs {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestHalfBlockShapeAndColors(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	green := color.RGBA{0, 255, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	img := rows(2, red, blue, green, white)

	lines := imgrender.HalfBlock(img, 2, 2, black)
	checkLines(t, lines, 2, 2)
	if !strings.Contains(lines[0], "38;2;255;0;0") || !strings.Contains(lines[0], "48;2;0;0;255") {
		t.Fatalf("line 0 = %q, want fg red (top) and bg blue (bottom)", lines[0])
	}
	if !strings.Contains(lines[1], "38;2;0;255;0") || !strings.Contains(lines[1], "48;2;255;255;255") {
		t.Fatalf("line 1 = %q, want fg green and bg white", lines[1])
	}
}

func TestHalfBlockContainLetterbox(t *testing.T) {
	bg := color.RGBA{17, 34, 51, 255}
	green := color.RGBA{0, 255, 0, 255}
	img := rows(100, green, green, green, green, green, green, green, green, green, green) // 100x10

	lines := imgrender.HalfBlock(img, 10, 5, bg)
	checkLines(t, lines, 10, 5)
	bgFG, bgBG := "38;2;17;34;51", "48;2;17;34;51"
	for _, i := range []int{0, 4} {
		if !strings.Contains(lines[i], bgFG) || !strings.Contains(lines[i], bgBG) {
			t.Fatalf("letterbox line %d = %q, want only bg", i, lines[i])
		}
		if strings.Contains(lines[i], "0;255;0") {
			t.Fatalf("letterbox line %d = %q has image pixels", i, lines[i])
		}
	}
	mid := strings.Join(lines[1:4], "")
	if !strings.Contains(mid, "0;255;0") {
		t.Fatalf("middle lines %q, want the green strip", lines[1:4])
	}
}

func TestFitCoverFillsBox(t *testing.T) {
	bg := color.RGBA{17, 34, 51, 255}
	green := color.RGBA{0, 255, 0, 255}
	img := rows(100, green, green, green, green, green, green, green, green, green, green)

	got := imgrender.Fit(img, 10, 5, imgrender.FitCover, bg)
	if s := got.Bounds().Size(); s != image.Pt(10, 10) {
		t.Fatalf("cover size = %v, want 10x10 pixels", s)
	}
	b := got.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := got.At(x, y).RGBA()
			if r>>8 > 8 || g>>8 < 247 || bl>>8 > 8 {
				t.Fatalf("cover pixel (%d,%d) = %v, want green everywhere", x, y, got.At(x, y))
			}
		}
	}

	contain := imgrender.Fit(img, 10, 5, imgrender.FitContain, bg)
	if s := contain.Bounds().Size(); s != image.Pt(10, 10) {
		t.Fatalf("contain size = %v, want 10x10 pixels", s)
	}
	if c := color.RGBAModel.Convert(contain.At(0, 0)).(color.RGBA); c != bg {
		t.Fatalf("contain corner = %v, want bg %v", c, bg)
	}
}

func TestHalfBlockTransparentUsesBG(t *testing.T) {
	bg := color.RGBA{17, 34, 51, 255}
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4)) // fully transparent
	lines := imgrender.HalfBlock(img, 4, 2, bg)
	checkLines(t, lines, 4, 2)
	for i, l := range lines {
		if !strings.Contains(l, "38;2;17;34;51") || !strings.Contains(l, "48;2;17;34;51") {
			t.Fatalf("line %d = %q, want transparent pixels drawn as bg", i, l)
		}
	}
}

func TestHalfBlockDegenerate(t *testing.T) {
	img := fixtures.TestImage()
	for _, c := range []struct{ cols, rows int }{{0, 3}, {3, 0}, {-1, 2}, {2, -1}} {
		if got := imgrender.HalfBlock(img, c.cols, c.rows, black); got != nil {
			t.Fatalf("HalfBlock(%d,%d) = %q, want nil", c.cols, c.rows, got)
		}
	}

	one := rows(1, color.RGBA{9, 8, 7, 255})
	lines := imgrender.HalfBlock(one, 3, 2, black)
	checkLines(t, lines, 3, 2)
	if !strings.Contains(strings.Join(lines, ""), "9;8;7") {
		t.Fatalf("1x1 image color missing: %q", lines)
	}

	lines = imgrender.HalfBlock(img, 1, 1, black)
	checkLines(t, lines, 1, 1)

	// No image (or an empty one) and no bg still give an exact box.
	checkLines(t, imgrender.HalfBlock(nil, 4, 2, nil), 4, 2)
	checkLines(t, imgrender.HalfBlock(image.NewRGBA(image.Rect(0, 0, 0, 0)), 4, 2, black), 4, 2)
}
