package workspace

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// tinyView is the tiny layout: a summary only (spec B6.7).
func (s *Surface) tinyView(ctx *ui.Context, w, h int) []string {
	t := ctx.T.Workspace
	lines := []string{s.bandLine(ctx, w)}
	dim := lipgloss.NewStyle().Foreground(ctx.Theme.P.Dim)
	lines = append(lines, dim.Render(textwidth.Truncate(" "+t.HubStatusPrivate(textwidth.Sanitize(s.name), s.term.count()), w, "…")))
	if n := len(s.docs.Dirty()); n > 0 {
		lines = append(lines, ctx.Theme.S.Warning.Render(textwidth.Truncate(" "+t.TermTitleUnsaved(n), w, "…")))
	}
	return lines
}

// tinyKey: the tiny layout has no cells to type into.
func (s *Surface) tinyKey(*ui.Context, tea.KeyPressMsg) (bool, tea.Cmd) { return false, nil }
