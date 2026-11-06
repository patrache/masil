package imgrender

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrMalformed is returned for a WebP or GIF whose container PrepareUpload
// cannot walk exactly (truncated data, chunk or block sizes that do not add
// up, a missing image or trailer). Such files are refused rather than
// uploaded as they are. It wraps ErrUnsupported.
var ErrMalformed = fmt.Errorf("%w: malformed image container", ErrUnsupported)

func malformed(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrMalformed}, a...)...)
}

// stripMetadata rewrites a WebP or GIF so that only pixel and animation data
// remain (pending item 12, api.md 5.14). Other types are returned unchanged.
func stripMetadata(data []byte, ct string) ([]byte, error) {
	switch ct {
	case ctWebP:
		return stripWebP(data)
	case ctGIF:
		return stripGIF(data)
	}
	return data, nil
}

// --- WebP (RIFF) ---

type riffChunk struct {
	id   string
	data []byte
}

// VP8X flag bits (WebP container spec, golang.org/x/image/webp).
const (
	vp8xAnimation = 1 << 1
	vp8xAlpha     = 1 << 4
)

// readChunks splits b into RIFF chunks. Every chunk, including the padding
// byte after an odd-sized payload, must lie inside b, and b must end exactly
// after the last one.
func readChunks(b []byte) ([]riffChunk, error) {
	var cs []riffChunk
	for i := 0; i < len(b); {
		if len(b)-i < 8 {
			return nil, malformed("webp: truncated chunk header")
		}
		n := int64(binary.LittleEndian.Uint32(b[i+4:]))
		end := int64(i) + 8 + n + n&1
		if end > int64(len(b)) {
			return nil, malformed("webp: chunk %q overruns its container", b[i:i+4])
		}
		cs = append(cs, riffChunk{string(b[i : i+4]), b[i+8 : i+8+int(n)]})
		i = int(end)
	}
	return cs, nil
}

func writeChunks(w *bytes.Buffer, cs []riffChunk) {
	var size [4]byte
	for _, c := range cs {
		w.WriteString(c.id)
		binary.LittleEndian.PutUint32(size[:], uint32(len(c.data)))
		w.Write(size[:])
		w.Write(c.data)
		if len(c.data)&1 == 1 {
			w.WriteByte(0)
		}
	}
}

// imageChunks picks the first bitstream (VP8 or VP8L) of a still image or an
// animation frame, with the ALPH chunk right before a VP8 one. Everything
// else (EXIF, XMP, ICCP, unknown chunks, further bitstreams) is dropped.
func imageChunks(cs []riffChunk) ([]riffChunk, error) {
	for k, c := range cs {
		switch c.id {
		case "VP8L":
			return cs[k : k+1], nil
		case "VP8 ":
			if k > 0 && cs[k-1].id == "ALPH" {
				return cs[k-1 : k+1], nil
			}
			return cs[k : k+1], nil
		}
	}
	return nil, malformed("webp: no VP8/VP8L image data")
}

// stripWebP keeps VP8X (with only its alpha and animation flags), ALPH,
// VP8 / VP8L, ANIM and ANMF (whose frames are filtered the same way), and
// recomputes every chunk and RIFF size.
func stripWebP(b []byte) ([]byte, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return nil, malformed("webp: not a RIFF/WEBP file")
	}
	if int64(binary.LittleEndian.Uint32(b[4:]))+8 != int64(len(b)) {
		return nil, malformed("webp: RIFF size does not match the file size")
	}
	cs, err := readChunks(b[12:])
	if err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return nil, malformed("webp: no chunks")
	}

	var keep []riffChunk
	switch cs[0].id {
	case "VP8 ", "VP8L": // simple format: one bitstream chunk
		keep = cs[:1]
	case "VP8X":
		if keep, err = stripExtended(cs); err != nil {
			return nil, err
		}
	default:
		return nil, malformed("webp: first chunk is %q", cs[0].id)
	}

	var body bytes.Buffer
	body.WriteString("WEBP")
	writeChunks(&body, keep)
	out := make([]byte, 8, 8+body.Len())
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(body.Len()))
	return append(out, body.Bytes()...), nil
}

// stripExtended handles a VP8X file: cs[0] is the VP8X chunk.
func stripExtended(cs []riffChunk) ([]riffChunk, error) {
	if len(cs[0].data) != 10 {
		return nil, malformed("webp: VP8X chunk is %d bytes", len(cs[0].data))
	}
	vp8x := append([]byte(nil), cs[0].data...)
	vp8x[0] &= vp8xAlpha | vp8xAnimation // clear ICC, EXIF, XMP and reserved bits
	keep := []riffChunk{{"VP8X", vp8x}}

	var anim *riffChunk
	var frames []riffChunk
	for _, c := range cs[1:] {
		switch c.id {
		case "VP8X":
			return nil, malformed("webp: second VP8X chunk")
		case "ANIM":
			if anim != nil || len(frames) > 0 || len(c.data) != 6 {
				return nil, malformed("webp: misplaced or bad ANIM chunk")
			}
			anim = &c
		case "ANMF":
			f, err := stripFrame(c.data)
			if err != nil {
				return nil, err
			}
			frames = append(frames, riffChunk{"ANMF", f})
		}
	}

	if vp8x[0]&vp8xAnimation == 0 {
		if anim != nil || len(frames) > 0 {
			return nil, malformed("webp: animation chunks without the animation flag")
		}
		img, err := imageChunks(cs[1:])
		if err != nil {
			return nil, err
		}
		return append(keep, img...), nil
	}
	if anim == nil || len(frames) == 0 {
		return nil, malformed("webp: animation flag without ANIM and ANMF chunks")
	}
	keep = append(keep, *anim)
	return append(keep, frames...), nil
}

