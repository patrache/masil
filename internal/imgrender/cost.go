package imgrender

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
)

// The pre-decode bomb check (checkConfig) compares decodeCost with the byte
// limit. DecodeConfig's colour model alone understates what the decoders
// allocate (pending item 13), so each format is costed from the buffers its
// decoder really makes, read from the same headers the decoder reads; where a
// header cannot be walked the way the decoder walks it, the worst case of
// that format is assumed instead.
//
// What is counted (image/png, image/jpeg, image/gif from the standard
// library, golang.org/x/image/webp):
//
//   - PNG: Gray decodes as NRGBA (4 B) and Gray16 as NRGBA64 (8 B) when a
//     tRNS chunk precedes the image data; an Adam7 image allocates each pass
//     as well as the full image (twice the bytes); two row buffers.
//   - JPEG: the sample planes padded to whole MCUs; a progressive JPEG keeps
//     a 4-byte coefficient per sample on top of them; an RGB or a
//     4-component (CMYK/YCCK) JPEG is converted into another 4-byte copy at
//     the end. The decoder takes the RGB path for an Adobe APP14 segment
//     anywhere in the file, while DecodeConfig only reads up to the first
//     SOF (JFIF) or SOS, so any "Adobe" in the data counts the copy.
//   - GIF: the first frame (inside the logical screen) plus the copy made
//     when it is interlaced.
//   - WebP: the first VP8 or VP8L chunk decides the decoder; a VP8X header
//     does not say which follows. A lossless image may need its packed
//     colour-indexed pixels and the unpacked image at once, plus the
//     transform and Huffman meta images, plus its Huffman groups (up to
//     2600 groups of five trees, whose sizes only the bitstream tells:
//     vp8lHuffmanBytes reads them); an ALPH plane is itself a lossless
//     image plus a 1-byte copy; a lossy frame buffers its partitions.
//
// decodeOverhead covers the fixed buffers (zlib window, bufio readers, the
// fixed-size Huffman and quantisation tables of PNG, JPEG, GIF and VP8, a
// lossless sub-image's single Huffman group) on top of that.
const decodeOverhead = 1 << 20

// decodeCost is the most bytes decoding data (of type ct, header cfg) can
// allocate. It fails for a header the decoder would size a buffer from but
// that the file cannot back (a WebP VP8 chunk longer than the bytes left).
// Working the estimate out allocates too (a lossless WebP's sub-images and
// Huffman groups are read from its bitstream); limit bounds that, and the
// estimate fails with ErrTooLarge as soon as it is sure to exceed limit.
func decodeCost(ct string, data []byte, cfg image.Config, limit int64) (int64, error) {
	w, h := int64(cfg.Width), int64(cfg.Height)
	var n int64
	switch ct {
	case ctPNG:
		n = pngCost(data, cfg.ColorModel, w, h)
	case ctJPEG:
		n = jpegCost(data, cfg.ColorModel, w, h)
	case ctGIF:
		n = 2 * w * h
	case ctWebP:
		var err error
		if n, err = webpCost(data, w, h, limit-decodeOverhead); err != nil {
			return 0, err
		}
	default:
		n = 8 * w * h
	}
	return n + decodeOverhead, nil
}

func ceilDiv(a, b int64) int64 { return (a + b - 1) / b }

// --- PNG ---

func pngCost(data []byte, m color.Model, w, h int64) int64 {
	interlaced, trns := pngFlags(data)
	var bpp int64
	switch m {
	case color.GrayModel, color.AlphaModel:
		bpp = 1
		if trns {
			bpp = 4 // *image.NRGBA
		}
	case color.Gray16Model, color.Alpha16Model:
		bpp = 2
		if trns {
			bpp = 8 // *image.NRGBA64
		}
	case color.RGBAModel, color.NRGBAModel:
		bpp = 4
	default:
		bpp = 8 // RGBA64, NRGBA64 and anything unexpected
		if _, ok := m.(color.Palette); ok {
			bpp = 1
		}
	}
	img := w * h * bpp
	rows := 2 * (1 + 8*w) // current and previous row, at most 8 bytes a pixel
	if interlaced {
		// Every pass is a separate image of the same type (their pixels add
		// up to the full image), each with its own row buffers.
		return 2*img + 7*rows
	}
	return img + rows
}

// pngFlags walks the chunks before the first IDAT the way image/png does
// (unknown chunks are skipped, even before IHDR) and reports whether IHDR
// asks for Adam7 interlacing and whether a tRNS chunk came first. A file it
// cannot walk to its image data counts as both.
func pngFlags(b []byte) (interlaced, trns bool) {
	seenIHDR := false
	for i := int64(8); i+8 <= int64(len(b)); {
		n := int64(binary.BigEndian.Uint32(b[i:]))
		typ := string(b[i+4 : i+8])
		if typ == "IDAT" && seenIHDR {
			return interlaced, trns
		}
		if n > 0x7fffffff || i+12+n > int64(len(b)) {
			break
		}
		switch typ {
		case "IHDR":
			if seenIHDR || n != 13 {
				return true, true
			}
			seenIHDR = true
			interlaced = b[i+8+12] != 0
		case "tRNS":
			trns = true
		}
		i += 12 + n
	}
	return true, true
}

