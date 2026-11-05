package widgets

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// Spinner is a theme-colored dot spinner. Uses bubbles spinner.Dot frames only;
// ticks via ui.After (never spinner.Model.Tick). config ui.animations=false =>
// static first frame, Start returns nil.
type Spinner struct { /* frame index, generation */
}

// NewSpinner creates a spinner.
func NewSpinner() *Spinner {
	// TODO(core): Task 21
	return nil
}

// Start arms the first tick.
func (s *Spinner) Start(ctx *ui.Context) tea.Cmd {
	// TODO(core): Task 21
	return nil
}

// Stop stops ticking.
func (s *Spinner) Stop() {
	// TODO(core): Task 21
}

// Update consumes its own tick msg and re-arms.
func (s *Spinner) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	// TODO(core): Task 21
	return nil
}

// View renders the current frame.
func (s *Spinner) View(t *theme.Theme) string {
	// TODO(core): Task 21
	return ""
}
