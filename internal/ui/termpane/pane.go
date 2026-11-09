// Package termpane is the screen of an embedded program (v0.1.3 A7.4–A7.7,
// P0 Task 16): a ui.Component over a running program (a termhost.Pane, or a
// test fake) that owns the keyboard while focused, the ctrl+\ prefix keys out
// of it, the scroll-back view, the shared badge and the exit title. Launch
// (launch.go) is how tools open programs; it builds this component for the
// pane mode.
package termpane

import (
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Handle is the running program a Pane shows: *termhost.Pane, or a fake.
type Handle interface {
	Program() string
	Resize(termhost.Size) error
	SendKey(tea.KeyPressMsg)
	SendPaste(string)
	SendMouse(tea.MouseMsg, int, int)
	SetFocus(bool)
	Render() string
	Cursor() (int, int, bool)
	Title() string
	MouseMode() bool
	Scrollback() []string
	Close() error
	Done() <-chan termhost.ExitInfo
	Shared() bool
}

var _ Handle = (*termhost.Pane)(nil)

// Options configures a Pane.
type Options struct {
	ToolID, InstanceID string
	// SingleCell: the tool is this one pane (lazygit), so ctrl+\ z is the
	// tool's wide view (tools.zoom). Otherwise z goes to OnPrefix (a
	// workspace cell zooms itself).
	SingleCell bool
	// OnPrefix gets the tool's own keys after ctrl+\ (ToolSpec.PrefixKeys)
	// and, without SingleCell, z.
	OnPrefix func(ctx *ui.Context, key string) tea.Cmd
	// OnExit runs once when the program ends, in pane mode only: the Pane
	// calls it when ExitedMsg reaches its Update. exec and window have no Pane;
	// there (and in pane mode too) the tool gets the broadcast ExitedMsg —
	// handle that for anything that must happen in every mode.
	OnExit func(ctx *ui.Context, info termhost.ExitInfo) tea.Cmd
}

// DirtyMsg says the program's screen changed (posted coalesced as
// "pane:"+InstanceID by the Launch wiring); receiving it is enough to redraw.
type DirtyMsg struct{ InstanceID string }

// ExitedMsg is broadcast once when instance InstanceID's program ends, in
// every open mode (pane: posted by the Pane; exec, window: by Launch).
type ExitedMsg struct {
	InstanceID string
	Info       termhost.ExitInfo
}

// Pane is the component.
type Pane struct {
	h      Handle
	opts   Options
	prefix *Prefix
	extra  []ui.PrefixKey // the tool's own prefix keys (help)

	grab    bool // the program has the keyboard while the pane is focused
	exited  bool
	info    termhost.ExitInfo
	started bool // the exit watcher runs

	size ui.Size       // inner size from the app
	sent termhost.Size // last size given to the program
	tiny bool          // drawn without a border (tiny mode)

	// Focus reports (syncFocus): the app's focus is on this pane's tool
	// instance (ui.FocusChangedMsg), the tool's own focus is on this cell
	// (SetCellFocus), the terminal has focus (tea.FocusMsg/BlurMsg).
	appFocus, cellFocus, termBlur bool
	sentFocus                     int // 0 = never sent, 1 = false, 2 = true

	scroll scrollView
}

var (
	_ ui.Component    = (*Pane)(nil)
	_ ui.KeyHandler   = (*Pane)(nil)
	_ ui.MouseHandler = (*Pane)(nil)
	_ ui.DragHandler  = (*Pane)(nil)
	_ ui.Cursorer     = (*Pane)(nil)
	_ ui.Titled       = (*Pane)(nil)
	_ ui.Helper       = (*Pane)(nil)
	_ ui.KeyGrabber   = (*Pane)(nil)
	_ ui.StatusTexter = (*Pane)(nil)
)

// New is the screen of h. The tool's registered PrefixKeys (opts.ToolID) are
// accepted after ctrl+\ and listed in the pane help.
func New(h Handle, opts Options) *Pane {
	var extra []ui.PrefixKey
	if s, ok := ui.ToolByID(opts.ToolID); ok {
		extra = s.PrefixKeys
	}
	keys := make([]string, len(extra))
	for i, k := range extra {
		keys[i] = k.Key
	}
	// A pane opens where the user is (focused) until the app says otherwise.
	return &Pane{h: h, opts: opts, prefix: NewPrefix(keys), extra: extra, grab: true, appFocus: true, cellFocus: true}
}

// Handle is the program the pane shows.
func (p *Pane) Handle() Handle { return p.h }

// Exited reports whether the program ended, and how.
func (p *Pane) Exited() (termhost.ExitInfo, bool) { return p.info, p.exited }

// GrabsKeys implements ui.KeyGrabber: while the program runs and the user has
// not left it (ctrl+\ esc), every key goes to it.
func (p *Pane) GrabsKeys() bool { return !p.exited && p.grab }

// Init starts the exit watcher (once; Launch already started it for its
// views): when the program ends, ExitedMsg is posted.
func (p *Pane) Init(ctx *ui.Context) tea.Cmd {
	if ctx != nil && ctx.Post != nil {
		p.watchExit(ctx.Post)
	}
	return nil
}

// watchExit posts ExitedMsg through post when the program ends; only the
// first call starts the watcher.
func (p *Pane) watchExit(post ui.Poster) {
	if p.started {
		return
	}
	p.started = true
	id, done := p.opts.InstanceID, p.h.Done()
	if done == nil {
		return
	}
	go func() {
		info, ok := <-done
		if ok {
			post.Post(ExitedMsg{InstanceID: id, Info: info})
		}
	}()
}

// Update implements ui.Component.
func (p *Pane) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case ExitedMsg:
		if msg.InstanceID != p.opts.InstanceID || p.exited {
			return nil
		}
		p.exited, p.info = true, msg.Info
		p.prefix.Cancel()
		p.scroll.end()
		if p.opts.OnExit != nil {
			return p.opts.OnExit(ctx, msg.Info)
		}
	case tea.PasteMsg: // the app gives pastes to the focused component only
		if p.GrabsKeys() && !p.scroll.on {
			p.h.SendPaste(msg.Content)
		}
	case ui.FocusChangedMsg: // the pane is hidden or shown, focused or not
		p.appFocus = msg.RouteKey == p.routeKey()
		p.syncFocus()
	case tea.BlurMsg:
		p.termBlur = true
		p.syncFocus()
	case tea.FocusMsg:
		p.termBlur = false
		p.syncFocus()
	default:
		p.prefix.Update(msg)
	}
	return nil
}

