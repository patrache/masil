// Portions of this file are adapted from golang.org/x/image/vp8l v0.47.0
// (decode.go, huffman.go, transform.go): Copyright 2014 The Go Authors. All
// rights reserved. Use of that source code is governed by a BSD-style license
// that can be found in the LICENSE file of the golang.org/x/image module.

package imgrender

import (
	"bytes"
	"errors"
	"io"
	"unsafe"
)

// vp8lHuffmanBytes walks a VP8L bitstream the way x/image/vp8l.Decode does,
// up to where the top-level pixel data begins, and returns the bytes the
// decoder allocates for the top-level image's Huffman groups (pending item
// 13): the group array, every tree it builds (nodes) and the code-length
// buffers each tree is read through. The number of groups (up to 2600) and
// the size of each tree are declared by the bitstream, not by the image
// size, so a small image can name more than 100 MB of them.
//
// w x h is the size the caller costed the image at. The ALPH plane is a VP8L
// stream without the 5-byte header (headerless) of exactly that size (the
// canvas). A VP8L chunk whose header says otherwise counts nothing:
// x/image/webp refuses a VP8L that disagrees with its VP8X canvas before
// decoding it, and without VP8X the size was read from that header. The
// caller walks only when the rest of its estimate is within its limit, since
// the walk decodes the transform and meta images as the decoder would.
//
// If the walk fails, the decoder fails at the same point; the bytes counted
// up to there are what it allocated first.
//
// The transform and meta-Huffman images (and their single groups) are
// decoded here as the decoder decodes them; their pixels are part of
// vp8lCost and their groups of decodeOverhead.
func vp8lHuffmanBytes(b []byte, headerless bool, w, h int32) (n int64) {
	defer func() {
		// The walk mirrors the decoder, so this is not expected; if it does
		// happen, assume the worst rather than fail the caller.
		if recover() != nil {
			n = vp8lWorstHuffmanBytes(int64(w), int64(h))
		}
	}()
	d := &vlDecoder{r: bytes.NewReader(b)}
	if !headerless {
		hw, hh, err := d.header()
		if err != nil || hw != w || hh != h {
			return 0
		}
	}
	var seen [4]bool
	for {
		more, err := d.read(1)
		if err != nil || more == 0 {
			if err != nil {
				return d.counted
			}
			break
		}
		var typ uint32
		if typ, w, err = d.decodeTransform(w, h); err != nil {
			return d.counted
		}
		if seen[typ] {
			return d.counted // "vp8l: repeated transform"
		}
		seen[typ] = true
	}
	// decodePix(w, h, 0, true) up to its pixel loop.
	ccBits, err := d.colorCacheBits()
	if err != nil {
		return d.counted
	}
	if ccBits > 0 {
		d.counted += 4 << ccBits // ccEntries
	}
	_, _, _, _ = d.decodeHuffmanGroups(w, h, true, ccBits)
	return d.counted
}

// vp8lWorstHuffmanBytes bounds vp8lHuffmanBytes for a w x h image whose
// bitstream cannot be read: at most one group per 4x4 tile and never more
// than 2600, each with the largest trees (green 280 + 2048 colour-cache
// symbols, red/blue/alpha 256, distance 40).
func vp8lWorstHuffmanBytes(w, h int64) int64 {
	groups := min(vlMaxHuffImageSize, ceilDiv(w, 4)*ceilDiv(h, 4))
	var perGroup int64 = int64(unsafe.Sizeof(vlGroup{}))
	for j, a := range vlAlphabetSizes {
		if j == vlHuffGreen {
			a += 1 << 11
		}
		perGroup += vlTreeBytes(int64(a), int64(a), true)
	}
	return groups*perGroup + 2*vlMaxHuffImageSize + 4<<11
}

// vlTreeBytes is what decodeHuffmanTree allocates for a tree over alphabet
// symbols of which nSymbols have a code: the code lengths and canonical codes
// (normal trees), the code-length code's own small tree, and, when built, the
// nodes.
func vlTreeBytes(alphabet, nSymbols int64, built bool) int64 {
	var n int64 = 4*alphabet + vlCodeLengthTreeBytes // codeLengths, code-length tree
	if built {
		n += 4*alphabet + int64(unsafe.Sizeof(vlNode{}))*(2*nSymbols-1) // codes, nodes
	}
	return n
}

