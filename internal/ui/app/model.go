// Package app is the root tea.Model: shell, navigation, focus, overlays, status bar (CA 5.4).
package app

import (
	"log/slog"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Options configures the app.
type Options struct {
	Paths           config.Paths
	Config          *config.Config
	Credentials     *auth.Credentials // nil => login screen
	InitialRoute    *ui.Route         // `masil join <code>` => RouteInvite
	Now             func() time.Time
	Loc             *time.Location
	DisableRealtime bool             // tests
	Notifier        *notify.Notifier // nil => notify.New(NewOSBackend("masil"), OpenLedger(paths), log)
	API             *api.Client      // nil => api.New(config.ServerURL(), WithToken(...))
	Timers          ui.Timers        // nil => ui.RealTimers(); uitest passes its FakeClock
}

// Model implements tea.Model.
type Model struct {
	ctx      *ui.Context
	opts     Options
	creds    *auth.Credentials
	notifier *notify.Notifier
	bp       layout.Breakpoints
	pin      *layout.Mode // runtime pin (nil = auto)
	pinFell  bool         // the pin did not fit this frame (Resolve fell back to auto)

	nav       NavState    // logical navigation (CA 5.3.3); projected onto the layout every frame
	intent    FocusIntent // remembered focus intent (CA 5.3.5)
	focus     FocusIntent // focus resolved for the current layout
	sentFocus FocusIntent // focus of the last FocusChangedMsg broadcast

	prefix    prefixState // pending ctrl+w prefix (CA 5.3.4 2.2)
	quitArmed bool        // the first ctrl+c armed quitting (CA 5.5 app.quit)
	quitGen   int         // generation of the pending quitArmMsg

	cols, rows int
	sized      bool // a WindowSizeMsg arrived
	laidOut    bool // relayout ran at least once
	lay        layout.Layout
	routes     map[layout.SlotID]ui.Route // Project(lay, nav)

	cache *instanceCache
	panes map[layout.SlotID]*instance // pane-factory slots, built once
	tabs  *widgets.Tabs               // SlotTabs bar (Hit for clicks)

	initCmds []tea.Cmd // emitted by Init (New cannot return Cmds)
	frame    FrameInfo // geometry of the last View

	overlays []ui.Overlay                                     // open overlays, bottom -> top (drawing: Task 28)
	palette  ui.Overlay                                       // the palette instance opened by palette.open (nil = none)
	openPal  func(ctx *ui.Context, initial string) ui.Overlay // palette.Open (tests swap it)

	toasts    toastState // queued toasts (newest last) and the drawn box
	tickArmed bool       // a statusTickMsg is pending
	tickGen   int        // generation of the pending statusTickMsg
}

var _ tea.Model = (*Model)(nil)

// New builds the app model. The Context is initialized in a fixed order so that the
// harness and Run take the same path (plan 03 Task 25).
func New(o Options) *Model {
	m := &Model{
		opts:    o,
		creds:   o.Credentials,
		nav:     newNavState(),
		intent:  FocusIntent{Slot: layout.SlotMain},
		cache:   newInstanceCache(),
		panes:   map[layout.SlotID]*instance{},
		tabs:    widgets.NewTabs(),
		routes:  map[layout.SlotID]ui.Route{},
		openPal: palette.Open,
	}

	// ① Config.
	cfg := o.Config
	if cfg == nil {
		cfg = config.Defaults()
	}
	// ② Theme (a~d aliases, case-insensitive; unknown => default even past config.Load).
	name, ok := theme.Parse(cfg.Theme)
	if !ok {
		name = theme.Default
	}
	// ③ Keys ([keys] overrides; Load always returns a usable keymap).
	keys, errs := keymap.Load(cfg.Keys)
	if len(errs) > 0 {
		for _, e := range errs {
			slog.Warn("keymap override ignored", "err", e)
		}
		m.initCmds = append(m.initCmds, ui.Toast("키 설정 일부를 무시했어요", ui.ToastError))
	}
	// ④ Terminal capabilities.
	caps := termcaps.Detect(os.Environ(), cfg.UI.Images)
	// ⑤ Clock, location, timers.
	now := o.Now
	if now == nil {
		now = time.Now
	}
	loc := o.Loc
	if loc == nil {
		loc = time.Local
	}
	timers := o.Timers
	if timers == nil {
		timers = ui.RealTimers()
	}
	// ⑥ API (same clock as the UI so cooldowns compare deterministically).
	client := o.API
	if client == nil {
		opts := []api.Option{api.WithClock(now)}
		if o.Credentials != nil && o.Credentials.Token != "" {
			opts = append(opts, api.WithToken(o.Credentials.Token))
		}
		client = api.New(cfg.ServerURL(), opts...)
	}
	m.notifier = o.Notifier
	if m.notifier == nil {
		m.notifier = notify.New(notify.NewOSBackend("masil"), notify.OpenLedger(o.Paths), slog.Default())
	}

	m.ctx = &ui.Context{
		Theme:  theme.Get(name),
		Mode:   layout.ModeTooSmall,
		Keys:   keys,
		Store:  store.New(),
		API:    client,
		RT:     noopRealtime{state: realtime.State{Phase: realtime.PhaseLive}},
		Config: cfg,
		Paths:  o.Paths,
		Caps:   caps,
		Now:    now,
		Timers: timers,
		Loc:    loc,
	}

	m.bp = layout.FromConfig(cfg.Layout)
	if pm, ok := layout.ParseMode(cfg.Layout.Pin); ok && pm != layout.ModeTooSmall {
		m.pin = &pm
	}
	if o.InitialRoute != nil {
		m.nav, m.intent = m.nav.navigate(ui.NavigateMsg{Route: *o.InitialRoute}, m.ctx.Mode)
		if m.intent.Slot == "" {
			m.intent = FocusIntent{Slot: layout.SlotMain}
		}
	}
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	cmds := m.initCmds
	m.initCmds = nil
	return tea.Batch(cmds...)
}

// Update implements tea.Model (CA 5.3.4). After every message the focus is resolved
// (and a change broadcast) and the status-line countdown tick is (re)armed when a
// focus session or a 429 cooldown is showing.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.dispatch(msg)
	// The focus slot is resolved at the end of every Update (CA 5.3.5); a change is
	// broadcast as ui.FocusChangedMsg after this Update's relayout/projection.
	return m, tea.Batch(cmd, m.syncFocus(), m.armStatusTick())
}

