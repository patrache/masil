package termhost

import (
	"bytes"
	"fmt"
	"image/color"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
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
	// Snapshot returns bytes that redraw the current screen on a fresh
	// screen of the same size: contents, styles, cursor and input modes,
	// plus the state later output depends on (pen, margins, autowrap,
	// origin and newline modes, saved cursor, G0-G3 charsets) and the bytes
	// of a sequence the output so far left unfinished. Not carried (x/vt
	// does not expose them): the pending-wrap flag at the last column, a
	// SO/SI shift, tab stops, and the main screen under the alternate one.
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
	st       vtState
	stopping atomic.Bool
	respDone chan struct{}
}

// vtState mirrors the emulator state that x/vt keeps private but Snapshot
// must carry so that later output lands the same on a replay: the pen, the
// saved cursors and the margins of each screen (0 main, 1 alternate), and
// the bytes of a sequence the last Write left unfinished. It is kept by
// handlers that run just before x/vt's own (they return false), following
// x/vt's rules rather than xterm's (for example ?1049l does not restore the
// cursor there).
type vtState struct {
	// pen is a 1x1 emulator fed every SGR: its pen is the active screen's
	// pen, computed by x/vt's own SGR code.
	pen     *vt.Emulator
	alt     bool
	mainPen string // the main screen's pen (SGR) while the alternate one is active
	saved   [2]savedCursor
	tb, lr  [2]margins // DECSTBM, DECSLRM as last applied (1-based)
	noWrap  bool       // DECAWM reset
	charset [4]byte    // SCS final byte designated to G0..G3 (0 = US ASCII)
	parser  *ansi.Parser
	tail    []byte // unfinished sequence at the end of the output so far
}

type savedCursor struct {
	set    bool
	x, y   int
	hidden bool
	pen    string
}

type margins struct {
	set    bool
	lo, hi int
}

// maxTail bounds the unfinished-sequence bytes kept for Snapshot; a longer
// one (a big OSC or DCS string) keeps its head, which is enough to put the
// replay in the same parser state.
const maxTail = 4096

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
		EnableMode: func(m ansi.Mode) {
			v.modes[m] = true
			if m == ansi.ModeAutoWrap {
				v.st.noWrap = false
			}
		},
		DisableMode: func(m ansi.Mode) {
			delete(v.modes, m)
			if m == ansi.ModeAutoWrap {
				v.st.noWrap = true
			}
		},
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
		v.resetState()
		return false
	})
	v.st.pen = vt.NewEmulator(1, 1)
	v.st.parser = ansi.NewParser()
	v.st.parser.SetDataSize(1) // only its state is used
	v.trackState()
	return v
}

// trackState registers the handlers that keep v.st (see vtState).
func (v *vtScreen) trackState() {
	e := v.e
	e.RegisterCsiHandler('m', func(params ansi.Params) bool {
		v.st.pen.WriteString(sgrOf(params))
		return false
	})
	e.RegisterEscHandler('7', func() bool { v.saveCursor(); return false })
	e.RegisterEscHandler('8', func() bool { v.restoreCursor(); return false })
	e.RegisterCsiHandler('s', func(params ansi.Params) bool {
		if !v.modes[ansi.ModeLeftRightMargin] {
			v.saveCursor() // SCOSC
			return false
		}
		left, _, _ := params.Param(0, 1)
		left = max(left, 1)
		right, _, _ := params.Param(1, e.Width())
		if right < 1 {
			right = e.Width()
		}
		if left < right {
			v.st.lr[v.active()] = margins{set: true, lo: left, hi: right}
		}
		return false
	})
	e.RegisterCsiHandler('r', func(params ansi.Params) bool {
		top, _, _ := params.Param(0, 1)
		top = max(top, 1)
		bottom, _, _ := params.Param(1, e.Height())
		if bottom < 1 {
			bottom = e.Height()
		}
		if top < bottom {
			v.st.tb[v.active()] = margins{set: true, lo: top, hi: bottom}
		}
		return false
	})
	decMode := func(set bool) func(ansi.Params) bool {
		return func(params ansi.Params) bool {
			for _, p := range params {
				switch ansi.DECMode(p.Param(-1)) {
				case ansi.ModeSaveCursor: // 1048
					if set {
						v.saveCursor()
					} else {
						v.restoreCursor()
					}
				case ansi.ModeAltScreen: // 1047
					v.switchScreen(set)
				case ansi.ModeAltScreenSaveCursor: // 1049
					if set {
						v.saveCursor()
					}
					v.switchScreen(set)
				}
			}
			return false
		}
	}
	e.RegisterCsiHandler(ansi.Command('?', 0, 'h'), decMode(true))
	e.RegisterCsiHandler(ansi.Command('?', 0, 'l'), decMode(false))
	for g, inter := range []byte("()*+") {
		for _, final := range []byte("AB0") { // the sets x/vt knows
			e.RegisterEscHandler(ansi.Command(0, inter, final), func() bool {
				v.st.charset[g] = final
				if final == 'B' {
					v.st.charset[g] = 0
				}
				return false
			})
		}
	}
}

// sgrOf rebuilds the SGR sequence for params (':' between sub-parameters).
func sgrOf(params ansi.Params) string {
	b := []byte("\x1b[")
	for i, p := range params {
		if n := p.Param(-1); n >= 0 {
			b = strconv.AppendInt(b, int64(n), 10)
		}
		if p.HasMore() {
			b = append(b, ':')
		} else if i < len(params)-1 {
			b = append(b, ';')
		}
	}
	return string(append(b, 'm'))
}