// routeKey is the route of the tool instance the pane belongs to (the key
// ui.FocusChangedMsg names when the app focuses it).
func (p *Pane) routeKey() string {
	return ui.Route{Kind: ui.RouteTool, ID: p.opts.InstanceID, Arg: p.opts.ToolID}.Key()
}

// SetCellFocus is for a tool whose instance shows several cells (a workspace:
// tree, code, terminal): it tells the pane whether the tool's own focus is on
// it. A pane that is the whole instance never needs it (default true).
func (p *Pane) SetCellFocus(on bool) {
	p.cellFocus = on
	p.syncFocus()
}

// syncFocus tells the program whether it has the keyboard (focus reports).
// It runs from Update and the key/mouse handlers, never from View.
func (p *Pane) syncFocus() {
	if p.exited {
		return
	}
	on := p.appFocus && p.cellFocus && p.grab && !p.termBlur
	v := 1
	if on {
		v = 2
	}
	if v != p.sentFocus {
		p.sentFocus = v
		p.h.SetFocus(on)
	}
}

// header is the rows above the program: tiny mode has no border, so the exit
// title gets a line of its own there once the program ended (it no longer
// needs the room). The shared mark in tiny mode is on the tab line (Title's
// badge), so the program's size never depends on sharing.
func (p *Pane) header() int {
	if p.tiny && p.exited {
		return 1
	}
	return 0
}

// applySize gives the program the pane's inner size (less the header). A
// size with no room (never sent by the app, but a host may) is not passed on:
// the program keeps its last size.
func (p *Pane) applySize() {
	if p.exited {
		return
	}
	s := termhost.Size{W: p.size.W, H: p.size.H - p.header()}
	if s.W <= 0 || s.H <= 0 || s == p.sent {
		return
	}
	p.sent = s
	_ = p.h.Resize(s)
}

// Resize implements ui.Component.
func (p *Pane) Resize(ctx *ui.Context, size ui.Size) {
	p.size = size
	p.tiny = ctx != nil && ctx.Mode == layout.ModeTiny
	p.applySize()
}

