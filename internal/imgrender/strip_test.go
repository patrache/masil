package imgrender_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/webp"

	"github.com/patrache/masil/internal/imgrender"
)

// Pending item 12: WebP and GIF uploads must leave the client without EXIF,
// XMP, ICC profiles, comments or any block we cannot vouch for, in a shape
// the server's check (api.md 5.14) accepts. Anything we cannot parse exactly
// is refused, never passed through.

// Strings planted in the metadata fixtures; none may survive PrepareUpload.
var leaks = [][]byte{
	[]byte("GPS"), []byte("Exif"), []byte("xmpmeta"), []byte("ICCPROFILE"),
	[]byte("iPhone"), []byte("XMP Data"), []byte("ICCRGBG1"), []byte("SECRET"),
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

type riffChunk struct {
	id   string
	data []byte
}

// parseRIFF splits a WebP into its top-level chunks, failing the test on any
// size the server would call broken.
func parseRIFF(t *testing.T, b []byte) []riffChunk {
	t.Helper()
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		t.Fatalf("not a RIFF/WEBP file")
	}
	if int(binary.LittleEndian.Uint32(b[4:]))+8 != len(b) {
		t.Fatalf("RIFF size %d + 8 != file size %d", binary.LittleEndian.Uint32(b[4:]), len(b))
	}
	var out []riffChunk
	for i := 12; i < len(b); {
		if i+8 > len(b) {
			t.Fatalf("truncated chunk header at %d", i)
		}
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		end := i + 8 + n + n&1
		if end > len(b) {
			t.Fatalf("chunk %q overruns the file", b[i:i+4])
		}
		out = append(out, riffChunk{string(b[i : i+4]), b[i+8 : i+8+n]})
		i = end
	}
	return out
}

// buildWebP assembles a RIFF/WEBP file from chunks (padding odd sizes).
func buildWebP(chunks ...riffChunk) []byte {
	var body bytes.Buffer
	body.WriteString("WEBP")
	for _, c := range chunks {
		body.WriteString(c.id)
		_ = binary.Write(&body, binary.LittleEndian, uint32(len(c.data)))
		body.Write(c.data)
		if len(c.data)&1 == 1 {
			body.WriteByte(0)
		}
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func ids(cs []riffChunk) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.id
	}
	return out
}

func chunk(cs []riffChunk, id string) []byte {
	for _, c := range cs {
		if c.id == id {
			return c.data
		}
	}
	return nil
}

func assertNoLeaks(t *testing.T, out []byte) {
	t.Helper()
	for _, l := range leaks {
		if bytes.Contains(out, l) {
			t.Errorf("output still carries %q", l)
		}
	}
}

// assertServerAcceptsWebP mirrors api.md 5.14: only VP8 /VP8L/VP8X/ALPH/
// ANIM/ANMF chunks, exact RIFF and chunk sizes.
func assertServerAcceptsWebP(t *testing.T, out []byte) []riffChunk {
	t.Helper()
	cs := parseRIFF(t, out)
	if len(cs) == 0 {
		t.Fatal("no chunks")
	}
	for _, c := range cs {
		switch c.id {
		case "VP8 ", "VP8L", "VP8X", "ALPH", "ANIM", "ANMF":
		default:
			t.Errorf("chunk %q would be refused by the server", c.id)
		}
	}
	return cs
}

func prepare(t *testing.T, name string, in []byte) ([]byte, string) {
	t.Helper()
	out, ct, err := imgrender.PrepareUpload(writeTemp(t, name, in))
	if err != nil {
		t.Fatalf("PrepareUpload: %v", err)
	}
	return out, ct
}

func sameConfig(t *testing.T, in, out []byte) {
	t.Helper()
	ci, err := webp.DecodeConfig(bytes.NewReader(in))
	if err != nil {
		t.Fatalf("DecodeConfig(in): %v", err)
	}
	co, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("image.DecodeConfig(out): %v", err)
	}
	if ci.Width != co.Width || ci.Height != co.Height {
		t.Fatalf("size %dx%d, want %dx%d", co.Width, co.Height, ci.Width, ci.Height)
	}
}

