package widgets_test

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui/widgets"
)

// A display name the screen shows as nothing (Hangul fillers, zero-width,
// noncharacters, U+2800 BRAILLE PATTERN BLANK, which the server accepts)
// renders as the handle, and the avatar shows the handle initial instead of a
// blank cell (pending-fixes 1).
func TestUserNameBlankFallsBack(t *testing.T) {
	for _, name := range []string{"", "\u3164", "\u200b\u202e", "\ufffe", "\u2800", "\u2800 \u2800", "\U0001d159"} {
		u := api.UserSummary{ID: "u_sam", Handle: "sam", DisplayName: name}
		if got := ansi.Strip(widgets.UserName(th, u, false)); got != "sam" {
			t.Errorf("UserName(%q) = %q, want the handle", name, got)
		}
		if got := ansi.Strip(widgets.Avatar(th, u, 2)); got != "S " {
			t.Errorf("Avatar(%q) = %q, want the handle initial", name, got)
		}
	}
}