// View implements ui.Component. It only reads: the program's size comes from
// Resize and its focus from Update (focused is the app's frame highlight).
func (p *Pane) View(ctx *ui.Context, _ bool) string {
	w, h := p.size.W, p.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	var lines []string
	if p.header() > 0 {
		lines = append(lines, ctx.Theme.S.Title.Render(p.headerLine(ctx, w)))
	}
	body := h - len(lines)
	if p.scroll.on {
		lines = append(lines, p.scroll.window(scrollLines(p.h), body)...)
	} else {
		lines = append(lines, strings.Split(p.h.Render(), "\n")...)
	}
	return strings.Join(textwidth.Fit(lines, w, h), "\n")
}

// headerLine is the tiny-mode exit line: the title, cut first, then the exit
// note, never cut.
func (p *Pane) headerLine(ctx *ui.Context, w int) string {
	return joinTail(p.baseTitle(), ctx.T.Termpane.Exited(p.info.Code), w)
}

// joinTail is "<title> · <tail>" in w cells: the title is cut, the tail kept.
func joinTail(title, tail string, w int) string {
	sep := " · "
	room := w - textwidth.Width(tail) - textwidth.Width(sep)
	if room < 2 || title == "" {
		return textwidth.Truncate(tail, max(0, w), "")
	}
	return textwidth.Truncate(title, room, "…") + sep + tail
}

// baseTitle is the program's own title (sanitized), else its name.
func (p *Pane) baseTitle() string {
	if t := textwidth.Sanitize(p.h.Title()); t != "" {
		return t
	}
	return p.program()
}

// program is the program's base name ("lazygit"), sanitized for drawing.
func (p *Pane) program() string { return textwidth.Sanitize(filepath.Base(p.h.Program())) }

// Title implements ui.Titled: "<title> · <shared badge>" while shared (the
// title is cut first so the badge always shows), "<title> · 끝남 · 종료 코드 n"
// once the program ended. In tiny mode (no border) sharing is a dot badge
// instead, which the app puts on the tab line's tool cell (A7.7).
func (p *Pane) Title(ctx *ui.Context) (string, ui.Badge) {
	title := p.baseTitle()
	switch {
	case p.exited:
		return title + " · " + ctx.T.Termpane.Exited(p.info.Code), ui.Badge{}
	case p.h.Shared() && ctx.Mode == layout.ModeTiny:
		return title, ui.Badge{Dot: true}
	case p.h.Shared():
		// The frame shows at most inner width - 4 cells of title; the tool
		// surface's context line adds the tool icon and " ▾" too.
		return joinTail(title, ctx.T.Termpane.SharedBadge(), max(1, p.size.W-6)), ui.Badge{}
	}
	return title, ui.Badge{}
}

// escapeKey is the prefix key's label (ctrl+\ unless [keys] changed it).
func escapeKey(ctx *ui.Context) string {
	return ctx.Keys.Binding(keymap.TermhostEscape).Help().Key
}

// StatusText implements ui.StatusTexter: always shown while the pane has the
// keyboard, so the user knows where the keys go and how to leave (A7.9).
func (p *Pane) StatusText(ctx *ui.Context) (string, bool) {
	switch {
	case p.exited:
		return "", false
	case p.prefix.Active():
		return ctx.T.Termpane.PrefixHint(escapeKey(ctx)), true
	case p.scroll.on:
		return ctx.T.Termpane.ScrollHint(), true
	case p.grab:
		return ctx.T.Termpane.InputHint(p.program(), escapeKey(ctx)), true
	}
	return "", false
}

// Help implements ui.Helper (status hints while the pane does not have the
// keyboard: enter gives it back).
func (p *Pane) Help(ctx *ui.Context) []key.Binding {
	if p.exited {
		return nil
	}
	if !p.grab {
		return []key.Binding{key.NewBinding(key.WithKeys("enter", "i"), key.WithHelp("enter", ctx.T.Termpane.EnterHint()))}
	}
	return ctx.Keys.Help(keymap.TermhostEscape)
}

// Cursor implements ui.Cursorer: the program's cursor while it has the
// keyboard.
func (p *Pane) Cursor() *tea.Cursor {
	if !p.GrabsKeys() || p.scroll.on {
		return nil
	}
	x, y, ok := p.h.Cursor()
	y += p.header()
	if !ok || x < 0 || y < 0 || x >= p.size.W || y >= p.size.H {
		return nil
	}
	return tea.NewCursor(x, y)
}

