// Package app is the root tea.Model: shell, navigation, focus, overlays, status bar (CA 5.4).
package app

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/clipboard"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/authui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	_ "github.com/patrache/masil/internal/ui/settings" // registers RouteSettings (CA 2.2)
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
	Clipboard       clipboard.Writer // nil => clipboard.System(); uitest passes a clipboard.Recorder
	Locale          *i18n.Resolution // nil => i18n.Resolve(i18n.DetectOS() + [ui] language)
	Secrets         *secrets.Store   // nil => secrets.Open(Paths) with this OS's store (run.go)
	NestedPane      bool             // inside a masil pane (MASIL_PANE=1, read by Run only): a start warning
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
	hist      history     // places Main showed (CA 5.3.3 "기록"), recorded after every Update
	intent    FocusIntent // remembered focus intent (CA 5.3.5)
	focus     FocusIntent // focus resolved for the current layout
	sentFocus FocusIntent // focus of the last FocusChangedMsg broadcast

	prefix    prefixState // pending ctrl+w prefix (CA 5.3.4 2.2) or ctrl+\ while sharing
	escGen    int         // generation of the pending escapeArmMsg
	quitArmed bool        // the first ctrl+c armed quitting (CA 5.5 app.quit)
	quitGen   int         // generation of the pending quitArmMsg
	quitAsk   ui.Overlay  // the open "panes still run" quit question (quit.go), nil = none

	cols, rows int
	sized      bool // a WindowSizeMsg arrived
	laidOut    bool // relayout ran at least once
	lay        layout.Layout
	routes     map[layout.SlotID]ui.Route // Project(lay, nav)

	cache *instanceCache
	panes map[layout.SlotID]*instance // pane-factory slots, built once
	tabs  *widgets.Tabs               // SlotTabs bar (Hit for clicks)

	grab      dragGrab       // the pane following the held left button (mouse.go)
	dragOwner ui.DragHandler // the component a press last grabbed (its selection is cleared by a press elsewhere)

	initCmds []tea.Cmd // emitted by Init (New cannot return Cmds)
	frame    FrameInfo // geometry of the last View

	overlays []ui.Overlay                                     // open overlays, bottom -> top (CA 5.3.7)
	ovSizes  []ui.Size                                        // inner size last given to overlays[i].Resize
	ovSized  []bool                                           // overlays[i] has been Resize'd
	palette  ui.Overlay                                       // the palette instance opened by palette.open (nil = none)
	openPal  func(ctx *ui.Context, initial string) ui.Overlay // palette.Open (tests swap it)

	toasts    toastState // queued toasts (newest last) and the drawn box
	tickArmed bool       // a statusTickMsg is pending
	tickGen   int        // generation of the pending statusTickMsg

	// Session (Task 30).
	login      *ui.Route   // non-nil: the login screen is shown instead of the shell (logged out or RouteLogin forced)
	loggingOut bool        // the logout command's POST /v1/auth/logout is in flight
	unauthFlag atomic.Bool // set by api.WithOnUnauthorized (HTTP goroutine), taken at the start of Update
	// upgradeFlag is set by api.WithOnUpgradeRequired (HTTP goroutine).
	upgradeFlag     atomic.Bool
	upgradeRequired bool // 426 / close 4010 seen: status "✕ 업데이트 필요"

	// Realtime bridge (Task 30, bridge.go).
	send         func(tea.Msg)              // tea.Program.Send (Run); nil = no realtime, hooks only set flags
	q            atomic.Pointer[eventQueue] // non-blocking delivery to send (deliver.go); nil until a sender exists
	rtDial       dialFunc                   // tests: scripted socket (nil = coder/websocket)
	rt           *realtime.Client
	rtRun        *rtRun
	rtCancel     context.CancelFunc
	rtGen        int                  // generation of the current realtime run (readyTimeoutMsg)
	resume       realtime.ResumePoint // position the next run resumes from
	offRT        ui.Realtime          // the disabled no-op restored when realtime stops
	readyThisRun bool                 // ready received on the current run
	observe      func(tea.Msg)        // tests: sees every broadcast msg
	excl         execWatch            // exec-mode alerts and the return toast (exclusive.go)

	// Bootstrap / resync (Task 30, sync.go).
	booting       bool // a bootstrap is in flight
	bootAgain     bool // another resync was asked for while booting
	bootGen       int  // generation of the latest bootstrap (bootstrapDoneMsg / bootstrapRetryMsg)
	resyncOnReady bool // the socket was late: resync once more on the next ready

	// Idle and watch (Task 30, bridge.go).
	lastInput time.Time
	idle      bool
	idleArmed bool
	idleGen   int
	watchGen  int
	watched   []string // topics of the last Watch
	watchSent bool

	// Alerts (Task 31, alerts.go).
	termFocused bool     // the last focus report was FocusMsg (meaningful once Caps.FocusReports)
	summaries   []string // focus-summary alert ids opened in this process, oldest first (last 50)

	// state.json (Task 32, state.go).
	lastSpaceID string // space remembered from state.json (savedState falls back to it)
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
	// Without credentials (login screen) the client is built the same way, tokenless;
	// authui.LoggedInMsg sets the token. The hooks only set flags (HTTP goroutine).
	client := o.API
	if client == nil {
		opts := []api.Option{api.WithClock(now), api.WithOnUnauthorized(m.onUnauthorized), api.WithOnUpgradeRequired(m.onUpgrade)}
		if o.Credentials != nil && o.Credentials.Token != "" {
			opts = append(opts, api.WithToken(o.Credentials.Token))
		}
		client = api.New(cfg.ServerURL(), opts...)
	}
	m.notifier = o.Notifier
	if m.notifier == nil {
		m.notifier = notify.New(notify.NewOSBackend("masil"), notify.OpenLedger(o.Paths), slog.Default())
	}

	// ⑦ Language: the effective (gated) language drives ctx.T, the API's
	// Accept-Language and OS notifications.
	res := resolveLocale(o.Locale, cfg)
	client.SetLanguage(string(res.Effective))
	m.notifier.SetLocale(res.Effective)

	m.offRT = noopRealtime{state: realtime.State{Phase: realtime.PhaseLive}}
	m.ctx = &ui.Context{
		Theme:  theme.Get(name),
		Mode:   layout.ModeTooSmall,
		Keys:   keys,
		Store:  store.New(),
		API:    client,
		RT:     m.offRT,
		Config: cfg,
		Paths:  o.Paths,
		Caps:   caps,
		Now:    now,
		Timers: timers,
		Loc:    loc,
		T:      i18n.For(res.Effective),
		Lang:   res.Effective,
		Post:   appPoster{m},
	}
	m.ctx.Keys.Localize(m.ctx.T) // help descriptions in the shown language
	m.initTools(o.Paths)         // ctx.ToolState (never nil), ctx.OpenTools
	// ctx.Secrets (never nil).
	m.ctx.Secrets = openSecrets(o)
	m.ctx.Clipboard = o.Clipboard
	if m.ctx.Clipboard == nil {
		m.ctx.Clipboard = clipboard.System()
	}

	m.bp = layout.FromConfig(cfg.Layout)
	// ⑧ state.json (CA 3.5): restored only for the logged-in user; the start pin is
	// state.pinned_mode (≠ auto) > config layout.pin (≠ auto) > none.
	var saved *SavedState
	if o.Credentials != nil {
		if s, ok := readState(o.Paths, o.Credentials.UserID); ok {
			saved = &s
			m.restore(s)
		}
	}
	m.pin = startPin(saved, cfg.Layout.Pin)
	if o.Credentials == nil { // CA 5.4 start order: no credentials => the login screen
		r := ui.Route{Kind: ui.RouteLogin}
		if o.InitialRoute != nil && o.InitialRoute.Kind == ui.RouteLogin { // `masil login` / `masil signup`
			r = *o.InitialRoute
		}
		m.login = &r
	} else if o.InitialRoute != nil && (!isToolKind(o.InitialRoute.Kind) || toolRouteOK(*o.InitialRoute)) {
		if !isRootKind(o.InitialRoute.Kind) {
			// A non-root start route (`masil join` => RouteInvite) opens on the home tab,
			// as after a fresh login, never on top of the restored active channel.
			m.nav, _ = m.nav.navigate(ui.NavigateMsg{Route: homeRoute}, m.ctx.Mode, m.isDM)
		}
		m.nav, m.intent = m.nav.navigate(ui.NavigateMsg{Route: *o.InitialRoute}, m.ctx.Mode, m.isDM)
		if m.intent.Slot == "" {
			m.intent = FocusIntent{Slot: layout.SlotMain}
		}
	}
	return m
}

