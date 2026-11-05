package fixtures

import (
	"image"
	"image/color"
)

// gradientStops is the mockup `.img` background: linear-gradient(135deg,
// #fb4934, #fabd2f 40%, #83a598 70%, #458588).
var gradientStops = []struct {
	at float64
	c  color.RGBA
}{
	{0, color.RGBA{0xfb, 0x49, 0x34, 0xff}},
	{0.4, color.RGBA{0xfa, 0xbd, 0x2f, 0xff}},
	{0.7, color.RGBA{0x83, 0xa5, 0x98, 0xff}},
	{1, color.RGBA{0x45, 0x85, 0x88, 0xff}},
}

// TestImage is a 64x48 gradient (mockup .img colors).
func TestImage() image.Image {
	const w, h = 64, 48
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			// 135deg: top-left to bottom-right.
			img.SetRGBA(x, y, gradientAt((float64(x)/(w-1)+float64(y)/(h-1))/2))
		}
	}
	return img
}

func gradientAt(t float64) color.RGBA {
	for i := 1; i < len(gradientStops); i++ {
		a, b := gradientStops[i-1], gradientStops[i]
		if t > b.at && i < len(gradientStops)-1 {
			continue
		}
		f := (t - a.at) / (b.at - a.at)
		f = min(max(f, 0), 1)
		mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*f + 0.5) }
		return color.RGBA{mix(a.c.R, b.c.R), mix(a.c.G, b.c.G), mix(a.c.B, b.c.B), 0xff}
	}
	return gradientStops[len(gradientStops)-1].c
}