// HandleKey implements ui.KeyHandler.
func (p *Pane) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if p.exited {
		return false, nil
	}
	if !p.grab { // left with ctrl+\ esc: masil's keys, enter (or i) goes back in
		switch msg.String() {
		case "enter", "i":
			p.grab = true
			p.syncFocus()
			return true, nil
		}
		return false, nil
	}
	if p.scroll.on && !p.prefix.Active() && !ctx.Keys.MatchesEscape(msg) {
		all := scrollLines(p.h)
		p.scroll.key(msg.String(), len(all), p.size.H-p.header())
		return true, nil
	}
	r := p.prefix.Key(ctx, msg)
	switch r.Action {
	case PrefixNone:
		if p.scroll.on {
			p.scroll.end()
		}
		p.h.SendKey(msg)
		return true, nil
	case PrefixArm:
		return true, r.Cmd
	case PrefixLeave:
		p.grab = false
		p.scroll.end()
		p.syncFocus()
		return true, nil
	case PrefixFocus:
		return true, shell(focusActions[r.Dir])
	case PrefixZoom:
		if p.opts.SingleCell {
			return true, func() tea.Msg { return ui.ToolSurfaceMsg{Action: ui.ToolSurfaceZoom} }
		}
		return true, p.onPrefix(ctx, "z")
	case PrefixPalette:
		return true, shell(ui.ShellPalette)
	case PrefixSurface:
		return true, func() tea.Msg { return ui.ToolSurfaceMsg{Action: ui.ToolSurfaceToggle} }
	case PrefixScroll:
		p.scroll.start()
		return true, nil
	case PrefixKill:
		if ui.KillSwitchActive() { // sharing is cut first, without asking (B8.5)
			return true, ui.KillAll(ctx)
		}
		return true, ui.OpenOverlay(p.exitConfirm(ctx))
	case PrefixHelp:
		return true, ui.OpenOverlay(newHelpOverlay(ctx, p.extra))
	case PrefixLiteral:
		p.h.SendKey(literal(msg))
		return true, nil
	case PrefixExtra:
		return true, p.onPrefix(ctx, r.Key)
	}
	return true, nil // PrefixCancel: dropped
}

func (p *Pane) onPrefix(ctx *ui.Context, k string) tea.Cmd {
	if p.opts.OnPrefix == nil {
		return nil
	}
	return p.opts.OnPrefix(ctx, k)
}

// literal is the prefix key to send to the program: any spelling of ctrl+\
// (ctrl+4, the raw 0x1c) goes as ctrl+\; a rebound prefix key goes as is.
func literal(msg tea.KeyPressMsg) tea.KeyPressMsg {
	if msg.Code == 0x1c || (msg.Mod == tea.ModCtrl && (msg.Code == '\\' || msg.Code == '4')) {
		return tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl}
	}
	return msg
}

var focusActions = map[string]ui.ShellAction{
	"h": ui.ShellFocusLeft, "j": ui.ShellFocusDown, "k": ui.ShellFocusUp, "l": ui.ShellFocusRight,
}

func shell(a ui.ShellAction) tea.Cmd {
	return func() tea.Msg { return ui.ShellActionMsg{Action: a} }
}

// exitConfirm asks before ending the program (ctrl+\ x without sharing).
func (p *Pane) exitConfirm(ctx *ui.Context) ui.Overlay {
	t := ctx.T.Termpane
	h := p.h
	return widgets.Confirm(t.ExitTitle(), t.ExitConfirm(p.program()), t.ExitYes(), t.ExitNo(), func() tea.Msg {
		_ = h.Close() // SIGHUP now, SIGKILL after 2s; the end arrives as ExitedMsg
		return nil
	})
}

// HandleMouse implements ui.MouseHandler: a program in mouse mode gets the
// mouse at pane coordinates; otherwise the wheel scrolls back and a click
// gives the program the keyboard again.
func (p *Pane) HandleMouse(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if p.exited {
		return nil
	}
	_, click := msg.(tea.MouseClickMsg)
	if click && !p.grab {
		p.grab = true
		p.syncFocus()
	}
	if p.h.MouseMode() && !p.scroll.on {
		if y := local.Y - p.header(); y >= 0 {
			p.h.SendMouse(msg, local.X, y)
		}
		return nil
	}
	if wh, ok := msg.(tea.MouseWheelMsg); ok {
		all := scrollLines(p.h)
		switch wh.Button {
		case tea.MouseWheelUp:
			p.scroll.by(wheelStep, len(all), p.size.H-p.header())
		case tea.MouseWheelDown:
			p.scroll.by(-wheelStep, len(all), p.size.H-p.header())
		}
	}
	return nil
}