// stripFrame keeps an ANMF payload's 16-byte frame header and its image
// chunks, dropping any other sub-chunk.
func stripFrame(d []byte) ([]byte, error) {
	if len(d) < 16 {
		return nil, malformed("webp: ANMF chunk is %d bytes", len(d))
	}
	sub, err := readChunks(d[16:])
	if err != nil {
		return nil, err
	}
	img, err := imageChunks(sub)
	if err != nil {
		return nil, err
	}
	var w bytes.Buffer
	w.Write(d[:16])
	writeChunks(&w, img)
	return w.Bytes(), nil
}

// --- GIF ---

// gifSubBlocks reads the data sub-blocks starting at b[i] and returns their
// payloads and the index after the zero terminator.
func gifSubBlocks(b []byte, i int) ([][]byte, int, error) {
	var blocks [][]byte
	for i < len(b) {
		n := int(b[i])
		if n == 0 {
			return blocks, i + 1, nil
		}
		if i+1+n > len(b) {
			break
		}
		blocks = append(blocks, b[i+1:i+1+n])
		i += 1 + n
	}
	return nil, 0, malformed("gif: truncated data sub-blocks")
}

// gifColorTable is the byte size of a colour table flagged in packed.
func gifColorTable(packed byte) int {
	if packed&0x80 == 0 {
		return 0
	}
	return 3 << ((packed & 7) + 1)
}

// stripGIF keeps the header, colour tables, image data, graphic control
// extensions (one 4-byte sub-block each) and the loop sub-block of
// NETSCAPE2.0 / ANIMEXTS1.0. Comments, plain text (with the graphic control
// extension that belongs to it), other application extensions (XMP, ICC)
// and unknown extensions are dropped. The trailer must end the file.
func stripGIF(b []byte) ([]byte, error) {
	if len(b) < 13 || (string(b[:6]) != "GIF87a" && string(b[:6]) != "GIF89a") {
		return nil, malformed("gif: bad header")
	}
	i := 13 + gifColorTable(b[10])
	if i > len(b) {
		return nil, malformed("gif: truncated global colour table")
	}
	out := append(make([]byte, 0, len(b)), b[:i]...)
	var gce []byte // pending graphic control extension, emitted with its image
	frames := 0
	for i < len(b) {
		switch b[i] {
		case 0x3B:
			if i+1 != len(b) {
				return nil, malformed("gif: data after the trailer")
			}
			if frames == 0 {
				return nil, malformed("gif: no image")
			}
			return append(out, 0x3B), nil
		case 0x2C:
			if i+10 > len(b) {
				return nil, malformed("gif: truncated image descriptor")
			}
			j := i + 10 + gifColorTable(b[i+9]) + 1 // + LZW minimum code size
			if j > len(b) {
				return nil, malformed("gif: truncated image")
			}
			_, end, err := gifSubBlocks(b, j)
			if err != nil {
				return nil, err
			}
			out = append(out, gce...)
			out = append(out, b[i:end]...)
			gce = nil
			frames++
			i = end
		case 0x21:
			if i+2 > len(b) {
				return nil, malformed("gif: truncated extension")
			}
			label := b[i+1]
			blocks, end, err := gifSubBlocks(b, i+2)
			if err != nil {
				return nil, err
			}
			switch label {
			case 0xF9: // graphic control
				if len(blocks) == 0 || len(blocks[0]) != 4 {
					return nil, malformed("gif: bad graphic control extension")
				}
				gce = append([]byte{0x21, 0xF9, 4}, append(blocks[0][:4:4], 0)...)
			case 0xFF: // application
				if len(blocks) == 0 || len(blocks[0]) != 11 {
					return nil, malformed("gif: bad application extension")
				}
				if app := string(blocks[0]); app == "NETSCAPE2.0" || app == "ANIMEXTS1.0" {
					for _, sb := range blocks[1:] {
						if len(sb) == 3 && sb[0] == 1 { // loop count
							out = append(out, 0x21, 0xFF, 11)
							out = append(out, app...)
							out = append(out, 3, sb[0], sb[1], sb[2], 0)
							break
						}
					}
				}
			case 0x01: // plain text: drop it and the GCE that applies to it
				gce = nil
			}
			// 0xFE comments and unknown extensions are dropped.
			i = end
		default:
			return nil, malformed("gif: bad block introducer %#x", b[i])
		}
	}
	return nil, malformed("gif: no trailer")
}

// headerErr turns a header decode failure of an image we rewrote into
// ErrMalformed, keeping ErrTooLarge as it is.
func headerErr(err error) error {
	if err == nil || errors.Is(err, ErrTooLarge) || errors.Is(err, ErrUnsupported) {
		return err
	}
	return malformed("%v", err)
}
