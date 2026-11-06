package imgrender

import (
	"bytes"
	"container/list"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/iterm2"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/patrache/masil/internal/termcaps"
)

// Options is the target cell box of a render.
type Options struct {
	Cols, Rows int         // target cell box
	BG         color.Color // half-block background (theme bg)
	Fit        FitMode     // FitContain (default) | FitCover
}

// Rendered is one image drawn into cells.
type Rendered struct {
	// Lines holds exactly Rows lines, each exactly Cols cells wide
	// (kitty placeholders, half blocks or spaces).
	Lines []string
	// Setup is an escape sequence to emit once via tea.Raw before the frame
	// that shows Lines (kitty transmit, preceded by deletes of evicted
	// images); "" otherwise.
	Setup string
	// ID is the kitty image id (0 when not kitty).
	ID uint32
}

const (
	defaultRenderLimit = 64
	// maxKittyID keeps ids within 24 bits so the id fits the placeholder's
	// truecolor foreground (no third diacritic needed).
	maxKittyID = 1<<24 - 1
	// kittyCellPx is the assumed pixel width of a cell (height is twice
	// that) when sizing the transmitted image; the terminal scales it into
	// the placement anyway, so this only bounds sharpness and payload size.
	kittyCellPx = 10
	// maxKittyPixels caps the transmitted RGBA image (4 bytes per pixel).
	maxKittyPixels = 1_500_000
	noImageIcon    = "🖼"
)

type method int

const (
	methodHalfBlock method = iota
	methodKitty
	methodNone
)

type renderKey struct {
	key        string
	cols, rows int
	fit        FitMode
	bg         color.RGBA
	m          method
}

type renderEntry struct {
	k   renderKey
	out Rendered // Setup is always "" here
}

// Renderer draws images into cells with the method the terminal supports:
// kitty Unicode placeholders (kitty outside tmux), half blocks (everything
// else, including iTerm2 in lists and cards) or a blank box with 🖼
// (graphics off). It remembers the last 64 renders; for kitty each
// (key, Cols, Rows, Fit, BG) combination owns its own image id.
//
// A Renderer is safe for concurrent use.
type Renderer struct {
	mu      sync.Mutex
	caps    termcaps.Caps
	cache   *Cache
	limit   int
	lru     *list.List // front = most recent; values *renderEntry
	idx     map[renderKey]*list.Element
	live    map[uint32]bool
	nextID  uint32
	deletes []string // kitty deletes of evicted ids, sent with the next transmit
}

// New returns a Renderer for caps. c is the media cache the caller loads
// images through; it may be nil.
func New(caps termcaps.Caps, c *Cache) *Renderer {
	return &Renderer{
		caps:  caps,
		cache: c,
		limit: defaultRenderLimit,
		lru:   list.New(),
		idx:   map[renderKey]*list.Element{},
		live:  map[uint32]bool{},
	}
}

// Caps returns the terminal capabilities the Renderer was built with.
func (r *Renderer) Caps() termcaps.Caps { return r.caps }

func (r *Renderer) method() method {
	switch r.caps.Graphics {
	case termcaps.GraphicsKitty:
		if r.caps.InTmux {
			return methodHalfBlock
		}
		return methodKitty
	case termcaps.GraphicsNone:
		return methodNone
	default: // half block, and iTerm2 outside the full-screen viewer
		return methodHalfBlock
	}
}

func (r *Renderer) keyOf(key string, o Options) renderKey {
	return renderKey{key: key, cols: o.Cols, rows: o.Rows, fit: o.Fit, bg: opaque(o.BG), m: r.method()}
}