// --- JPEG ---

type jpegFrame struct {
	progressive bool
	w, h        int64
	n           int
	id          [4]byte
	hs, vs      [4]int64
}

func jpegCost(data []byte, m color.Model, w, h int64) int64 {
	nComp := 3
	switch m {
	case color.GrayModel:
		nComp = 1
	case color.CMYKModel:
		nComp = 4
	}
	f, ok := jpegSOF(data)
	if !ok || f.w != w || f.h != h || f.n != nComp {
		// Worst case: planes padded to the largest MCU (32x16), progressive,
		// converted to a 4-byte copy.
		return int64(nComp)*(w+31)*(h+15)*5 + 4*w*h
	}
	mxx, myy := ceilDiv(w, 8*f.hs[0]), ceilDiv(h, 8*f.vs[0])
	var planes int64
	for i := range f.n {
		planes += 64 * f.hs[i] * f.vs[i] * mxx * myy
	}
	cost := planes
	if f.progressive {
		cost += 4 * planes // one [64]int32 block per 8x8 samples
	}
	rgb := f.n == 3 && (f.id == [4]byte{'R', 'G', 'B'} || bytes.Contains(data, []byte("Adobe")))
	if f.n == 4 || rgb {
		cost += 4 * w * h
	}
	return cost
}

// jpegSOF walks the segments up to the first SOF0/1/2 the way image/jpeg
// does (stray bytes before a marker, fill bytes and RSTn are skipped) and
// parses that frame header. It fails on anything the decoder would not read
// past before its SOF.
func jpegSOF(b []byte) (jpegFrame, bool) {
	var f jpegFrame
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xD8 {
		return f, false
	}
	i := 2
	next := func() (byte, bool) {
		if i >= len(b) {
			return 0, false
		}
		i++
		return b[i-1], true
	}
	for {
		t0, ok0 := next()
		t1, ok1 := next()
		if !ok0 || !ok1 {
			return f, false
		}
		for t0 != 0xFF {
			t0 = t1
			if t1, ok1 = next(); !ok1 {
				return f, false
			}
		}
		marker := t1
		if marker == 0 {
			continue
		}
		for marker == 0xFF {
			if marker, ok1 = next(); !ok1 {
				return f, false
			}
		}
		if marker == 0xD9 { // EOI
			return f, false
		}
		if 0xD0 <= marker && marker <= 0xD7 { // RSTn
			continue
		}
		if i+2 > len(b) {
			return f, false
		}
		n := (int(b[i])<<8 | int(b[i+1])) - 2
		i += 2
		if n < 0 || i+n > len(b) {
			return f, false
		}
		seg := b[i : i+n]
		i += n
		switch {
		case marker == 0xC0, marker == 0xC1, marker == 0xC2:
			return parseSOF(seg, marker == 0xC2)
		case marker == 0xC4, marker == 0xDB, marker == 0xDD, marker == 0xFE,
			0xE0 <= marker && marker <= 0xEF:
			// DHT, DQT, DRI, COM, APPn: read and passed over.
		default: // SOS before SOF, or a marker the decoder refuses
			return f, false
		}
	}
}

func parseSOF(seg []byte, progressive bool) (jpegFrame, bool) {
	f := jpegFrame{progressive: progressive}
	switch len(seg) {
	case 6 + 3*1:
		f.n = 1
	case 6 + 3*3:
		f.n = 3
	case 6 + 3*4:
		f.n = 4
	default:
		return f, false
	}
	if seg[0] != 8 || int(seg[5]) != f.n {
		return f, false
	}
	f.h = int64(seg[1])<<8 | int64(seg[2])
	f.w = int64(seg[3])<<8 | int64(seg[4])
	for c := range f.n {
		f.id[c] = seg[6+3*c]
		hv := seg[7+3*c]
		hs, vs := int64(hv>>4), int64(hv&0x0f)
		if hs < 1 || hs > 4 || vs < 1 || vs > 4 {
			return f, false
		}
		if f.n == 1 { // a single component is never subsampled
			hs, vs = 1, 1
		}
		f.hs[c], f.vs[c] = hs, vs
	}
	return f, true
}

// --- WebP ---

// maxVP8FirstPartition is the largest first partition a VP8 frame header can
// name (19 bits); x/image/vp8 allocates it before reading it.
const maxVP8FirstPartition = 1<<19 - 1

