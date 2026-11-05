// Package app is the root tea.Model: shell, navigation, focus, overlays, status bar (CA 5.4).
package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
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
	nav NavState // logical navigation (CA 5.3.3); projected onto the layout every frame
}

var _ tea.Model = (*Model)(nil)

// New builds the app model.
func New(o Options) *Model {
	// TODO(core): Task 25
	return nil
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	// TODO(core): Task 25
	return nil
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// TODO(core): Task 25
	return m, nil
}

// View implements tea.Model.
func (m *Model) View() tea.View {
	// TODO(core): Task 25
	return tea.View{}
}

// Context returns the shared ui.Context (tests).
func (m *Model) Context() *ui.Context {
	// TODO(core): Task 25
	return nil
}

// Nav returns the current logical navigation state (tests, CA 5.3.3).
func (m *Model) Nav() NavState {
	return m.nav.clone()
}

// Instance returns the cached route instance or nil (tests).
func (m *Model) Instance(routeKey string) ui.Component {
	// TODO(core): Task 25
	return nil
}

// Focus returns the resolved focus slot + route key this frame (tests).
func (m *Model) Focus() FocusIntent {
	// TODO(core): Task 27
	return FocusIntent{}
}

// Frame returns the geometry of the last View() (tests, CA 10.2 AssertIntegrity).
func (m *Model) Frame() FrameInfo {
	// TODO(core): Task 25
	return FrameInfo{}
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
