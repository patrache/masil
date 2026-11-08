package widgets

import (
	"sync/atomic"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// widgetIDs hands out ids so that tick msgs of different widget instances never mix.
var widgetIDs atomic.Int64

func nextWidgetID() int { return int(widgetIDs.Add(1)) }

// spinnerTickMsg advances spinner id while gen is current.
type spinnerTickMsg struct{ id, gen int }

// Spinner is a theme-colored dot spinner. Uses bubbles spinner.Dot frames only;
// ticks via ui.After (never spinner.Model.Tick). config ui.animations=false =>
// static first frame, Start returns nil.
type Spinner struct {
	id      int
	frame   int
	gen     int
	running bool
}

// NewSpinner creates a spinner.
func NewSpinner() *Spinner {
	return &Spinner{id: nextWidgetID()}
}

// animationsOn reports config ui.animations (on when there is no config).
func animationsOn(ctx *ui.Context) bool {
	return ctx == nil || ctx.Config == nil || ctx.Config.UI.Animations
}

// Start arms the first tick (restarting invalidates any pending tick).
func (s *Spinner) Start(ctx *ui.Context) tea.Cmd {
	if s.id == 0 {
		s.id = nextWidgetID()
	}
	s.gen++
	s.frame = 0
	if !animationsOn(ctx) {
		s.running = false
		return nil
	}
	s.running = true
	return ui.After(ctx, spinner.Dot.FPS, spinnerTickMsg{id: s.id, gen: s.gen})
}

// Stop stops ticking; a pending tick is dropped when it arrives.
func (s *Spinner) Stop() {
	s.running = false
	s.gen++
}

// Update consumes its own tick msg and re-arms.
func (s *Spinner) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	m, ok := msg.(spinnerTickMsg)
	if !ok || m.id != s.id || m.gen != s.gen || !s.running {
		return nil
	}
	if !animationsOn(ctx) {
		s.running = false
		s.frame = 0
		return nil
	}
	s.frame = (s.frame + 1) % len(spinner.Dot.Frames)
	return ui.After(ctx, spinner.Dot.FPS, spinnerTickMsg{id: s.id, gen: s.gen})
}

// View renders the current frame (acc on Base).
func (s *Spinner) View(t *theme.Theme) string {
	return t.S.Base.Foreground(t.P.Acc).Render(spinner.Dot.Frames[s.frame%len(spinner.Dot.Frames)])
}
