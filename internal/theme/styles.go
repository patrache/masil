package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Styles are derived from Base (fg+bg).
type Styles struct {
	Base, Dim, Bold, Title, Subtitle lipgloss.Style
	PaneFocused, PaneBlurred         lipgloss.Style // border styles (edge=acc unless P.Edge / line)
	Selected, Cursorline             lipgloss.Style // selbg bg + acc fg
	Badge, BadgeMention              lipgloss.Style // acc2 bg + bg fg
	Pill                             lipgloss.Style // acc bg + bg fg + bold (status mode pill, active tab)
	Tab, TabActive                   lipgloss.Style
	StatusBar, StatusHint            lipgloss.Style
	Mention, MentionMe               lipgloss.Style
	Link, Hashtag                    lipgloss.Style // acc4 underline / acc3
	InlineCode, CodeBlock, CodeBar   lipgloss.Style // codebg, acc3 / codebg / acc
	Reaction, ReactionMine           lipgloss.Style // line border-ish chip / acc fg
	Timestamp, System, Deleted       lipgloss.Style
	Input, InputFocused, Placeholder lipgloss.Style
	Online, Away, DND, Offline       lipgloss.Style // ● acc3 / ◐ acc / ⊘ acc2 / ○ dim
	Error, Warning, Success          lipgloss.Style // acc2 / acc / acc3
	NewHighlight                     lipgloss.Style // selbg bg, faded by widgets.Fade
	TextSelect                       lipgloss.Style // bg fg + fg bg (reverse): mouse drag selection
	Overlay, OverlayBorder           lipgloss.Style
}

// newStyles derives every style from base (fg+bg) so no cell is ever unstyled;
// clear leaves the base background (and border background) unset so the
// terminal's own background shows.
func newStyles(p Palette, border lipgloss.Border, clear bool) Styles {
	edge := p.Acc
	if p.Edge != nil {
		edge = p.Edge
	}
	base := lipgloss.NewStyle().Foreground(p.FG)
	if !clear {
		base = base.Background(p.BG)
	}
	boxed := func(edge lipgloss.Style, c color.Color) lipgloss.Style {
		st := edge.Border(border).BorderForeground(c)
		if !clear {
			st = st.BorderBackground(p.BG)
		}
		return st
	}
	selected := base.Background(p.SelBG).Foreground(p.Acc)
	badge := base.Background(p.Acc2).Foreground(p.BG)
	pill := base.Background(p.Acc).Foreground(p.BG).Bold(true)
	if clear { // a Clear theme fills nothing brightly: the active tab and badges sit on a chip
		badge = base.Background(p.Chip).Foreground(p.FG)
		pill = base.Background(p.Chip).Foreground(p.FG).Bold(true)
	}
	dim := base.Foreground(p.Dim)
	mention := base.Background(p.SelBG).Foreground(p.Acc)
	chip := base.Background(p.Chip)
	return Styles{
		Base:          base,
		Dim:           dim,
		Bold:          base.Bold(true),
		Title:         base.Foreground(p.Acc).Bold(true),
		Subtitle:      dim,
		PaneFocused:   boxed(base, edge),
		PaneBlurred:   boxed(base, p.Line),
		Selected:      selected,
		Cursorline:    selected,
		Badge:         badge,
		BadgeMention:  badge,
		Pill:          pill,
		Tab:           dim,
		TabActive:     pill,
		StatusBar:     chip,
		StatusHint:    chip.Foreground(p.Dim),
		Mention:       mention,
		MentionMe:     mention.Bold(true),
		Link:          base.Foreground(p.Acc4).Underline(true),
		Hashtag:       base.Foreground(p.Acc3),
		InlineCode:    base.Background(p.CodeBG).Foreground(p.Acc3),
		CodeBlock:     base.Background(p.CodeBG),
		CodeBar:       base.Background(p.CodeBG).Foreground(p.Acc),
		Reaction:      chip,
		ReactionMine:  chip.Foreground(p.Acc),
		Timestamp:     dim,
		System:        dim,
		Deleted:       dim.Italic(true),
		Input:         boxed(base, p.Line),
		InputFocused:  boxed(base, edge),
		Placeholder:   dim,
		Online:        base.Foreground(p.Acc3),
		Away:          base.Foreground(p.Acc),
		DND:           base.Foreground(p.Acc2),
		Offline:       dim,
		Error:         base.Foreground(p.Acc2),
		Warning:       base.Foreground(p.Acc),
		Success:       base.Foreground(p.Acc3),
		NewHighlight:  base.Background(p.SelBG),
		TextSelect:    base.Foreground(p.BG).Background(p.FG),
		Overlay:       base,
		OverlayBorder: boxed(base, edge),
	}
}