// Render draws img under key (normally the media id; the same key must
// always mean the same image) into o's box. With kitty, the first Render of a
// combination returns the transmit in Setup and later ones return Setup "".
// A non-positive box yields the zero Rendered; a nil img is drawn as an
// empty box and never transmitted.
func (r *Renderer) Render(key string, img image.Image, o Options) Rendered {
	if o.Cols <= 0 || o.Rows <= 0 {
		return Rendered{}
	}
	m := r.method()
	if img == nil {
		return Rendered{Lines: r.draw(methodOr(m, methodHalfBlock), nil, o)}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.keyOf(key, o)
	if el, ok := r.idx[k]; ok {
		r.lru.MoveToFront(el)
		return el.Value.(*renderEntry).out
	}

	out := Rendered{}
	setup := ""
	if m == methodKitty {
		id := r.allocID()
		tx, err := kittyTransmit(img, id, o)
		if err != nil {
			// Not cached: the next Render tries kitty again.
			return Rendered{Lines: halfBlockFit(img, o.Cols, o.Rows, o.Fit, o.BG)}
		}
		out.ID = id
		out.Lines = kittyLines(id, o.Cols, o.Rows)
		r.live[id] = true
		setup = tx
	} else {
		out.Lines = r.draw(m, img, o)
	}
	r.idx[k] = r.lru.PushFront(&renderEntry{k: k, out: out})
	r.evict()
	if setup != "" {
		out.Setup = strings.Join(r.deletes, "") + setup
		r.deletes = r.deletes[:0]
	}
	return out
}

// Peek is Render for View. With kitty it never changes the Renderer's state
// (no transmit is marked sent, no LRU update): a combination already sent by
// Render returns the same placeholder lines and ready=true; one not sent yet
// returns half-block lines of the same size and ready=false (the caller
// Renders it in its next Update and emits Setup). Other methods return what
// Render would, with ready=true, and remember it in the same LRU as Render:
// that touches no terminal state, and without it every View would rescale
// every visible image from its full-size source.
func (r *Renderer) Peek(key string, img image.Image, o Options) (out Rendered, ready bool) {
	if o.Cols <= 0 || o.Rows <= 0 {
		return Rendered{}, true
	}
	m := r.method()
	if img == nil {
		return Rendered{Lines: r.draw(methodOr(m, methodHalfBlock), nil, o)}, true
	}
	k := r.keyOf(key, o)
	r.mu.Lock()
	el, ok := r.idx[k]
	if ok {
		out = el.Value.(*renderEntry).out
		if m != methodKitty {
			r.lru.MoveToFront(el)
		}
	}
	r.mu.Unlock()
	if ok {
		return out, true
	}
	if m == methodKitty {
		return Rendered{Lines: halfBlockFit(img, o.Cols, o.Rows, o.Fit, o.BG)}, false
	}
	// Draw outside the lock (scaling is slow), then store unless a
	// concurrent Render or Peek got there first.
	out = Rendered{Lines: r.draw(m, img, o)}
	r.mu.Lock()
	defer r.mu.Unlock()
	if el, ok := r.idx[k]; ok {
		r.lru.MoveToFront(el)
		return el.Value.(*renderEntry).out, true
	}
	r.idx[k] = r.lru.PushFront(&renderEntry{k: k, out: out})
	r.evict()
	return out, true
}

// methodOr maps kitty to fallback (kitty needs an image to transmit).
func methodOr(m, fallback method) method {
	if m == methodKitty {
		return fallback
	}
	return m
}

func (r *Renderer) draw(m method, img image.Image, o Options) []string {
	if m == methodNone {
		return blankBox(o.Cols, o.Rows, o.BG)
	}
	return halfBlockFit(img, o.Cols, o.Rows, o.Fit, o.BG)
}

// evict drops least recently used entries over the limit, queueing a kitty
// delete (image data included) for each evicted kitty id. Callers hold mu.
func (r *Renderer) evict() {
	for r.lru.Len() > r.limit {
		el := r.lru.Back()
		e := el.Value.(*renderEntry)
		r.lru.Remove(el)
		delete(r.idx, e.k)
		if e.out.ID != 0 {
			delete(r.live, e.out.ID)
			r.deletes = append(r.deletes, kittyDelete(e.out.ID))
		}
	}
}

// allocID returns the next unused kitty id in 1..maxKittyID. Callers hold mu.
func (r *Renderer) allocID() uint32 {
	for {
		r.nextID++
		if r.nextID > maxKittyID {
			r.nextID = 1
		}
		if !r.live[r.nextID] {
			return r.nextID
		}
	}
}

func (r *Renderer) setLimit(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limit = max(n, 1)
}

// kittyDelete frees image id and its data in the terminal (a=d,d=I,i=id).
func kittyDelete(id uint32) string {
	o := &kitty.Options{Action: kitty.Delete, Delete: kitty.DeleteID, DeleteResources: true, ID: int(id), Quiet: 2}
	return ansi.KittyGraphics(nil, o.Options()...)
}

// kittyTransmit sends img (fitted to the box like the half-block renderer,
// letterboxed or cropped, over bg) as image id with a virtual placement of
// Cols × Rows cells for the Unicode placeholders to show.
func kittyTransmit(img image.Image, id uint32, o Options) (string, error) {
	k := kittyScale(img.Bounds().Size(), o.Cols, o.Rows)
	px := fitPx(img, o.Cols*k, o.Rows*2*k, o.Fit, o.BG)
	opts := &kitty.Options{
		Action:           kitty.TransmitAndPut,
		ID:               int(id),
		Format:           kitty.RGBA,
		Transmission:     kitty.Direct,
		ImageWidth:       px.Bounds().Dx(),
		ImageHeight:      px.Bounds().Dy(),
		Chunk:            true,
		VirtualPlacement: true,
		Columns:          o.Cols,
		Rows:             o.Rows,
		Quiet:            2,
	}
	var b strings.Builder
	if err := kitty.EncodeGraphics(&b, px, opts); err != nil {
		return "", fmt.Errorf("imgrender: kitty encode: %w", err)
	}
	return b.String(), nil
}

// kittyScale picks pixels per cell column (rows get twice that): at most
// kittyCellPx, no more than needed to show the source at full resolution,
// and within maxKittyPixels.
func kittyScale(src image.Point, cols, rows int) int {
	k := kittyCellPx
	for k > 1 && cols*k > src.X && rows*2*k > src.Y {
		k--
	}
	for k > 1 && cols*k*rows*2*k > maxKittyPixels {
		k--
	}
	return k
}

// kittyLines builds rows lines of cols placeholder cells. Each cell is the
// placeholder with its row and column diacritics; the foreground color
// carries the 24-bit image id.
func kittyLines(id uint32, cols, rows int) []string {
	st := lipgloss.NewStyle().Foreground(color.RGBA{byte(id >> 16), byte(id >> 8), byte(id), 0xff})
	lines := make([]string, rows)
	var sb strings.Builder
	for r := range rows {
		sb.Reset()
		for c := range cols {
			sb.WriteRune(kitty.Placeholder)
			sb.WriteRune(kitty.Diacritic(r))
			sb.WriteRune(kitty.Diacritic(c))
		}
		lines[r] = st.Render(sb.String())
	}
	return lines
}

// blankBox is the graphics-off image: a bg box with one 🖼 in the middle.
// The icon is U+1F5BC without VS16, one cell wide by grapheme, wcwidth and
// textwidth rules alike.
func blankBox(cols, rows int, bg color.Color) []string {
	st := lipgloss.NewStyle().Background(opaque(bg))
	iw := ansi.StringWidth(noImageIcon)
	lines := make([]string, rows)
	for r := range rows {
		s := strings.Repeat(" ", cols)
		if r == (rows-1)/2 && cols >= iw {
			left := (cols - iw) / 2
			s = strings.Repeat(" ", left) + noImageIcon + strings.Repeat(" ", cols-left-iw)
		}
		lines[r] = st.Render(s)
	}
	return lines
}

// ITerm2Inline returns the escape that draws img as an iTerm2 inline image
// over the cols × rows cells whose top-left cell is (x, y) (0-based): save
// cursor, move there, the File escape with a base64 PNG, restore cursor. It
// is for the full-screen viewer only; the cells must be left blank.
func ITerm2Inline(img image.Image, x, y, cols, rows int) (string, error) {
	if img == nil {
		return "", errors.New("imgrender: iterm2 inline: no image")
	}
	if cols <= 0 || rows <= 0 {
		return "", fmt.Errorf("imgrender: iterm2 inline: bad box %dx%d", cols, rows)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return "", fmt.Errorf("imgrender: iterm2 inline: encode png: %w", err)
	}
	n := b.Len()
	content := make([]byte, base64.StdEncoding.EncodedLen(n))
	base64.StdEncoding.Encode(content, b.Bytes())
	f := iterm2.File{
		Inline:  true,
		Width:   iterm2.Cells(cols),
		Height:  iterm2.Cells(rows),
		Size:    int64(n),
		Content: content,
	}
	return ansi.SaveCursor + ansi.CursorPosition(x+1, y+1) + ansi.ITerm2(f) + ansi.RestoreCursor, nil
}
