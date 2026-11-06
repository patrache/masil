package palette

import (
	"testing"

	"github.com/patrache/masil/internal/api"
)

// A display name the screen shows as nothing (only Hangul fillers, zero-width,
// bidi runes, noncharacters or U+2800 BRAILLE PATTERN BLANK) falls back to the handle in a person row (pending-fixes 1).
func TestPersonItemInvisibleNameFallsBack(t *testing.T) {
	for _, name := range []string{"", "\u3164", "\u3164\uffa0\u115f\u1160", "\u200b\u202e", "\u034f\U000e0100",
		"\ufffe", "\uffff\ufdd0", "\u2800", "\u2800 \u2800", "\U0001d159"} {
		if got := personItem(api.UserSummary{ID: "u1", Handle: "sam", DisplayName: name}).Title; got != "sam" {
			t.Errorf("personItem(%q).Title = %q, want the handle", name, got)
		}
	}
}
