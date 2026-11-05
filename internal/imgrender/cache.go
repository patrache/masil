package imgrender

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/config"
)

const (
	defaultDiskLimit = 200 << 20 // bytes of <Cache>/media
	memEntries       = 32        // decoded images kept in memory
	memBytes         = 256 << 20 // and their pixel bytes (estimated)
	etagSuffix       = ".etag"
	// maxDisplaySide caps the long side of images Load keeps (cards and
	// the full-screen viewer never need more).
	maxDisplaySide = 2048
)

// Cache loads media through the API with a disk cache
// (<Paths.Cache>/media/<upload_id> + <upload_id>.etag, 200MB LRU by
// modification time, revalidated with If-None-Match) and a memory LRU of the
// last 32 decoded images (at most 256MB of pixels). Images are kept at most
// 2048 pixels on the long side: remote images have no dimension limit (only
// the 5MB upload limit, which a large solid image passes easily), so a
// full-size decode would cost hundreds of MB per image. A Cache is safe for
// concurrent use.
type Cache struct {
	dir string

	mu    sync.Mutex
	limit int64
	// memLimit bounds the memory LRU's pixel bytes (memBytes).
	memLimit int64
	lru      *list.List // front = most recent; values *memEntry
	idx      map[string]*list.Element
	bytes    int64 // sum of memEntry.size
	// diskMu serializes disk writes and eviction.
	diskMu sync.Mutex
}

type memEntry struct {
	id   string
	img  image.Image
	size int64
}

// NewCache returns a cache under p.Cache/media. The directory is created on
// the first write.
func NewCache(p config.Paths) *Cache {
	dir := ""
	if p.Cache != "" {
		dir = filepath.Join(p.Cache, "media")
	}
	return &Cache{dir: dir, limit: defaultDiskLimit, memLimit: memBytes, lru: list.New(), idx: map[string]*list.Element{}}
}

func (c *Cache) setMemLimit(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memLimit = n
}

func (c *Cache) setLimit(n int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = n
}

// Load returns m decoded: from memory, else fetched with FetchMedia (sending
// the stored ETag as If-None-Match; a 304 is served from disk). Fresh bytes
// are decoded before they are written to disk, so undecodable responses are
// never cached. API errors are returned as they are (api.IsCode works).
func (c *Cache) Load(ctx context.Context, a *api.Client, m api.Media) (image.Image, error) {
	if c == nil || a == nil {
		return nil, errors.New("imgrender: cache load without cache or client")
	}
	if img := c.memGet(m.ID); img != nil {
		return img, nil
	}

	rel := m.URL
	if rel == "" {
		if m.ID == "" {
			return nil, errors.New("imgrender: media has neither id nor url")
		}
		rel = "/v1/media/" + url.PathEscape(m.ID)
	}
	data := c.diskPath(m.ID)
	cached, etag := c.diskGet(data)

	blob, err := a.FetchMedia(ctx, a.MediaURL(rel), etag)
	if err != nil {
		return nil, err
	}
	if blob.NotModified {
		if cached == nil {
			return nil, fmt.Errorf("imgrender: media %s: 304 without a cached copy", m.ID)
		}
		now := time.Now()
		_ = os.Chtimes(data, now, now) // LRU touch; best effort
		cached = displayImage(cached)
		c.memPut(m.ID, cached)
		return cached, nil
	}

	ct := blob.ContentType
	if ct == "" {
		ct = m.ContentType
	}
	img, err := Decode(blob.Data, ct)
	if err != nil {
		return nil, fmt.Errorf("imgrender: media %s: %w", m.ID, err)
	}
	if data != "" {
		c.store(data, blob.Data, blob.ETag) // best effort: a full disk still shows the image
	}
	img = displayImage(img)
	c.memPut(m.ID, img)
	return img, nil
}

// diskGet returns the decoded disk copy at path and its ETag, or nil and ""
// when there is no usable pair (a 304 is only trustworthy if the disk copy
// decodes, so a damaged copy is refetched without If-None-Match).
func (c *Cache) diskGet(path string) (image.Image, string) {
	if path == "" {
		return nil, ""
	}
	e, err := os.ReadFile(path + etagSuffix)
	if err != nil || strings.TrimSpace(string(e)) == "" {
		return nil, ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, ""
	}
	img, err := Decode(b, "")
	if err != nil {
		return nil, ""
	}
	return img, strings.TrimSpace(string(e))
}

