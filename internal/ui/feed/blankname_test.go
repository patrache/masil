package feed

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
)

// blankNames are display names the screen shows as nothing: only runes
// textwidth.Sanitize drops, or U+2800 BRAILLE PATTERN BLANK (JS trim keeps
// it, so the server accepts it) and other empty-cell runes.
var blankNames = []string{"", "\u3164", "\u200b\u202e", "\u034f\U000e0100", "\ufffe", "\u2800", "\u2800 \u2800", "\U0001d159"}

// Cards and notifications name the user by handle when the display name shows
// nothing, never a nameless author or "님이 ..." (pending-fixes 1).
func TestBlankDisplayNameFallsBack(t *testing.T) {
	for _, name := range blankNames {
		u := api.UserSummary{ID: "u1", Handle: "sam", DisplayName: name}
		if got := displayName(u); got != "sam" {
			t.Errorf("displayName(%q) = %q, want the handle", name, got)
		}
		got := notifText("{n}님이", api.Notification{Actor: &u})
		if !strings.Contains(got, "sam") {
			t.Errorf("notifText with actor name %q = %q, want the handle", name, got)
		}
	}
}
