package feed

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

// openMedia opens the full-screen viewer on media idx of postID.
func openMedia(h *uitest.Harness, postID string, idx int) {
	h.T.Helper()
	h.Send(ui.OpenOverlayMsg{ID: overlayMedia, Args: fmt.Sprintf("%s %d", postID, idx)})
}

// mediaInner is the open viewer's inner rect (the image box) on screen.
func mediaInner(t *testing.T, h *uitest.Harness) layout.Rect {
	t.Helper()
	ov := h.Model.Frame().Overlays
	if len(ov) != 1 {
		t.Fatalf("want 1 overlay, got %d", len(ov))
	}
	if !ov[0].Bordered {
		return ov[0].Rect
	}
	return ov[0].Rect.Inner()
}

// itermRaws are the iTerm2 inline-image escapes emitted since TeaMsgs()[n].
func itermRaws(h *uitest.Harness, n int) []string {
	var out []string
	for _, m := range h.TeaMsgs()[n:] {
		if r, ok := m.(tea.RawMsg); ok {
			if s := fmt.Sprint(r.Msg); strings.Contains(s, "1337;File=") && strings.Contains(s, "inline=1") {
				out = append(out, s)
			}
		}
	}
	return out
}

// anyRaws counts every tea.RawMsg emitted since TeaMsgs()[n].
func anyRaws(h *uitest.Harness, n int) int {
	c := 0
	for _, m := range h.TeaMsgs()[n:] {
		if _, ok := m.(tea.RawMsg); ok {
			c++
		}
	}
	return c
}

func joined(lines []string) string { return strings.Join(lines, "\n") }

func TestMediaOverlayGolden(t *testing.T) {
	sizes := append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6})
	sweep(t, sizes, "media", feedOpts(t, "following", false), func(h *uitest.Harness) {
		openMedia(h, postBy("hana").ID, 0)
	})
}

func TestMediaITerm2EmitsRaw(t *testing.T) {
	// Review Focus 6: the iTerm2 escape leaves with the Update that got the image.
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Resize(120, 40)
	h.Context().Caps.Graphics = termcaps.GraphicsITerm2
	n := len(h.TeaMsgs())
	openMedia(h, postBy("hana").ID, 0) // nothing else is sent afterwards
	raws := itermRaws(h, n)
	if len(raws) != 1 {
		t.Fatalf("want 1 iTerm2 inline escape right after opening, got %d", len(raws))
	}
	in := mediaInner(t, h)
	if !strings.Contains(raws[0], fmt.Sprintf("width=%d", in.W)) || !strings.Contains(raws[0], fmt.Sprintf("height=%d", in.H)) {
		t.Fatalf("escape is not sized to the %dx%d box: %.80q", in.W, in.H, raws[0])
	}
	if !strings.Contains(raws[0], fmt.Sprintf("\x1b[%d;%dH", in.Y+1, in.X+1)) {
		t.Fatalf("escape does not move to the box origin (%d,%d): %.80q", in.X, in.Y, raws[0])
	}
	if got := joined(region(h.Screen(), in)); strings.Contains(got, "▀") {
		t.Fatalf("iTerm2 box must be blank under the inline image:\n%s", got)
	}
	if !strings.Contains(h.Screen(), "사진 1/1") {
		t.Fatalf("no title 사진 1/1:\n%s", h.Screen())
	}

	// Same mode resize: no Update reaches the viewer, so no escape; the new box
	// shows half blocks instead of a stale blank.
	n = len(h.TeaMsgs())
	h.Resize(160, 45)
	if raws := itermRaws(h, n); len(raws) != 0 {
		t.Fatalf("same-mode resize emitted %d escapes", len(raws))
	}
	in = mediaInner(t, h)
	if got := joined(region(h.Screen(), in)); !strings.Contains(got, "▀") {
		t.Fatalf("after same-mode resize the box must fall back to half blocks:\n%s", got)
	}
	h.Check()

	// The next Update (any message) sends the new box.
	type nudge struct{}
	n = len(h.TeaMsgs())
	h.Send(nudge{})
	raws = itermRaws(h, n)
	if len(raws) != 1 || !strings.Contains(raws[0], fmt.Sprintf("width=%d", in.W)) {
		t.Fatalf("nudge: want 1 escape with width=%d, got %d", in.W, len(raws))
	}
	if got := joined(region(h.Screen(), in)); strings.Contains(got, "▀") {
		t.Fatalf("after the nudge the box must be blank again:\n%s", got)
	}

	// A mode change re-sends without any further message.
	n = len(h.TeaMsgs())
	h.Resize(200, 60)
	in = mediaInner(t, h)
	raws = itermRaws(h, n)
	if len(raws) != 1 || !strings.Contains(raws[0], fmt.Sprintf("width=%d", in.W)) {
		t.Fatalf("mode change: want 1 escape with width=%d, got %d", in.W, len(raws))
	}
	if got := joined(region(h.Screen(), in)); strings.Contains(got, "▀") {
		t.Fatalf("200x60: box must be blank:\n%s", got)
	}
}