// resolveLocale is o.Locale, or the i18n spec 6.2 decision from the
// environment, the OS and [ui] language.
func resolveLocale(r *i18n.Resolution, cfg *config.Config) i18n.Resolution {
	if r != nil {
		return *r
	}
	in := i18n.DetectOS()
	in.Config = cfg.UI.Language
	return i18n.Resolve(in)
}

// Init implements tea.Model. With credentials and realtime enabled it starts the
// realtime bridge (once a sender is set, i.e. under Run).
func (m *Model) Init() tea.Cmd {
	cmds := m.initCmds
	m.initCmds = nil
	cmds = append(cmds, m.startRealtime(), m.nestedPaneWarning())
	return tea.Batch(cmds...)
}

// Update implements tea.Model (CA 5.3.4). After every message the focus is resolved
// (and a change broadcast) and the status-line countdown tick is (re)armed when a
// focus session or a 429 cooldown is showing.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// API hook flags first (401 / 426 seen on an HTTP goroutine since the last Update).
	pre := tea.Batch(m.takeFlags(), m.takeReturn())
	var watch tea.Cmd
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseMsg, tea.PasteMsg:
		m.noteInput()
		watch = m.armWatch() // input may scroll
	}
	_, cmd := m.dispatch(msg)
	m.syncOverlays()
	m.noteHistory()
	// The focus slot is resolved at the end of every Update (CA 5.3.5); a change is
	// broadcast as ui.FocusChangedMsg after this Update's relayout/projection.
	return m, tea.Batch(pre, watch, cmd, m.syncFocus(), m.armStatusTick(), m.armIdle())
}

