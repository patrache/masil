package termhost

import (
	"runtime"
	"sync"
)

// Replay is a VT screen without a PTY: it draws output that came from
// elsewhere (a shared terminal's peer view, A7.7). It starts no program, is
// not counted in MaxPanes and works on every OS. It never answers queries
// and ignores OSC 52, titles, hyperlinks and images. Safe for concurrent use.
type Replay struct {
	mu  sync.Mutex
	scr *vtScreen
}

// NewReplay returns a replay screen of size s (non-positive => 80x24) keeping
// scrollback lines (<= 0 => 2000; the shared-terminal peer view uses 5000).
func NewReplay(s Size, scrollback int) *Replay {
	scr := newVTScreen(s, scrollback, screenHooks{})
	go scr.Responses(func([]byte) {}) // query replies go nowhere
	r := &Replay{scr: scr}
	runtime.AddCleanup(r, func(v *vtScreen) { v.stop() }, scr)
	return r
}

// Write feeds program output.
func (r *Replay) Write(b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scr.Write(b)
}

// Resize changes the screen size; non-positive sizes are ignored.
func (r *Replay) Resize(s Size) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scr.Resize(s)
}

// Size returns the screen size.
func (r *Replay) Size() Size {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scr.Size()
}

// Render returns the whole screen: Size().H lines of Size().W cells.
func (r *Replay) Render() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.scr.Size()
	return r.scr.RenderRegion(0, 0, s.W, s.H)
}

// RenderRegion returns exactly h lines of w cells from screen cell (x0, y0)
// (panning a screen larger than the view, JX6). Cells outside the screen and
// half-cut wide characters are blanks.
func (r *Replay) RenderRegion(x0, y0, w, h int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scr.RenderRegion(x0, y0, w, h)
}

// Cursor returns the cursor cell and whether it is shown.
func (r *Replay) Cursor() (x, y int, visible bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scr.Cursor()
}

// Scrollback returns the lines pushed off the screen, oldest first (at most
// the scrollback size).
func (r *Replay) Scrollback() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scr.Scrollback()
}
