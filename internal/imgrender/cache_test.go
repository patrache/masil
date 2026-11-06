package imgrender_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/imgrender"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func testPaths(t *testing.T) config.Paths {
	t.Helper()
	d := t.TempDir()
	return config.Paths{Dir: filepath.Join(d, "config"), Cache: filepath.Join(d, "cache")}
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return b.Bytes()
}

func media(id string) api.Media {
	return api.Media{ID: id, URL: "/v1/media/" + id, ContentType: "image/png"}
}

// mediaServer serves data with an ETag and answers a matching If-None-Match with
// 304. It counts requests and records the last If-None-Match.
type mediaServer struct {
	hits    atomic.Int32
	lastINM atomic.Value
}

func (ms *mediaServer) handler(data []byte, etag string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ms.hits.Add(1)
		ms.lastINM.Store(r.Header.Get("If-None-Match"))
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
	}
}

func sameSize(t *testing.T, img image.Image, want image.Point) {
	t.Helper()
	if img == nil {
		t.Fatalf("image is nil")
	}
	if got := img.Bounds().Size(); got != want {
		t.Fatalf("image size = %v, want %v", got, want)
	}
}

func TestCacheLoadFetchesOnceThenMemory(t *testing.T) {
	srv := apitest.New(t)
	ms := &mediaServer{}
	srv.Handle("GET /v1/media/{id}", ms.handler(pngBytes(t, fixtures.TestImage()), `"e1"`))
	c := imgrender.NewCache(testPaths(t))
	a := srv.Client()
	ctx := context.Background()

	for range 2 {
		img, err := c.Load(ctx, a, media("up_1"))
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		sameSize(t, img, image.Pt(64, 48))
	}
	if n := ms.hits.Load(); n != 1 {
		t.Fatalf("requests = %d, want 1 (second Load from memory)", n)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Method != "GET" || reqs[0].Path != "/v1/media/up_1" {
		t.Fatalf("requests = %+v", reqs)
	}
}

func TestCacheUsesETagAcrossInstances(t *testing.T) {
	srv := apitest.New(t)
	ms := &mediaServer{}
	srv.Handle("GET /v1/media/{id}", ms.handler(pngBytes(t, fixtures.TestImage()), `"e1"`))
	p := testPaths(t)
	a := srv.Client()
	ctx := context.Background()

	if _, err := imgrender.NewCache(p).Load(ctx, a, media("up_1")); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if inm := ms.lastINM.Load(); inm != "" {
		t.Fatalf("first request If-None-Match = %q, want none", inm)
	}
	data := filepath.Join(p.Cache, "media", "up_1")
	if fi, err := os.Stat(data); err != nil || fi.Size() == 0 {
		t.Fatalf("disk cache %s: %v", data, err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Fatalf("disk cache mode = %v, want 0600", fi.Mode().Perm())
	}
	if b, err := os.ReadFile(data + ".etag"); err != nil || string(b) != `"e1"` {
		t.Fatalf("etag file = %q, %v", b, err)
	}

	img, err := imgrender.NewCache(p).Load(ctx, a, media("up_1"))
	if err != nil {
		t.Fatalf("second instance Load: %v", err)
	}
	if inm := ms.lastINM.Load(); inm != `"e1"` {
		t.Fatalf("second request If-None-Match = %q, want the stored etag", inm)
	}
	if n := ms.hits.Load(); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
	sameSize(t, img, image.Pt(64, 48))
}

func TestCacheRefetchesWhenDiskCopyIsBad(t *testing.T) {
	srv := apitest.New(t)
	ms := &mediaServer{}
	srv.Handle("GET /v1/media/{id}", ms.handler(pngBytes(t, fixtures.TestImage()), `"e1"`))
	p := testPaths(t)
	a := srv.Client()
	ctx := context.Background()
	if _, err := imgrender.NewCache(p).Load(ctx, a, media("up_1")); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	// Corrupt the cached bytes but keep the etag: a 304 must not be trusted.
	if err := os.WriteFile(filepath.Join(p.Cache, "media", "up_1"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	img, err := imgrender.NewCache(p).Load(ctx, a, media("up_1"))
	if err != nil {
		t.Fatalf("Load after corruption: %v", err)
	}
	sameSize(t, img, image.Pt(64, 48))
	if inm := ms.lastINM.Load(); inm != "" {
		t.Fatalf("refetch If-None-Match = %q, want none (disk copy unusable)", inm)
	}
}

func TestCacheEvictsOverLimit(t *testing.T) {
	srv := apitest.New(t)
	sizes := map[string][]byte{}
	for i, id := range []string{"up_a", "up_b", "up_c"} {
		img := image.NewRGBA(image.Rect(0, 0, 16, 16))
		rng := rand.New(rand.NewPCG(uint64(i), 7))
		for p := range img.Pix {
			img.Pix[p] = byte(rng.UintN(256)) // noise: ~1KB even as PNG
		}
		sizes[id] = pngBytes(t, img)
		if n := len(sizes[id]); n < 700 || n > 1400 {
			t.Fatalf("fixture %s is %d bytes, want ~1KB", id, n)
		}
	}
	srv.Handle("GET /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(sizes[r.PathValue("id")])
	})
	p := testPaths(t)
	c := imgrender.NewCache(p)
	imgrender.SetCacheLimit(c, 1500)
	a := srv.Client()
	for _, id := range []string{"up_a", "up_b", "up_c"} {
		if _, err := c.Load(context.Background(), a, media(id)); err != nil {
			t.Fatalf("Load %s: %v", id, err)
		}
	}
	dir := filepath.Join(p.Cache, "media")
	if _, err := os.Stat(filepath.Join(dir, "up_a")); !os.IsNotExist(err) {
		t.Fatalf("oldest up_a still on disk (err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "up_c")); err != nil {
		t.Fatalf("newest up_c missing: %v", err)
	}
	var total int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		fi, _ := e.Info()
		total += fi.Size()
	}
	if total > 1500 {
		t.Fatalf("disk cache holds %d bytes, want <= 1500", total)
	}
}

func TestCacheLoadNotFound(t *testing.T) {
	srv := apitest.New(t)
	srv.Error("GET /v1/media/{id}", http.StatusNotFound, "not_found")
	p := testPaths(t)
	_, err := imgrender.NewCache(p).Load(context.Background(), srv.Client(), media("up_x"))
	if !api.IsCode(err, "not_found") {
		t.Fatalf("err = %v, want not_found", err)
	}
	if _, err := os.Stat(filepath.Join(p.Cache, "media", "up_x")); !os.IsNotExist(err) {
		t.Fatalf("404 left a cache file (err %v)", err)
	}
}

// TestCacheLoadRefusesDotSegmentMedia (pending-fixes 5): a server-supplied
// media id or URL that a URL normaliser would rewrite ("..", "%2e%2e", "//")
// is refused by the api client before any request, so it cannot aim the
// bearer-token fetch at another endpoint.
func TestCacheLoadRefusesDotSegmentMedia(t *testing.T) {
	srv := apitest.New(t)
	for _, m := range []api.Media{
		{ID: "..", ContentType: "image/png"},
		{ID: ".", ContentType: "image/png"},
		{ID: "up_1", URL: "/v1/media/../auth/sessions", ContentType: "image/png"},
		{ID: "up_2", URL: "/v1/media/%2e%2e/auth/sessions", ContentType: "image/png"},
		{ID: "up_3", URL: "/v1//media/up_3", ContentType: "image/png"},
		// One encoded-dot media segment: only checkPath's decoding stops it
		// (WHATWG would turn "/v1/media/%2e%2e" into "/v1/").
		{ID: "up_4", URL: "/v1/media/%2e%2e", ContentType: "image/png"},
		{ID: "up_5", URL: "/v1/media/.%2E", ContentType: "image/png"},
		{ID: "up_6", URL: "/v1/media/%2E", ContentType: "image/png"},
		{ID: "up_7", URL: "/v1/media/%2e.", ContentType: "image/png"},
		{ID: "up_8", URL: srv.URL + "/v1/media/%2e%2e", ContentType: "image/png"},
	} {
		_, err := imgrender.NewCache(testPaths(t)).Load(context.Background(), srv.Client(), m)
		if !api.IsCode(err, "invalid_request") {
			t.Errorf("Load(%+v) err = %v, want invalid_request", m, err)
		}
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("requests = %d, want 0: %+v", n, srv.Requests())
	}
}

func TestCacheRejectsUndecodable(t *testing.T) {
	srv := apitest.New(t)
	srv.Handle("GET /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("not a png"))
	})
	p := testPaths(t)
	if _, err := imgrender.NewCache(p).Load(context.Background(), srv.Client(), media("up_bad")); err == nil {
		t.Fatalf("want a decode error")
	}
	if _, err := os.Stat(filepath.Join(p.Cache, "media", "up_bad")); !os.IsNotExist(err) {
		t.Fatalf("undecodable bytes were cached (err %v)", err)
	}
}

func TestCacheUnsafeIDSkipsDisk(t *testing.T) {
	srv := apitest.New(t)
	srv.Handle("GET /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes(t, fixtures.TestImage()))
	})
	p := testPaths(t)
	m := api.Media{ID: "../evil", URL: "/v1/media/evil"}
	if _, err := imgrender.NewCache(p).Load(context.Background(), srv.Client(), m); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Cache, "evil")); !os.IsNotExist(err) {
		t.Fatalf("unsafe id escaped the media dir (err %v)", err)
	}
}