// dispatch handles one message (CA 5.3.4 steps).
func (m *Model) dispatch(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.toolMsg(msg); ok { // the tool surface's msgs (tools.go)
		return m, cmd
	}
	if cmd, ok := m.grabMsg(msg); ok { // key grabbing: ctrl+\ timeout, shell actions (keys.go)
		return m, cmd
	}
	switch msg := msg.(type) {
	case statusTickMsg:
		if msg.gen == m.tickGen {
			m.tickArmed = false
		}
		return m, nil
	case ui.ToastMsg:
		return m, m.toasts.push(m.ctx, msg)
	case ui.ErrorMsg:
		if api.CodeOf(msg.Err) == "unauthorized" { // the session ends instead (takeFlags)
			return m, nil
		}
		return m, m.toasts.push(m.ctx, errorToast(msg))
	case toastExpireMsg:
		m.toasts.remove(msg.id)
		return m, nil
	case quitRequestMsg: // ctrl+c twice and the palette's 종료 (quit.go)
		return m, m.requestQuit(msg.confirm)
	case quitArmMsg:
		if msg.gen == m.quitGen {
			m.quitArmed = false
		}
		return m, nil
	case pinMsg:
		return m, m.setPin(msg.mode)
	case ui.RunCommandMsg: // consumed, never broadcast (CR-B)
		return m, m.runCommand(msg)
	case ui.SetThemeMsg:
		return m, m.setTheme(msg.Name)
	case helpMsg:
		return m, m.openHelp()
	case reconnectMsg:
		return m, m.reconnect()
	case tea.MouseMsg:
		return m, m.routeMouse(msg)
	case tea.WindowSizeMsg:
		m.cols, m.rows, m.sized = msg.Width, msg.Height, true
		return m, m.relayout()
	case tea.FocusMsg:
		m.onTerminalFocus(true)
		return m, m.broadcast(msg)
	case tea.BlurMsg:
		m.onTerminalFocus(false)
		return m, m.broadcast(msg)
	case tea.ModeReportMsg:
		if msg.Mode == ansi.ModeUnicodeCore {
			m.ctx.Caps.UnicodeCore = msg.Value.IsSet()
		}
		return m, nil
	case ui.NavigateMsg:
		return m, m.applyNavigate(msg)
	case ui.BackMsg:
		return m, m.goBack()
	case ui.OpenOverlayMsg:
		return m, m.openOverlay(msg)
	case ui.CloseOverlayMsg:
		m.popOverlay()
		return m, nil
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
		return m, m.onEvent(msg)
	case ui.RealtimeStateMsg:
		cmd := m.onRealtimeState(msg.State)
		return m, tea.Batch(cmd, m.broadcast(msg))
	case readyTimeoutMsg:
		return m, m.onReadyTimeout(msg)
	case resyncNeededMsg: // the bridge queue overflowed and dropped events
		return m, m.startBootstrap()
	case ExecReturnedMsg: // the return toast follows termhost.Exclusive (takeReturn)
		return m, nil
	case bootstrapDoneMsg:
		return m, m.onBootstrapDone(msg)
	case bootstrapRetryMsg:
		return m, m.onBootstrapRetry(msg)
	case osShownMsg:
		onOSShown(msg)
		return m, nil
	case idleMsg:
		m.onIdle(msg)
		return m, nil
	case watchDebounceMsg:
		m.onWatch(msg)
		return m, nil
	case unauthorizedMsg, upgradeMsg: // the flags were taken at the start of Update
		return m, nil
	case logoutConfirmedMsg:
		return m, m.logoutConfirmed(msg.wipeTools)
	case logoutDoneMsg:
		if msg.err != nil {
			slog.Debug("logout request failed; logging out locally", "err", msg.err)
		}
		return m, m.logout(reasonLogout)
	case authui.LoggedInMsg:
		return m, m.loggedIn(msg)
	}
	return m, m.broadcast(msg)
}

