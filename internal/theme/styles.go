package theme

import "charm.land/lipgloss/v2"

// Styles are derived from Base (fg+bg).
type Styles struct {
	Base, Dim, Bold, Title, Subtitle lipgloss.Style
	PaneFocused, PaneBlurred         lipgloss.Style // border styles (acc / line)
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
	Overlay, OverlayBorder           lipgloss.Style
}
