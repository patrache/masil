package termhost

import (
	"bytes"
	"image/color"
	"net/url"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"github.com/patrache/masil/internal/textwidth"
)

// This is the only file that imports x/vt: everything else talks to the
// emulator through the screen interface.

// screen is a VT emulator screen. It is not safe for concurrent use: Pane and
// Replay serialise every call with their own lock. Write may call the
// screenHooks synchronously (inside the caller's lock).
type screen interface {
	Write(b []byte)
	Resize(s Size)
	Size() Size
	// RenderRegion draws exactly h lines of w cells starting at screen cell
	// (x0, y0); cells outside the screen, and wide characters cut by the
	// region edge, are blanks. Lines are joined by "\n"; SGR only (no links).
	RenderRegion(x0, y0, w, h int) string
	Cursor() (x, y int, visible bool)
	Scrollback() []string
	SendKey(k tea.KeyPressMsg)
	SendPaste(s string)
	SendMouse(m tea.MouseMsg, x, y int)
	SetFocus(focused bool)
	BracketedPaste() bool
	MouseMode() bool
	// Snapshot returns bytes that redraw the current screen (contents,
	// styles, cursor and input modes) on a fresh screen of the same size.
	Snapshot() []byte
	// Responses runs until stop, passing every chunk the emulator wants to
	// send to the child (query replies, encoded keys/mouse/paste) to out.
	// out must never block. Some goroutine must be in Responses whenever
	// another method is called, or that call can block.
	Responses(out func([]byte))
	// stop ends Responses. No other method may be called afterwards.
	stop()
}

// screenHooks receive what the child reports. They run inside Write.
type screenHooks struct {
	Title  func(title string) // OSC 0/2, already sanitised
	Cwd    func(raw string)   // OSC 7 payload after "7;"
	Notify func(kind string)  // "bel", "osc9", "osc777"
	FG, BG color.Color        // default colours answered to OSC 10/11 queries
}

// vtScreen adapts *vt.Emulator to screen.
type vtScreen struct {
	e        *vt.Emulator
	modes    map[ansi.Mode]bool
	stopping atomic.Bool
	respDone chan struct{}
}

// mouseModes are the DEC modes that turn mouse reporting on.
var mouseModes = []ansi.Mode{
	ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseHighlight,
	ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent,
}

// snapshotModes are restored by Snapshot when set (beyond alt screen and cursor visibility).
var snapshotModes = []ansi.DECMode{
	ansi.ModeCursorKeys, ansi.ModeBracketedPaste, ansi.ModeFocusEvent,
	ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseHighlight,
	ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent, ansi.ModeMouseExtSgr,
}

func newVTScreen(s Size, scrollback int, h screenHooks) *vtScreen {
	s = s.orDefault()
	e := vt.NewEmulator(s.W, s.H)
	if scrollback <= 0 {
		scrollback = defaultScrollback
	}
	e.SetScrollbackSize(scrollback)
	if h.FG != nil {
		e.SetDefaultForegroundColor(h.FG)
	}
	if h.BG != nil {
		e.SetDefaultBackgroundColor(h.BG)
	}
	v := &vtScreen{e: e, modes: map[ansi.Mode]bool{}, respDone: make(chan struct{})}
	e.SetCallbacks(vt.Callbacks{
		Bell: func() {
			if h.Notify != nil {
				h.Notify("bel")
			}
		},
		Title: func(t string) {
			if h.Title != nil {
				h.Title(textwidth.Sanitize(t))
			}
		},
		EnableMode:  func(m ansi.Mode) { v.modes[m] = true },
		DisableMode: func(m ansi.Mode) { delete(v.modes, m) },
	})
	// Later registrations run first; returning true stops the default.
	swallow := func([]byte) bool { return true }
	e.RegisterOscHandler(8, swallow)  // hyperlinks: never kept or re-emitted
	e.RegisterOscHandler(52, swallow) // clipboard: a child must not change it
	e.RegisterOscHandler(7, func(data []byte) bool {
		if h.Cwd != nil {
			_, raw, _ := bytes.Cut(data, []byte{';'})
			h.Cwd(string(raw))
		}
		return true
	})
	e.RegisterOscHandler(9, func(data []byte) bool {
		// "9;4;..." is the ConEmu progress report, not a notification.
		if h.Notify != nil && !bytes.HasPrefix(data, []byte("9;4;")) {
			h.Notify("osc9")
		}
		return true
	})
	e.RegisterOscHandler(777, func([]byte) bool {
		if h.Notify != nil {
			h.Notify("osc777")
		}
		return true
	})
	// RIS resets the modes without callbacks.
	e.RegisterEscHandler('c', func() bool {
		clear(v.modes)
		return false
	})
	return v
}

func (v *vtScreen) Write(b []byte) { _, _ = v.e.Write(b) }

func (v *vtScreen) Resize(s Size) {
	if s.W <= 0 || s.H <= 0 || (s.W == v.e.Width() && s.H == v.e.Height()) {
		return
	}
	v.e.Resize(s.W, s.H)
}

func (v *vtScreen) Size() Size { return Size{W: v.e.Width(), H: v.e.Height()} }

func (v *vtScreen) RenderRegion(x0, y0, w, h int) string {
	return v.render(false, x0, y0, w, h)
}

// zeroOf returns the zero value of *p's type (lets us hold x/vt's cell and
// style types without importing the package that defines them).
func zeroOf[T any](*T) T {
	var z T
	return z
}