// dispatch handles one message (CA 5.3.4 steps).
func (m *Model) dispatch(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusTickMsg:
		if msg.gen == m.tickGen {
			m.tickArmed = false
		}
		return m, nil
	case ui.ToastMsg:
		return m, m.toasts.push(m.ctx, msg)
	case ui.ErrorMsg:
		return m, m.toasts.push(m.ctx, errorToast(msg))
	case toastExpireMsg:
		m.toasts.remove(msg.id)
		return m, nil
	case quitArmMsg:
		if msg.gen == m.quitGen {
			m.quitArmed = false
		}
		return m, nil
	case pinMsg:
		return m, m.setPin(msg.mode)
	case tea.MouseMsg:
		return m, m.routeMouse(msg)
	case tea.WindowSizeMsg:
		m.cols, m.rows, m.sized = msg.Width, msg.Height, true
		return m, m.relayout()
	case tea.ModeReportMsg:
		if msg.Mode == ansi.ModeUnicodeCore {
			m.ctx.Caps.UnicodeCore = msg.Value.IsSet()
		}
		return m, nil
	case ui.NavigateMsg:
		return m, m.applyNavigate(msg)
	case ui.BackMsg:
		return m, m.goBack()
	case tea.KeyPressMsg:
		return m, m.routeKey(msg)
	case tea.PasteMsg:
		if n := len(m.overlays); n > 0 { // CA 5.3.4 step 4: never below an open overlay
			return m, m.overlays[n-1].Update(m.ctx, msg)
		}
		if in := m.focusedInstance(); in != nil {
			return m, in.comp.Update(m.ctx, msg)
		}
		return m, nil
	case ui.EventMsg:
		m.ctx.Store.Apply(msg.Event)
		return m, m.broadcast(msg)
	}
	return m, m.broadcast(msg)
}

// broadcast sends msg to every live component: route instances (creation order),
// then pane-factory components (CA 5.3.4 steps 5–6).
func (m *Model) broadcast(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, in := range m.cache.all() {
		cmds = append(cmds, in.comp.Update(m.ctx, msg))
	}
	for _, s := range factorySlots {
		if in := m.panes[s]; in != nil {
			cmds = append(cmds, in.comp.Update(m.ctx, msg))
		}
	}
	return tea.Batch(cmds...)
}

