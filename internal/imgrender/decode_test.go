package imgrender_test

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/patrache/masil/internal/imgrender"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

const webp1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==" // 1x1 lossless WebP (base64)

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return b.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return b.Bytes()
}

func encodeGIF(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := gif.Encode(&b, img, nil); err != nil {
		t.Fatalf("gif.Encode: %v", err)
	}
	return b.Bytes()
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// exifAPP1 builds a JPEG APP1 segment holding a big-endian TIFF header with a
// single IFD0 entry (Orientation) followed by a GPS-looking marker string.
func exifAPP1(orientation uint16) []byte {
	var tiff bytes.Buffer
	tiff.WriteString("MM\x00\x2a")
	_ = binary.Write(&tiff, binary.BigEndian, uint32(8)) // IFD0 offset
	_ = binary.Write(&tiff, binary.BigEndian, uint16(1)) // entry count
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.BigEndian, uint16(3)) // SHORT
	_ = binary.Write(&tiff, binary.BigEndian, uint32(1))
	_ = binary.Write(&tiff, binary.BigEndian, orientation)
	_ = binary.Write(&tiff, binary.BigEndian, uint16(0))
	_ = binary.Write(&tiff, binary.BigEndian, uint32(0)) // next IFD
	tiff.WriteString("GPS 37.5665N 126.9780E secret-camera-serial")

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	return append(seg, payload...)
}

// injectAfterSOI inserts seg right after the JPEG SOI marker.
func injectAfterSOI(t *testing.T, jpg, seg []byte) []byte {
	t.Helper()
	if len(jpg) < 2 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		t.Fatalf("not a JPEG")
	}
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