// render draws w x h cells from (x0, y0) of the screen, or of the scrollback
// when fromScrollback (y0 = line index, each line its own width).
func (v *vtScreen) render(fromScrollback bool, x0, y0, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	at := v.e.CellAt
	sw, sh := v.e.Width(), v.e.Height()
	if fromScrollback {
		at = v.e.ScrollbackCellAt
		sh = v.e.ScrollbackLen()
	}
	blank := zeroOf(at(0, 0))
	pen := blank.Style
	var b strings.Builder
	reset := func() {
		if !pen.IsZero() {
			b.WriteString(ansi.ResetStyle)
			pen = blank.Style
		}
	}
	for row := 0; row < h; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		y := y0 + row
		if fromScrollback && y >= 0 && y < sh {
			sw = len(v.e.Scrollback().Line(y))
		}
		for x := x0; x < x0+w; {
			var c = at(x, y)
			if y < 0 || y >= sh || x < 0 || x >= sw || c == nil || c.Width <= 0 || c.Content == "" ||
				x+c.Width > x0+w {
				// Outside the screen, the right half of a wide cell (its head
				// is left of the region) or a wide cell cut by the right edge.
				reset()
				b.WriteByte(' ')
				x++
				continue
			}
			if c.Style.IsZero() {
				reset()
			} else if !c.Style.Equal(&pen) {
				b.WriteString(c.Style.Diff(&pen))
				pen = c.Style
			}
			b.WriteString(c.Content)
			x += c.Width
		}
		reset()
	}
	return b.String()
}

func (v *vtScreen) Cursor() (x, y int, visible bool) {
	p := v.e.CursorPosition()
	return p.X, p.Y, !v.e.CursorHidden()
}

func (v *vtScreen) Scrollback() []string {
	n := v.e.ScrollbackLen()
	out := make([]string, n)
	for i := range n {
		out[i] = v.render(true, 0, i, len(v.e.Scrollback().Line(i)), 1)
	}
	return out
}

func (v *vtScreen) SendKey(k tea.KeyPressMsg) {
	v.e.SendKey(vt.KeyPressEvent{
		Text: k.Text, Mod: k.Mod, Code: k.Code,
		ShiftedCode: k.ShiftedCode, BaseCode: k.BaseCode, IsRepeat: k.IsRepeat,
	})
}

func (v *vtScreen) SendPaste(s string) { v.e.Paste(s) }

func (v *vtScreen) SendMouse(m tea.MouseMsg, x, y int) {
	mm := m.Mouse()
	switch m.(type) {
	case tea.MouseClickMsg:
		v.e.SendMouse(vt.MouseClick{X: x, Y: y, Button: mm.Button, Mod: mm.Mod})
	case tea.MouseReleaseMsg:
		v.e.SendMouse(vt.MouseRelease{X: x, Y: y, Button: mm.Button, Mod: mm.Mod})
	case tea.MouseWheelMsg:
		v.e.SendMouse(vt.MouseWheel{X: x, Y: y, Button: mm.Button, Mod: mm.Mod})
	case tea.MouseMotionMsg:
		v.e.SendMouse(vt.MouseMotion{X: x, Y: y, Button: mm.Button, Mod: mm.Mod})
	}
}

func (v *vtScreen) SetFocus(focused bool) {
	if focused {
		v.e.Focus()
	} else {
		v.e.Blur()
	}
}

func (v *vtScreen) BracketedPaste() bool { return v.modes[ansi.ModeBracketedPaste] }

func (v *vtScreen) MouseMode() bool {
	for _, m := range mouseModes {
		if v.modes[m] {
			return true
		}
	}
	return false
}

func (v *vtScreen) Snapshot() []byte {
	var b bytes.Buffer
	b.WriteString(ansi.ResetStyle)
	if v.e.IsAltScreen() {
		b.WriteString(ansi.SetModeAltScreenSaveCursor)
	}
	b.WriteString(ansi.EraseEntireScreen)
	sw, sh := v.e.Width(), v.e.Height()
	// Line by line with absolute positioning, so auto-wrap at the last
	// column never scrolls the receiving screen.
	for y := range sh {
		b.WriteString(ansi.CursorPosition(1, y+1))
		b.WriteString(v.render(false, 0, y, sw, 1))
	}
	for _, m := range snapshotModes {
		if v.modes[m] {
			b.WriteString(ansi.SetMode(m))
		}
	}
	x, y, visible := v.Cursor()
	b.WriteString(ansi.CursorPosition(x+1, y+1))
	if !visible {
		b.WriteString(ansi.HideCursor)
	} else {
		b.WriteString(ansi.ShowCursor)
	}
	return b.Bytes()
}

func (v *vtScreen) Responses(out func([]byte)) {
	defer close(v.respDone)
	buf := make([]byte, 4096)
	for {
		n, err := v.e.Read(buf)
		if v.stopping.Load() || err != nil {
			return
		}
		if n > 0 {
			out(bytes.Clone(buf[:n]))
		}
	}
}

// stop wakes Responses with an empty write (Emulator.Close would race with
// the blocked Read) and waits for it to return.
func (v *vtScreen) stop() {
	if !v.stopping.Swap(true) {
		select {
		case <-v.respDone:
		default:
			v.e.SendText("") // an empty write unblocks Read; Responses then sees stopping
		}
	}
	<-v.respDone
}

// parseCwd turns an OSC 7 payload ("file://host/path", percent-encoded)
// into a local path; anything that is not a file URL with an absolute path
// gives ok == false.
func parseCwd(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" || !strings.HasPrefix(u.Path, "/") {
		return "", false
	}
	return u.Path, true
}