// diskPath is <dir>/<id>, or "" when there is no dir or id is not a plain
// file name (such media are cached in memory only).
func (c *Cache) diskPath(id string) string {
	if c.dir == "" || id == "" || strings.HasPrefix(id, ".") ||
		strings.ContainsAny(id, `/\:`) || strings.HasSuffix(id, etagSuffix) || filepath.Base(id) != id {
		return ""
	}
	return filepath.Join(c.dir, id)
}

func (c *Cache) store(path string, data []byte, etag string) {
	c.diskMu.Lock()
	defer c.diskMu.Unlock()
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return
	}
	if err := config.WriteFileAtomic(path, data, 0o600); err != nil {
		return
	}
	if etag != "" {
		_ = config.WriteFileAtomic(path+etagSuffix, []byte(etag), 0o600)
	} else {
		_ = os.Remove(path + etagSuffix)
	}
	c.evictDisk(filepath.Base(path))
}

// evictDisk removes the least recently modified media (with their etag
// files) until the directory is within the limit. keep (the media just
// written) is never removed. Callers hold diskMu.
func (c *Cache) evictDisk(keep string) {
	c.mu.Lock()
	limit := c.limit
	c.mu.Unlock()

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type item struct {
		name  string
		size  int64
		mtime time.Time
	}
	var items []item
	var total int64
	sizes := map[string]int64{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		total += fi.Size()
		name := e.Name()
		if base, ok := strings.CutSuffix(name, etagSuffix); ok {
			sizes[base] += fi.Size()
			continue
		}
		if strings.HasPrefix(name, ".") { // temp files of an atomic write
			continue
		}
		sizes[name] += fi.Size()
		items = append(items, item{name: name, mtime: fi.ModTime()})
	}
	if total <= limit {
		return
	}
	for i := range items {
		items[i].size = sizes[items[i].name]
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].mtime.Before(items[j].mtime) })
	for _, it := range items {
		if total <= limit {
			break
		}
		if it.name == keep {
			continue
		}
		p := filepath.Join(c.dir, it.name)
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		_ = os.Remove(p + etagSuffix)
		total -= it.size
	}
}

func (c *Cache) memGet(id string) image.Image {
	if id == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.idx[id]
	if !ok {
		return nil
	}
	c.lru.MoveToFront(el)
	return el.Value.(*memEntry).img
}

func (c *Cache) memPut(id string, img image.Image) {
	if id == "" {
		return
	}
	size := imageBytes(img)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.idx[id]; ok {
		e := el.Value.(*memEntry)
		c.bytes += size - e.size
		e.img, e.size = img, size
		c.lru.MoveToFront(el)
	} else {
		c.idx[id] = c.lru.PushFront(&memEntry{id: id, img: img, size: size})
		c.bytes += size
	}
	// The newest entry always stays, even if it alone is over budget.
	for c.lru.Len() > 1 && (c.lru.Len() > memEntries || c.bytes > c.memLimit) {
		el := c.lru.Back()
		e := el.Value.(*memEntry)
		c.lru.Remove(el)
		delete(c.idx, e.id)
		c.bytes -= e.size
	}
}

// displayImage returns img, or a copy scaled (CatmullRom, alpha kept) so its
// long side is maxDisplaySide when it is longer. The copy is a compact
// 8-bit RGBA whatever the source's pixel format.
func displayImage(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	long := max(w, h)
	if long <= maxDisplaySide {
		return img
	}
	dw := max(1, (w*maxDisplaySide+long/2)/long)
	dh := max(1, (h*maxDisplaySide+long/2)/long)
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

// imageBytes estimates the memory held by img's pixels.
func imageBytes(img image.Image) int64 {
	switch p := img.(type) {
	case *image.RGBA:
		return int64(len(p.Pix))
	case *image.NRGBA:
		return int64(len(p.Pix))
	case *image.RGBA64:
		return int64(len(p.Pix))
	case *image.NRGBA64:
		return int64(len(p.Pix))
	case *image.Gray:
		return int64(len(p.Pix))
	case *image.Gray16:
		return int64(len(p.Pix))
	case *image.Paletted:
		return int64(len(p.Pix))
	case *image.YCbCr:
		return int64(len(p.Y) + len(p.Cb) + len(p.Cr))
	case *image.NYCbCrA:
		return int64(len(p.Y) + len(p.Cb) + len(p.Cr) + len(p.A))
	case *image.CMYK:
		return int64(len(p.Pix))
	}
	s := img.Bounds().Size()
	return int64(s.X) * int64(s.Y) * 4
}
