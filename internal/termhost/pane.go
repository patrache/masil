package termhost

import (
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Size is a terminal size in cells.
type Size struct{ W, H int }

// defaultSize is used when StartPane or NewReplay get a non-positive size.
var defaultSize = Size{W: 80, H: 24}

func (s Size) valid() bool { return s.W > 0 && s.H > 0 }

func (s Size) orDefault() Size {
	if !s.valid() {
		return defaultSize
	}
	return s
}

const (
	defaultScrollback = 2000
	// inputQueue is how many pending writes to the child are buffered; more
	// are dropped (and counted) instead of blocking masil.
	inputQueue = 4096
	// dirtyInterval batches redraw signals to at most ~30 per second.
	dirtyInterval = 33 * time.Millisecond
	// drainAfterExit is how long the reader may keep draining output after the
	// child exited before the PTY is closed (a grandchild may hold it open).
	drainAfterExit = time.Second
	// notifyEvery folds repeated notifications of one kind.
	notifyEvery = time.Second
)

// closeGrace is how long Close waits after SIGHUP before SIGKILL (a seam for tests).
var closeGrace = 2 * time.Second

// PaneOptions configures StartPane. Every callback runs on a pane goroutine,
// never under the pane's lock, and must not block (post a message instead).
type PaneOptions struct {
	FG, BG          color.Color // default colours (theme); answered to OSC 10/11 queries
	ScrollbackLines int         // default 2000
	OnDirty         func()      // the screen changed; batched to <= 30 Hz
	Logger          *slog.Logger
	// OnCwd gets the path of an OSC 7 file://<host>/<path> report
	// (percent-decoded). Repeats of the same value and non-file URLs are dropped.
	OnCwd func(dir string)
	// OnNotify gets "bel", "osc9" or "osc777" when the child asks for
	// attention (the body is not passed); at most once per second per kind.
	OnNotify func(kind string)
}

// proc is a child process attached to a PTY master (pane_unix.go), or a fake.
type proc interface {
	Read(b []byte) (int, error)  // child output
	Write(b []byte) (int, error) // child input
	Resize(s Size) error
	Signal(kill bool) error // false = SIGHUP, true = SIGKILL, to the process group
	Wait() (code int, err error)
	Close() error // close the PTY master
	Pid() int
}

// startProc starts req on a new PTY of size with the full environment env.
// pane_unix.go / pane_windows.go set it; tests replace it.
var startProc func(req Request, size Size, env []string) (proc, error)

// paneEvent is a hook call collected inside screen.Write and run after the
// lock is released.
type paneEvent struct {
	cwd    string
	notify string
}

// Pane is a program running on an embedded PTY, drawn by a VT emulator.
// All methods are safe for concurrent use and never block on the child.
type Pane struct {
	req  Request
	opts PaneOptions
	log  *slog.Logger
	proc proc

	mu         sync.Mutex
	scr        screen
	view       Size // area the host draws (Render size)
	fixed      Size // fixed PTY size (shared terminal); zero = follow view
	title      string
	closed     bool // the emulator no longer takes input
	events     []paneEvent
	lastCwd    string
	lastNotify map[string]time.Time

	in           chan []byte       // bytes for the child (writeLoop)
	ops          chan func(screen) // input operations (inputLoop)
	dropped      atomic.Int64
	dirtyPending atomic.Bool
	closing      atomic.Bool
	stopWriter   chan struct{}
	done         chan ExitInfo
	exited       chan struct{} // closed once the pane is fully torn down
}

// StartPane starts req on an embedded PTY of size (non-positive => 80x24).
// It fails with ErrPaneLimit when MaxPanes panes are alive and with
// ErrUnsupported on Windows. The child gets TERM=xterm-256color,
// COLORTERM=truecolor and MASIL_PANE=1 on top of os.Environ() and req.Env.
func StartPane(req Request, size Size, opts PaneOptions) (*Pane, error) {
	size = size.orDefault()
	if !reserveSlot() {
		return nil, ErrPaneLimit
	}
	env := append(os.Environ(), req.Env...)
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "MASIL_PANE=1")
	pr, err := startProc(req, size, env)
	if err != nil {
		releaseSlot()
		return nil, err
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	p := &Pane{
		req:        req,
		opts:       opts,
		log:        log,
		proc:       pr,
		view:       size,
		lastNotify: map[string]time.Time{},
		in:         make(chan []byte, inputQueue),
		ops:        make(chan func(screen), inputQueue),
		stopWriter: make(chan struct{}),
		done:       make(chan ExitInfo, 1),
		exited:     make(chan struct{}),
	}
	p.scr = newVTScreen(size, opts.ScrollbackLines, screenHooks{
		FG: opts.FG, BG: opts.BG,
		Title:  func(t string) { p.title = t }, // inside Write, under p.mu
		Cwd:    p.queueCwd,
		Notify: p.queueNotify,
	})
	registerPane(p)
	p.log.Info("termhost pane started", "program", p.name(), "mode", string(ModePane), "cols", size.W, "rows", size.H)

	readerDone := make(chan struct{})
	go p.scr.Responses(p.enqueue)
	go p.writeLoop()
	go p.inputLoop()
	go p.readLoop(readerDone)
	go p.waitLoop(readerDone)
	return p, nil
}

// name is the program's base name (logs, Running).
func (p *Pane) name() string { return filepath.Base(p.req.Program) }

// Program returns Request.Program.
func (p *Pane) Program() string { return p.req.Program }

// enqueue hands bytes to the writer without ever blocking.
func (p *Pane) enqueue(b []byte) {
	if len(b) == 0 {
		return
	}
	select {
	case p.in <- b:
	default:
		if p.dropped.Add(1) == 1 {
			p.log.Warn("termhost pane input dropped", "program", p.name())
		}
	}
}

func (p *Pane) writeLoop() {
	for {
		select {
		case b := <-p.in:
			_, _ = p.proc.Write(b)
		case <-p.stopWriter:
			return
		}
	}
}

func (p *Pane) readLoop(done chan<- struct{}) {
	defer close(done)
	buf := make([]byte, 32*1024)
	for {
		n, err := p.proc.Read(buf)
		if n > 0 {
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				return
			}
			chunk := buf[:n]
			p.scr.Write(chunk)
			ev := p.events
			p.events = nil
			p.mu.Unlock()
			p.dispatch(ev)
			p.markDirty()
		}
		if err != nil {
			return
		}
	}
}