// relayout is CA 5.3.4 step 1: Resolve → Compute(…, nav.LayoutState(mode)) → Project →
// Resize the placed instances whose inner size (or mode) changed → ModeChangedMsg when
// the mode changed. New instances' Init Cmds are returned with it.
func (m *Model) relayout() tea.Cmd {
	if !m.sized {
		return nil
	}
	prev := m.lay.Mode
	mode, fell := layout.Resolve(m.cols, m.rows, m.bp, m.pin)
	l := layout.Compute(mode, m.cols, m.rows, m.nav.LayoutState(mode))
	routes := Project(l, m.nav)
	m.lay, m.routes, m.pinFell = l, routes, fell
	m.ctx.Mode, m.ctx.Layout = l.Mode, l

	var cmds []tea.Cmd
	m.cache.tick++
	pinned := map[string]bool{}
	for _, k := range m.nav.Keys() {
		pinned[k] = true
	}
	for _, p := range l.Panes {
		var in *instance
		var cmd tea.Cmd
		if r, ok := routes[p.Slot]; ok {
			in, cmd = m.cache.get(m.ctx, r)
			in.lastSeen = m.cache.tick
			pinned[r.Key()] = true
		} else if isFactorySlot(p.Slot) {
			in, cmd = m.pane(p.Slot)
		} else {
			continue
		}
		cmds = append(cmds, cmd)
		size := innerSize(p)
		if !in.sized || in.size != size || in.mode != l.Mode {
			in.size, in.sized, in.mode = size, true, l.Mode
			in.comp.Resize(m.ctx, size)
		}
	}
	m.cache.evict(pinned)
	m.focus = resolveFocus(m.intent, l, routes)
	if m.laidOut && prev != l.Mode {
		cmds = append(cmds, m.broadcast(ui.ModeChangedMsg{From: prev, To: l.Mode}))
	}
	m.laidOut = true
	return tea.Batch(cmds...)
}

// innerSize is the size a pane's component draws in: the inner rect when bordered.
func innerSize(p layout.Pane) ui.Size {
	r := p.Rect
	if p.Bordered {
		r = r.Inner()
	}
	return ui.Size{W: r.W, H: r.H}
}

// slotInstance returns the component placed in slot this frame, or nil.
func (m *Model) slotInstance(s layout.SlotID) *instance {
	if r, ok := m.routes[s]; ok {
		return m.cache.lookup(r.Key())
	}
	if isFactorySlot(s) {
		if _, ok := m.lay.Pane(s); ok {
			return m.panes[s]
		}
	}
	return nil
}

// focusedInstance is the component in the resolved focus slot, or nil.
func (m *Model) focusedInstance() *instance {
	return m.slotInstance(m.focus.Slot)
}

// View implements tea.Model: the composed frame plus the per-frame flags (CA 5.4).
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeNone
	if m.ctx.Config.UI.Mouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	v.Cursor = m.cursor()
	v.WindowTitle = "masil · " + m.currentTitle()
	if m.ctx.Config.UI.PaintBackground {
		v.BackgroundColor = m.ctx.Theme.P.BG
		v.ForegroundColor = m.ctx.Theme.P.FG
	}
	return v
}

// cursor is the real cursor (CA 5.4): with an overlay open it is the top overlay's
// (Task 28 adds the overlay origin; until then nil, never the pane cursor below);
// otherwise the focused component's Cursorer result plus its pane's inner origin.
func (m *Model) cursor() *tea.Cursor {
	if len(m.overlays) > 0 || !m.sized || m.lay.Mode == layout.ModeTooSmall {
		return nil
	}
	cr, ok := m.focusedComponent().(ui.Cursorer)
	if !ok {
		return nil
	}
	c := cr.Cursor()
	p, has := m.lay.Pane(m.focus.Slot)
	if c == nil || !has {
		return nil
	}
	inner := p.Rect
	if p.Bordered {
		inner = inner.Inner()
	}
	out := *c
	out.X += inner.X
	out.Y += inner.Y
	return &out
}

// Context returns the shared ui.Context (tests).
func (m *Model) Context() *ui.Context {
	return m.ctx
}

// Nav returns the current logical navigation state (tests, CA 5.3.3).
func (m *Model) Nav() NavState {
	return m.nav.clone()
}

// Instance returns the cached route instance or nil (tests).
func (m *Model) Instance(routeKey string) ui.Component {
	if in := m.cache.lookup(routeKey); in != nil {
		return in.comp
	}
	return nil
}

// Focus returns the resolved focus slot + route key this frame (tests).
func (m *Model) Focus() FocusIntent {
	return m.focus
}

// Frame returns the geometry of the last View() (tests, CA 10.2 AssertIntegrity). The
// frame is re-rendered first, so it also reflects messages sent since that View (a
// real program renders after every Update; the harness does not).
func (m *Model) Frame() FrameInfo {
	m.render()
	return m.frame
}

// FocusIntent is the remembered focus target.
type FocusIntent struct {
	Slot     layout.SlotID
	RouteKey string
}

// FrameInfo describes the last drawn frame.
type FrameInfo struct {
	Layout   layout.Layout
	Routes   map[layout.SlotID]ui.Route // Project result
	Overlays []Box                      // bottom -> top, final clamped rects (CA 5.3.7)
	Toast    *Box                       // nil when no toast box is drawn (or tiny: shown in status line)
}

// Box is a drawn rectangle.
type Box struct {
	Rect     layout.Rect
	Bordered bool
}
