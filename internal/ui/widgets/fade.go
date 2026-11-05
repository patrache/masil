package widgets

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// Fade is the new-item highlight, selbg -> bg in 4 steps x 250ms via ui.After
// (config ui.animations=false => none, Start returns nil).
type Fade struct { /* per item id */
}

// Start begins fading id.
func (f *Fade) Start(ctx *ui.Context, id string) tea.Cmd {
	// TODO(core): Task 21
	return nil
}

// Style returns the current highlight style for id.
func (f *Fade) Style(t *theme.Theme, id string) lipgloss.Style {
	// TODO(core): Task 21
	return lipgloss.Style{}
}

// Update consumes fade tick msgs.
func (f *Fade) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	// TODO(core): Task 21
	return nil
}
