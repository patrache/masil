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

// TestSystemEventSentences (P5 Task 15): every SystemEvent.event keeps its ko
// sentence; en has Hangul only in the argument values (names, topics).
func TestSystemEventSentences(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	ctx := h.Context()
	minji := userBy(t, ctx.Store, "minji")
	enCtx := *ctx
	enCtx.T, enCtx.Lang = i18n.For(i18n.En), i18n.En
	target := minji.ID
	for _, c := range []struct {
		ev     api.SystemEvent
		ko, en string
	}{
		{api.SystemEvent{Event: "member_joined", ActorID: minji.ID}, "민지님이 들어왔어요", "민지 joined"},
		{api.SystemEvent{Event: "group_member_added", ActorID: "x", TargetUserID: &target}, "민지님이 들어왔어요", "민지 joined"},
		{api.SystemEvent{Event: "member_left", ActorID: minji.ID}, "민지님이 나갔어요", "민지 left"},
		{api.SystemEvent{Event: "member_kicked", ActorID: "x", TargetUserID: &target}, "민지님이 내보내졌어요", "민지 was removed"},
		{api.SystemEvent{Event: "group_member_removed", ActorID: "x", TargetUserID: &target}, "민지님이 빠졌어요", "민지 left the conversation"},
		{api.SystemEvent{Event: "channel_created", Data: api.SystemEventData{Name: "개발"}}, "#개발 채널을 만들었어요", "Created #개발"},
		{api.SystemEvent{Event: "channel_renamed", Data: api.SystemEventData{Name: "개발"}}, "채널 이름 변경: #개발", "Channel renamed: #개발"},
		{api.SystemEvent{Event: "channel_topic_changed", Data: api.SystemEventData{Topic: "배포 주간"}}, "주제 변경: 배포 주간", "Topic changed: 배포 주간"},
		{api.SystemEvent{Event: "message_pinned"}, "메시지를 고정했어요", "Pinned a message"},
		{api.SystemEvent{Event: "group_renamed", Data: api.SystemEventData{Name: "점심팟"}}, "대화 이름 변경: 점심팟", "Conversation renamed: 점심팟"},
	} {
		if got := systemText(ctx, c.ev); got != c.ko {
			t.Errorf("%s ko = %q, want %q", c.ev.Event, got, c.ko)
		}
		got := systemText(&enCtx, c.ev)
		if got != c.en {
			t.Errorf("%s en = %q, want %q", c.ev.Event, got, c.en)
		}
		args := strings.NewReplacer("민지", "", "개발", "", "배포 주간", "", "점심팟", "").Replace(got)
		if hasHangul(args) {
			t.Errorf("%s en %q: Hangul outside the arguments", c.ev.Event, got)
		}
	}
}

// TestRowsFollowLocaleSwitch (P5 Task 15): the day dividers and the thread's
// replies divider are built into the rows, so a language switch rebuilds them.
func TestRowsFollowLocaleSwitch(t *testing.T) {
	t.Setenv("MASIL_LANG", "")
	i18n.SetOpenForTest(t, i18n.Ko, i18n.En)
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	if !strings.Contains(h.Screen(), "요일") {
		t.Fatalf("no ko day divider before the switch:\n%s", h.Screen())
	}
	h.SetLocale("en")
	if s := h.Screen(); strings.Contains(s, "요일") || !strings.Contains(s, "day,") {
		t.Fatalf("day divider not rebuilt after the switch:\n%s", s)
	}
}