// injectPNGText inserts a tEXt chunk right after IHDR.
func injectPNGText(t *testing.T, p []byte, keyword, text string) []byte {
	t.Helper()
	const ihdrEnd = 8 + 4 + 4 + 13 + 4
	if len(p) < ihdrEnd || string(p[12:16]) != "IHDR" {
		t.Fatalf("not a PNG with IHDR first")
	}
	data := []byte(keyword + "\x00" + text)
	chunk := make([]byte, 4, 12+len(data))
	binary.BigEndian.PutUint32(chunk, uint32(len(data)))
	chunk = append(chunk, "tEXt"...)
	chunk = append(chunk, data...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	out := append([]byte{}, p[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, p[ihdrEnd:]...)
}

func TestDecodeFormats(t *testing.T) {
	src := fixtures.TestImage()
	want := src.Bounds()
	webp, err := base64.StdEncoding.DecodeString(webp1x1)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ct   string
		data []byte
		size image.Point
	}{
		{"image/png", encodePNG(t, src), want.Size()},
		{"image/jpeg", encodeJPEG(t, src), want.Size()},
		{"image/gif", encodeGIF(t, src), want.Size()},
		{"image/webp", webp, image.Pt(1, 1)},
		{"IMAGE/PNG; charset=binary", encodePNG(t, src), want.Size()},
		{"", encodePNG(t, src), want.Size()},               // sniffed
		{"application/octet-stream", webp, image.Pt(1, 1)}, // sniffed
	}
	for _, c := range cases {
		img, err := imgrender.Decode(c.data, c.ct)
		if err != nil {
			t.Fatalf("Decode(%q): %v", c.ct, err)
		}
		if got := img.Bounds().Size(); got != c.size {
			t.Fatalf("Decode(%q) size = %v, want %v", c.ct, got, c.size)
		}
	}

	if _, err := imgrender.Decode(encodePNG(t, src), "image/bmp"); !errors.Is(err, imgrender.ErrUnsupported) {
		t.Fatalf("Decode(image/bmp) err = %v, want ErrUnsupported", err)
	}
	if _, err := imgrender.Decode([]byte("not an image at all"), ""); !errors.Is(err, imgrender.ErrUnsupported) {
		t.Fatalf("Decode(text, \"\") err = %v, want ErrUnsupported", err)
	}
	if _, err := imgrender.Decode([]byte("\x89PNG\r\n\x1a\ngarbage"), "image/png"); err == nil {
		t.Fatalf("Decode(corrupt png) err = nil")
	}
}

// A tiny PNG that claims gigantic dimensions must be rejected before any
// pixel buffer is allocated (decompression bomb).
func TestDecodeRejectsHugeDimensions(t *testing.T) {
	p := encodePNG(t, image.NewGray(image.Rect(0, 0, 1, 1)))
	// Patch IHDR width/height to 60000x60000 and fix its CRC.
	binary.BigEndian.PutUint32(p[16:], 60000)
	binary.BigEndian.PutUint32(p[20:], 60000)
	binary.BigEndian.PutUint32(p[29:], crc32.ChecksumIEEE(p[12:29]))
	if _, err := imgrender.Decode(p, "image/png"); !errors.Is(err, imgrender.ErrTooLarge) {
		t.Fatalf("Decode(60000x60000) err = %v, want ErrTooLarge", err)
	}
	if _, _, err := imgrender.PrepareUpload(writeTemp(t, "bomb.png", p)); !errors.Is(err, imgrender.ErrTooLarge) {
		t.Fatalf("PrepareUpload(60000x60000) err = %v, want ErrTooLarge", err)
	}
}

func TestPrepareUploadStripsJPEGExif(t *testing.T) {
	in := injectAfterSOI(t, encodeJPEG(t, fixtures.TestImage()), exifAPP1(1))
	if !bytes.Contains(in, []byte("Exif")) {
		t.Fatal("fixture lost its Exif segment")
	}
	// The extension lies on purpose: the format is sniffed from the bytes.
	data, ct, err := imgrender.PrepareUpload(writeTemp(t, "photo.png", in))
	if err != nil {
		t.Fatalf("PrepareUpload: %v", err)
	}
	if ct != "image/jpeg" {
		t.Fatalf("ct = %q, want image/jpeg", ct)
	}
	if bytes.Contains(data, []byte("Exif")) || bytes.Contains(data, []byte("GPS")) {
		t.Fatal("output still carries Exif metadata")
	}
	img, err := imgrender.Decode(data, ct)
	if err != nil {
		t.Fatalf("Decode(output): %v", err)
	}
	if got := img.Bounds().Size(); got != fixtures.TestImage().Bounds().Size() {
		t.Fatalf("output size = %v", got)
	}
}

// Stripping Exif also drops the Orientation tag, so the rotation it describes
// is baked into the pixels first (otherwise phone portraits upload sideways).
func TestPrepareUploadAppliesExifOrientation(t *testing.T) {
	// 40x20: left half red, right half blue.
	src := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := range 20 {
		for x := range 40 {
			c := color.RGBA{0xff, 0, 0, 0xff}
			if x >= 20 {
				c = color.RGBA{0, 0, 0xff, 0xff}
			}
			src.SetRGBA(x, y, c)
		}
	}
	in := injectAfterSOI(t, encodeJPEG(t, src), exifAPP1(6)) // 6 = rotate 90° clockwise to display
	data, ct, err := imgrender.PrepareUpload(writeTemp(t, "portrait.jpg", in))
	if err != nil {
		t.Fatalf("PrepareUpload: %v", err)
	}
	img, err := imgrender.Decode(data, ct)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := img.Bounds().Size(); got != image.Pt(20, 40) {
		t.Fatalf("size = %v, want 20x40 (rotated)", got)
	}
	isRed := func(c color.Color) bool { r, g, b, _ := c.RGBA(); return r > 0xc000 && g < 0x4000 && b < 0x4000 }
	isBlue := func(c color.Color) bool { r, g, b, _ := c.RGBA(); return b > 0xc000 && r < 0x4000 && g < 0x4000 }
	b := img.Bounds()
	if top := img.At(b.Min.X+10, b.Min.Y+5); !isRed(top) {
		t.Fatalf("top pixel = %v, want red (left edge rotated to top)", top)
	}
	if bottom := img.At(b.Min.X+10, b.Min.Y+35); !isBlue(bottom) {
		t.Fatalf("bottom pixel = %v, want blue", bottom)
	}
}

func TestPrepareUploadReencodesPNG(t *testing.T) {
	in := injectPNGText(t, encodePNG(t, fixtures.TestImage()), "Comment", "taken at home")
	if _, err := png.Decode(bytes.NewReader(in)); err != nil {
		t.Fatalf("fixture PNG invalid: %v", err)
	}
	data, ct, err := imgrender.PrepareUpload(writeTemp(t, "shot.jpg", in))
	if err != nil {
		t.Fatalf("PrepareUpload: %v", err)
	}
	if ct != "image/png" {
		t.Fatalf("ct = %q, want image/png", ct)
	}
	if bytes.Contains(data, []byte("tEXt")) || bytes.Contains(data, []byte("taken at home")) {
		t.Fatal("output still carries the tEXt chunk")
	}
	if _, err := imgrender.Decode(data, ct); err != nil {
		t.Fatalf("Decode(output): %v", err)
	}
}

func TestPrepareUploadPassesSmallGIF(t *testing.T) {
	in := encodeGIF(t, fixtures.TestImage())
	out, ct, err := imgrender.PrepareUpload(writeTemp(t, "anim.gif", in))
	if err != nil {
		t.Fatalf("PrepareUpload: %v", err)
	}
	if ct != "image/gif" {
		t.Fatalf("ct = %q, want image/gif", ct)
	}
	if !bytes.Equal(out, in) {
		t.Fatal("small GIF was modified")
	}
}

func TestPrepareUploadPassesWebP(t *testing.T) {
	in, _ := base64.StdEncoding.DecodeString(webp1x1)
	out, ct, err := imgrender.PrepareUpload(writeTemp(t, "x.webp", in))
	if err != nil {
		t.Fatalf("PrepareUpload: %v", err)
	}
	if ct != "image/webp" || !bytes.Equal(out, in) {
		t.Fatalf("ct = %q, equal = %v; want image/webp passed through", ct, bytes.Equal(out, in))
	}
}

func TestPrepareUploadDownscalesHuge(t *testing.T) {
	if testing.Short() {
		t.Skip("encodes a 3000x3000 noise image")
	}
	const n = 3000
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.UintN(256))
	}
	var b bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if b.Len() <= imgrender.MaxUploadBytes {
		t.Fatalf("fixture is only %d bytes", b.Len())
	}
	out, ct, err := imgrender.PrepareUpload(writeTemp(t, "noise.png", b.Bytes()))
	if err != nil {
		t.Fatalf("PrepareUpload: %v", err)
	}
	if ct != "image/jpeg" {
		t.Fatalf("ct = %q, want image/jpeg", ct)
	}
	if len(out) > imgrender.MaxUploadBytes {
		t.Fatalf("len(out) = %d > %d", len(out), imgrender.MaxUploadBytes)
	}
	dec, err := imgrender.Decode(out, ct)
	if err != nil {
		t.Fatalf("Decode(output): %v", err)
	}
	if s := dec.Bounds().Size(); s.X != s.Y || s.X >= n || s.X < 320 {
		t.Fatalf("output size = %v, want a square smaller than %d and >= 320", s, n)
	}
}

func TestPrepareUploadMissingFile(t *testing.T) {
	_, _, err := imgrender.PrepareUpload(filepath.Join(t.TempDir(), "nope.jpg"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestPrepareUploadUnsupported(t *testing.T) {
	_, _, err := imgrender.PrepareUpload(writeTemp(t, "notes.png", []byte("just some text, not an image")))
	if !errors.Is(err, imgrender.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