// active is the index of the screen x/vt draws on (0 main, 1 alternate).
func (v *vtScreen) active() int {
	if v.st.alt {
		return 1
	}
	return 0
}

// currentPen returns the active pen as an SGR sequence.
func (v *vtScreen) currentPen() string {
	v.st.pen.WriteString("\rx")
	c := v.st.pen.CellAt(0, 0)
	if c == nil {
		return ansi.ResetStyle
	}
	return c.Style.String()
}

func (v *vtScreen) setPen(sgr string) { v.st.pen.WriteString(ansi.ResetStyle + sgr) }

// saveCursor mirrors Screen.SaveCursor (DECSC, SCOSC, ?1048h, ?1049h).
func (v *vtScreen) saveCursor() {
	p := v.e.CursorPosition()
	v.st.saved[v.active()] = savedCursor{set: true, x: p.X, y: p.Y, hidden: v.e.CursorHidden(), pen: v.currentPen()}
}

// restoreCursor mirrors Screen.RestoreCursor (DECRC, ?1048l): the pen comes
// back too; with nothing saved it is the default pen.
func (v *vtScreen) restoreCursor() { v.setPen(v.st.saved[v.active()].pen) }

// switchScreen mirrors setAltScreenMode: entering copies the main cursor
// (pen included) to the alternate screen; leaving returns to the main pen.
func (v *vtScreen) switchScreen(alt bool) {
	if alt == v.st.alt {
		return
	}
	if alt {
		v.st.mainPen = v.currentPen()
	} else {
		v.setPen(v.st.mainPen)
	}
	v.st.alt = alt
}

// resetState mirrors RIS.
func (v *vtScreen) resetState() {
	v.setPen("")
	v.st.alt, v.st.mainPen, v.st.noWrap = false, "", false
	v.st.saved = [2]savedCursor{}
	v.st.tb, v.st.lr = [2]margins{}, [2]margins{}
	v.st.charset = [4]byte{}
}

// trackTail keeps the bytes of a sequence b leaves unfinished (see maxTail).
func (v *vtScreen) trackTail(b []byte) {
	cut := -1
	for i, c := range b {
		v.st.parser.Advance(c)
		if v.st.parser.State() == parser.GroundState {
			cut = i + 1
		}
	}
	if cut >= 0 {
		v.st.tail = v.st.tail[:0]
		b = b[cut:]
	}
	if room := maxTail - len(v.st.tail); room > 0 && len(b) > 0 {
		v.st.tail = append(v.st.tail, b[:min(len(b), room)]...)
	}
}

func (v *vtScreen) Write(b []byte) {
	_, _ = v.e.Write(b)
	v.trackTail(b)
}

func (v *vtScreen) Resize(s Size) {
	if s.W <= 0 || s.H <= 0 || (s.W == v.e.Width() && s.H == v.e.Height()) {
		return
	}
	v.e.Resize(s.W, s.H)
	v.st.tb, v.st.lr = [2]margins{}, [2]margins{} // Screen.Resize resets the scroll region
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
	// State that changes where later output lands (vtState), then the
	// cursor, then the pen, then the unfinished sequence, so the tap bytes
	// that follow continue exactly where the source is.
	i := v.active()
	if v.modes[ansi.ModeLeftRightMargin] {
		b.WriteString(ansi.SetMode(ansi.ModeLeftRightMargin))
		if m := v.st.lr[i]; m.set {
			fmt.Fprintf(&b, "\x1b[%d;%ds", m.lo, m.hi)
		}
	}
	if m := v.st.tb[i]; m.set {
		fmt.Fprintf(&b, "\x1b[%d;%dr", m.lo, m.hi)
	}
	if sc := v.st.saved[i]; sc.set {
		b.WriteString(ansi.CursorPosition(sc.x+1, sc.y+1))
		b.WriteString(ansi.ResetStyle + sc.pen)
		if sc.hidden {
			b.WriteString(ansi.HideCursor)
		} else {
			b.WriteString(ansi.ShowCursor)
		}
		b.WriteString(ansi.SaveCursor)
	}
	for _, m := range snapshotModes {
		if v.modes[m] {
			b.WriteString(ansi.SetMode(m))
		}
	}
	if v.modes[ansi.ModeLineFeedNewLine] {
		b.WriteString(ansi.SetMode(ansi.ModeLineFeedNewLine))
	}
	if v.st.noWrap {
		b.WriteString(ansi.ResetMode(ansi.ModeAutoWrap))
	}
	for g, final := range v.st.charset {
		if final != 0 {
			b.Write([]byte{ansi.ESC, "()*+"[g], final})
		}
	}
	x, y, visible := v.Cursor()
	b.WriteString(ansi.CursorPosition(x+1, y+1))
	if v.modes[ansi.ModeOrigin] { // after the absolute CUP: setting it does not move the cursor
		b.WriteString(ansi.SetMode(ansi.ModeOrigin))
	}
	if !visible {
		b.WriteString(ansi.HideCursor)
	} else {
		b.WriteString(ansi.ShowCursor)
	}
	b.WriteString(ansi.ResetStyle + v.currentPen())
	b.Write(v.st.tail)
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
// into a local path; anything that is not a file URL with an absolute, clean
// path gives ok == false. The path comes from child output, so one that
// decodes to invalid UTF-8, a control character (C0, DEL, C1) or a bidi
// control is refused: it would otherwise reach the screen or a command line.
func parseCwd(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" || !strings.HasPrefix(u.Path, "/") {
		return "", false
	}
	p := u.Path
	if !utf8.ValidString(p) || path.Clean(p) != p {
		return "", false
	}
	for _, r := range p {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return "", false
		}
	}
	return p, true
}
