package feed

import (
	"strings"
	"testing"
	"unicode"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
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

// TestInboxSentencesNoHonorificEN (P5 Task 13): every notification type's en
// sentence (wide and narrow) has no "님" and no Hangul of its own, and keeps the
// actor's name as given; ko keeps the 0.1.2 sentence.
func TestInboxSentencesNoHonorificEN(t *testing.T) {
	en, ko := i18n.For(i18n.En), i18n.KoT()
	space := "Nova"
	types := []api.NotificationType{"zzz_unknown"}
	for typ := range notifIcons {
		types = append(types, typ)
	}
	for _, typ := range types {
		x := api.Notification{Type: typ, Actor: &api.UserSummary{ID: "u1", DisplayName: "sam"}}
		x.Context.SpaceName = &space
		for _, narrow := range []bool{false, true} {
			s := notifSentence(en, x, narrow)
			if strings.Contains(s, "님") || hasHangul(s) {
				t.Errorf("en %s (narrow=%v) = %q: Korean left", typ, narrow, s)
			}
			if !strings.Contains(s, "sam") && !strings.Contains(s, space) {
				t.Errorf("en %s (narrow=%v) = %q: no name", typ, narrow, s)
			}
			if k := notifSentence(ko, x, narrow); !strings.Contains(k, "sam님") && !strings.Contains(k, space) {
				t.Errorf("ko %s (narrow=%v) = %q: lost 님", typ, narrow, k)
			}
		}
	}
	if got := notifSentence(ko, api.Notification{Type: "post_like", Actor: &api.UserSummary{DisplayName: "하나"}}, false); got != "하나님이 내 글을 좋아해요" {
		t.Errorf("ko post_like = %q", got)
	}
	if got := en.Feed.ConvPerson("sam"); got != "sam" {
		t.Errorf("en ConvPerson = %q, want the bare name", got)
	}
	if got := ko.Feed.ConvPerson("민지"); got != "민지님" {
		t.Errorf("ko ConvPerson = %q", got)
	}
	if got := en.Feed.ConvNew(1); got != "1 new message" {
		t.Errorf("en ConvNew(1) = %q", got)
	}
	if got := ko.Feed.ConvNew(3); got != "새 메시지 3개" {
		t.Errorf("ko ConvNew(3) = %q", got)
	}
}

// TestPossessiveEN (P5 Task 13): report titles put the name first in en
// ("sam's guestbook entry", "sam's post"), ko keeps "의".
func TestPossessiveEN(t *testing.T) {
	en, ko := i18n.For(i18n.En), i18n.KoT()
	if got := en.Feed.ReportGuestbookOf("sam"); !strings.HasPrefix(got, "sam's guestbook") {
		t.Errorf("en ReportGuestbookOf = %q, want sam's guestbook…", got)
	}
	if got := en.Feed.ReportPostOf("sam"); got != "sam's post" {
		t.Errorf("en ReportPostOf = %q", got)
	}
	if got := ko.Feed.ReportGuestbookOf("민지"); got != "민지의 방명록 글" {
		t.Errorf("ko ReportGuestbookOf = %q", got)
	}
	if got := ko.Feed.ReportPostOf("민지"); got != "민지의 글" {
		t.Errorf("ko ReportPostOf = %q", got)
	}
}

// TestNotificationsShortLabel (P5 Task 13): the inbox title is the short
// "Alerts" in en widget / tiny and the full word elsewhere; ko is 알림 always.
func TestNotificationsShortLabel(t *testing.T) {
	en, ko := i18n.For(i18n.En), i18n.KoT()
	n := &notifications{}
	for _, c := range []struct {
		t    *i18n.T
		mode layout.Mode
		want string
	}{
		{en, layout.ModeWidget, "Alerts"},
		{en, layout.ModeTiny, "Alerts"},
		{en, layout.ModeStandard, "Notifications"},
		{en, layout.ModeCompact, "Notifications"},
		{ko, layout.ModeWidget, "알림"},
		{ko, layout.ModeStandard, "알림"},
	} {
		if got, _ := n.Title(&ui.Context{T: c.t, Lang: c.t.Locale(), Mode: c.mode}); got != c.want {
			t.Errorf("%s mode %v: Title = %q, want %q", c.t.Locale(), c.mode, got, c.want)
		}
	}
}
