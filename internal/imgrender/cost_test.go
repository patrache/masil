package imgrender_test

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"runtime"
	"testing"

	"golang.org/x/image/webp"

	"github.com/patrache/masil/internal/imgrender"
)

// Pending item 13: the pre-decode bomb check has to count what the decoder
// will really allocate, not what DecodeConfig's colour model suggests. The
// two disagree: a PNG with tRNS decodes Gray as NRGBA and Gray16 as NRGBA64,
// an interlaced PNG allocates every Adam7 pass on top of the image, a
// progressive JPEG keeps 4 bytes of coefficients per sample, an RGB or
// CMYK/YCCK JPEG is converted into a second 4-byte copy, a VP8X WebP's model
// says nothing about whether a VP8L bitstream follows, and its ALPH plane is
// itself a VP8L image.

// --- PNG ---

type pngSpec struct {
	w, h      int
	colorType byte // 0 gray, 2 RGB, 3 palette, 4 gray+alpha, 6 RGBA
	depth     byte
	interlace bool
	trns      bool
	before    [][]byte // chunks placed before IHDR
	pixels    bool     // real (all-zero) image data, so the file decodes
	noIDAT    bool     // stop after the header chunks
}

func pngChunk(typ string, data []byte) []byte {
	c := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	c = append(c, typ...)
	c = append(c, data...)
	return binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(c[4:]))
}

var adam7 = [7][4]int{ // xOffset, yOffset, xFactor, yFactor
	{0, 0, 8, 8}, {4, 0, 8, 8}, {0, 4, 4, 8}, {2, 0, 4, 4}, {0, 2, 2, 4}, {1, 0, 2, 2}, {0, 1, 1, 2},
}

