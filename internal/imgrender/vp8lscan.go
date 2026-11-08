package imgrender

// The Huffman tree, bit reader and sub-image decoding below are ported from
// golang.org/x/image/vp8l v0.47.0 (decode.go, huffman.go, transform.go),
// Copyright 2014 The Go Authors, BSD-style license
// (https://cs.opensource.google/go/x/image/+/refs/tags/v0.47.0:LICENSE).
// They must read a bitstream exactly as that decoder does: any difference
// would make the estimate count other trees than the decoder builds.

import (
	"bytes"
	"errors"
	"io"
	"math/bits"
	"unsafe"
)

var (
	errVP8LCodeLengths = errors.New("vp8l: invalid code lengths")
	errVP8LTree        = errors.New("vp8l: invalid Huffman tree")
)

// vp8lHuffmanBytes reads a VP8L bitstream (5-byte header included) the way
// golang.org/x/image/vp8l.Decode does, up to the first bit of the top-level
// pixels, and returns what that decoder allocates for the top-level Huffman
// groups and colour cache (pending item 13). Every distinct entry of the meta
// Huffman image has its own group of five trees (up to 2600 groups); a tree
// of n coded symbols has 2n-1 nodes, and decoding one also makes code-length
// scratch slices of the alphabet's size (280 + the colour cache for green).
// A 256x164 image can so make the decoder allocate over 200 MB.
//
// Sub-images (transform data, the meta Huffman image) are decoded for real:
// they have at most one pixel per 4x4 tile and vp8lCost counts them. A
// bitstream the decoder would refuse before its pixels is an error.
func vp8lHuffmanBytes(stream []byte) (int64, error) {
	d := &vp8lReader{r: bytes.NewReader(stream)}
	if magic, err := d.read(8); err != nil || magic != 0x2f {
		return 0, errors.New("vp8l: invalid header")
	}
	wm1, err := d.read(14)
	if err != nil {
		return 0, err
	}
	hm1, err := d.read(14)
	if err != nil {
		return 0, err
	}
	if _, err := d.read(1); err != nil { // alpha hint
		return 0, err
	}
	if v, err := d.read(3); err != nil || v != 0 {
		return 0, errors.New("vp8l: invalid version")
	}
	w, h := int32(wm1)+1, int32(hm1)+1

	var seen [4]bool
	for {
		more, err := d.read(1)
		if err != nil {
			return 0, err
		}
		if more == 0 {
			break
		}
		var typ uint32
		if typ, w, err = d.decodeTransform(w, h); err != nil {
			return 0, err
		}
		if seen[typ] {
			return 0, errors.New("vp8l: repeated transform")
		}
		seen[typ] = true
	}

	// decodePix(w, h, 0, true) up to its pixel loop.
	ccBits, n := uint32(0), int64(0)
	useCC, err := d.read(1)
	if err != nil {
		return 0, err
	}
	if useCC != 0 {
		if ccBits, err = d.read(4); err != nil {
			return 0, err
		}
		if ccBits < 1 || 11 < ccBits {
			return 0, errors.New("vp8l: invalid color cache parameters")
		}
		n += allocBytes(4 << ccBits)
	}
	g, err := d.topLevelGroupBytes(w, h, ccBits)
	if err != nil {
		return 0, err
	}
	return n + g, nil
}

// allocBytes bounds what the Go allocator hands out for an n-byte object:
// small objects round up to a size class (at most 12.5% more), large ones
// to whole 8 KiB pages.
func allocBytes(n int64) int64 {
	if n <= 0 {
		return 0
	}
	if n <= 32<<10 {
		return n + n/8 + 16
	}
	return (n + 8<<10 - 1) &^ (8<<10 - 1)
}

// vp8lReader is vp8l's decoder: a little-endian bit reader.
type vp8lReader struct {
	r     io.ByteReader
	bits  uint32
	nBits uint32
}