const (
	vp8xAnim  = 1 << 1
	vp8xXMP   = 1 << 2
	vp8xEXIF  = 1 << 3
	vp8xAlpha = 1 << 4
	vp8xICC   = 1 << 5
)

func TestPrepareUploadStripsWebPStillMetadata(t *testing.T) {
	in := readFixture(t, "still-alpha-meta.webp") // VP8X ICCP ALPH VP8 EXIF XMP
	src := parseRIFF(t, in)
	if f := chunk(src, "VP8X")[0]; f&(vp8xICC|vp8xEXIF|vp8xXMP) != vp8xICC|vp8xEXIF|vp8xXMP {
		t.Fatalf("fixture flags %#x lack ICC/EXIF/XMP", f)
	}
	out, ct := prepare(t, "x.webp", in)
	if ct != "image/webp" {
		t.Fatalf("ct = %q", ct)
	}
	cs := assertServerAcceptsWebP(t, out)
	if got := ids(cs); len(got) != 3 || got[0] != "VP8X" || got[1] != "ALPH" || got[2] != "VP8 " {
		t.Fatalf("chunks = %q, want [VP8X ALPH VP8 ]", got)
	}
	if f := chunk(cs, "VP8X")[0]; f != vp8xAlpha {
		t.Fatalf("VP8X flags = %#x, want alpha only", f)
	}
	if !bytes.Equal(chunk(cs, "VP8 "), chunk(src, "VP8 ")) || !bytes.Equal(chunk(cs, "ALPH"), chunk(src, "ALPH")) {
		t.Fatal("pixel chunks changed")
	}
	if !bytes.Equal(chunk(cs, "VP8X")[4:], chunk(src, "VP8X")[4:]) {
		t.Fatal("canvas size changed")
	}
	assertNoLeaks(t, out)
	sameConfig(t, in, out)
	a, err := webp.Decode(bytes.NewReader(in))
	if err != nil {
		t.Fatalf("decode in: %v", err)
	}
	b, err := webp.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode out: %v", err)
	}
	if a.Bounds() != b.Bounds() || color.NRGBAModel.Convert(a.At(1, 1)) != color.NRGBAModel.Convert(b.At(1, 1)) {
		t.Fatal("decoded pixels differ")
	}
}

func TestPrepareUploadStripsWebPAnimatedMetadata(t *testing.T) {
	in := readFixture(t, "anim-meta.webp") // VP8X ICCP ANIM ANMF ANMF EXIF XMP
	src := parseRIFF(t, in)
	// Smuggle an unknown chunk inside the second frame too.
	var frames []riffChunk
	for _, c := range src {
		if c.id == "ANMF" {
			frames = append(frames, c)
		}
	}
	f2 := append(append([]byte{}, frames[1].data...), buildWebP(riffChunk{"EXIF", []byte("GPS SECRET")})[12:]...)
	in = buildWebP(src[0], src[1], src[2], frames[0], riffChunk{"ANMF", f2}, src[5], src[6])

	out, ct := prepare(t, "x.webp", in)
	if ct != "image/webp" {
		t.Fatalf("ct = %q", ct)
	}
	cs := assertServerAcceptsWebP(t, out)
	got := ids(cs)
	if len(got) != 4 || got[0] != "VP8X" || got[1] != "ANIM" || got[2] != "ANMF" || got[3] != "ANMF" {
		t.Fatalf("chunks = %q, want [VP8X ANIM ANMF ANMF] (same frame count)", got)
	}
	if f := chunk(cs, "VP8X")[0]; f != vp8xAlpha|vp8xAnim {
		t.Fatalf("VP8X flags = %#x, want alpha|animation", f)
	}
	if !bytes.Equal(cs[1].data, chunk(src, "ANIM")) || !bytes.Equal(cs[2].data, frames[0].data) || !bytes.Equal(cs[3].data, frames[1].data) {
		t.Fatal("animation or frame chunks changed (beyond the smuggled sub-chunk)")
	}
	assertNoLeaks(t, out)
	sameConfig(t, in, out)
}

