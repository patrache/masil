package uitest

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
)

// Box is a drawn rectangle. Z: panes 0, overlays 10+i, toast 100.
type Box struct {
	Rect     layout.Rect
	Bordered bool
	Z        int
}

// BoxesOf returns panes (Layout) + Overlays + Toast.
func BoxesOf(f app.FrameInfo) []Box {
	// TODO(core): Task 20
	return nil
}

// AssertIntegrity checks line count/width, borders, forbidden runes, status line and bounds (CA 10.2).
func AssertIntegrity(tb testing.TB, content string, w, h int, boxes []Box, border lipgloss.Border) {
	// TODO(core): Task 20
}