func (d *vp8lReader) read(n uint32) (uint32, error) {
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

func vp8lTiles(size int32, bits uint32) int32 { return (size + 1<<bits - 1) >> bits }

// decodeTransform reads one transform and returns its type and the width
// of the image after it.
func (d *vp8lReader) decodeTransform(w, h int32) (uint32, int32, error) {
	typ, err := d.read(2)
	if err != nil {
		return 0, 0, err
	}
	switch typ {
	case 0, 1: // predictor, cross-colour
		b, err := d.read(3)
		if err != nil {
			return 0, 0, err
		}
		b += 2
		if _, err := d.decodeSubPix(vp8lTiles(w, b), vp8lTiles(h, b), 0); err != nil {
			return 0, 0, err
		}
	case 2: // subtract green
	case 3: // colour indexing
		nColors, err := d.read(8)
		if err != nil {
			return 0, 0, err
		}
		nColors++
		b := uint32(0)
		switch {
		case nColors <= 2:
			b = 3
		case nColors <= 4:
			b = 2
		case nColors <= 16:
			b = 1
		}
		w = vp8lTiles(w, b)
		if _, err := d.decodeSubPix(int32(nColors), 1, 4*256); err != nil {
			return 0, 0, err
		}
	}
	return typ, w, nil
}

// --- Huffman trees (vp8l huffman.go) ---

type vp8lNode struct {
	symbol   uint32
	children int32 // first child's index; 0 uninitialised, -1 leaf
}

const (
	vp8lLeaf               = -1
	vp8lLutSize, vp8lLutMk = 7, 1<<7 - 1
)

type vp8lTree struct {
	nodes []vp8lNode
	lut   [1 << vp8lLutSize]uint32
}

// vp8lGroupSize is the size of one group of five trees ([nHuff]hTree in
// vp8l, the same layout).
const vp8lGroupSize = int64(unsafe.Sizeof([5]vp8lTree{}))

func reverse8(b uint32) uint32 { return uint32(bits.Reverse8(uint8(b))) }

func (h *vp8lTree) insert(symbol, code, codeLength uint32) error {
	if symbol > 0xffff || codeLength > 0xfe {
		return errVP8LTree
	}
	baseCode := uint32(0)
	if codeLength > vp8lLutSize {
		baseCode = reverse8((code>>(codeLength-vp8lLutSize))&0xff) >> (8 - vp8lLutSize)
	} else {
		baseCode = reverse8(code&0xff) >> (8 - codeLength)
		for i := 0; i < 1<<(vp8lLutSize-codeLength); i++ {
			h.lut[baseCode|uint32(i)<<codeLength] = symbol<<8 | (codeLength + 1)
		}
	}
	n := uint32(0)
	for jump := vp8lLutSize; codeLength > 0; {
		codeLength--
		if int(n) > len(h.nodes) {
			return errVP8LTree
		}
		switch h.nodes[n].children {
		case vp8lLeaf:
			return errVP8LTree
		case 0:
			if len(h.nodes) == cap(h.nodes) {
				return errVP8LTree
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
	case vp8lLeaf:
	case 0:
		h.nodes[n].children = vp8lLeaf
	default:
		return errVP8LTree
	}
	h.nodes[n].symbol = symbol
	return nil
}

func vp8lCodes(codeLengths []uint32) ([]uint32, error) {
	maxCodeLength := uint32(0)
	for _, cl := range codeLengths {
		maxCodeLength = max(maxCodeLength, cl)
	}
	const maxAllowed = 15
	if len(codeLengths) == 0 || maxCodeLength > maxAllowed {
		return nil, errVP8LTree
	}
	var histogram, nextCodes [maxAllowed + 1]uint32
	for _, cl := range codeLengths {
		histogram[cl]++
	}
	currCode := uint32(0)
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

// codedSymbols is the number of non-zero code lengths (vp8l build's nSymbols).
func codedSymbols(codeLengths []uint32) uint32 {
	n := uint32(0)
	for _, cl := range codeLengths {
		if cl != 0 {
			n++
		}
	}
	return n
}

func (h *vp8lTree) build(codeLengths []uint32) error {
	var nSymbols, lastSymbol uint32
	for symbol, cl := range codeLengths {
		if cl != 0 {
			nSymbols++
			lastSymbol = uint32(symbol)
		}
	}
	if nSymbols == 0 {
		return errVP8LTree
	}
	h.nodes = make([]vp8lNode, 1, 2*nSymbols-1)
	if nSymbols == 1 {
		if len(codeLengths) <= int(lastSymbol) {
			return errVP8LTree
		}
		return h.insert(lastSymbol, 0, 0)
	}
	codes, err := vp8lCodes(codeLengths)
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

func (h *vp8lTree) buildSimple(nSymbols uint32, symbols [2]uint32, alphabetSize uint32) error {
	h.nodes = make([]vp8lNode, 1, 2*nSymbols-1)
	for i := uint32(0); i < nSymbols; i++ {
		if symbols[i] >= alphabetSize {
			return errVP8LTree
		}
		if err := h.insert(symbols[i], i, nSymbols-1); err != nil {
			return err
		}
	}
	return nil
}

func (h *vp8lTree) next(d *vp8lReader) (uint32, error) {
	var n uint32
	if d.nBits < vp8lLutSize {
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
	n = h.lut[d.bits&vp8lLutMk]
	if b := n & 0xff; b != 0 {
		b--
		d.bits >>= b
		d.nBits -= b
		return n >> 8, nil
	}
	n >>= 8
	d.bits >>= vp8lLutSize
	d.nBits -= vp8lLutSize

slowPath:
	for h.nodes[n].children != vp8lLeaf {
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

// --- Code lengths and groups (vp8l decode.go) ---

var vp8lCodeLengthOrder = [19]uint8{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

var vp8lAlphabet = [5]uint32{256 + 24, 256, 256, 256, 40}

func (d *vp8lReader) decodeCodeLengths(dst []uint32, codeLengthCodeLengths []uint32) error {
	h := vp8lTree{}
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
			return errVP8LCodeLengths
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
		repeat, err := d.read(uint32([3]uint8{2, 3, 7}[codeLength-16]))
		if err != nil {
			return err
		}
		repeat += uint32([3]uint8{3, 3, 11}[codeLength-16])
		if symbol+int(repeat) > len(dst) {
			return errVP8LCodeLengths
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

// readTree reads one Huffman tree's code: a simple code (its symbols) or
// the code lengths of the whole alphabet. It allocates what vp8l's
// decodeHuffmanTree allocates before building (the code-length slice and
// the code-length code's tree) and reports those bytes as scratch.
func (d *vp8lReader) readTree(alphabetSize uint32) (simple bool, nSimple uint32, symbols [2]uint32, lengths []uint32, scratch int64, err error) {
	useSimple, err := d.read(1)
	if err != nil {
		return
	}
	if useSimple != 0 {
		simple = true
		if nSimple, err = d.read(1); err != nil {
			return
		}
		nSimple++
		var first uint32
		if first, err = d.read(1); err != nil {
			return
		}
		if symbols[0], err = d.read(7*first + 1); err != nil {
			return
		}
		if nSimple == 2 {
			if symbols[1], err = d.read(8); err != nil {
				return
			}
		}
		return
	}
	nCodes, err := d.read(4)
	if err != nil {
		return
	}
	nCodes += 4
	if int(nCodes) > len(vp8lCodeLengthOrder) {
		err = errVP8LTree
		return
	}
	var clcl [len(vp8lCodeLengthOrder)]uint32
	for i := uint32(0); i < nCodes; i++ {
		if clcl[vp8lCodeLengthOrder[i]], err = d.read(3); err != nil {
			return
		}
	}
	lengths = make([]uint32, alphabetSize)
	// codeLengths, then the code-length code's tree (nodes and codes).
	scratch = allocBytes(4*int64(alphabetSize)) + int64(unsafe.Sizeof(vp8lTree{})) +
		allocBytes(8*(2*int64(len(clcl))-1)) + allocBytes(4*int64(len(clcl)))
	err = d.decodeCodeLengths(lengths, clcl[:])
	return
}

// decodeTree is vp8l's decodeHuffmanTree into h.
func (d *vp8lReader) decodeTree(h *vp8lTree, alphabetSize uint32) error {
	simple, n, symbols, lengths, _, err := d.readTree(alphabetSize)
	if err != nil {
		return err
	}
	if simple {
		return h.buildSimple(n, symbols, alphabetSize)
	}
	return h.build(lengths)
}

// treeBytes reads one tree and returns what vp8l allocates for it: its
// nodes (and, for a normal code, the canonical codes) when keep, plus the
// scratch every read makes. It fails where vp8l's build would.
func (d *vp8lReader) treeBytes(alphabetSize uint32, keep bool) (int64, error) {
	simple, n, symbols, lengths, scratch, err := d.readTree(alphabetSize)
	if err != nil {
		return 0, err
	}
	if !keep {
		return scratch, nil
	}
	if simple {
		for i := range n {
			if symbols[i] >= alphabetSize {
				return 0, errVP8LTree
			}
		}
		return allocBytes(8 * (2*int64(n) - 1)), nil
	}
	n = codedSymbols(lengths)
	if n == 0 {
		return 0, errVP8LTree
	}
	b := scratch + allocBytes(8*(2*int64(n)-1))
	if n > 1 {
		b += allocBytes(4 * int64(alphabetSize))
	}
	return b, nil
}

// topLevelGroupBytes is vp8l's decodeHuffmanGroups for the top-level image,
// counting the groups and trees instead of keeping them.
func (d *vp8lReader) topLevelGroupBytes(w, h int32, ccBits uint32) (int64, error) {
	var mapping []int16
	maxIndex := 0
	numGroups := 1
	var n int64
	useMeta, err := d.read(1)
	if err != nil {
		return 0, err
	}
	if useMeta != 0 {
		hBits, err := d.read(3)
		if err != nil {
			return 0, err
		}
		hBits += 2
		tileW, tileH := vp8lTiles(w, hBits), vp8lTiles(h, hBits)
		numTiles := tileW * tileH
		hPix, err := d.decodeSubPix(tileW, tileH, 0)
		if err != nil {
			return 0, err
		}
		for p := 0; p < len(hPix); p += 4 {
			maxIndex = max(maxIndex, int(hPix[p])<<8|int(hPix[p+1]))
		}
		const maxHuffImageSize = 2600
		if maxIndex >= maxHuffImageSize {
			return 0, errors.New("vp8l: too many Huffman trees")
		}
		if maxIndex >= 1000 || int32(maxIndex) >= numTiles {
			mapping = make([]int16, maxIndex+1)
			n += allocBytes(2 * int64(maxIndex+1))
			for i := range mapping {
				mapping[i] = -1
			}
			numGroups = 0
			for p := 0; p < len(hPix); p += 4 {
				i := int(hPix[p])<<8 | int(hPix[p+1])
				if mapping[i] == -1 {
					mapping[i] = int16(numGroups)
					numGroups++
				}
			}
		} else {
			numGroups = maxIndex + 1
		}
	}
	n += allocBytes(int64(numGroups) * vp8lGroupSize)
	for i := range maxIndex + 1 {
		keep := mapping == nil || mapping[i] != -1
		for j, size := range vp8lAlphabet {
			if j == 0 && ccBits > 0 {
				size += 1 << ccBits
			}
			b, err := d.treeBytes(size, keep)
			if err != nil {
				return 0, err
			}
			n += b
		}
	}
	return n, nil
}

// decodeSubPix is vp8l's decodePix for a sub-image (topLevel false): one
// group of trees, no meta Huffman image.
func (d *vp8lReader) decodeSubPix(w, h, minCap int32) ([]byte, error) {
	ccBits, ccShift, ccEntries := uint32(0), uint32(0), []uint32(nil)
	useColorCache, err := d.read(1)
	if err != nil {
		return nil, err
	}
	if useColorCache != 0 {
		if ccBits, err = d.read(4); err != nil {
			return nil, err
		}
		if ccBits < 1 || 11 < ccBits {
			return nil, errors.New("vp8l: invalid color cache parameters")
		}
		ccShift = 32 - ccBits
		ccEntries = make([]uint32, 1<<ccBits)
	}
	var hg [5]vp8lTree
	for j, size := range vp8lAlphabet {
		if j == 0 && ccBits > 0 {
			size += 1 << ccBits
		}
		if err := d.decodeTree(&hg[j], size); err != nil {
			return nil, err
		}
	}
	if minCap < 4*w*h {
		minCap = 4 * w * h
	}
	pix := make([]byte, 4*w*h, minCap)
	p, cachedP := 0, 0
	x, y := int32(0), int32(0)
	for p < len(pix) {
		green, err := hg[0].next(d)
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
			if x++; x == w {
				x, y = 0, y+1
			}
		case green < 256+24:
			length, err := d.lz77Param(green - 256)
			if err != nil {
				return nil, err
			}
			distSym, err := hg[4].next(d)
			if err != nil {
				return nil, err
			}
			distCode, err := d.lz77Param(distSym)
			if err != nil {
				return nil, err
			}
			dist := vp8lDistance(w, distCode)
			pEnd := p + 4*int(length)
			q := p - 4*int(dist)
			qEnd := pEnd - 4*int(dist)
			if p < 0 || len(pix) < pEnd || q < 0 || len(pix) < qEnd {
				return nil, errors.New("vp8l: invalid LZ77 parameters")
			}
			for ; p < pEnd; p, q = p+1, q+1 {
				pix[p] = pix[q]
			}
			x += int32(length)
			for x >= w {
				x, y = x-w, y+1
			}
		default:
			for ; cachedP < p; cachedP += 4 {
				argb := uint32(pix[cachedP+0])<<16 | uint32(pix[cachedP+1])<<8 |
					uint32(pix[cachedP+2]) | uint32(pix[cachedP+3])<<24
				ccEntries[(argb*0x1e35a7bd)>>ccShift] = argb
			}
			green -= 256 + 24
			if int(green) >= len(ccEntries) {
				return nil, errors.New("vp8l: invalid color cache index")
			}
			argb := ccEntries[green]
			pix[p+0], pix[p+1], pix[p+2], pix[p+3] = uint8(argb>>16), uint8(argb>>8), uint8(argb), uint8(argb>>24)
			p += 4
			if x++; x == w {
				x, y = 0, y+1
			}
		}
	}
	return pix, nil
}

func (d *vp8lReader) lz77Param(symbol uint32) (uint32, error) {
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

var vp8lDistanceTable = [120]uint8{
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

func vp8lDistance(w int32, code uint32) int32 {
	if int32(code) > int32(len(vp8lDistanceTable)) {
		return int32(code) - int32(len(vp8lDistanceTable))
	}
	distCode := int32(vp8lDistanceTable[code-1])
	yOffset := distCode >> 4
	xOffset := 8 - distCode&0xf
	if d := yOffset*w + xOffset; d >= 1 {
		return d
	}
	return 1
}
