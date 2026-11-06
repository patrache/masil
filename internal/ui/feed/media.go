package feed

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	xdraw "golang.org/x/image/draw"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/imgrender"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// feed.media: the full-screen photo viewer (args "<postID> <media idx>").
//
// The overlay fills the body; the photo fills the overlay's inner box. Kitty
// (outside tmux) draws Unicode placeholders with the transmit sent as tea.Raw;
// iTerm2 (outside tmux) leaves the box blank and draws an inline image over it
// with tea.Raw (on failure: half blocks from then on); everything else draws
// half blocks.
//
// View cannot return Cmds and core does not promise an Update after a Resize,
// so every escape leaves from Update: the photo's arrival, ←/→, mode and theme
// changes, and the flush at the end of every Update for a box View drew as
// half blocks because it had not been sent yet (a same-mode resize). The box is
// a pure function of ctx.Layout (the app's overlay geometry), so Update knows
// it without waiting for Resize.
//
// An iTerm2 image is not in the cell grid, so whatever stops covering it
// clears the screen: esc and ctrl+k (closing), and a switch to a photo with no
// image to draw yet (loading or failed).

const tagMediaPost = "feed.media_post:" // GET /v1/posts/{id} for a post the FeedStore lacks

// Viewer copy.
const (
	textMediaTitle   = "사진"
	textMediaLoading = "사진을 불러오는 중…"
	textMediaFailed  = "사진을 불러오지 못했어요"
	textMediaNone    = "볼 수 있는 사진이 없어요"
	errContextMedia  = "사진"
)

// App overlay degrade thresholds (CA 5.3.7): a body narrower or shorter than
// this (or tiny mode) gets the overlay full screen without a border.
const (
	mediaMinW = 24
	mediaMinH = 6
)

// iTerm2 inline images are pre-fitted to the box (theme letterbox) at most
// itermCellPx pixels per cell column (rows twice that) and itermMaxPixels in
// all: the escape stays small and a new photo covers the previous one.
const (
	itermCellPx    = 10
	itermMaxPixels = 1_500_000
)

func init() {
	ui.RegisterOverlay(overlayMedia, func(ctx *ui.Context, args string) ui.Overlay {
		if v := newMediaView(args); v != nil {
			return v
		}
		return nil
	})
}

// mediaMethod is how the viewer draws the photo.
type mediaMethod int

const (
	mediaHalfBlock mediaMethod = iota
	mediaKitty
	mediaITerm2
)

// mediaBox is the photo box on screen: top-left cell (0-based) and size.
type mediaBox struct{ X, Y, Cols, Rows int }

// mediaShot is one iTerm2 inline image sent to the terminal.
type mediaShot struct {
	id  string
	box mediaBox
	bg  color.RGBA
}

type mediaView struct {
	postID string
	idx    int

	fetching bool            // GET /v1/posts/{id} in flight
	missing  bool            // that GET failed
	failed   map[string]bool // media ids whose load failed

	imgs     imageSlots
	size     ui.Size
	sent     mediaShot // the last iTerm2 image sent (id "" = none on screen)
	itermOff bool      // an iTerm2 escape failed: half blocks from now on
	want     bool      // View drew a box the next Update has to send
}

var (
	_ ui.Overlay    = (*mediaView)(nil)
	_ ui.KeyHandler = (*mediaView)(nil)
	_ ui.Titled     = (*mediaView)(nil)
	_ ui.Helper     = (*mediaView)(nil)
)

// newMediaView parses "<postID> <idx>" (a missing or bad idx is 0).
func newMediaView(args string) *mediaView {
	f := strings.Fields(args)
	if len(f) == 0 {
		return nil
	}
	v := &mediaView{postID: f[0], failed: map[string]bool{}}
	if len(f) > 1 {
		if n, err := strconv.Atoi(f[1]); err == nil && n > 0 {
			v.idx = n
		}
	}
	return v
}

