package feed

import (
	"strings"
	"testing"
	"unicode"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui/uitest"
)

func hasHangul(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Hangul, r) })
}

// entry is the catalog metadata of feed.<key>.
func entry(t *testing.T, key string) i18n.Entry {
	t.Helper()
	for _, e := range i18n.Entries() {
		if e.Area == "feed" && e.Key == key {
			return e
		}
	}
	t.Fatalf("feed.%s not in the catalog", key)
	return i18n.Entry{}
}

// TestFeedTabsBudget (P5 Task 12): the feed tab names have a max and every
// language fits it; at 80x24 (widget) the en tab line keeps the frame intact
// and shows the English names.
func TestFeedTabsBudget(t *testing.T) {
	for _, k := range []string{"tab_discover", "tab_following", "tab_trends"} {
		e := entry(t, k)
		if e.Max == 0 {
			t.Errorf("feed.%s: no max", k)
		}
		for l, f := range e.Text {
			if w := textwidth.Width(f.Other); w > e.Max {
				t.Errorf("feed.%s (%s) %q is %d cells > max %d", k, l, f.Other, w, e.Max)
			}
		}
	}
	h := uitest.New(t, append(feedOpts(t, "following", false), uitest.WithLocale(i18n.En))...)
	h.Resize(80, 24)
	h.Check()
	s := h.Screen()
	en := i18n.For(i18n.En)
	for _, want := range []string{en.Feed.TabDiscover(), en.Feed.TabFollowing(), en.Feed.TabTrends()} {
		if !strings.Contains(s, want) {
			t.Errorf("80x24 en: tab %q missing:\n%s", want, s)
		}
	}
	for _, bad := range []string{"팔로잉", "#트렌드"} {
		if strings.Contains(s, bad) {
			t.Errorf("80x24 en: Korean tab %q left:\n%s", bad, s)
		}
	}
}

// TestRepostQuoteGlossary (P5 Task 12): en uses the glossary's Repost / Quote.
func TestRepostQuoteGlossary(t *testing.T) {
	en := i18n.For(i18n.En)
	for _, c := range []struct{ got, want string }{
		{en.Feed.ComposeQuote(), "Quote"},
		{en.Feed.ErrCtxRepost(), "Repost"},
		{en.Feed.HelpRepost(), "repost"},
		{en.Feed.CardRepostedBy("sam"), "sam reposted"},
	} {
		if c.got != c.want {
			t.Errorf("en = %q, want %q", c.got, c.want)
		}
	}
	for _, e := range i18n.Entries() {
		if e.Area != "feed" {
			continue
		}
		ko, en := e.Text[i18n.Ko].Other, e.Text[i18n.En].Other
		if strings.Contains(ko, "리포스트") && !strings.Contains(strings.ToLower(en), "repost") {
			t.Errorf("feed.%s: ko %q says 리포스트, en %q lacks Repost", e.Key, ko, en)
		}
		if strings.Contains(ko, "인용") && !strings.Contains(strings.ToLower(en), "quot") {
			t.Errorf("feed.%s: ko %q says 인용, en %q lacks Quote", e.Key, ko, en)
		}
	}
}
