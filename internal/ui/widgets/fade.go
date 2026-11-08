package widgets

import (
	"image/color"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

const (
	fadeSteps    = 4
	fadeInterval = 250 * time.Millisecond
)

// fadeTickMsg advances the fade of id in Fade owner while gen is current.
type fadeTickMsg struct {
	owner int
	id    string
	gen   int
}

// Fade is the new-item highlight, selbg -> bg in 4 steps x 250ms via ui.After
// (config ui.animations=false => none, Start returns nil). The zero value is ready.
//
// Step k (0..3) paints NewHighlight with lipgloss.Blend1D(5, SelBG, BG)[k]; the 4th
// tick reaches BG and the item is forgotten (Style is Base again).
type Fade struct {
	owner int
	gen   int
	items map[string]fadeState

	blendFor *theme.Theme
	blend    []color.Color
}

// fadeState is one item's step and generation.
type fadeState struct{ step, gen int }

// Start begins fading id (restarting an id invalidates its pending tick).
func (f *Fade) Start(ctx *ui.Context, id string) tea.Cmd {
	if !animationsOn(ctx) {
		delete(f.items, id)
		return nil
	}
	if f.owner == 0 {
		f.owner = nextWidgetID()
	}
	if f.items == nil {
		f.items = map[string]fadeState{}
	}
	f.gen++
	f.items[id] = fadeState{step: 0, gen: f.gen}
	return ui.After(ctx, fadeInterval, fadeTickMsg{owner: f.owner, id: id, gen: f.gen})
}

// Style returns the current highlight style for id (Base when not fading).
func (f *Fade) Style(t *theme.Theme, id string) lipgloss.Style {
	st, ok := f.items[id]
	if !ok {
		return t.S.Base
	}
	if f.blendFor != t || len(f.blend) != fadeSteps+1 {
		f.blendFor, f.blend = t, lipgloss.Blend1D(fadeSteps+1, t.P.SelBG, t.P.BG)
	}
	return t.S.NewHighlight.Background(f.blend[min(st.step, fadeSteps)])
}

// Update consumes fade tick msgs.
func (f *Fade) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	m, ok := msg.(fadeTickMsg)
	if !ok || f.owner == 0 || m.owner != f.owner {
		return nil
	}
	st, ok := f.items[m.id]
	if !ok || st.gen != m.gen {
		return nil
	}
	st.step++
	if st.step >= fadeSteps || !animationsOn(ctx) {
		delete(f.items, m.id)
		return nil
	}
	f.items[m.id] = st
	return ui.After(ctx, fadeInterval, fadeTickMsg{owner: f.owner, id: m.id, gen: m.gen})
}
