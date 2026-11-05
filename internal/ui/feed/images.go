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
