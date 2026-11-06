package feed

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// titleEscapes holds what an OSC 0/2 title must never carry: ESC/BEL end or
// forge the sequence, and APC/DCS/C1 pass lipgloss's Compositor untouched.
const titleEscapes = "\x1b]0;PWNED\x07\x1b_Gf=100;AAAA\x1b\\\x1bP1$qm\x1b\\\u009b2J\u009c\x07"

// TestWindowTitleHostileFeed (pending-fixes 0-b, 1): the window title of the
// feed screens whose title comes from user data (#tag from a post's hashtags,
// @handle from a mention or link) carries no control rune and no bidi,
// zero-width, separator, variation-selector or filler rune (fixtures.FormatSoup;
// the OS GUI draws the title with full bidi), while its text stays.
// Two layers clean it: the screen's Title() and app currentTitle (pinned alone
// by app's TestWindowTitleSanitizedCentrally); this fails if both go.
func TestWindowTitleHostileFeed(t *testing.T) {
	cases := []struct {
		name string
		opts []uitest.Option
		want string
	}{
		{"hashtag", hashtagOptsWith(snsAPI(t, true), "태그"+titleEscapes+"끝", true), "masil · #태그"},
		{"hashtag format runes", hashtagOptsWith(snsAPI(t, true), "태그"+fixtures.FormatSoup+"끝", true), "masil · #태그"},
		{"profile before load", profileOpts(t, "@nobody"+titleEscapes, true, nil), "masil · @nobody"},
		{"profile before load format runes", profileOpts(t, "@nobody"+fixtures.FormatSoup, true, nil), "masil · @nobody"},
		{"profile", profileOpts(t, "@"+hostileProfileHandle, true, nil), "masil · @" + hostileProfileHandle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := uitest.New(t, c.opts...)
			for _, s := range []uitest.Size{{W: 120, H: 40}, {W: 60, H: 20}} {
				h.Resize(s.W, s.H)
				title := h.Model.View().WindowTitle
				uitest.AssertTitle(t, c.name, title)
				if !strings.HasPrefix(title, c.want) {
					t.Errorf("title %q, want prefix %q", title, c.want)
				}
				h.Check()
			}
		})
	}
}