func TestPrepareUploadStripsSimpleWebPTrailingChunks(t *testing.T) {
	clean := readFixture(t, "simple-lossless.webp") // a single VP8L chunk
	src := parseRIFF(t, clean)
	in := buildWebP(src[0],
		riffChunk{"EXIF", []byte("Exif\x00\x00MM GPS 37.5665")},
		riffChunk{"XMP ", []byte("<x:xmpmeta/>")},
		riffChunk{"ICCP", []byte("ICCPROFILE")},
		riffChunk{"VP8L", src[0].data}, // a second, hidden image (e.g. the uncropped original)
		riffChunk{"ZZZZ", []byte("SECRET")})
	out, _ := prepare(t, "x.webp", in)
	if !bytes.Equal(out, clean) {
		t.Fatalf("out = %q, want just the first image chunk", ids(parseRIFF(t, out)))
	}
	sameConfig(t, in, out)

	// A clean simple file stays byte-identical.
	if out, _ := prepare(t, "y.webp", clean); !bytes.Equal(out, clean) {
		t.Fatal("clean simple WebP was modified")
	}
}

func TestPrepareUploadRefusesBrokenWebP(t *testing.T) {
	still := readFixture(t, "still-alpha-meta.webp")
	src := parseRIFF(t, still)
	vp8x, iccp, alph, vp8, exif := src[0], src[1], src[2], src[3], src[4]
	anim := parseRIFF(t, readFixture(t, "anim-meta.webp"))
	simple := parseRIFF(t, readFixture(t, "simple-lossless.webp"))[0]

	setU32 := func(b []byte, at int, v uint32) []byte {
		c := append([]byte{}, b...)
		binary.LittleEndian.PutUint32(c[at:], v)
		return c
	}
	noAnimFlag := riffChunk{"VP8X", append([]byte{vp8xAlpha}, anim[0].data[1:]...)}
	shortFrame := riffChunk{"ANMF", anim[3].data[:12]}

	cases := map[string][]byte{
		"truncated":            still[:len(still)-5],
		"RIFF size too big":    setU32(still, 4, uint32(len(still))),
		"trailing junk":        append(append([]byte{}, still...), "GPS SECRET"...),
		"chunk size overflows": setU32(still, 12+8+10+4, 0x7fffffff), // ICCP size
		"chunk size short":     setU32(buildWebP(simple), 16, 3),
		"no image":             buildWebP(vp8x, iccp, exif),
		"VP8X not first":       buildWebP(iccp, vp8x, alph, vp8),
		"two VP8X":             buildWebP(vp8x, vp8x, alph, vp8),
		"VP8X wrong size":      buildWebP(riffChunk{"VP8X", vp8x.data[:9]}, alph, vp8),
		"animation, no ANIM":   buildWebP(anim[0], anim[3], anim[4]),
		"ANIM, no frames":      buildWebP(anim[0], anim[2]),
		"frames, no anim flag": buildWebP(noAnimFlag, anim[2], anim[3], anim[4]),
		"frame header short":   buildWebP(anim[0], anim[2], shortFrame),
		"frame without image":  buildWebP(anim[0], anim[2], riffChunk{"ANMF", anim[3].data[:16]}),
		"empty":                buildWebP(),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out, _, err := imgrender.PrepareUpload(writeTemp(t, "x.webp", in))
			if err == nil {
				t.Fatalf("accepted (%d bytes), want refusal", len(out))
			}
			if !errors.Is(err, imgrender.ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported (malformed)", err)
			}
		})
	}
}

// --- GIF ---