func TestNilCacheAndNilClient(t *testing.T) {
	var c *imgrender.Cache
	if _, err := c.Load(context.Background(), nil, media("up_1")); err == nil {
		t.Fatalf("nil cache/client: want an error")
	}
}

func TestCacheLoadBoundsHugeImages(t *testing.T) {
	// A solid 7000x3000 image compresses to a few KB but would decode to a
	// full-size buffer; Load must keep only a display-sized copy.
	big := image.NewGray(image.Rect(0, 0, 7000, 3000))
	for i := range big.Pix {
		big.Pix[i] = 0x80
	}
	srv := apitest.New(t)
	ms := &mediaServer{}
	srv.Handle("GET /v1/media/{id}", ms.handler(pngBytes(t, big), `"e1"`))
	p := testPaths(t)
	a := srv.Client()
	ctx := context.Background()
	side := imgrender.MaxDisplaySide
	want := image.Pt(side, (3000*side+3500)/7000)

	img, err := imgrender.NewCache(p).Load(ctx, a, media("up_big"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sameSize(t, img, want)
	if c := color.GrayModel.Convert(img.At(side/2, want.Y/2)).(color.Gray); c.Y < 0x7e || c.Y > 0x82 {
		t.Fatalf("downscaled pixel = %v, want ~0x80", c)
	}

	// The 304 path (disk copy of another instance) is bounded too.
	img2, err := imgrender.NewCache(p).Load(ctx, a, media("up_big"))
	if err != nil {
		t.Fatalf("second instance Load: %v", err)
	}
	if n := ms.hits.Load(); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
	sameSize(t, img2, want)
}

func TestCacheMemoryBoundedByBytes(t *testing.T) {
	srv := apitest.New(t)
	ms := &mediaServer{}
	data := pngBytes(t, fixtures.TestImage())
	srv.Handle("GET /v1/media/{id}", ms.handler(data, `"e1"`))
	dec, err := imgrender.Decode(data, "image/png")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	one := imgrender.ImageBytes(dec)
	c := imgrender.NewCache(config.Paths{}) // memory only
	imgrender.SetMemLimit(c, 2*one)         // room for two, not three
	a := srv.Client()
	ctx := context.Background()
	load := func(id string, wantHits int32) {
		t.Helper()
		if _, err := c.Load(ctx, a, media(id)); err != nil {
			t.Fatalf("Load %s: %v", id, err)
		}
		if n := ms.hits.Load(); n != wantHits {
			t.Fatalf("after Load %s: requests = %d, want %d", id, n, wantHits)
		}
	}
	load("up_1", 1)
	load("up_2", 2)
	load("up_1", 2) // memory; up_2 is now the oldest
	load("up_3", 3) // evicts up_2 over the byte budget
	load("up_1", 3)
	load("up_3", 3)
	load("up_2", 4)

	// One image over the whole budget is still kept.
	imgrender.SetMemLimit(c, 1)
	load("up_4", 5)
	load("up_4", 5)
}

// Remote media that would decode to hundreds of MB (16-bit, large) is refused
// before decoding, with ErrTooLarge.
func TestCacheLoadRejects16BitBomb(t *testing.T) {
	px := image.NewRGBA64(image.Rect(0, 0, 1, 1))
	px.SetRGBA64(0, 0, color.RGBA64{1, 2, 3, 0x8000})
	p := pngBytes(t, px)
	binary.BigEndian.PutUint32(p[16:], 7000)
	binary.BigEndian.PutUint32(p[20:], 7000)
	binary.BigEndian.PutUint32(p[29:], crc32.ChecksumIEEE(p[12:29]))
	srv := apitest.New(t)
	ms := &mediaServer{}
	srv.Handle("GET /v1/media/{id}", ms.handler(p, `"e1"`))
	_, err := imgrender.NewCache(testPaths(t)).Load(context.Background(), srv.Client(), media("up_bomb"))
	if !errors.Is(err, imgrender.ErrTooLarge) {
		t.Fatalf("Load err = %v, want ErrTooLarge", err)
	}
}

// Concurrent loads (one per card near the screen) decode at most MaxDecodes
// images at a time.
func TestCacheLoadLimitsConcurrentDecodes(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 700, 700))
	rng := rand.New(rand.NewPCG(3, 4))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.UintN(256))
	}
	data := pngBytes(t, img)
	srv := apitest.New(t)
	ms := &mediaServer{}
	srv.Handle("GET /v1/media/{id}", ms.handler(data, `"e1"`))
	c := imgrender.NewCache(config.Paths{})
	a := srv.Client()
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Load(context.Background(), a, media(fmt.Sprintf("up_c%d", i))); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Load: %v", err)
	}
	if p := imgrender.DecodePeak(c); p < 1 || p > imgrender.MaxDecodes {
		t.Fatalf("peak concurrent decodes = %d, want 1..%d", p, imgrender.MaxDecodes)
	}
}