// post is the cached post, or nil.
func (v *mediaView) post(ctx *ui.Context) *api.Post {
	if ctx == nil || ctx.Store == nil {
		return nil
	}
	fs := feedstore.Get(ctx.Store)
	if fs == nil {
		return nil
	}
	return fs.Post(v.postID)
}

// photos is the post's media when it can be shown (not deleted, author not blocked).
func (v *mediaView) photos(ctx *ui.Context) []api.Media {
	p := v.post(ctx)
	if p == nil || p.Deleted || ctx.Store.IsBlocked(p.Author.ID) {
		return nil
	}
	return p.Media
}

// current is the photo shown now (idx clamped to the post's media).
func (v *mediaView) current(ctx *ui.Context) (api.Media, bool) {
	ms := v.photos(ctx)
	if len(ms) == 0 {
		return api.Media{}, false
	}
	v.idx = min(max(v.idx, 0), len(ms)-1)
	return ms[v.idx], true
}

func (v *mediaView) Init(ctx *ui.Context) tea.Cmd {
	if v.post(ctx) != nil {
		return v.show(ctx)
	}
	v.fetching = true
	id := v.postID
	return ui.Do(ctx, tagMediaPost+id, func(c context.Context, a *api.Client) (*api.Post, error) {
		if a == nil {
			return nil, errNoClient
		}
		return a.GetPost(c, id)
	})
}

// show requests the current photo and, when it is loaded already, sends it
// for the current box (Update / HandleKey only).
func (v *mediaView) show(ctx *ui.Context) tea.Cmd {
	m, ok := v.current(ctx)
	if !ok {
		return nil
	}
	return tea.Batch(v.imgs.request(ctx, m), v.emit(ctx, false))
}

func (v *mediaView) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	forwardResync(ctx, msg)
	cmds := []tea.Cmd{applyResults(ctx, msg)}
	switch m := msg.(type) {
	case ui.ResultMsg[*api.Post]:
		if m.Tag == tagMediaPost+v.postID && v.fetching {
			cmds = append(cmds, v.onPost(ctx, m))
		}
	case ui.ResultMsg[image.Image]:
		id := strings.TrimPrefix(m.Tag, mediaTagPrefix)
		if strings.HasPrefix(m.Tag, mediaTagPrefix) && v.imgs.requested[id] && (m.Err != nil || m.Value == nil) {
			v.failed[id] = true
		}
	case ui.ModeChangedMsg, ui.ThemeChangedMsg:
		// The box or the letterbox color changed and the terminal repaints: send again.
		cmds = append(cmds, v.emit(ctx, true))
	}
	if id, _, ok := v.imgs.loaded(msg); ok {
		delete(v.failed, id)
		if m, cur := v.current(ctx); cur && m.ID == id {
			cmds = append(cmds, v.emit(ctx, false))
		}
	}
	if v.want {
		v.want = false
		cmds = append(cmds, v.emit(ctx, false))
	}
	cmds = append(cmds, v.clearStale(ctx)) // e.g. the post was deleted or its author blocked
	return tea.Batch(cmds...)
}

// onPost stores the fetched post and shows its photo.
func (v *mediaView) onPost(ctx *ui.Context, m ui.ResultMsg[*api.Post]) tea.Cmd {
	v.fetching = false
	if m.Err != nil || m.Value == nil {
		v.missing = true
		if m.Err == nil || api.IsCode(m.Err, "not_found") {
			return nil
		}
		err := m.Err
		return func() tea.Msg { return ui.ErrorMsg{Err: err, Context: errContextMedia} }
	}
	if ctx != nil && ctx.Store != nil {
		if fs := feedstore.Get(ctx.Store); fs != nil {
			fs.ObservePosts(*m.Value)
		}
	}
	return v.show(ctx)
}

// method is the drawing method for the terminal.
func (v *mediaView) method(ctx *ui.Context) mediaMethod {
	switch {
	case ctx.Caps.InTmux:
		return mediaHalfBlock
	case ctx.Caps.Graphics == termcaps.GraphicsKitty:
		return mediaKitty
	case ctx.Caps.Graphics == termcaps.GraphicsITerm2 && !v.itermOff:
		return mediaITerm2
	}
	return mediaHalfBlock
}