// twoFrameGIF encodes a looping two-frame GIF (Go writes NETSCAPE2.0 and a
// graphic control extension per frame).
func twoFrameGIF(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White, color.RGBA{255, 0, 0, 255}}
	f1 := image.NewPaletted(image.Rect(0, 0, 4, 3), pal)
	f2 := image.NewPaletted(image.Rect(0, 0, 4, 3), pal)
	f1.SetColorIndex(1, 1, 2)
	f2.SetColorIndex(2, 2, 1)
	var b bytes.Buffer
	err := gif.EncodeAll(&b, &gif.GIF{
		Image: []*image.Paletted{f1, f2}, Delay: []int{10, 25},
		Disposal: []byte{gif.DisposalNone, gif.DisposalBackground}, LoopCount: 0,
	})
	if err != nil {
		t.Fatalf("EncodeAll: %v", err)
	}
	return b.Bytes()
}

// gifHeaderLen is the size of header + logical screen descriptor + global
// colour table.
func gifHeaderLen(b []byte) int {
	n := 13
	if b[10]&0x80 != 0 {
		n += 3 << ((b[10] & 7) + 1)
	}
	return n
}

func subBlocks(data ...string) []byte {
	var b []byte
	for _, d := range data {
		b = append(b, byte(len(d)))
		b = append(b, d...)
	}
	return append(b, 0)
}

func ext(label byte, blocks []byte) []byte { return append([]byte{0x21, label}, blocks...) }

var (
	gifComment  = ext(0xFE, subBlocks("GPS 37.5665,126.9780 iPhone"))
	gifXMP      = ext(0xFF, subBlocks("XMP DataXMP", "<x:xmpmeta>GPS</x:xmpmeta>"))
	gifICC      = ext(0xFF, subBlocks("ICCRGBG1012", "ICCPROFILE"))
	gifUnknown  = ext(0x99, subBlocks("SECRET"))
	gifPlain    = ext(0x01, subBlocks("\x00\x00\x00\x00\x04\x00\x03\x00\x01\x01\x01\x00", "SECRET text"))
	gifGCEAlone = ext(0xF9, subBlocks("\x04\x00\x00\x00"))
)

func insertAt(b []byte, at int, parts ...[]byte) []byte {
	out := append([]byte{}, b[:at]...)
	for _, p := range parts {
		out = append(out, p...)
	}
	return append(out, b[at:]...)
}

// gifBlocks walks a GIF the way the server does (api.md 5.14) and returns
// the extension labels / application ids it saw; it fails on any structure
// the server would refuse, and also on plain text (0x01), which the server
// allows but the client drops.
func gifBlocks(t *testing.T, b []byte) (labels []string) {
	t.Helper()
	i := gifHeaderLen(b)
	skip := func(i int) int {
		for i < len(b) {
			n := int(b[i])
			if n == 0 {
				return i + 1
			}
			i += 1 + n
		}
		t.Fatalf("truncated sub-blocks")
		return 0
	}
	for i < len(b) {
		switch b[i] {
		case 0x3B:
			if i+1 != len(b) {
				t.Fatalf("data after the trailer")
			}
			return labels
		case 0x2C:
			p := b[i+9]
			i += 10
			if p&0x80 != 0 {
				i += 3 << ((p & 7) + 1)
			}
			i = skip(i + 1)
			labels = append(labels, "image")
		case 0x21:
			switch b[i+1] {
			case 0xFF:
				app := string(b[i+3 : i+14])
				if app != "NETSCAPE2.0" && app != "ANIMEXTS1.0" {
					t.Errorf("application extension %q would be refused", app)
				}
				labels = append(labels, app)
			case 0xF9:
				labels = append(labels, "gce")
			default:
				t.Errorf("extension %#x would be refused", b[i+1])
			}
			i = skip(i + 2)
		default:
			t.Fatalf("bad block %#x at %d", b[i], i)
		}
	}
	t.Fatal("no trailer")
	return nil
}

