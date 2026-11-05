package markup

import (
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/theme"
)

// RenderOptions controls Render.
type RenderOptions struct {
	Width       int
	MyHandle    string           // highlight mentions of me
	Mentions    []api.MentionRef // only these @handles are styled as mentions (resolved by server)
	MentionHere bool
	Compact     bool // single-line summary mode: code blocks => "‹코드 N줄›", newlines => " "
	MaxLines    int  // 0 = unlimited; overflow adds "… 더 보기"
	Hyperlinks  bool // OSC 8 via lipgloss Style.Hyperlink
}

// Render returns lines, each Width <= o.Width, sanitized.
func Render(d Doc, t *theme.Theme, o RenderOptions) []string {
	// TODO(core): Task 17
	return nil
}