// vlCodeLengthTreeBytes over-counts decodeCodeLengths' tree over the 19
// code-length symbols (nodes, codes, the hTree if it escapes).
const vlCodeLengthTreeBytes = int64(unsafe.Sizeof(vlTree{})) + 37*8 + 19*4

const vlMaxHuffImageSize = 2600

var errVLInvalid = errors.New("vp8l walk: invalid bitstream")

// --- bit reader and header (vp8l decode.go) ---

type vlDecoder struct {
	r       io.ByteReader
	bits    uint32
	nBits   uint32
	counted int64
}

func (d *vlDecoder) read(n uint32) (uint32, error) {
	for d.nBits < n {
		c, err := d.r.ReadByte()
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		d.bits |= uint32(c) << d.nBits
		d.nBits += 8
	}
	u := d.bits & (1<<n - 1)
	d.bits >>= n
	d.nBits -= n
	return u, nil
}

func (d *vlDecoder) header() (w, h int32, err error) {
	magic, err := d.read(8)
	if err != nil || magic != 0x2f {
		return 0, 0, errVLInvalid
	}
	width, err := d.read(14)
	if err != nil {
		return 0, 0, err
	}
	height, err := d.read(14)
	if err != nil {
		return 0, 0, err
	}
	if _, err = d.read(1); err != nil {
		return 0, 0, err
	}
	version, err := d.read(3)
	if err != nil || version != 0 {
		return 0, 0, errVLInvalid
	}
	return int32(width) + 1, int32(height) + 1, nil
}

func vlTiles(size int32, bits uint32) int32 { return (size + 1<<bits - 1) >> bits }

// decodeTransform consumes the next transform and returns its type and the
// image width after it.
func (d *vlDecoder) decodeTransform(w, h int32) (uint32, int32, error) {
	typ, err := d.read(2)
	if err != nil {
		return 0, 0, err
	}
	switch typ {
	case 0, 1: // predictor, cross-colour
		bits, err := d.read(3)
		if err != nil {
			return 0, 0, err
		}
		bits += 2
		if _, err := d.decodePix(vlTiles(w, bits), vlTiles(h, bits), 0, false); err != nil {
			return 0, 0, err
		}
	case 2: // subtract green
	case 3: // colour indexing
		nColors, err := d.read(8)
		if err != nil {
			return 0, 0, err
		}
		nColors++
		var bits uint32
		switch {
		case nColors <= 2:
			bits = 3
		case nColors <= 4:
			bits = 2
		case nColors <= 16:
			bits = 1
		}
		w = vlTiles(w, bits)
		if _, err := d.decodePix(int32(nColors), 1, 4*256, false); err != nil {
			return 0, 0, err
		}
	}
	return typ, w, nil
}

