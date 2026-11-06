// Package imgrender decodes images, prepares uploads (metadata stripping and
// downscaling) and draws images into terminal cells (half blocks, kitty
// placeholders, iTerm2 inline images).
package imgrender

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// MaxUploadBytes is the server's per-image upload limit (api.md 5.14).
const MaxUploadBytes = 5242880

var (
	// ErrUnsupported is returned for anything other than PNG, JPEG, GIF or WebP.
	ErrUnsupported = errors.New("imgrender: unsupported image format")
	// ErrTooLarge is returned when an image cannot be brought under the
	// upload limit, or its dimensions/file size exceed what we decode at all.
	ErrTooLarge = errors.New("imgrender: image too large even after downscaling")
)

// limits bound a decode before any pixel buffer is allocated, so a tiny file
// claiming huge dimensions (or a deep bit depth) cannot exhaust memory.
// bytes is checked against decodeCost: what the format's decoder allocates
// for the header, not just its colour model (cost.go).
type limits struct {
	pixels int64
	bytes  int64
}

var (
	// uploadLimits apply to the user's own files (PrepareUpload, Decode):
	// 50MP still covers 48MP phone photos (8000x6000).
	uploadLimits = limits{pixels: 50_000_000, bytes: 256 << 20}
	// displayLimits apply to remote media (Cache.Load). The server stores
	// uploads as sent (api.md 5.14), so anyone could post a few-hundred-KB
	// 16-bit PNG that decodes to ~400MB; every viewer would pay for it.
	// 24MP keeps 24MP camera photos (baseline 8-bit JPEG, 8-bit PNG)
	// viewable; 16-bit images get 12MP, progressive 4:2:0 JPEGs about 13MP.
	displayLimits = limits{pixels: 24_000_000, bytes: 96 << 20}
)

const (
	// maxInputBytes caps how much of a local file PrepareUpload reads.
	maxInputBytes = 128 << 20
	// minUploadSide is the smallest long side PrepareUpload downscales to.
	minUploadSide = 320

	ctPNG  = "image/png"
	ctJPEG = "image/jpeg"
	ctGIF  = "image/gif"
	ctWebP = "image/webp"
)

type codec struct {
	config func(io.Reader) (image.Config, error)
	decode func(io.Reader) (image.Image, error)
}

var codecs = map[string]codec{
	ctPNG:  {png.DecodeConfig, png.Decode},
	ctJPEG: {jpeg.DecodeConfig, jpeg.Decode},
	ctGIF:  {gif.DecodeConfig, gif.Decode}, // gif.Decode returns the first frame
	ctWebP: {webp.DecodeConfig, webp.Decode},
}

// normalizeType lowercases a Content-Type and drops its parameters. An empty
// or generic type is replaced by the sniffed type of data.
func normalizeType(contentType string, data []byte) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if mt, _, err := mime.ParseMediaType(ct); err == nil {
		ct = mt
	}
	if ct == "" || ct == "application/octet-stream" {
		ct = http.DetectContentType(data)
	}
	return ct
}

// Decode decodes PNG, JPEG, GIF (first frame) or WebP data. contentType names
// the format; an empty or application/octet-stream type is sniffed from data.
// Images over the upload limits fail with ErrTooLarge before decoding.
func Decode(data []byte, contentType string) (image.Image, error) {
	return decodeAs(data, normalizeType(contentType, data), uploadLimits)
}

// decodeDisplay is Decode with the (lower) remote-media limits.
func decodeDisplay(data []byte, contentType string) (image.Image, error) {
	return decodeAs(data, normalizeType(contentType, data), displayLimits)
}

func decodeAs(data []byte, ct string, lim limits) (image.Image, error) {
	c, ok := codecs[ct]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, ct)
	}
	if err := checkConfig(c, data, ct, lim); err != nil {
		return nil, err
	}
	img, err := c.decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("imgrender: decode %s: %w", ct, err)
	}
	return img, nil
}