func TestMediaITerm2EscClearsScreen(t *testing.T) {
	// The inline image is not part of the cell grid: closing forces a full repaint.
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Context().Caps.Graphics = termcaps.GraphicsITerm2
	openMedia(h, postBy("hana").ID, 0)
	n := len(h.TeaMsgs())
	h.Press("esc")
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("esc did not close the viewer")
	}
	cleared := false
	for _, m := range h.TeaMsgs()[n:] {
		if fmt.Sprintf("%T", m) == fmt.Sprintf("%T", tea.ClearScreen()) {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("closing an iTerm2 image did not clear the screen")
	}
}

func TestMediaKittyEmitsSetupOnce(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Resize(120, 40)
	h.Context().Caps.Graphics = termcaps.GraphicsKitty
	n := len(h.TeaMsgs())
	openMedia(h, postBy("hana").ID, 0) // no follow-up message
	in := mediaInner(t, h)
	box := 0
	for _, r := range rawSince(h, n) {
		if strings.Contains(r, fmt.Sprintf("c=%d,", in.W)) && strings.Contains(r, fmt.Sprintf("r=%d", in.H)) {
			box++
		}
	}
	if box != 1 {
		t.Fatalf("want exactly 1 kitty transmit for the %dx%d box, got %d", in.W, in.H, box)
	}
	if got := joined(region(h.Screen(), in)); !strings.ContainsRune(got, kitty.Placeholder) {
		t.Fatalf("box shows no kitty placeholder:\n%s", got)
	}
	n = len(h.TeaMsgs())
	h.Send(ui.ModeChangedMsg{From: layout.ModeStandard, To: layout.ModeStandard})
	if c := anyRaws(h, n); c != 0 {
		t.Fatalf("ModeChangedMsg for the same box emitted %d raws", c)
	}
}

func TestMediaTmuxUsesHalfBlock(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Context().Caps = termcaps.Caps{Graphics: termcaps.GraphicsKitty, InTmux: true}
	n := len(h.TeaMsgs())
	openMedia(h, postBy("hana").ID, 0)
	if c := anyRaws(h, n); c != 0 {
		t.Fatalf("tmux: emitted %d raws", c)
	}
	if got := joined(region(h.Screen(), mediaInner(t, h))); !strings.Contains(got, "▀") {
		t.Fatalf("tmux: no half blocks:\n%s", got)
	}
}

// threePhotos is hana's post with three photos.
func threePhotos() api.Post {
	p := postBy("hana")
	base := p.Media[0]
	p.Media = nil
	for i := range 3 {
		m := base
		m.ID = sid(200 + i)
		m.URL = "/v1/media/" + m.ID
		p.Media = append(p.Media, m)
	}
	return p
}

func TestMediaNextPrev(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	p := threePhotos()
	feedstore.Get(h.Context().Store).ObservePosts(p)
	openMedia(h, p.ID, 0)
	if !strings.Contains(h.Screen(), "사진 1/3") {
		t.Fatalf("no 사진 1/3:\n%s", h.Screen())
	}
	h.Press("right")
	if !strings.Contains(h.Screen(), "사진 2/3") {
		t.Fatalf("→: no 사진 2/3:\n%s", h.Screen())
	}
	if _, ok := lastReq(h, "GET", "/v1/media/"+sid(201)); !ok {
		t.Fatal("→ did not load the second photo")
	}
	h.Press("right", "right")
	if !strings.Contains(h.Screen(), "사진 3/3") {
		t.Fatalf("→ at the end must stay on 사진 3/3:\n%s", h.Screen())
	}
	h.Press("left", "left")
	if !strings.Contains(h.Screen(), "사진 1/3") {
		t.Fatalf("←: no 사진 1/3:\n%s", h.Screen())
	}
	h.Press("esc")
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("esc did not close the viewer")
	}
}

func TestMediaOpensFromKeyAndStartIndex(t *testing.T) {
	// "o" on the selected card opens photo 1; args idx picks the start photo.
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Press("o")
	if !strings.Contains(h.Screen(), "사진 1/1") {
		t.Fatalf("o: no viewer:\n%s", h.Screen())
	}
	h.Press("esc")
	p := threePhotos()
	feedstore.Get(h.Context().Store).ObservePosts(p)
	openMedia(h, p.ID, 2)
	if !strings.Contains(h.Screen(), "사진 3/3") {
		t.Fatalf("idx 2: no 사진 3/3:\n%s", h.Screen())
	}
}

func TestMediaUnknownPostFetches(t *testing.T) {
	// A post not in the FeedStore is fetched with GET /v1/posts/{id}.
	h := uitest.New(t, feedOpts(t, "following", false)...)
	p := postBy("hana")
	openMedia(h, quotePost().ID, 0) // quotePost has no media: the viewer says so
	if _, ok := lastReq(h, "GET", "/v1/posts/"+quotePost().ID); !ok {
		t.Fatal("unknown post was not fetched")
	}
	if !strings.Contains(h.Screen(), textMediaNone) {
		t.Fatalf("no-media notice missing:\n%s", h.Screen())
	}
	h.Press("esc")
	openMedia(h, p.ID, 0)
	if got := joined(region(h.Screen(), mediaInner(t, h))); !strings.Contains(got, "▀") {
		t.Fatalf("hana photo not drawn:\n%s", got)
	}
}

func TestMediaHostile(t *testing.T) {
	sizes := append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6}, uitest.Size{W: 19, H: 10})
	sweep(t, sizes, "", feedOpts(t, "following", true), func(h *uitest.Harness) {
		openMedia(h, postBy("hana").ID, 0)
		h.Press("right", "left")
	})
}
