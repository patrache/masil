package imgrender_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The server refuses WebP/GIF uploads that still carry metadata or blocks it
// cannot vouch for (api.md 5.14, server/src/lib/uploads.ts). What
// PrepareUpload sends must always pass that check, so the client's output for
// metadata-laden inputs is pinned here as a golden file that the server's
// vitest suite (server/test/unit/uploads-metadata.test.ts) runs through the
// real imageMetadataCheck: "in" must be refused, "out" accepted.
//
// When the stripping changes on purpose, regenerate it with
//
//	MASIL_UPDATE_FIXTURES=1 go test ./internal/imgrender -run TestStrippedUploadsServerFixture
//
// and run the server suite.
var serverFixture = filepath.Join("..", "..", "server", "test", "unit", "client-stripped.json")

type strippedPair struct {
	In  string `json:"in"`
	Out string `json:"out"`
}

func strippedFixtureInputs(t *testing.T) map[string][]byte {
	t.Helper()
	ins := map[string][]byte{}

	// VP8X ICCP ALPH VP8 EXIF XMP
	ins["webp-still-alpha.webp"] = readFixture(t, "still-alpha-meta.webp")

	// VP8X ICCP ANIM ANMF ANMF EXIF XMP, plus an EXIF hidden in the second frame.
	src := parseRIFF(t, readFixture(t, "anim-meta.webp"))
	var frames []riffChunk
	for _, c := range src {
		if c.id == "ANMF" {
			frames = append(frames, c)
		}
	}
	f2 := append(append([]byte{}, frames[1].data...), buildWebP(riffChunk{"EXIF", []byte("GPS SECRET")})[12:]...)
	ins["webp-animated.webp"] = buildWebP(src[0], src[1], src[2], frames[0], riffChunk{"ANMF", f2}, src[5], src[6])

	// A simple-format VP8L followed by EXIF and XMP chunks.
	simple := parseRIFF(t, readFixture(t, "simple-lossless.webp"))
	ins["webp-simple.webp"] = buildWebP(simple[0],
		riffChunk{"EXIF", []byte("Exif\x00\x00MM GPS 37.5665")}, riffChunk{"XMP ", []byte("<x:xmpmeta>GPS</x:xmpmeta>")})

	// A looping two-frame GIF with comments, XMP / ICC / unknown extensions,
	// plain text, a loop extension and a GCE each carrying an extra sub-block.
	clean := twoFrameGIF(t)
	h := gifHeaderLen(clean)
	netscape := ext(0xFF, subBlocks("NETSCAPE2.0", "\x01\x00\x00", "SECRET"))
	gin := insertAt(clean, len(clean)-1, gifComment, gifUnknown)
	if at := bytes.Index(gin, []byte{0x21, 0xF9, 0x04}); at >= 0 {
		gin = insertAt(gin, at+7, []byte("\x06SECRET"))
	} else {
		t.Fatal("GIF fixture has no GCE")
	}
	ins["gif-animated.gif"] = insertAt(gin, h, gifXMP, gifICC, gifComment, gifGCEAlone, gifPlain, netscape)
	return ins
}

func TestStrippedUploadsServerFixture(t *testing.T) {
	want := map[string]strippedPair{}
	for name, in := range strippedFixtureInputs(t) {
		out, _ := prepare(t, name, in)
		assertNoLeaks(t, out)
		if bytes.Equal(in, out) {
			t.Fatalf("%s: fixture input carries no metadata to strip", name)
		}
		want[name] = strippedPair{base64.StdEncoding.EncodeToString(in), base64.StdEncoding.EncodeToString(out)}
	}
	b, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')

	if os.Getenv("MASIL_UPDATE_FIXTURES") == "1" {
		if err := os.WriteFile(serverFixture, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(serverFixture)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with MASIL_UPDATE_FIXTURES=1)", serverFixture, err)
	}
	if !bytes.Equal(got, b) {
		t.Fatalf("%s is stale: PrepareUpload's output changed; regenerate with MASIL_UPDATE_FIXTURES=1 and rerun the server suite", serverFixture)
	}
}
