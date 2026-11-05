package imgrender

import (
	"image"
	"image/color"
	"image/draw"
	"strings"

	"charm.land/lipgloss/v2"
	xdraw "golang.org/x/image/draw"
)

// FitMode says how an image is fitted into a cell box.
type FitMode int

const (
	// FitContain scales the whole image into the box, letterboxing with bg.
	FitContain FitMode = iota
	// FitCover fills the box, cropping the overflowing (centered) part.
	FitCover
)

const halfBlock = "▀"

// HalfBlock draws img into cols×rows cells. Each cell is "▀" with the top
// pixel as foreground and the bottom pixel as background; the image is
// contained (aspect kept) and the rest of the box is bg. It returns nil when
// cols or rows is not positive; otherwise exactly rows lines of cols cells.
func HalfBlock(img image.Image, cols, rows int, bg color.Color) []string {
	return halfBlockFit(img, cols, rows, FitContain, bg)
}

func halfBlockFit(img image.Image, cols, rows int, mode FitMode, bg color.Color) []string {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	px := fit(img, cols, rows, mode, bg)
	lines := make([]string, rows)
	var sb strings.Builder
	for r := range rows {
		sb.Reset()
		for c := range cols {
			top := px.RGBAAt(c, 2*r)
			bottom := px.RGBAAt(c, 2*r+1)
			sb.WriteString(lipgloss.NewStyle().Foreground(top).Background(bottom).Render(halfBlock))
		}
		lines[r] = sb.String()
	}
	return lines
}

// fit scales img into a pixel box of cols × rows*2 (two pixels per cell) with
// CatmullRom, compositing it over an opaque bg. Uncovered pixels are bg. A
// nil or empty img yields a bg-only box. The result's bounds start at (0,0).
func fit(img image.Image, cols, rows int, mode FitMode, bg color.Color) *image.RGBA {
	cols, rows = max(cols, 0), max(rows, 0)
	W, H := cols, rows*2
	dst := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(opaque(bg)), image.Point{}, draw.Src)
	if img == nil || W == 0 || H == 0 {
		return dst
	}
	sb := img.Bounds()
	w, h := sb.Dx(), sb.Dy()
	if w <= 0 || h <= 0 {
		return dst
	}

	dr := dst.Bounds()
	sr := sb
	switch mode {
	case FitCover:
		// Crop the source to the box's aspect ratio, centered.
		if w*H > W*h { // source is wider than the box
			cw := max(1, (h*W+H/2)/H)
			x0 := sb.Min.X + (w-cw)/2
			sr = image.Rect(x0, sb.Min.Y, x0+cw, sb.Max.Y)
		} else {
			ch := max(1, (w*H+W/2)/W)
			y0 := sb.Min.Y + (h-ch)/2
			sr = image.Rect(sb.Min.X, y0, sb.Max.X, y0+ch)
		}
	default: // FitContain
		dw, dh := W, H
		if w*H > W*h { // source is wider: full width
			dh = max(1, (h*W+w/2)/w)
		} else {
			dw = max(1, (w*H+h/2)/h)
		}
		x0, y0 := (W-dw)/2, (H-dh)/2
		dr = image.Rect(x0, y0, x0+dw, y0+dh)
	}

	if dr.Dx() == sr.Dx() && dr.Dy() == sr.Dy() {
		draw.Draw(dst, dr, img, sr.Min, draw.Over)
	} else {
		xdraw.CatmullRom.Scale(dst, dr, img, sr, xdraw.Over, nil)
	}
	return dst
}

// opaque returns c with full alpha (black for nil), so composited pixels and
// the letterbox are always opaque terminal colors.
func opaque(c color.Color) color.RGBA {
	if c == nil {
		return color.RGBA{0, 0, 0, 0xff}
	}
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return color.RGBA{n.R, n.G, n.B, 0xff}
}