// checkConfig reads only the header and rejects bombs before decoding: too
// many pixels, or more bytes than the decoder would allocate (decodeCost).
func checkConfig(c codec, data []byte, ct string, lim limits) error {
	cfg, err := c.config(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("imgrender: decode %s header: %w", ct, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("imgrender: decode %s: empty image %dx%d", ct, cfg.Width, cfg.Height)
	}
	px := int64(cfg.Width) * int64(cfg.Height)
	if px > lim.pixels {
		return fmt.Errorf("%w: %dx%d pixels", ErrTooLarge, cfg.Width, cfg.Height)
	}
	if n := decodeCost(ct, data, cfg); n > lim.bytes {
		return fmt.Errorf("%w: %dx%d pixels decode to about %d MB", ErrTooLarge, cfg.Width, cfg.Height, n>>20)
	}
	return nil
}

// PrepareUpload reads an image file and returns bytes safe to upload with
// their Content-Type. The format is sniffed from the bytes (the extension is
// ignored). JPEG and PNG are decoded and re-encoded, which drops all metadata
// (Exif, text chunks); a JPEG's Exif orientation is applied to the pixels
// first. GIF and WebP keep their pixel and animation data byte for byte but
// are rewritten without metadata (stripMetadata: WebP EXIF/XMP/ICCP and
// unknown chunks, GIF comments, plain text and application extensions other
// than the NETSCAPE2.0/ANIMEXTS1.0 loop count); a container that cannot be
// walked exactly fails with ErrMalformed instead of being sent. If the result exceeds
// MaxUploadBytes, the (first frame of the) image is re-encoded as JPEG q=85,
// shrinking the long side by 0.8 each attempt; once the long side would drop
// below 320 pixels it gives up with ErrTooLarge.
func PrepareUpload(path string) (data []byte, contentType string, err error) {
	in, err := readLimited(path)
	if err != nil {
		return nil, "", err
	}
	ct := http.DetectContentType(in)
	c, ok := codecs[ct]
	if !ok {
		return nil, "", fmt.Errorf("%w: %s is %q", ErrUnsupported, path, ct)
	}

	var img image.Image
	out := in
	switch ct {
	case ctJPEG:
		if img, err = decodeAs(in, ct, uploadLimits); err != nil {
			return nil, "", err
		}
		img = applyOrientation(img, jpegOrientation(in))
		if out, err = encodeJPEG(img, 90); err != nil {
			return nil, "", err
		}
	case ctPNG:
		if img, err = decodeAs(in, ct, uploadLimits); err != nil {
			return nil, "", err
		}
		var b bytes.Buffer
		if err = png.Encode(&b, img); err != nil {
			return nil, "", fmt.Errorf("imgrender: encode png: %w", err)
		}
		out = b.Bytes()
	default: // GIF, WebP: rewritten without metadata, then checked.
		if out, err = stripMetadata(in, ct); err != nil {
			return nil, "", err
		}
		if err = headerErr(checkConfig(c, out, ct, uploadLimits)); err != nil {
			return nil, "", err
		}
	}
	if len(out) <= MaxUploadBytes {
		return out, ct, nil
	}

	if img == nil {
		if img, err = decodeAs(out, ct, uploadLimits); err != nil {
			return nil, "", err
		}
	}
	out, err = downscaleToFit(img)
	if err != nil {
		return nil, "", err
	}
	return out, ctJPEG, nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("imgrender: read %s: %w", path, err)
	}
	if len(data) > maxInputBytes {
		return nil, fmt.Errorf("%w: %s is over %d bytes", ErrTooLarge, path, maxInputBytes)
	}
	return data, nil
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("imgrender: encode jpeg: %w", err)
	}
	return b.Bytes(), nil
}

// downscaleToFit flattens img onto white (JPEG has no alpha) and re-encodes
// it as JPEG q=85, first at full size and then with the long side shrunk by
// 0.8 per attempt, until it fits MaxUploadBytes.
func downscaleToFit(img image.Image) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	long := max(w, h)
	for side := long; ; side = side * 8 / 10 {
		if side < minUploadSide && side != long {
			return nil, fmt.Errorf("%w: %dx%d", ErrTooLarge, w, h)
		}
		dw := max(1, (w*side+long/2)/long)
		dh := max(1, (h*side+long/2)/long)
		dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
		draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		if dw == w && dh == h {
			draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
		} else {
			xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
		}
		out, err := encodeJPEG(dst, 85)
		if err != nil {
			return nil, err
		}
		if len(out) <= MaxUploadBytes {
			return out, nil
		}
		if side < minUploadSide {
			return nil, fmt.Errorf("%w: %dx%d", ErrTooLarge, w, h)
		}
	}
}