// HandleDrag implements ui.DragHandler: in mouse mode the program also gets
// the motion and the release of a press it got (clamped into the pane).
func (p *Pane) HandleDrag(ctx *ui.Context, msg tea.MouseMsg, local ui.Point) tea.Cmd {
	if p.exited || !p.h.MouseMode() || p.scroll.on {
		return nil
	}
	x := min(max(0, local.X), max(0, p.size.W-1))
	y := min(max(0, local.Y-p.header()), max(0, p.size.H-p.header()-1))
	p.h.SendMouse(msg, x, y)
	return nil
}

// ClearDrag implements ui.DragHandler (the pane keeps no selection).
func (p *Pane) ClearDrag() {}

// ---- the pane help (ctrl+\ ?) ----

// helpOverlay lists the prefix keys and the keyboard limits (A7.5, A7.6).
type helpOverlay struct {
	lines []string
	size  ui.Size
	title string
}

func newHelpOverlay(ctx *ui.Context, extra []ui.PrefixKey) *helpOverlay {
	t := ctx.T.Termpane
	k := escapeKey(ctx)
	rows := [][2]string{
		{k + " esc", t.HelpLeave()},
		{k + " h/j/k/l", t.HelpFocus()},
		{k + " z", t.HelpZoom()},
		{k + " p", t.HelpPalette()},
		{k + " t", t.HelpSurface()},
		{k + " [", t.HelpScroll()},
		{k + " x", t.HelpKill()},
		{k + " ?", t.HelpHelp()},
		{k + " " + k, t.HelpLiteral()},
	}
	for _, e := range extra {
		if e.Help != nil {
			rows = append(rows, [2]string{k + " " + e.Key, e.Help(ctx.T)})
		}
	}
	kw := 0
	for _, r := range rows {
		kw = max(kw, textwidth.Width(r[0]))
	}
	var lines []string
	for _, r := range rows {
		lines = append(lines, textwidth.PadRight(r[0], kw)+"  "+textwidth.Sanitize(r[1]))
	}
	lines = append(lines, "", t.KbdLimits(), t.KbdNvim(k), "", t.HelpClose())
	return &helpOverlay{lines: lines, title: t.HelpTitle()}
}

var (
	_ ui.Overlay    = (*helpOverlay)(nil)
	_ ui.KeyHandler = (*helpOverlay)(nil)
	_ ui.Titled     = (*helpOverlay)(nil)
)

func (o *helpOverlay) Init(*ui.Context) tea.Cmd            { return nil }
func (o *helpOverlay) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (o *helpOverlay) Resize(_ *ui.Context, s ui.Size)     { o.size = s }
func (o *helpOverlay) Modal() bool                         { return true }
func (o *helpOverlay) Title(*ui.Context) (string, ui.Badge) {
	return o.title, ui.Badge{}
}

// Bounds is a box wide enough for the rows (with the border), centered.
func (o *helpOverlay) Bounds(body layout.Rect) layout.Rect {
	w := 0
	for _, l := range o.lines {
		w = max(w, textwidth.Width(l))
	}
	w = min(body.W, w+4)
	h := min(body.H, len(o.lines)+2)
	return layout.Rect{X: body.X + max(0, (body.W-w)/2), Y: body.Y + max(0, (body.H-h)/2), W: w, H: h}
}

func (o *helpOverlay) View(*ui.Context, bool) string {
	lines := make([]string, len(o.lines))
	for i, l := range o.lines {
		if l != "" {
			l = " " + l
		}
		lines[i] = l
	}
	return strings.Join(textwidth.Fit(lines, o.size.W, o.size.H), "\n")
}

// HandleKey: any key closes the help.
func (o *helpOverlay) HandleKey(*ui.Context, tea.KeyPressMsg) (bool, tea.Cmd) {
	return true, func() tea.Msg { return ui.CloseOverlayMsg{} }
}
