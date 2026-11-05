package feed

import (
	"context"
	"image"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/imgrender"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// mediaTagPrefix tags image loads: "feed.media:<media id>".
const mediaTagPrefix = "feed.media:"

// rendererKey identifies one shared Renderer: the cache directory and the
// terminal graphics method it draws for.
type rendererKey struct {
	cache    string
	graphics termcaps.Graphics
	tmux     bool
}

var (
	mediaMu        sync.Mutex
	mediaCaches    = map[string]*imgrender.Cache{}
	mediaRenderers = map[rendererKey]*imgrender.Renderer{}
)

// mediaCache is the media cache shared by every ui/feed component for ctx.Paths.Cache.
func mediaCache(ctx *ui.Context) *imgrender.Cache {
	mediaMu.Lock()
	defer mediaMu.Unlock()
	return mediaCacheLocked(ctx)
}

func mediaCacheLocked(ctx *ui.Context) *imgrender.Cache {
	c := mediaCaches[ctx.Paths.Cache]
	if c == nil {
		c = imgrender.NewCache(ctx.Paths)
		mediaCaches[ctx.Paths.Cache] = c
	}
	return c
}

// mediaRenderer is the Renderer shared by every ui/feed component, one per
// (ctx.Paths.Cache, ctx.Caps.Graphics, ctx.Caps.InTmux): kitty image ids and
// the "already transmitted" state are per Renderer, so every screen that shows
// the same image reuses one transmission.
func mediaRenderer(ctx *ui.Context) *imgrender.Renderer {
	mediaMu.Lock()
	defer mediaMu.Unlock()
	k := rendererKey{cache: ctx.Paths.Cache, graphics: ctx.Caps.Graphics, tmux: ctx.Caps.InTmux}
	r := mediaRenderers[k]
	if r == nil {
		r = imgrender.New(ctx.Caps, mediaCacheLocked(ctx))
		mediaRenderers[k] = r
	}
	return r
}

// loadImage loads m through the shared media cache off the UI goroutine; the
// result is ui.ResultMsg[image.Image] tagged "feed.media:<m.ID>". Update only.
func loadImage(ctx *ui.Context, m api.Media) tea.Cmd {
	cache := mediaCache(ctx)
	return ui.Do(ctx, mediaTagPrefix+m.ID, func(c context.Context, a *api.Client) (image.Image, error) {
		return cache.Load(c, a, m)
	})
}

// renderOpts is the Renderer box for cols x rows on the theme background.
func renderOpts(ctx *ui.Context, cols, rows int) imgrender.Options {
	t := theme.Get(theme.Default)
	if ctx != nil && ctx.Theme != nil {
		t = ctx.Theme
	}
	return imgrender.Options{Cols: cols, Rows: rows, BG: t.P.BG}
}

// want is a (media, box) combination View drew as half blocks because its
// kitty transmission has not been sent yet.
type want struct {
	m          api.Media
	cols, rows int
}

// imageSlots tracks one component's images: which media it requested, the
// decoded images that arrived, and the combinations View still needs sent.
// View never returns Cmds (CA 5.3), so every terminal escape (kitty Setup) is
// computed and returned from Update: on arrival, on mode/theme change, and by
// flush at the end of every Update for what View recorded in the want-list.
type imageSlots struct {
	requested map[string]bool
	imgs      map[string]image.Image
	wants     []want
}

// request returns loadImage for every media ID not requested yet (Update only).
func (s *imageSlots) request(ctx *ui.Context, ms ...api.Media) tea.Cmd {
	if s.requested == nil {
		s.requested = map[string]bool{}
	}
	var cmds []tea.Cmd
	for _, m := range ms {
		if m.ID == "" || s.requested[m.ID] {
			continue
		}
		s.requested[m.ID] = true
		cmds = append(cmds, loadImage(ctx, m))
	}
	return tea.Batch(cmds...)
}

// track is request for card lists: it loads every media of ms not requested
// yet and forgets (drops the decoded image of) every media not in ms, so a
// component only ever holds the images of the cards near the screen. A
// forgotten image that comes back near the screen is loaded again (from the
// shared Cache's memory or disk tier). Update / HandleKey / HandleMouse only.
func (s *imageSlots) track(ctx *ui.Context, ms ...api.Media) tea.Cmd {
	keep := make(map[string]bool, len(ms))
	for _, m := range ms {
		keep[m.ID] = true
	}
	for id := range s.requested {
		if !keep[id] {
			delete(s.requested, id)
		}
	}
	for id := range s.imgs {
		if !keep[id] {
			delete(s.imgs, id)
		}
	}
	return s.request(ctx, ms...)
}

// retained is how many decoded images s holds.
func (s *imageSlots) retained() int { return len(s.imgs) }

// Near-screen image window (track): cards within one view height of the screen
// and of the selection, plus imageMargin cards either side.
const (
	imageMargin   = 2
	fallbackViewW = 80 // before the first Resize
	fallbackViewH = 40
)

// viewSize is w x h, or the fallback size before the first Resize.
func viewSize(w, h int) (int, int) {
	if w <= 0 {
		w = fallbackViewW
	}
	if h <= 0 {
		h = fallbackViewH
	}
	return w, h
}

// nearItems marks the items of l on screen or close to it: from the scroll
// anchor down one view height (viewH rows), and one view height above and below
// the selection (a key press moves the selection; the List scrolls to it on the
// next View), each range widened by imageMargin items. height is an item's
// rendered line count.
func nearItems[T any](l *widgets.List[T], items []T, idOf func(T) string, viewH int, height func(T) int) []bool {
	near := make([]bool, len(items))
	if len(items) == 0 {
		return near
	}
	h := func(i int) int { return max(height(items[i]), 1) }
	mark := func(lo, hi int) {
		for i := max(lo-imageMargin, 0); i <= min(hi+imageMargin, len(items)-1); i++ {
			near[i] = true
		}
	}
	a := l.Anchor()
	top, sel := 0, -1
	selID := ""
	if it, ok := l.Selected(); ok {
		selID = idOf(it)
	}
	for i, it := range items {
		id := idOf(it)
		if id == a.ItemID {
			top = i
		}
		if selID != "" && id == selID {
			sel = i
		}
	}
	end := top
	for acc := -a.LineOffset; end < len(items)-1 && acc+h(end) < viewH; end++ {
		acc += h(end)
	}
	mark(top, end)
	if sel >= 0 {
		lo, hi := sel, sel
		for acc := 0; lo > 0 && acc < viewH; {
			lo--
			acc += h(lo)
		}
		for acc := h(sel); hi < len(items)-1 && acc < viewH; {
			hi++
			acc += h(hi)
		}
		mark(lo, hi)
	}
	return near
}

// image returns the loaded image of media id, or nil.
func (s *imageSlots) image(id string) image.Image { return s.imgs[id] }

// cell is the cardOpts.Image callback (View): nil until the image arrived, else
// Renderer.Peek. A kitty combination not transmitted yet is drawn as half blocks
// and remembered so that the next Update's flush sends it.
func (s *imageSlots) cell(ctx *ui.Context, m api.Media, cols, rows int) []string {
	img := s.imgs[m.ID]
	if img == nil || cols <= 0 || rows <= 0 {
		return nil
	}
	out, ready := mediaRenderer(ctx).Peek(m.ID, img, renderOpts(ctx, cols, rows))
	if !ready {
		w := want{m: m, cols: cols, rows: rows}
		dup := false
		for _, x := range s.wants {
			if x.m.ID == w.m.ID && x.cols == w.cols && x.rows == w.rows {
				dup = true
				break
			}
		}
		if !dup {
			s.wants = append(s.wants, w)
		}
	}
	return out.Lines
}

// render draws m's loaded image into cols x rows (Update only); a non-empty
// Setup (kitty transmit) is returned as tea.Raw, otherwise nil.
func (s *imageSlots) render(ctx *ui.Context, m api.Media, cols, rows int) tea.Cmd {
	img := s.imgs[m.ID]
	if img == nil || cols <= 0 || rows <= 0 {
		return nil
	}
	if r := mediaRenderer(ctx).Render(m.ID, img, renderOpts(ctx, cols, rows)); r.Setup != "" {
		return tea.Raw(r.Setup)
	}
	return nil
}

// hasWants reports whether View recorded combinations flush has to send.
func (s *imageSlots) hasWants() bool { return len(s.wants) > 0 }

// flush renders every want-list entry and clears it (Update only); the kitty
// Setups come back as one batched tea.Raw Cmd.
func (s *imageSlots) flush(ctx *ui.Context) tea.Cmd {
	if len(s.wants) == 0 {
		return nil
	}
	ws := s.wants
	s.wants = nil
	var cmds []tea.Cmd
	for _, w := range ws {
		cmds = append(cmds, s.render(ctx, w.m, w.cols, w.rows))
	}
	return tea.Batch(cmds...)
}

// loaded recognizes the ResultMsg[image.Image] of a media this component
// requested ("feed.media:<id>") and stores the image. ok is false for any other
// message and for failed loads (the placeholder stays; no retry).
func (s *imageSlots) loaded(msg tea.Msg) (id string, img image.Image, ok bool) {
	r, isRes := msg.(ui.ResultMsg[image.Image])
	if !isRes || !strings.HasPrefix(r.Tag, mediaTagPrefix) {
		return "", nil, false
	}
	id = strings.TrimPrefix(r.Tag, mediaTagPrefix)
	if !s.requested[id] || r.Err != nil || r.Value == nil {
		return id, nil, false
	}
	if s.imgs == nil {
		s.imgs = map[string]image.Image{}
	}
	s.imgs[id] = r.Value
	return id, r.Value, true
}