func buildPNG(t *testing.T, s pngSpec) []byte {
	t.Helper()
	out := []byte("\x89PNG\r\n\x1a\n")
	for _, c := range s.before {
		out = append(out, c...)
	}
	ihdr := binary.BigEndian.AppendUint32(nil, uint32(s.w))
	ihdr = binary.BigEndian.AppendUint32(ihdr, uint32(s.h))
	il := byte(0)
	if s.interlace {
		il = 1
	}
	ihdr = append(ihdr, s.depth, s.colorType, 0, 0, il)
	out = append(out, pngChunk("IHDR", ihdr)...)
	channels := map[byte]int{0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[s.colorType]
	if s.colorType == 3 {
		out = append(out, pngChunk("PLTE", []byte{10, 20, 30})...)
	}
	if s.trns {
		n := map[byte]int{0: 2, 2: 6, 3: 1}[s.colorType]
		out = append(out, pngChunk("tRNS", make([]byte, n))...)
	}
	if s.noIDAT {
		return out
	}
	var z bytes.Buffer
	if s.pixels {
		zw, _ := zlib.NewWriterLevel(&z, zlib.BestSpeed)
		bpp := channels * int(s.depth)
		writeRows := func(w, h int) {
			row := make([]byte, 1+(w*bpp+7)/8)
			for range h {
				_, _ = zw.Write(row)
			}
		}
		if s.interlace {
			for _, p := range adam7 {
				pw := (s.w - p[0] + p[2] - 1) / p[2]
				ph := (s.h - p[1] + p[3] - 1) / p[3]
				if pw > 0 && ph > 0 {
					writeRows(pw, ph)
				}
			}
		} else {
			writeRows(s.w, s.h)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	}
	out = append(out, pngChunk("IDAT", z.Bytes())...)
	return append(out, pngChunk("IEND", nil)...)
}

// --- JPEG ---

type jcomp struct{ id, hv byte }

var (
	ycc420 = []jcomp{{1, 0x22}, {2, 0x11}, {3, 0x11}}
	ycc444 = []jcomp{{1, 0x11}, {2, 0x11}, {3, 0x11}}
	rgb444 = []jcomp{{'R', 0x11}, {'G', 0x11}, {'B', 0x11}}
	cmyk   = []jcomp{{1, 0x11}, {2, 0x11}, {3, 0x11}, {4, 0x11}}
	gray1  = []jcomp{{1, 0x11}}
)

func jpegSeg(marker byte, payload []byte) []byte {
	s := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(s[2:], uint16(len(payload)+2))
	return append(s, payload...)
}

// adobeAPP14 is an Adobe segment with the given colour transform (0: RGB or
// CMYK, 1: YCbCr, 2: YCCK).
func adobeAPP14(transform byte) []byte {
	return jpegSeg(0xEE, append([]byte("Adobe\x00\x64\x00\x00\x00\x00"), transform))
}

// jpegHeader is a JPEG header that DecodeConfig accepts (it stops at the first
// SOS): SOI, segs, the SOF segment, a scan header, then trailing segs.
func jpegHeader(sof byte, w, h int, comps []jcomp, segs []byte, trailing ...[]byte) []byte {
	b := append([]byte{0xFF, 0xD8}, segs...)
	f := []byte{8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), byte(len(comps))}
	sos := []byte{byte(len(comps))}
	for _, c := range comps {
		f = append(f, c.id, c.hv, 0)
		sos = append(sos, c.id, 0)
	}
	b = append(b, jpegSeg(sof, f)...)
	b = append(b, jpegSeg(0xDA, append(sos, 0, 63, 0))...)
	for _, s := range trailing {
		b = append(b, s...)
	}
	return b
}

// --- WebP ---

func vp8xChunk(flags byte, w, h int) riffChunk {
	d := make([]byte, 10)
	d[0] = flags
	w, h = w-1, h-1
	d[4], d[5], d[6] = byte(w), byte(w>>8), byte(w>>16)
	d[7], d[8], d[9] = byte(h), byte(h>>8), byte(h>>16)
	return riffChunk{"VP8X", d}
}

// vp8lChunk is a VP8L chunk whose header claims w x h (the bitstream itself
// is zeros: enough for DecodeConfig).
func vp8lChunk(w, h int) riffChunk {
	d := append([]byte{0x2f}, binary.LittleEndian.AppendUint32(nil, uint32(w-1)|uint32(h-1)<<14)...)
	return riffChunk{"VP8L", append(d, make([]byte, 16)...)}
}

// vp8Chunk is a VP8 key frame header claiming w x h.
func vp8Chunk(w, h int) riffChunk {
	d := []byte{0x10, 0x02, 0x00, 0x9d, 0x01, 0x2a, byte(w), byte(w >> 8), byte(h), byte(h >> 8)}
	return riffChunk{"VP8 ", append(d, make([]byte, 16)...)}
}

// TestDecodeCostCountsDecoderBuffers pins the header check at the limits
// with headers whose colour model understates the decode.
func TestDecodeCostCountsDecoderBuffers(t *testing.T) {
	unknownFirst := pngChunk("zzZz", []byte("ignored before IHDR"))
	type tc struct {
		name            string
		ct              string
		data            []byte
		display, upload bool // true = accepted
	}
	png := func(s pngSpec) []byte { return buildPNG(t, s) }
	webpOf := func(cs ...riffChunk) []byte { return buildWebP(cs...) }
	cases := []tc{
		// PNG: tRNS promotes Gray to NRGBA (4 B) and Gray16 to NRGBA64 (8 B).
		// 8-bit gray at 4 B still fits both byte limits at their pixel
		// limits (96 MB, 200 MB); interlacing it does not.
		{"png gray8 tRNS 6000x4000", ctPNG, png(pngSpec{w: 6000, h: 4000, depth: 8, trns: true}), true, true},
		{"png gray16 6000x4000", ctPNG, png(pngSpec{w: 6000, h: 4000, depth: 16}), true, true},
		{"png gray16 tRNS 6000x4000", ctPNG, png(pngSpec{w: 6000, h: 4000, depth: 16, trns: true}), false, true},
		{"png gray16 7000x7000", ctPNG, png(pngSpec{w: 7000, h: 7000, depth: 16}), false, true},
		{"png gray16 tRNS 7000x7000", ctPNG, png(pngSpec{w: 7000, h: 7000, depth: 16, trns: true}), false, false},
		// Adam7 allocates every pass as well as the image.
		{"png gray8 interlaced 6000x4000", ctPNG, png(pngSpec{w: 6000, h: 4000, depth: 8, interlace: true}), true, true},
		{"png gray8 tRNS interlaced 6000x4000", ctPNG, png(pngSpec{w: 6000, h: 4000, depth: 8, trns: true, interlace: true}), false, true},
		{"png palette interlaced 6000x4000", ctPNG, png(pngSpec{w: 6000, h: 4000, colorType: 3, depth: 8, trns: true, interlace: true}), true, true},
		{"png RGBA 6000x4000", ctPNG, png(pngSpec{w: 6000, h: 4000, colorType: 6, depth: 8}), true, true},
		{"png RGBA interlaced 6000x4000", ctPNG, png(pngSpec{w: 6000, h: 4000, colorType: 6, depth: 8, interlace: true}), false, true},
		// The decoder skips unknown chunks before IHDR; so must the check.
		{"png RGBA interlaced after an unknown chunk", ctPNG, png(pngSpec{w: 6000, h: 4000, colorType: 6, depth: 8, interlace: true, before: [][]byte{unknownFirst}}), false, true},
		{"png RGBA after an unknown chunk", ctPNG, png(pngSpec{w: 6000, h: 4000, colorType: 6, depth: 8, before: [][]byte{unknownFirst}}), true, true},
		// A header we cannot walk to its IDAT counts tRNS and interlacing.
		{"png gray8 without IDAT", ctPNG, png(pngSpec{w: 6000, h: 4000, depth: 8, noIDAT: true}), false, true},

		// JPEG: progressive scans keep 4 bytes of coefficients per sample.
		{"jpeg baseline 4:2:0 6000x4000", ctJPEG, jpegHeader(0xC0, 6000, 4000, ycc420, nil), true, true},
		{"jpeg progressive 4:2:0 6000x4000", ctJPEG, jpegHeader(0xC2, 6000, 4000, ycc420, nil), false, true},
		{"jpeg progressive 4:2:0 4000x3000", ctJPEG, jpegHeader(0xC2, 4000, 3000, ycc420, nil), true, true},
		{"jpeg progressive 4:2:0 8000x6000", ctJPEG, jpegHeader(0xC2, 8000, 6000, ycc420, nil), false, false},
		{"jpeg baseline 4:2:0 8000x6000", ctJPEG, jpegHeader(0xC0, 8000, 6000, ycc420, nil), false, true},
		{"jpeg baseline 4:4:4 6000x4000", ctJPEG, jpegHeader(0xC0, 6000, 4000, ycc444, nil), true, true},
		{"jpeg progressive 4:4:4 4000x3000", ctJPEG, jpegHeader(0xC2, 4000, 3000, ycc444, nil), false, true},
		{"jpeg progressive gray 6000x4000", ctJPEG, jpegHeader(0xC2, 6000, 4000, gray1, nil), false, true},
		{"jpeg baseline gray 6000x4000", ctJPEG, jpegHeader(0xC0, 6000, 4000, gray1, nil), true, true},
		// RGB and CMYK/YCCK are converted into a 4-byte copy of the planes.
		{"jpeg RGB 6000x4000", ctJPEG, jpegHeader(0xC0, 6000, 4000, rgb444, nil), false, true},
		{"jpeg RGB 4000x3000", ctJPEG, jpegHeader(0xC0, 4000, 3000, rgb444, nil), true, true},
		{"jpeg Adobe RGB 6000x4000", ctJPEG, jpegHeader(0xC0, 6000, 4000, ycc444, adobeAPP14(0)), false, true},
		{"jpeg Adobe APP14 after the first scan", ctJPEG, jpegHeader(0xC0, 6000, 4000, ycc444, nil, adobeAPP14(0)), false, true},
		{"jpeg CMYK 6000x4000", ctJPEG, jpegHeader(0xC0, 6000, 4000, cmyk, adobeAPP14(2)), false, true},
		{"jpeg CMYK 4000x3000", ctJPEG, jpegHeader(0xC0, 4000, 3000, cmyk, adobeAPP14(2)), true, true},

		// WebP: VP8X's model does not say which bitstream follows.
		{"webp VP8 6000x4000", ctWebP, webpOf(vp8Chunk(6000, 4000)), true, true},
		{"webp unknown chunk then VP8 6000x4000", ctWebP, webpOf(riffChunk{"XYZW", []byte("skip")}, vp8Chunk(6000, 4000)), true, true},
		{"webp VP8X + VP8 6000x4000", ctWebP, webpOf(vp8xChunk(0, 6000, 4000), vp8Chunk(6000, 4000)), true, true},
		{"webp VP8X + VP8L 6000x4000", ctWebP, webpOf(vp8xChunk(0, 6000, 4000), vp8lChunk(6000, 4000)), false, true},
		{"webp VP8L 4000x3000", ctWebP, webpOf(vp8lChunk(4000, 3000)), true, true},
		{"webp VP8L 6000x4000", ctWebP, webpOf(vp8lChunk(6000, 4000)), false, true},
		// The ALPH plane is a VP8L image of its own.
		{"webp VP8X alpha + ALPH + VP8 6000x4000", ctWebP, webpOf(vp8xChunk(vp8xAlpha, 6000, 4000), riffChunk{"ALPH", []byte{1, 0}}, vp8Chunk(6000, 4000)), false, true},
		{"webp VP8X alpha + ALPH + VP8 3000x2000", ctWebP, webpOf(vp8xChunk(vp8xAlpha, 3000, 2000), riffChunk{"ALPH", []byte{1, 0}}, vp8Chunk(3000, 2000)), true, true},
		// A VP8X with no bitstream we can find counts the worst case.
		{"webp VP8X without a bitstream", ctWebP, webpOf(vp8xChunk(0, 6000, 4000)), false, false},
	}
	for _, c := range cases {
		for _, display := range []bool{true, false} {
			want := c.upload
			if display {
				want = c.display
			}
			err := imgrender.CheckHeader(c.data, c.ct, display)
			if want && err != nil {
				t.Errorf("%s display=%v: err = %v, want accepted", c.name, display, err)
			}
			if !want && !errors.Is(err, imgrender.ErrTooLarge) {
				t.Errorf("%s display=%v: err = %v, want ErrTooLarge", c.name, display, err)
			}
		}
	}
}

const (
	ctPNG  = "image/png"
	ctJPEG = "image/jpeg"
	ctGIF  = "image/gif"
	ctWebP = "image/webp"
)

var stdDecode = map[string]func(io.Reader) (image.Image, error){
	ctPNG: png.Decode, ctJPEG: jpeg.Decode, ctGIF: gif.Decode, ctWebP: webp.Decode,
}

// allocated is the number of heap bytes f allocates.
func allocated(f func()) uint64 {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// gradient is a w x h image with every 8-bit RGB channel varying.
func gradient(w, h int) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x + y), 255})
		}
	}
	return m
}

