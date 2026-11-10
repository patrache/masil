package chat

import (
	"strings"
	"testing"
	"unicode"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui/uitest"
)

func hasHangul(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Hangul, r) })
}

// TestMembersCount (P5 Task 14): member counts are a ko counter (멤버 3명, the
// pane title 멤버 3) and an en plural (1 member / 3 members).
func TestMembersCount(t *testing.T) {
	en, ko := i18n.For(i18n.En), i18n.KoT()
	for _, c := range []struct{ got, want string }{
		{ko.Chat.MemberCount(3), "멤버 3명"},
		{en.Chat.MemberCount(1), "1 member"},
		{en.Chat.MemberCount(3), "3 members"},
		{membersTitleFor(ko, 3), "멤버 3"},
		{membersTitleFor(ko, 0), "멤버"},
		{membersTitleFor(en, 1), "1 member"},
		{membersTitleFor(en, 3), "3 members"},
		{membersTitleFor(en, 0), "Members"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// TestTypingLineJosa (P5 Task 14): the typing line keeps its ko wording and
// reads "sam is typing…" / "sam and 민지 are typing…" in en, with Hangul only
// in the names.
func TestTypingLineJosa(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	ctx := h.Context()
	s := ctx.Store
	minji, junho, hana := userBy(t, s, "minji"), userBy(t, s, "junho"), userBy(t, s, "hana")
	sam := api.UserSummary{ID: "01JATYPESAM000000000000000", Handle: "sam", DisplayName: "sam"}
	s.ObserveUser(sam)
	enCtx := *ctx
	enCtx.T, enCtx.Lang = i18n.For(i18n.En), i18n.En
	for _, c := range []struct {
		ids    []string
		ko, en string
	}{
		{[]string{sam.ID}, "sam 입력 중…", "sam is typing…"},
		{[]string{minji.ID}, "민지 입력 중…", "민지 is typing…"},
		{[]string{sam.ID, minji.ID}, "sam, 민지 입력 중…", "sam and 민지 are typing…"},
		{[]string{minji.ID, junho.ID, hana.ID}, "여러 명이 입력 중…", "Several people are typing…"},
	} {
		if got := typingText(ctx, c.ids); got != c.ko {
			t.Errorf("ko typingText = %q, want %q", got, c.ko)
		}
		got := typingText(&enCtx, c.ids)
		if got != c.en {
			t.Errorf("en typingText = %q, want %q", got, c.en)
		}
		if rest := strings.NewReplacer("민지", "", "준호", "", "하나", "").Replace(got); hasHangul(rest) {
			t.Errorf("en typingText %q: Hangul outside the names", got)
		}
	}
}