// geom is the photo box the app gives this overlay now: the whole body
// (Bounds), inside the border unless the app draws the overlay without one
// (tiny mode, or a body under 24x6). ok is false when no overlay is drawn.
func (v *mediaView) geom(ctx *ui.Context) (mediaBox, bool) {
	if ctx == nil {
		return mediaBox{}, false
	}
	l := ctx.Layout
	if ctx.Mode == layout.ModeTooSmall || l.Cols <= 0 || l.Rows <= 1 {
		return mediaBox{}, false
	}
	r := layout.Rect{W: l.Cols, H: l.Rows - 1} // the body: everything above the status line
	if ctx.Mode != layout.ModeTiny && r.W >= mediaMinW && r.H >= mediaMinH {
		r = r.Inner()
	}
	if r.W <= 0 || r.H <= 0 {
		return mediaBox{}, false
	}
	return mediaBox{X: r.X, Y: r.Y, Cols: r.W, Rows: r.H}, true
}

// bgOf is the letterbox color.
func bgOf(ctx *ui.Context) color.RGBA {
	return color.RGBAModel.Convert(themeOf(ctx).P.BG).(color.RGBA)
}

// emit sends the current photo for the current box (Update / HandleKey only):
// kitty's first Render of the combination returns its transmit; iTerm2 sends an
// inline image unless the same one is on screen (force: send anyway).
func (v *mediaView) emit(ctx *ui.Context, force bool) tea.Cmd {
	m, ok := v.current(ctx)
	if !ok {
		return nil
	}
	img := v.imgs.image(m.ID)
	box, ok := v.geom(ctx)
	if img == nil || !ok {
		return nil
	}
	switch v.method(ctx) {
	case mediaKitty:
		return v.imgs.render(ctx, m, box.Cols, box.Rows)
	case mediaITerm2:
		shot := mediaShot{id: m.ID, box: box, bg: bgOf(ctx)}
		if !force && shot == v.sent {
			return nil
		}
		s, err := imgrender.ITerm2Inline(itermFit(img, box.Cols, box.Rows, shot.bg), box.X, box.Y, box.Cols, box.Rows)
		if err != nil {
			slog.Debug("feed.media: iTerm2 inline image failed; using half blocks", "err", err)
			v.itermOff, v.sent = true, mediaShot{}
			return nil
		}
		v.sent = shot
		return tea.Raw(s)
	}
	return nil
}

// itermFit letterboxes img into a cols x rows cell box (a cell is taken as one
// pixel wide and two high, times k) over bg, never upscaling past the source.
func itermFit(img image.Image, cols, rows int, bg color.RGBA) image.Image {
	src := img.Bounds()
	k := itermCellPx
	for k > 1 && cols*k > src.Dx() && rows*2*k > src.Dy() {
		k--
	}
	for k > 1 && cols*k*rows*2*k > itermMaxPixels {
		k--
	}
	w, h := cols*k, rows*2*k
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.Draw(dst, dst.Bounds(), image.NewUniform(bg), image.Point{}, xdraw.Src)
	if src.Dx() <= 0 || src.Dy() <= 0 {
		return dst
	}
	s := min(float64(w)/float64(src.Dx()), float64(h)/float64(src.Dy()))
	dw, dh := max(int(float64(src.Dx())*s+0.5), 1), max(int(float64(src.Dy())*s+0.5), 1)
	x0, y0 := (w-dw)/2, (h-dh)/2
	xdraw.CatmullRom.Scale(dst, image.Rect(x0, y0, x0+dw, y0+dh), img, src, xdraw.Over, nil)
	return dst
}

func (v *mediaView) Resize(_ *ui.Context, size ui.Size) { v.size = size }

func (v *mediaView) Bounds(body layout.Rect) layout.Rect { return body }

func (v *mediaView) Modal() bool { return true }