// interlaceGIF sets the interlace flag on b's first image descriptor. The
// decoder then reorders the rows into a second buffer.
func interlaceGIF(t *testing.T, b []byte) []byte {
	t.Helper()
	b = append([]byte{}, b...)
	for i := gifHeaderLen(b); i < len(b); {
		switch b[i] {
		case 0x2C:
			b[i+9] |= 0x40
			return b
		case 0x21:
			i += 2
			for b[i] != 0 {
				i += 1 + int(b[i])
			}
			i++
		default:
			t.Fatalf("unexpected GIF block %#x", b[i])
		}
	}
	t.Fatal("no image descriptor")
	return nil
}

// lateAdobe inserts, before the second scan of a progressive JFIF JPEG, an
// APP0 that is not JFIF and an Adobe APP14 saying RGB. DecodeConfig stops at
// the SOF of a JFIF file and reports YCbCr; the decoder reads on and converts
// to RGBA at the end.
func lateAdobe(t *testing.T, b []byte) []byte {
	t.Helper()
	first := bytes.Index(b, []byte{0xFF, 0xDA})
	second := bytes.Index(b[first+2:], []byte{0xFF, 0xDA})
	if first < 0 || second < 0 {
		t.Fatal("not a multi-scan JPEG")
	}
	at := first + 2 + second
	return insertAt(b, at, jpegSeg(0xE0, []byte("NOPE\x00")), adobeAPP14(0))
}