func sameGIF(t *testing.T, in, out []byte) {
	t.Helper()
	a, err := gif.DecodeAll(bytes.NewReader(in))
	if err != nil {
		t.Fatalf("DecodeAll(in): %v", err)
	}
	b, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("DecodeAll(out): %v", err)
	}
	if len(a.Image) != len(b.Image) || a.LoopCount != b.LoopCount || a.Config.Width != b.Config.Width || a.Config.Height != b.Config.Height {
		t.Fatalf("frames %d loop %d %dx%d, want frames %d loop %d %dx%d",
			len(b.Image), b.LoopCount, b.Config.Width, b.Config.Height,
			len(a.Image), a.LoopCount, a.Config.Width, a.Config.Height)
	}
	for k := range a.Image {
		if a.Delay[k] != b.Delay[k] || a.Disposal[k] != b.Disposal[k] || !bytes.Equal(a.Image[k].Pix, b.Image[k].Pix) {
			t.Fatalf("frame %d differs", k)
		}
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(out)); err != nil {
		t.Fatalf("image.DecodeConfig(out): %v", err)
	}
}

func TestPrepareUploadStripsGIFMetadata(t *testing.T) {
	clean := twoFrameGIF(t)
	h := gifHeaderLen(clean)
	// NETSCAPE2.0 with a smuggled extra sub-block.
	netscape := ext(0xFF, subBlocks("NETSCAPE2.0", "\x01\x00\x00", "SECRET"))
	in := insertAt(clean, len(clean)-1, gifComment, gifUnknown) // before the trailer
	in = insertAt(in, h, gifXMP, gifICC, gifComment, gifGCEAlone, gifPlain, netscape)

	out, ct := prepare(t, "x.gif", in)
	if ct != "image/gif" {
		t.Fatalf("ct = %q", ct)
	}
	labels := gifBlocks(t, out)
	images := 0
	for _, l := range labels {
		if l == "image" {
			images++
		}
	}
	if images != 2 {
		t.Fatalf("blocks = %q, want two frames", labels)
	}
	assertNoLeaks(t, out)
	sameGIF(t, clean, out) // in itself does not decode: Go refuses the unknown 0x99 extension

	// A clean Go-encoded GIF is already canonical and stays byte-identical.
	if out, _ := prepare(t, "y.gif", clean); !bytes.Equal(out, clean) {
		t.Fatal("clean GIF was modified")
	}
}

func TestPrepareUploadStripsGIFSmuggledGCEData(t *testing.T) {
	clean := twoFrameGIF(t)
	// Find the first graphic control extension and append a sub-block to it.
	at := bytes.Index(clean, []byte{0x21, 0xF9, 0x04})
	if at < 0 {
		t.Fatal("fixture has no GCE")
	}
	end := at + 3 + 4 // label + size + 4 bytes; terminator follows
	in := insertAt(clean, end, []byte("\x06SECRET"))
	out, _ := prepare(t, "x.gif", in)
	assertNoLeaks(t, out)
	sameGIF(t, clean, out)
}

func TestPrepareUploadRefusesBrokenGIF(t *testing.T) {
	clean := twoFrameGIF(t)
	h := gifHeaderLen(clean)
	noImages := append(append([]byte{}, clean[:h]...), 0x3B)
	cases := map[string][]byte{
		"truncated":               clean[:len(clean)-4],
		"no trailer":              clean[:len(clean)-1],
		"data after trailer":      append(append([]byte{}, clean...), "GPS SECRET"...),
		"no frames":               noImages,
		"bad block introducer":    insertAt(clean, h, []byte{0x00}),
		"app ext id size not 11":  insertAt(clean, h, ext(0xFF, subBlocks("NETSCAPE2", "\x01\x00\x00"))),
		"sub-block overruns file": append(append([]byte{}, clean[:h]...), 0x21, 0xFE, 0x40, 'x'),
		"extension cut at label":  append(append([]byte{}, clean[:h]...), 0x21),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out, _, err := imgrender.PrepareUpload(writeTemp(t, "x.gif", in))
			if err == nil {
				t.Fatalf("accepted (%d bytes), want refusal", len(out))
			}
			if !errors.Is(err, imgrender.ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported (malformed)", err)
			}
		})
	}
}
