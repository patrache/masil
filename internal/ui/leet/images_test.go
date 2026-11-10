package leet_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
)

// pngOf is a w×h PNG of one color.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// serveImages answers image requests with body (and records the URLs).
func serveImages(t *testing.T, body []byte) *[]string {
	t.Helper()
	var asked []string
	t.Cleanup(leet.SetImageTransportForTest(func(r *http.Request) (*http.Response, error) {
		asked = append(asked, r.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}},
			Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	}))
	return &asked
}

// withBody puts today's problem with html as its body in h's cache.
func withBody(t *testing.T, h *uitest.Harness, html string) {
	t.Helper()
	q := q1606()
	q.ContentHTML = html
	q.Snippets = map[string]string{"golang": "func f() {}"}
	if err := leetcode.OpenCache(h.Context().Paths.Cache).PutDaily("2026-11-05", q); err != nil {
		t.Fatal(err)
	}
}

// A10.2: an https://assets.leetcode.com/ image is drawn with masil's image
// renderer; an image from anywhere else is never fetched and stays a link.
func TestProblemImagesDrawn(t *testing.T) {
	setup(t)
	asked := serveImages(t, pngOf(t, 40, 20))
	h := uitest.New(t)
	withBody(t, h, `<p>Graph:</p><img src="https://assets.leetcode.com/uploads/g.png" alt="graph"><p>Other:</p><img src="https://evil.example/x.png" alt="other">`)
	openToday(t, h)
	s := h.Screen()
	if !strings.Contains(s, "▀") {
		t.Fatalf("the assets.leetcode.com image is not drawn:\n%s", s)
	}
	if strings.Count(s, "[그림] o 열기") != 1 {
		t.Fatalf("want one link line (the other host):\n%s", s)
	}
	if len(*asked) != 1 || (*asked)[0] != "https://assets.leetcode.com/uploads/g.png" {
		t.Fatalf("fetched %q", *asked)
	}
	h.Check()
}

// An image that cannot be fetched stays the link line (and is not asked for
// again on every draw).
func TestProblemImageFailedStaysLink(t *testing.T) {
	setup(t)
	n := 0
	t.Cleanup(leet.SetImageTransportForTest(func(r *http.Request) (*http.Response, error) {
		n++
		return nil, errors.New("offline")
	}))
	h := uitest.New(t)
	withBody(t, h, `<img src="https://assets.leetcode.com/uploads/g.png" alt="graph">`)
	openToday(t, h)
	h.Press("j", "k")
	if s := h.Screen(); !strings.Contains(s, "[그림] o 열기") || strings.Contains(s, "▀") {
		t.Fatalf("failed image:\n%s", s)
	}
	if n != 1 {
		t.Fatalf("image asked %d times", n)
	}
}

// A tall and a wide image keep the frame whole at every size.
func TestProblemImagesIntegrityAllSizes(t *testing.T) {
	setup(t)
	tall, wide := pngOf(t, 10, 400), pngOf(t, 900, 30)
	t.Cleanup(leet.SetImageTransportForTest(func(r *http.Request) (*http.Response, error) {
		body := tall
		if strings.HasSuffix(r.URL.Path, "wide.png") {
			body = wide
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}},
			Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	}))
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
		withBody(t, h, `<ul><li><img src="https://assets.leetcode.com/uploads/tall.png" alt="tall"></li></ul><img src="https://assets.leetcode.com/uploads/wide.png">`)
		openToday(t, h)
		h.Check()
		h.Press("pgdown")
		h.Check()
	})
}
