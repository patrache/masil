package workspace

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// bandLine is the top line (spec B6.1): who can see this folder.
func (s *Surface) bandLine(ctx *ui.Context, w int) string {
	if txt, st, ok := s.ownerBand(ctx); ok {
		return st.Render(textwidth.Truncate(" "+txt, w, "…"))
	}
	st := lipgloss.NewStyle().Foreground(ctx.Theme.P.Acc3).Bold(true)
	return st.Render(textwidth.Truncate(" "+ctx.T.Workspace.BandPrivate(textwidth.Sanitize(s.name)), w, "…"))
}

// subTabsLine is the widget's cell tabs: 트리 · 코드 · 터미널 (· 사람(n)).
func (s *Surface) subTabsLine(ctx *ui.Context, w int) string {
	t := ctx.T.Workspace
	type tab struct {
		cell  int
		label string
	}
	tabs := []tab{{PaneTree, t.SubtabTree()}, {PaneCode, t.SubtabCode()}, {PaneTerm, t.SubtabTerm()}}
	if s.sharing() {
		tabs = append(tabs, tab{PanePeople, t.SubtabPeople(s.peopleCount())})
	}
	dim := lipgloss.NewStyle().Foreground(ctx.Theme.P.Dim)
	parts := make([]string, len(tabs))
	for i, tb := range tabs {
		if tb.cell == s.plan.Single {
			parts[i] = ctx.Theme.S.TabActive.Render(" " + tb.label + " ")
		} else {
			parts[i] = dim.Render(" " + tb.label + " ")
		}
	}
	return textwidth.Truncate(strings.Join(parts, dim.Render("·")), w, "…")
}