func (p *Pane) waitLoop(readerDone <-chan struct{}) {
	code, err := p.proc.Wait()
	select { // let the reader take what the child wrote last
	case <-readerDone:
	case <-time.After(drainAfterExit):
	}
	_ = p.proc.Close()
	select {
	case <-readerDone:
	case <-time.After(drainAfterExit):
	}
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.scr.stop()
	close(p.stopWriter)
	unregisterPane(p)
	if n := p.dropped.Load(); n > 0 {
		p.log.Warn("termhost pane dropped input", "program", p.name(), "writes", n)
	}
	p.log.Info("termhost pane exited", "program", p.name(), "mode", string(ModePane), "code", code)
	p.done <- ExitInfo{Program: p.req.Program, Title: p.req.Title, Mode: ModePane, Code: code, Err: err}
	close(p.done)
	close(p.exited)
	p.markDirty()
}

// queueCwd runs inside screen.Write (under p.mu).
func (p *Pane) queueCwd(raw string) {
	if p.opts.OnCwd == nil {
		return
	}
	dir, ok := parseCwd(raw)
	if !ok || dir == p.lastCwd {
		return
	}
	p.lastCwd = dir
	p.events = append(p.events, paneEvent{cwd: dir})
}

// queueNotify runs inside screen.Write (under p.mu).
func (p *Pane) queueNotify(kind string) {
	if p.opts.OnNotify == nil {
		return
	}
	now := time.Now()
	if last, ok := p.lastNotify[kind]; ok && now.Sub(last) < notifyEvery {
		return
	}
	p.lastNotify[kind] = now
	p.events = append(p.events, paneEvent{notify: kind})
}

func (p *Pane) dispatch(ev []paneEvent) {
	for _, e := range ev {
		switch {
		case e.cwd != "":
			p.opts.OnCwd(e.cwd)
		case e.notify != "":
			p.opts.OnNotify(e.notify)
		}
	}
}

func (p *Pane) markDirty() {
	if p.opts.OnDirty == nil || !p.dirtyPending.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(dirtyInterval, func() {
		p.dirtyPending.Store(false)
		p.opts.OnDirty()
	})
}

