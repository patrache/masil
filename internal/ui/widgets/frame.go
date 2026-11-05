// Package widgets holds shared drawing helpers and components (CA 5.7).
package widgets

import (
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// Frame draws a themed pane border with title/badge; content is fit to inner size.
func Frame(t *theme.Theme, title string, badge ui.Badge, focused bool, size ui.Size, content string) string {
	// TODO(core): Task 21
	return ""
}

// Box draws a nested box (composer input).
func Box(t *theme.Theme, style lipgloss.Style, size ui.Size, content string) string {
	// TODO(core): Task 21
	return ""
}