func (v *mediaView) Title(ctx *ui.Context) (string, ui.Badge) {
	ms := v.photos(ctx)
	if len(ms) == 0 {
		return textMediaTitle, ui.Badge{}
	}
	return fmt.Sprintf("%s %d/%d", textMediaTitle, min(max(v.idx, 0), len(ms)-1)+1, len(ms)), ui.Badge{}
}

func (v *mediaView) Help(ctx *ui.Context) []key.Binding {
	var out []key.Binding
	if len(v.photos(ctx)) > 1 {
		out = append(out,
			key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "이전")),
			key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "다음")))
	}
	return append(out, key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "닫기")))
}

func (v *mediaView) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "left":
		if _, ok := v.current(ctx); ok && v.idx > 0 {
			v.idx--
			return true, tea.Batch(v.clearStale(ctx), v.show(ctx))
		}
		return true, nil
	case "right":
		if _, ok := v.current(ctx); ok && v.idx < len(v.photos(ctx))-1 {
			v.idx++
			return true, tea.Batch(v.clearStale(ctx), v.show(ctx))
		}
		return true, nil
	}
	if v.sent.id == "" {
		return false, nil
	}
	// The inline image is not in the cell grid: repaint everything so no piece
	// of it stays on the screen below.
	switch {
	case ctx.Keys.Matches(msg, keymap.NavBack):
		v.sent = mediaShot{}
		return true, tea.Batch(closeOverlay, tea.ClearScreen)
	case ctx.Keys.Matches(msg, keymap.PaletteOpen):
		// Unhandled: the app closes the viewer and opens the palette itself
		// (CA 5.3.4 step 2.1), batching this Cmd with it.
		v.sent = mediaShot{}
		return false, tea.ClearScreen
	}
	return false, nil
}

// clearStale clears the screen when an iTerm2 image is on it but the current
// photo has none to cover it (still loading, failed, or gone): View then draws
// text over a box the old image still shows through. The next arrival sends
// the image again (v.sent is reset). Update / HandleKey only.
func (v *mediaView) clearStale(ctx *ui.Context) tea.Cmd {
	if v.sent.id == "" {
		return nil
	}
	if m, ok := v.current(ctx); ok && !v.failed[m.ID] && v.imgs.image(m.ID) != nil {
		return nil
	}
	v.sent = mediaShot{}
	return tea.ClearScreen
}

func (v *mediaView) View(ctx *ui.Context, _ bool) string {
	t := themeOf(ctx)
	w, h := v.size.W, v.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	m, ok := v.current(ctx)
	switch {
	case v.fetching:
		return widgets.Empty(t, v.size, "", textMediaLoading, "")
	case !ok:
		return widgets.Empty(t, v.size, "🖼", textMediaNone, "")
	case v.failed[m.ID]:
		return widgets.Empty(t, v.size, "🖼", textMediaFailed, "")
	}
	img := v.imgs.image(m.ID)
	if img == nil {
		return widgets.Empty(t, v.size, "", textMediaLoading, "")
	}
	return blankRows(t, v.photoLines(ctx, m, img), w, h)
}

// photoLines draws the photo into the inner size (View): kitty placeholders
// once transmitted, the blank box under the iTerm2 image sent for exactly this
// box, else half blocks — and a box not sent yet is left for the next
// Update's flush.
func (v *mediaView) photoLines(ctx *ui.Context, m api.Media, img image.Image) []string {
	w, h := v.size.W, v.size.H
	r := mediaRenderer(ctx)
	o := renderOpts(ctx, w, h)
	switch v.method(ctx) {
	case mediaKitty:
		out, ready := r.Peek(m.ID, img, o)
		if !ready {
			v.want = true
		}
		return out.Lines
	case mediaITerm2:
		box, ok := v.geom(ctx)
		if ok && box.Cols == w && box.Rows == h && v.sent == (mediaShot{id: m.ID, box: box, bg: bgOf(ctx)}) {
			return nil // blankRows fills the box
		}
		v.want = true
	}
	out, _ := r.Peek(m.ID, img, o) // half blocks (the iTerm2 Renderer draws those too)
	return out.Lines
}