// webpCost is decodeCost for WebP; budget is the byte limit less
// decodeOverhead. A lossless bitstream's scan may use what budget has left
// once the pixel buffers counted before it are taken out, so that it fails
// (ErrTooLarge) exactly when the finished estimate would be over budget.
func webpCost(data []byte, w, h, budget int64) (int64, error) {
	mbs := ceilDiv(w, 16) * ceilDiv(h, 16)
	// 4:2:0 frame over whole macroblocks, per-macroblock state, partitions.
	frame := 400*mbs + 64*ceilDiv(w, 16) + maxVP8FirstPartition
	vp8 := frame + int64(len(data))
	vp8l := vp8lCost(w, h)
	vp8lSub := vp8l - vp8lPixelBytes(w, h) // the part the scan allocates
	alpha := vp8l + w*h
	bs := webpBitstream(data)
	cost := max(vp8, vp8l)
	switch bs.id {
	case "VP8 ":
		// x/image/vp8 allocates the partition buffer at the length the
		// chunk header claims and only then reads it, so a claim beyond the
		// file is refused (decoding it would fail anyway).
		if bs.claimed > int64(len(bs.data)) {
			return 0, fmt.Errorf("imgrender: decode %s: VP8 chunk claims %d bytes, %d present", ctWebP, bs.claimed, len(bs.data))
		}
		cost = frame + bs.claimed
	case "VP8L":
		groups, err := vp8lHuffmanBytes(bs.data, w, h, budget-(vp8l-vp8lSub))
		if err != nil {
			return 0, fmt.Errorf("imgrender: decode %s: %w", ctWebP, err)
		}
		cost = vp8l + groups
	default:
		bs.alph = true
	}
	if bs.alph {
		cost += alpha
		if bs.alphData != nil {
			groups, err := alphHuffmanBytes(bs.alphData, w, h, budget-(cost-vp8lSub))
			if err != nil {
				return 0, fmt.Errorf("imgrender: decode %s: ALPH: %w", ctWebP, err)
			}
			cost += groups
		}
	}
	return cost, nil
}

// alphHuffmanBytes is vp8lHuffmanBytes for an ALPH chunk payload: with
// compression 1 x/image/webp decodes it as a VP8L image of the canvas size
// behind a header it makes up. Other compressions hold no Huffman groups.
func alphHuffmanBytes(payload []byte, w, h, budget int64) (int64, error) {
	if len(payload) == 0 || payload[0]&3 != 1 {
		return 0, nil
	}
	if w-1 > 0x3fff || h-1 > 0x3fff {
		return 0, fmt.Errorf("alpha plane %dx%d too large", w, h)
	}
	wm1, hm1 := uint32(w-1), uint32(h-1)
	head := []byte{0x2f, uint8(wm1), uint8(wm1>>8) | uint8(hm1<<6), uint8(hm1 >> 2), uint8(hm1 >> 10)}
	return vp8lHuffmanBytes(append(head, payload[1:]...), w, h, budget)
}

// vp8lCost: the decoded pixels; with colour indexing of 16 or fewer colours,
// the packed pixels (half the width at most) alongside the unpacked copy;
// the predictor, cross-colour and Huffman meta images (one pixel per 4x4
// tile at the finest); the colour table. The top-level Huffman groups are
// read from the bitstream (vp8lHuffmanBytes).
func vp8lCost(w, h int64) int64 {
	tiles := 4 * ceilDiv(w, 4) * ceilDiv(h, 4)
	return vp8lPixelBytes(w, h) + 3*tiles + 4*256
}

// vp8lPixelBytes is the part of vp8lCost the top-level pixels take: the
// decoded image and the packed colour-indexed copy.
func vp8lPixelBytes(w, h int64) int64 { return 4*w*h + 4*ceilDiv(w, 2)*h }

// webpChunks is what webpBitstream finds.
type webpChunks struct {
	id       string // "VP8 ", "VP8L", or "" when the walk ends before one
	data     []byte // that chunk's bytes present in the file
	claimed  int64  // the length its header claims
	alph     bool   // an ALPH chunk came before it
	alphData []byte // the first ALPH chunk's payload present in the file
}

// webpBitstream walks the RIFF chunks the way golang.org/x/image/riff does
// and returns the first VP8 or VP8L chunk (the one x/image/webp decodes) and
// any ALPH chunk before it.
func webpBitstream(b []byte) webpChunks {
	var c webpChunks
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return c
	}
	end := min(int64(len(b)), 8+int64(binary.LittleEndian.Uint32(b[4:])))
	present := func(i, n int64) []byte { return b[i+8 : min(int64(len(b)), i+8+n)] }
	for i := int64(12); i+8 <= end; {
		cid := string(b[i : i+4])
		n := int64(binary.LittleEndian.Uint32(b[i+4:]))
		if cid == "VP8 " || cid == "VP8L" {
			c.id, c.data, c.claimed = cid, present(i, n), n
			return c
		}
		if cid == "ALPH" && !c.alph {
			c.alph, c.alphData = true, present(i, n)
		}
		i += 8 + n + n&1
		if i > end {
			break
		}
	}
	c.id, c.data, c.claimed = "", nil, 0
	return c
}