// TestDecodeCostCoversDecoderAllocations decodes real images of every shape
// the check distinguishes and requires the estimate to be at least what the
// decoder allocated (TotalAlloc, so transient buffers count too).
func TestDecodeCostCoversDecoderAllocations(t *testing.T) {
	const w, h = 2048, 1536
	png := func(s pngSpec) []byte {
		s.w, s.h, s.pixels = w, h, true
		return buildPNG(t, s)
	}
	grayImg := image.NewGray(image.Rect(0, 0, w, h))
	pal := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White})
	plainGIF := encodeGIF(t, pal)
	lossless := readFixture(t, "cost-lossless.webp")
	palette := readFixture(t, "cost-lossless-palette.webp")
	asVP8X := func(simple []byte) []byte {
		cfg, err := webp.DecodeConfig(bytes.NewReader(simple))
		if err != nil {
			t.Fatal(err)
		}
		return buildWebP(vp8xChunk(0, cfg.Width, cfg.Height), parseRIFF(t, simple)[0])
	}

	cases := []struct {
		name    string
		ct      string
		data    []byte
		decoded any // the decoder's image type, where the model understates it
	}{
		{"png gray8", ctPNG, png(pngSpec{depth: 8}), nil},
		{"png gray8 tRNS", ctPNG, png(pngSpec{depth: 8, trns: true}), &image.NRGBA{}},
		{"png gray16 tRNS", ctPNG, png(pngSpec{depth: 16, trns: true}), &image.NRGBA64{}},
		{"png gray8 tRNS interlaced", ctPNG, png(pngSpec{depth: 8, trns: true, interlace: true}), &image.NRGBA{}},
		{"png gray16 tRNS interlaced", ctPNG, png(pngSpec{depth: 16, trns: true, interlace: true}), &image.NRGBA64{}},
		{"png gray1 tRNS interlaced", ctPNG, png(pngSpec{depth: 1, trns: true, interlace: true}), &image.NRGBA{}},
		{"png RGB16 tRNS interlaced", ctPNG, png(pngSpec{colorType: 2, depth: 16, trns: true, interlace: true}), nil},
		{"png RGBA interlaced", ctPNG, png(pngSpec{colorType: 6, depth: 8, interlace: true}), nil},
		{"png gray+alpha16 interlaced", ctPNG, png(pngSpec{colorType: 4, depth: 16, interlace: true}), nil},
		{"png palette interlaced", ctPNG, png(pngSpec{colorType: 3, depth: 8, trns: true, interlace: true}), nil},

		{"jpeg baseline 4:2:0", ctJPEG, encodeJPEG(t, gradient(w, h)), nil},
		{"jpeg baseline gray", ctJPEG, encodeJPEG(t, grayImg), nil},
		{"jpeg progressive 4:2:0", ctJPEG, readFixture(t, "cost-progressive-420.jpg"), nil},
		{"jpeg progressive 4:4:4", ctJPEG, readFixture(t, "cost-progressive-444.jpg"), nil},
		{"jpeg progressive, Adobe RGB after the first scan", ctJPEG, lateAdobe(t, readFixture(t, "cost-progressive-420.jpg")), &image.RGBA{}},
		{"jpeg Adobe RGB", ctJPEG, readFixture(t, "cost-rgb.jpg"), nil},
		{"jpeg YCCK", ctJPEG, readFixture(t, "cost-ycck.jpg"), nil},
		{"jpeg YCCK progressive", ctJPEG, readFixture(t, "cost-ycck-progressive.jpg"), nil},

		{"gif", ctGIF, plainGIF, nil},
		{"gif interlaced", ctGIF, interlaceGIF(t, plainGIF), nil},

		{"webp lossy", ctWebP, readFixture(t, "cost-lossy.webp"), nil},
		{"webp lossy + ALPH", ctWebP, readFixture(t, "cost-lossy-alpha.webp"), nil},
		{"webp lossless", ctWebP, lossless, nil},
		{"webp lossless palette", ctWebP, palette, nil},
		{"webp VP8X + lossless", ctWebP, asVP8X(lossless), &image.NRGBA{}},
		{"webp VP8X + lossless palette", ctWebP, asVP8X(palette), &image.NRGBA{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cost, err := imgrender.DecodeCost(c.data, c.ct)
			if err != nil {
				t.Fatalf("DecodeCost: %v", err)
			}
			var img image.Image
			got := allocated(func() { img, err = stdDecode[c.ct](bytes.NewReader(c.data)) })
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if c.decoded != nil {
				if gt, wt := fmt.Sprintf("%T", img), fmt.Sprintf("%T", c.decoded); gt != wt {
					t.Fatalf("decoded %s, want %s (the fixture no longer shows the gap)", gt, wt)
				}
			}
			t.Logf("%dx%d: decoder allocated %d, estimate %d", img.Bounds().Dx(), img.Bounds().Dy(), got, cost)
			if uint64(cost) < got {
				t.Fatalf("estimate %d < %d bytes the decoder allocated", cost, got)
			}
		})
	}
}
