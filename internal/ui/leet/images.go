package leet

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/imgrender"
	"github.com/patrache/masil/internal/leetcode/htmldoc"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/ui"
)

// Problem images (A10.2): only images on https://assets.leetcode.com/
// (htmldoc keeps no other URL) are fetched — by this device, with no cookie
// and a size cap — and drawn with masil's image renderer in half blocks. An
// image from anywhere else (no IP leaks to an arbitrary host), one still
// loading or failed, and every image with graphics off stay the
// "[그림] o 열기" link line.

const (
	imageHost     = "assets.leetcode.com"
	maxImageBytes = 4 << 20
	imageTimeout  = 20 * time.Second
	maxImageCols  = 64
	maxImageRows  = 20
	maxImages     = 32 // decoded images kept in this run
)

// imageLoadedMsg is one fetched image (err: it stays a link).
type imageLoadedMsg struct {
	url string
	err error
}

var (
	imageMu sync.Mutex
	images  = map[string]image.Image{} // url -> decoded (this run)
	asked   = map[string]bool{}        // urls fetched or being fetched (this run)
	// imageTransport is the HTTP transport of image fetches (nil = the
	// default one; export_test.go swaps it).
	imageTransport http.RoundTripper
)

// imageOf is url's decoded image (nil: not here).
func imageOf(u string) image.Image {
	imageMu.Lock()
	defer imageMu.Unlock()
	return images[u]
}

// allowedImage: an https URL on assets.leetcode.com (checked again here, not
// only in htmldoc).
func allowedImage(u string) bool {
	p, err := url.Parse(u)
	return err == nil && p.Scheme == "https" && p.Host == imageHost && p.User == nil
}

// requestImages fetches the body's images not asked for yet in this run
// (graphics off: none).
func requestImages(ctx *ui.Context, blocks []htmldoc.Block) tea.Cmd {
	if ctx.Caps.Graphics == termcaps.GraphicsNone {
		return nil
	}
	var cmds []tea.Cmd
	imageMu.Lock()
	tr := imageTransport
	for _, b := range blocks {
		if b.Kind != htmldoc.Image || b.URL == "" || asked[b.URL] || !allowedImage(b.URL) {
			continue
		}
		asked[b.URL] = true
		u := b.URL
		cmds = append(cmds, func() tea.Msg {
			img, err := fetchImage(context.Background(), tr, u)
			if err == nil {
				keepImage(u, img)
			}
			return imageLoadedMsg{url: u, err: err}
		})
	}
	imageMu.Unlock()
	return tea.Batch(cmds...)
}

// keepImage remembers a decoded image (at most maxImages: the oldest
// arrivals are not tracked, so any one goes when the map is full).
func keepImage(u string, img image.Image) {
	imageMu.Lock()
	defer imageMu.Unlock()
	if len(images) >= maxImages {
		for k := range images {
			delete(images, k)
			delete(asked, k)
			break
		}
	}
	images[u] = img
}

var errImage = errors.New("leet: image not fetched")

// fetchImage GETs u (no redirect off assets.leetcode.com, at most
// maxImageBytes) and decodes it.
func fetchImage(ctx context.Context, tr http.RoundTripper, u string) (image.Image, error) {
	c := &http.Client{Transport: tr, Timeout: imageTimeout,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !allowedImage(r.URL.String()) {
				return errImage
			}
			return nil
		}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", errImage, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("%w: over %d bytes", errImage, maxImageBytes)
	}
	return imgrender.Decode(data, resp.Header.Get("Content-Type"))
}

// imageBox is the cell box img is drawn in at width w: at most
// maxImageCols×maxImageRows, the aspect kept (a cell is two pixels tall),
// never scaled up.
func imageBox(img image.Image, w int) (cols, rows int) {
	b := img.Bounds()
	iw, ih := b.Dx(), b.Dy()
	if iw <= 0 || ih <= 0 || w <= 0 {
		return 0, 0
	}
	cols = min(w, maxImageCols, iw)
	rows = (cols*ih + iw - 1) / iw / 2
	if rows > maxImageRows {
		rows = maxImageRows
		cols = min(cols, max(1, rows*2*iw/ih))
	}
	return cols, max(rows, 1)
}