// Resize sets the drawn size. Non-positive sizes are ignored (nil). Unless a
// fixed size is set, the PTY and emulator follow (the child gets SIGWINCH);
// with a fixed size only the visible area changes.
func (p *Pane) Resize(s Size) error {
	if !s.valid() {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.view = s
	if p.fixed.valid() || p.closed {
		return nil
	}
	return p.resizePTYLocked(s)
}

func (p *Pane) resizePTYLocked(s Size) error {
	if p.scr.Size() == s {
		return nil
	}
	err := p.proc.Resize(s)
	p.scr.Resize(s)
	p.log.Debug("termhost pane resized", "program", p.name(), "cols", s.W, "rows", s.H)
	return err
}

// Size returns the PTY size the child sees.
func (p *Pane) Size() Size {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scr.Size()
}

// SetFixedSize pins the PTY size (shared terminal, JB20): the child keeps it
// whatever the drawn size; Render crops a larger screen from the top-left and
// pads a smaller one. A zero size returns to following the drawn size.
func (p *Pane) SetFixedSize(s Size) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !s.valid() {
		p.fixed = Size{}
		if !p.closed && p.view.valid() {
			_ = p.resizePTYLocked(p.view)
		}
		return
	}
	p.fixed = s
	if !p.closed {
		_ = p.resizePTYLocked(s)
	}
}

// BracketedPaste reports whether the child enabled bracketed paste (?2004h).
func (p *Pane) BracketedPaste() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scr.BracketedPaste()
}

// SendKey sends a key press to the child. It only queues the key (the
// emulator encodes it on the pane's input goroutine), so it never waits for
// the pane's lock; keys sent after the child is gone are dropped.
func (p *Pane) SendKey(k tea.KeyPressMsg) {
	p.queueOp(func(s screen) { s.SendKey(k) })
}

// SendPaste pastes s (bracketed when the child asked for it).
func (p *Pane) SendPaste(s string) {
	p.queueOp(func(sc screen) { sc.SendPaste(s) })
}

// SendMouse forwards a mouse event at pane cell (x, y); ignored unless the
// child turned mouse reporting on.
func (p *Pane) SendMouse(m tea.MouseMsg, x, y int) {
	p.queueOp(func(s screen) { s.SendMouse(m, x, y) })
}

// SetFocus reports focus changes to the child (if it enabled focus events).
func (p *Pane) SetFocus(focused bool) {
	p.queueOp(func(s screen) { s.SetFocus(focused) })
}

// queueOp hands an input operation to inputLoop without blocking.
func (p *Pane) queueOp(op func(screen)) {
	select {
	case p.ops <- op:
	default:
		if p.dropped.Add(1) == 1 {
			p.log.Warn("termhost pane input dropped", "program", p.name())
		}
	}
}

// inputLoop applies queued input operations in order under the lock.
func (p *Pane) inputLoop() {
	for {
		select {
		case op := <-p.ops:
			p.mu.Lock()
			if !p.closed {
				op(p.scr)
			}
			p.mu.Unlock()
		case <-p.stopWriter:
			return
		}
	}
}

// Render returns exactly H lines of W cells (the drawn size) with SGR styles.
func (p *Pane) Render() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scr.RenderRegion(0, 0, p.view.W, p.view.H)
}

// Cursor returns the cursor cell; visible is false when the child hid it or
// it is outside the drawn area.
func (p *Pane) Cursor() (x, y int, visible bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	x, y, visible = p.scr.Cursor()
	if x < 0 || y < 0 || x >= p.view.W || y >= p.view.H {
		visible = false
	}
	return x, y, visible
}

// Title returns the child's OSC 0/2 title, sanitised for display.
func (p *Pane) Title() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.title
}

// MouseMode reports whether the child turned mouse reporting on.
func (p *Pane) MouseMode() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scr.MouseMode()
}

// Scrollback returns the lines scrolled off the main screen, oldest first.
func (p *Pane) Scrollback() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scr.Scrollback()
}

// Close ends the program: SIGHUP to its process group now, SIGKILL if it is
// still alive after 2 seconds. It returns at once; Done reports the exit.
func (p *Pane) Close() error {
	if p.closing.Swap(true) {
		return nil
	}
	select {
	case <-p.exited:
		return nil
	default:
	}
	err := p.proc.Signal(false)
	grace := closeGrace
	go func() {
		select {
		case <-p.exited:
		case <-time.After(grace):
			_ = p.proc.Signal(true)
		}
	}()
	return err
}

// Done receives one ExitInfo when the program has exited and the pane is torn
// down, then is closed (later receivers get the zero value).
func (p *Pane) Done() <-chan ExitInfo { return p.done }