// broadcast sends msg to every live component: route instances (creation order),
// then pane-factory components, then the open overlays bottom -> top (CA 5.3.4 steps
// 5–6).
func (m *Model) broadcast(msg tea.Msg) tea.Cmd {
	if m.observe != nil {
		m.observe(msg)
	}
	var cmds []tea.Cmd
	for _, in := range m.cache.all() {
		cmds = append(cmds, in.comp.Update(m.ctx, msg))
	}
	for _, s := range factorySlots {
		if in := m.panes[s]; in != nil {
			cmds = append(cmds, in.comp.Update(m.ctx, msg))
		}
	}
	for _, o := range append([]ui.Overlay(nil), m.overlays...) {
		cmds = append(cmds, o.Update(m.ctx, msg))
	}
	return tea.Batch(cmds...)
}

// relayout is CA 5.3.4 step 1: Resolve → Compute(…, nav.LayoutState(mode)) → Project →
// Resize the placed instances whose inner size (or mode) changed → Store.SetViewing
// (CR-C: right after the projection, before any broadcast of this Update) →
// RouteUpdatedMsg to reused instances shown with a new Anchor/Query → ModeChangedMsg
// when the mode changed. New instances' Init Cmds are returned with it, and the watch
// debounce restarts. While logged out the login route fills the body (loginLayout).
func (m *Model) relayout() tea.Cmd {
	if !m.sized {
		m.cache.rejump = ""
		return nil
	}
	prev := m.lay.Mode
	mode, fell := layout.Resolve(m.cols, m.rows, m.bp, m.pin)
	var l layout.Layout
	var routes map[layout.SlotID]ui.Route
	switch {
	case m.login != nil && mode != layout.ModeTooSmall:
		l = loginLayout(mode, m.cols, m.rows)
		routes = map[layout.SlotID]ui.Route{layout.SlotMain: *m.login}
	default:
		l = layout.Compute(mode, m.cols, m.rows, m.nav.LayoutState(mode))
		routes = Project(l, m.nav)
		if m.login != nil {
			routes = map[layout.SlotID]ui.Route{}
		}
	}
	m.lay, m.routes, m.pinFell = l, routes, fell
	m.ctx.Mode, m.ctx.Layout, m.ctx.Routes = l.Mode, l, routes

	var cmds []tea.Cmd
	var updated []*instance
	m.cache.tick++
	pinned := map[string]bool{}
	for _, k := range m.nav.Keys() {
		pinned[k] = true
	}
	if m.login != nil {
		pinned[m.login.Key()] = true
	}
	for _, p := range l.Panes {
		var in *instance
		var cmd tea.Cmd
		if r, ok := routes[p.Slot]; ok {
			var upd bool
			in, cmd, upd = m.cache.get(m.ctx, r)
			in.lastSeen = m.cache.tick
			pinned[r.Key()] = true
			if upd {
				updated = append(updated, in)
			}
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
	m.cache.rejump = ""
	m.focus = resolveFocus(m.intent, l, routes)
	m.updateViewing()
	for _, in := range updated {
		cmds = append(cmds, in.comp.Update(m.ctx, ui.RouteUpdatedMsg{Route: in.route}))
	}
	if m.laidOut && prev != l.Mode {
		cmds = append(cmds, m.broadcast(ui.ModeChangedMsg{From: prev, To: l.Mode}))
	}
	m.laidOut = true
	cmds = append(cmds, m.armWatch())
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
// (CA 5.3.7, never the pane cursor below); otherwise the focused component's
// Cursorer result plus its pane's inner origin.
func (m *Model) cursor() *tea.Cursor {
	if !m.sized || m.lay.Mode == layout.ModeTooSmall {
		return nil
	}
	if len(m.overlays) > 0 {
		return m.overlayCursor()
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