var (
	vlCodeLengthCodeOrder = [19]uint8{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	vlRepeatBits          = [3]uint8{2, 3, 7}
	vlRepeatOffsets       = [3]uint8{3, 3, 11}
)

func (d *vlDecoder) decodeCodeLengths(dst []uint32, codeLengthCodeLengths []uint32) error {
	h := vlTree{}
	if err := h.build(codeLengthCodeLengths); err != nil {
		return err
	}
	maxSymbol := len(dst)
	useLength, err := d.read(1)
	if err != nil {
		return err
	}
	if useLength != 0 {
		n, err := d.read(3)
		if err != nil {
			return err
		}
		n = 2 + 2*n
		ms, err := d.read(n)
		if err != nil {
			return err
		}
		maxSymbol = int(ms) + 2
		if maxSymbol > len(dst) {
			return errVLInvalid
		}
	}
	prevCodeLength := uint32(8)
	for symbol := 0; symbol < len(dst); {
		if maxSymbol == 0 {
			break
		}
		maxSymbol--
		codeLength, err := h.next(d)
		if err != nil {
			return err
		}
		if codeLength < 16 {
			dst[symbol] = codeLength
			symbol++
			if codeLength != 0 {
				prevCodeLength = codeLength
			}
			continue
		}
		repeat, err := d.read(uint32(vlRepeatBits[codeLength-16]))
		if err != nil {
			return err
		}
		repeat += uint32(vlRepeatOffsets[codeLength-16])
		if symbol+int(repeat) > len(dst) {
			return errVLInvalid
		}
		cl := uint32(0)
		if codeLength == 16 {
			cl = prevCodeLength
		}
		for ; repeat > 0; repeat-- {
			dst[symbol] = cl
			symbol++
		}
	}
	return nil
}

// decodeHuffmanTree reads one tree. With h it builds it (the transform and
// meta images need their trees to decode); with count it adds what the
// decoder allocates for it instead (built says whether the decoder builds
// it: false for a top-level group the meta image never references).
func (d *vlDecoder) decodeHuffmanTree(h *vlTree, alphabetSize uint32, count, built bool) error {
	useSimple, err := d.read(1)
	if err != nil {
		return err
	}
	if useSimple != 0 {
		nSymbols, err := d.read(1)
		if err != nil {
			return err
		}
		nSymbols++
		firstSymbolLengthCode, err := d.read(1)
		if err != nil {
			return err
		}
		firstSymbolLengthCode = 7*firstSymbolLengthCode + 1
		var symbols [2]uint32
		if symbols[0], err = d.read(firstSymbolLengthCode); err != nil {
			return err
		}
		if nSymbols == 2 {
			if symbols[1], err = d.read(8); err != nil {
				return err
			}
		}
		if count && built {
			d.counted += int64(unsafe.Sizeof(vlNode{})) * int64(2*nSymbols-1)
		}
		if h == nil {
			return nil
		}
		return h.buildSimple(nSymbols, symbols, alphabetSize)
	}
	nCodes, err := d.read(4)
	if err != nil {
		return err
	}
	nCodes += 4
	if int(nCodes) > len(vlCodeLengthCodeOrder) {
		return errVLInvalid
	}
	codeLengthCodeLengths := [len(vlCodeLengthCodeOrder)]uint32{}
	for i := range nCodes {
		if codeLengthCodeLengths[vlCodeLengthCodeOrder[i]], err = d.read(3); err != nil {
			return err
		}
	}
	codeLengths := make([]uint32, alphabetSize)
	if count {
		d.counted += vlTreeBytes(int64(alphabetSize), 0, false)
	}
	if err = d.decodeCodeLengths(codeLengths, codeLengthCodeLengths[:]); err != nil {
		return err
	}
	if count && built {
		var nSymbols int64
		for _, cl := range codeLengths {
			if cl != 0 {
				nSymbols++
			}
		}
		if nSymbols == 0 {
			return errVLInvalid
		}
		d.counted += vlTreeBytes(int64(alphabetSize), nSymbols, true) - vlTreeBytes(int64(alphabetSize), 0, false)
	}
	if h == nil {
		return nil
	}
	return h.build(codeLengths)
}

const (
	vlHuffGreen    = 0
	vlHuffDistance = 4
	vlNHuff        = 5
)

type vlGroup [vlNHuff]vlTree

var vlAlphabetSizes = [vlNHuff]uint32{256 + 24, 256, 256, 256, 40}

// decodeHuffmanGroups mirrors vp8l's: a sub-image (topLevel false) gets one
// built group; the top-level image's groups are only counted.
func (d *vlDecoder) decodeHuffmanGroups(w, h int32, topLevel bool, ccBits uint32) ([]vlGroup, []byte, uint32, error) {
	var mapping []int16
	maxHGroupIndex := 0
	numHGroups := int16(1)
	var hPix []byte
	var hBits uint32
	if topLevel {
		useMeta, err := d.read(1)
		if err != nil {
			return nil, nil, 0, err
		}
		if useMeta != 0 {
			if hBits, err = d.read(3); err != nil {
				return nil, nil, 0, err
			}
			hBits += 2
			tileW, tileH := vlTiles(w, hBits), vlTiles(h, hBits)
			numTiles := tileH * tileW
			if hPix, err = d.decodePix(tileW, tileH, 0, false); err != nil {
				return nil, nil, 0, err
			}
			for p := 0; p < len(hPix); p += 4 {
				if i := int(hPix[p])<<8 | int(hPix[p+1]); maxHGroupIndex < i {
					maxHGroupIndex = i
				}
			}
			if maxHGroupIndex >= vlMaxHuffImageSize {
				return nil, nil, 0, errVLInvalid // "vp8l: too many Huffman trees"
			}
			if maxHGroupIndex >= 1000 || int32(maxHGroupIndex) >= numTiles {
				mapping = make([]int16, maxHGroupIndex+1)
				d.counted += 2 * int64(len(mapping))
				for i := range mapping {
					mapping[i] = -1
				}
				numHGroups = 0
				for p := 0; p < len(hPix); p += 4 {
					i := int(hPix[p])<<8 | int(hPix[p+1])
					if mapping[i] == -1 {
						mapping[i] = numHGroups
						numHGroups++
					}
				}
			} else {
				numHGroups = int16(maxHGroupIndex + 1)
			}
		}
	}

	var groups []vlGroup
	if topLevel {
		d.counted += int64(numHGroups) * int64(unsafe.Sizeof(vlGroup{}))
	} else {
		groups = make([]vlGroup, numHGroups)
	}
	for i := range maxHGroupIndex + 1 {
		used := mapping == nil || mapping[i] != -1
		for j, alphabetSize := range vlAlphabetSizes {
			if j == vlHuffGreen && ccBits > 0 {
				alphabetSize += 1 << ccBits
			}
			var dst *vlTree
			if !topLevel {
				dst = &groups[i][j]
			}
			if err := d.decodeHuffmanTree(dst, alphabetSize, topLevel, used); err != nil {
				return nil, nil, 0, err
			}
		}
	}
	return groups, hPix, hBits, nil
}

func (d *vlDecoder) colorCacheBits() (uint32, error) {
	use, err := d.read(1)
	if err != nil || use == 0 {
		return 0, err
	}
	ccBits, err := d.read(4)
	if err != nil {
		return 0, err
	}
	if ccBits < 1 || 11 < ccBits {
		return 0, errVLInvalid
	}
	return ccBits, nil
}

// decodePix decodes a transform or meta-Huffman sub-image (topLevel is
// always false here; the top-level image stops at its groups).
func (d *vlDecoder) decodePix(w, h, minCap int32, topLevel bool) ([]byte, error) {
	ccBits, err := d.colorCacheBits()
	if err != nil {
		return nil, err
	}
	var ccShift uint32
	var ccEntries []uint32
	if ccBits > 0 {
		ccShift = 32 - ccBits
		ccEntries = make([]uint32, 1<<ccBits)
	}
	hGroups, hPix, hBits, err := d.decodeHuffmanGroups(w, h, topLevel, ccBits)
	if err != nil {
		return nil, err
	}
	hMask, tilesPerRow := int32(0), int32(0)
	if hBits != 0 {
		hMask, tilesPerRow = 1<<hBits-1, vlTiles(w, hBits)
	}
	if minCap < 4*w*h {
		minCap = 4 * w * h
	}
	pix := make([]byte, 4*w*h, minCap)
	p, cachedP := 0, 0
	x, y := int32(0), int32(0)
	hg, lookupHG := &hGroups[0], hMask != 0
	for p < len(pix) {
		if lookupHG {
			i := 4 * (tilesPerRow*(y>>hBits) + (x >> hBits))
			hg = &hGroups[uint32(hPix[i])<<8|uint32(hPix[i+1])]
		}
		green, err := hg[vlHuffGreen].next(d)
		if err != nil {
			return nil, err
		}
		switch {
		case green < 256:
			red, err := hg[1].next(d)
			if err != nil {
				return nil, err
			}
			blue, err := hg[2].next(d)
			if err != nil {
				return nil, err
			}
			alpha, err := hg[3].next(d)
			if err != nil {
				return nil, err
			}
			pix[p+0], pix[p+1], pix[p+2], pix[p+3] = uint8(red), uint8(green), uint8(blue), uint8(alpha)
			p += 4
			x++
			if x == w {
				x, y = 0, y+1
			}
			lookupHG = hMask != 0 && x&hMask == 0
		case green < 256+24:
			length, err := d.lz77Param(green - 256)
			if err != nil {
				return nil, err
			}
			distSym, err := hg[vlHuffDistance].next(d)
			if err != nil {
				return nil, err
			}
			distCode, err := d.lz77Param(distSym)
			if err != nil {
				return nil, err
			}
			dist := vlDistanceMap(w, distCode)
			pEnd := p + 4*int(length)
			q := p - 4*int(dist)
			qEnd := pEnd - 4*int(dist)
			if p < 0 || len(pix) < pEnd || q < 0 || len(pix) < qEnd {
				return nil, errVLInvalid
			}
			for ; p < pEnd; p, q = p+1, q+1 {
				pix[p] = pix[q]
			}
			x += int32(length)
			for x >= w {
				x, y = x-w, y+1
			}
			lookupHG = hMask != 0
		default:
			for ; cachedP < p; cachedP += 4 {
				argb := uint32(pix[cachedP+0])<<16 | uint32(pix[cachedP+1])<<8 |
					uint32(pix[cachedP+2])<<0 | uint32(pix[cachedP+3])<<24
				ccEntries[(argb*0x1e35a7bd)>>ccShift] = argb
			}
			green -= 256 + 24
			if int(green) >= len(ccEntries) {
				return nil, errVLInvalid
			}
			argb := ccEntries[green]
			pix[p+0], pix[p+1], pix[p+2], pix[p+3] = uint8(argb>>16), uint8(argb>>8), uint8(argb), uint8(argb>>24)
			p += 4
			x++
			if x == w {
				x, y = 0, y+1
			}
			lookupHG = hMask != 0 && x&hMask == 0
		}
	}
	return pix, nil
}

func (d *vlDecoder) lz77Param(symbol uint32) (uint32, error) {
	if symbol < 4 {
		return symbol + 1, nil
	}
	extraBits := (symbol - 2) >> 1
	offset := (2 + symbol&1) << extraBits
	n, err := d.read(extraBits)
	if err != nil {
		return 0, err
	}
	return offset + n + 1, nil
}

var vlDistanceMapTable = [120]uint8{
	0x18, 0x07, 0x17, 0x19, 0x28, 0x06, 0x27, 0x29, 0x16, 0x1a,
	0x26, 0x2a, 0x38, 0x05, 0x37, 0x39, 0x15, 0x1b, 0x36, 0x3a,
	0x25, 0x2b, 0x48, 0x04, 0x47, 0x49, 0x14, 0x1c, 0x35, 0x3b,
	0x46, 0x4a, 0x24, 0x2c, 0x58, 0x45, 0x4b, 0x34, 0x3c, 0x03,
	0x57, 0x59, 0x13, 0x1d, 0x56, 0x5a, 0x23, 0x2d, 0x44, 0x4c,
	0x55, 0x5b, 0x33, 0x3d, 0x68, 0x02, 0x67, 0x69, 0x12, 0x1e,
	0x66, 0x6a, 0x22, 0x2e, 0x54, 0x5c, 0x43, 0x4d, 0x65, 0x6b,
	0x32, 0x3e, 0x78, 0x01, 0x77, 0x79, 0x53, 0x5d, 0x11, 0x1f,
	0x64, 0x6c, 0x42, 0x4e, 0x76, 0x7a, 0x21, 0x2f, 0x75, 0x7b,
	0x31, 0x3f, 0x63, 0x6d, 0x52, 0x5e, 0x00, 0x74, 0x7c, 0x41,
	0x4f, 0x10, 0x20, 0x62, 0x6e, 0x30, 0x73, 0x7d, 0x51, 0x5f,
	0x40, 0x72, 0x7e, 0x61, 0x6f, 0x50, 0x71, 0x7f, 0x60, 0x70,
}

func vlDistanceMap(w int32, code uint32) int32 {
	if int32(code) > int32(len(vlDistanceMapTable)) {
		return int32(code) - int32(len(vlDistanceMapTable))
	}
	distCode := int32(vlDistanceMapTable[code-1])
	yOffset := distCode >> 4
	xOffset := 8 - distCode&0xf
	if d := yOffset*w + xOffset; d >= 1 {
		return d
	}
	return 1
}

// --- Huffman trees (vp8l huffman.go) ---

type vlNode struct {
	symbol   uint32
	children int32 // > 0: index of the first child; 0: uninitialised; -1: leaf
}

const vlLeaf = -1

const vlLutSize, vlLutMask = 7, 1<<7 - 1

type vlTree struct {
	nodes []vlNode
	lut   [1 << vlLutSize]uint32
}

func vlReverse8(b uint8) uint8 {
	b = b>>4 | b<<4
	b = (b&0xcc)>>2 | (b&0x33)<<2
	return (b&0xaa)>>1 | (b&0x55)<<1
}

func (h *vlTree) insert(symbol, code, codeLength uint32) error {
	if symbol > 0xffff || codeLength > 0xfe {
		return errVLInvalid
	}
	baseCode := uint32(0)
	if codeLength > vlLutSize {
		baseCode = uint32(vlReverse8(uint8((code>>(codeLength-vlLutSize))&0xff))) >> (8 - vlLutSize)
	} else {
		baseCode = uint32(vlReverse8(uint8(code&0xff))) >> (8 - codeLength)
		for i := 0; i < 1<<(vlLutSize-codeLength); i++ {
			h.lut[baseCode|uint32(i)<<codeLength] = symbol<<8 | (codeLength + 1)
		}
	}
	n := uint32(0)
	for jump := vlLutSize; codeLength > 0; {
		codeLength--
		if int(n) > len(h.nodes) {
			return errVLInvalid
		}
		switch h.nodes[n].children {
		case vlLeaf:
			return errVLInvalid
		case 0:
			if len(h.nodes) == cap(h.nodes) {
				return errVLInvalid
			}
			h.nodes[n].children = int32(len(h.nodes))
			h.nodes = h.nodes[:len(h.nodes)+2]
		}
		n = uint32(h.nodes[n].children) + 1&(code>>codeLength)
		jump--
		if jump == 0 && h.lut[baseCode] == 0 {
			h.lut[baseCode] = n << 8
		}
	}
	switch h.nodes[n].children {
	case vlLeaf:
	case 0:
		h.nodes[n].children = vlLeaf
	default:
		return errVLInvalid
	}
	h.nodes[n].symbol = symbol
	return nil
}

func vlCodes(codeLengths []uint32) ([]uint32, error) {
	maxCodeLength := uint32(0)
	for _, cl := range codeLengths {
		maxCodeLength = max(maxCodeLength, cl)
	}
	const maxAllowed = 15
	if len(codeLengths) == 0 || maxCodeLength > maxAllowed {
		return nil, errVLInvalid
	}
	histogram := [maxAllowed + 1]uint32{}
	for _, cl := range codeLengths {
		histogram[cl]++
	}
	currCode, nextCodes := uint32(0), [maxAllowed + 1]uint32{}
	for cl := 1; cl < len(nextCodes); cl++ {
		currCode = (currCode + histogram[cl-1]) << 1
		nextCodes[cl] = currCode
	}
	codes := make([]uint32, len(codeLengths))
	for symbol, cl := range codeLengths {
		if cl > 0 {
			codes[symbol] = nextCodes[cl]
			nextCodes[cl]++
		}
	}
	return codes, nil
}

func (h *vlTree) build(codeLengths []uint32) error {
	var nSymbols, lastSymbol uint32
	for symbol, cl := range codeLengths {
		if cl != 0 {
			nSymbols++
			lastSymbol = uint32(symbol)
		}
	}
	if nSymbols == 0 {
		return errVLInvalid
	}
	h.nodes = make([]vlNode, 1, 2*nSymbols-1)
	if nSymbols == 1 {
		if len(codeLengths) <= int(lastSymbol) {
			return errVLInvalid
		}
		return h.insert(lastSymbol, 0, 0)
	}
	codes, err := vlCodes(codeLengths)
	if err != nil {
		return err
	}
	for symbol, cl := range codeLengths {
		if cl > 0 {
			if err := h.insert(uint32(symbol), codes[symbol], cl); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *vlTree) buildSimple(nSymbols uint32, symbols [2]uint32, alphabetSize uint32) error {
	h.nodes = make([]vlNode, 1, 2*nSymbols-1)
	for i := range nSymbols {
		if symbols[i] >= alphabetSize {
			return errVLInvalid
		}
		if err := h.insert(symbols[i], i, nSymbols-1); err != nil {
			return err
		}
	}
	return nil
}

func (h *vlTree) next(d *vlDecoder) (uint32, error) {
	var n uint32
	if d.nBits < vlLutSize {
		c, err := d.r.ReadByte()
		if err != nil {
			if err == io.EOF {
				goto slowPath
			}
			return 0, err
		}
		d.bits |= uint32(c) << d.nBits
		d.nBits += 8
	}
	n = h.lut[d.bits&vlLutMask]
	if b := n & 0xff; b != 0 {
		b--
		d.bits >>= b
		d.nBits -= b
		return n >> 8, nil
	}
	n >>= 8
	d.bits >>= vlLutSize
	d.nBits -= vlLutSize

slowPath:
	for h.nodes[n].children != vlLeaf {
		if d.nBits == 0 {
			c, err := d.r.ReadByte()
			if err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return 0, err
			}
			d.bits = uint32(c)
			d.nBits = 8
		}
		n = uint32(h.nodes[n].children) + 1&d.bits
		d.bits >>= 1
		d.nBits--
	}
	return h.nodes[n].symbol, nil
}
