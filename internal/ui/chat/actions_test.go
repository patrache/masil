package chat

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- helpers (Task 12) ----

// fixtureMsg is the held message of h's chat store whose body is body.
func fixtureMsg(t testing.TB, h *uitest.Harness, body string) api.Message {
	t.Helper()
	c := chatOf(h.Context())
	for id := range h.Context().Store.Channels {
		if !c.HasWindow(id) {
			continue
		}
		for _, m := range c.Window(id).Items() {
			if m.Body == body {
				return m
			}
		}
	}
	t.Fatalf("no held message with body %q", body)
	return api.Message{}
}

// selectText selects the message of the active channel view whose body (or
// poll question) contains text, with the list (not the composer) focused.
func selectText(t testing.TB, h *uitest.Harness, text string) api.Message {
	t.Helper()
	v := channelOf(t, h)
	for _, r := range v.pane.rows {
		if r.Kind != rowMessage || r.Msg == nil {
			continue
		}
		if strings.Contains(r.Msg.Body, text) || strings.Contains(markup.Plain(plainBody(r.Msg), 1<<16, koCat.Common.CodeSummary), text) {
			if !v.pane.selectID(r.ID) {
				t.Fatalf("cannot select %s", r.ID)
			}
			if v.composer.focused() {
				v.composer.blur()
			}
			return *r.Msg
		}
	}
	t.Fatalf("no message containing %q in the channel view", text)
	return api.Message{}
}

// hasMine reports whether m carries my reaction emoji.
func hasMine(m api.Message, emoji string) bool {
	return slices.ContainsFunc(m.Reactions, func(r api.ReactionSummary) bool { return r.Emoji == emoji && r.Me })
}

// reactionOf is m's reaction summary of emoji (ok=false when there is none).
func reactionOf(m api.Message, emoji string) (api.ReactionSummary, bool) {
	i := slices.IndexFunc(m.Reactions, func(r api.ReactionSummary) bool { return r.Emoji == emoji })
	if i < 0 {
		return api.ReactionSummary{}, false
	}
	return m.Reactions[i], true
}

// hasClipboard reports whether a tea.SetClipboard(s) msg was produced.
func hasClipboard(msgs []tea.Msg, s string) bool {
	for _, m := range msgs {
		if fmt.Sprintf("%T", m) == "tea.setClipboardMsg" && reflect.ValueOf(m).String() == s {
			return true
		}
	}
	return false
}

// cmdRunner is an overlay whose Init closes it and runs cmd (runCmdMsg).
type cmdRunner struct{ cmd tea.Cmd }

func (r *cmdRunner) Init(*ui.Context) tea.Cmd            { return tea.Batch(closeOverlay, r.cmd) }
func (r *cmdRunner) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (r *cmdRunner) Resize(*ui.Context, ui.Size)         {}
func (r *cmdRunner) View(*ui.Context, bool) string       { return "" }
func (r *cmdRunner) Bounds(body layout.Rect) layout.Rect { return centered(body, 30, 8) }
func (r *cmdRunner) Modal() bool                         { return true }

// runCmdMsg is a msg that makes the app run cmd through the harness (its
// resulting msgs are dispatched like any Cmd's).
func runCmdMsg(cmd tea.Cmd) tea.Msg { return ui.OpenOverlayMsg{Overlay: &cmdRunner{cmd: cmd}} }

// overlays is the number of open overlays (after a fresh View).
func overlays(h *uitest.Harness) int {
	h.Screen()
	return len(h.Model.Frame().Overlays)
}

// setRole sets my role in 노바 개발방.
func setRole(h *uitest.Harness, r api.SpaceRole) {
	h.Context().Store.Spaces[fixtures.SpaceNova].MyRole = r
}

// clickOn clicks the first cell of text anywhere on screen with button b.
func clickOn(t testing.TB, h *uitest.Harness, text string, b tea.MouseButton) {
	t.Helper()
	row, col := lineWith(t, h.Screen(), text)
	h.Send(tea.MouseClickMsg{X: col, Y: row, Button: b})
}

// ---- tests ----

func TestChipAt(t *testing.T) {
	rs := []api.ReactionSummary{{Emoji: "👍", Count: 2}, {Emoji: "👀", Count: 1}}
	line := "  👍 2  👀 1 · 💬 답글 3개"
	cases := []struct {
		x    int
		want string
		ok   bool
	}{{0, "", false}, {1, "", false}, {2, "👍", true}, {5, "👍", true}, {7, "👍", true}, {8, "👀", true}, {11, "👀", true}, {12, "", false}, {20, "", false}}
	for _, c := range cases {
		got, ok := chipAt(line, rs, c.x)
		if got != c.want || ok != c.ok {
			t.Errorf("chipAt(x=%d) = %q,%v want %q,%v", c.x, got, ok, c.want, c.ok)
		}
	}
	if _, ok := chipAt("", rs, 0); ok {
		t.Error("empty line")
	}
}

func TestReactKeyOpensPicker(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("r")
	if n := overlays(h); n != 1 {
		t.Fatalf("r opens the emoji picker, overlays = %d", n)
	}
}

func TestToggleReaction(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
	h.Send(runCmdMsg(toggleReactionCmd(h.Context(), m, "😀")))
	if r := reqs(h.API, "PUT", "/v1/messages/"+m.ID+"/reactions/"+url.PathEscape("😀")); len(r) != 1 {
		t.Fatal("PUT")
	}
	got, _ := chatstore.Get(h.Context().Store).FindMessage(m.ID)
	if !hasMine(got, "😀") {
		t.Fatal("optimistic me")
	}
	if !strings.Contains(h.Screen(), "😀 1") {
		t.Fatalf("the new chip is drawn:\n%s", h.Screen())
	}
	// a second toggle of the same emoji removes it
	h.Send(runCmdMsg(toggleReactionCmd(h.Context(), got, "😀")))
	if r := reqs(h.API, "DELETE", "/v1/messages/"+m.ID+"/reactions/"+url.PathEscape("😀")); len(r) != 1 {
		t.Fatal("DELETE")
	}
	got, _ = chatOf(h.Context()).FindMessage(m.ID)
	if _, ok := reactionOf(got, "😀"); ok {
		t.Fatal("toggled off")
	}
}

func TestReactionPickerToggles(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("r", "enter") // the picker's first emoji is 👍 (mine already): removing it
	if overlays(h) != 0 {
		t.Fatal("picking closes the picker")
	}
	if r := reqs(h.API, "DELETE", "/v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👍")); len(r) != 1 {
		t.Fatalf("pick toggles 👍 off: %+v", h.API.Requests())
	}
	if !strings.Contains(h.Screen(), "👍 1") {
		t.Fatalf("the chip is redrawn at once:\n%s", h.Screen())
	}
}

func TestReactionChipClickToggles(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
	clickText(t, h, layout.SlotMain, "👍")
	if r := reqs(h.API, "DELETE", "/v1/messages/"+m.ID+"/reactions/%F0%9F%91%8D"); len(r) != 1 {
		t.Fatalf("left click on my chip removes it: %+v", h.API.Requests())
	}
	got, _ := chatOf(h.Context()).FindMessage(m.ID)
	if r, ok := reactionOf(got, "👍"); !ok || r.Me || r.Count != 1 {
		t.Fatalf("optimistic off: %+v", got.Reactions)
	}
	if _, ok := threadRoute(h); ok {
		t.Fatal("a chip click never opens the thread")
	}
	clickText(t, h, layout.SlotMain, "👀")
	if r := reqs(h.API, "PUT", "/v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👀")); len(r) != 1 {
		t.Fatal("left click on another chip adds mine")
	}
	h.API.JSON("GET /v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👀"), 200, map[string]any{"items": []api.UserSummary{userBy(t, h.Context().Store, "junho")}, "next_cursor": nil})
	clickOn(t, h, "👀", tea.MouseRight)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "👀 누른 사람") || !strings.Contains(h.Screen(), "@junho") {
		t.Fatalf("right click opens chat.reaction_users:\n%s", h.Screen())
	}
	if n := len(reqs(h.API, "PUT", "/v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👀"))); n != 1 {
		t.Fatal("a right click never toggles")
	}
}

func TestReactionRollbackOnError(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
	h.API.Error("PUT /v1/messages/"+m.ID+"/reactions/"+url.PathEscape("😀"), http.StatusUnprocessableEntity, "limit_exceeded")
	h.Send(runCmdMsg(toggleReactionCmd(h.Context(), m, "😀")))
	got, _ := chatOf(h.Context()).FindMessage(m.ID)
	if _, ok := reactionOf(got, "😀"); ok || !slices.Equal(got.Reactions, m.Reactions) {
		t.Fatalf("rolled back: %+v", got.Reactions)
	}
	if tt := toastText(t, h); !strings.Contains(tt, reactFailed) || !strings.Contains(tt, "한도") {
		t.Fatalf("toast %q", tt)
	}
	if strings.Contains(h.Screen(), "😀 1") {
		t.Fatal("the chip is gone again")
	}

	// removing fails: my chip comes back
	h.API.Error("DELETE /v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👍"), http.StatusInternalServerError, "internal")
	h.Send(runCmdMsg(toggleReactionCmd(h.Context(), got, "👍")))
	got, _ = chatOf(h.Context()).FindMessage(m.ID)
	if r, ok := reactionOf(got, "👍"); !ok || !r.Me || r.Count != 2 {
		t.Fatalf("restored: %+v", got.Reactions)
	}
}

func TestEditKey(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, "@minji 점심 먹고 ㄱㄱ")
	h.Press("e")
	cc := channelOf(t, h).composer
	if !cc.focused() || cc.editing == nil || cc.c.Value() != "@minji 점심 먹고 ㄱㄱ" {
		t.Fatalf("e edits my message: focused=%v value=%q", cc.focused(), cc.c.Value())
	}
	mustContain(t, h.Screen(), "수정 중")
	h.Press("esc", "esc")

	selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("e")
	if cc.editing != nil || cc.focused() {
		t.Fatal("someone else's message is never edited")
	}
	if tt := toastText(t, h); !strings.Contains(tt, "내 메시지만 수정할 수 있어요") {
		t.Fatalf("toast %q", tt)
	}
}

func TestDeleteConfirm(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	mine := selectText(t, h, "@minji 점심 먹고 ㄱㄱ")
	h.Press("d")
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "이 메시지를 삭제할까요?") {
		t.Fatalf("d opens the confirmation:\n%s", h.Screen())
	}
	if n := len(reqs(h.API, "DELETE", "/v1/messages/"+mine.ID)); n != 0 {
		t.Fatal("nothing is deleted before confirming")
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", "/v1/messages/"+mine.ID)); n != 1 {
		t.Fatalf("y deletes (%d)", n)
	}
	if got, _ := chatOf(h.Context()).FindMessage(mine.ID); !got.Deleted {
		t.Fatal("the deleted copy is a tombstone at once")
	}
	if !strings.Contains(h.Screen(), deletedText) || strings.Contains(h.Screen(), "점심 먹고 ㄱㄱ") {
		t.Fatalf("drawn as deleted:\n%s", h.Screen())
	}

	h = devChannel(t, uitest.Size{W: 120, H: 40})
	m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
	h.Send(runCmdMsg(deleteMessageCmd(h.Context(), m)))
	if n := len(reqs(h.API, "DELETE", "/v1/messages/"+m.ID)); n != 1 {
		t.Fatal("deleteMessageCmd sends DELETE /v1/messages/<id>")
	}

	h = devChannel(t, uitest.Size{W: 120, H: 40})
	setRole(h, api.RoleMember)
	selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("d")
	if overlays(h) != 0 {
		t.Fatal("a member gets no confirmation for someone else's message")
	}
	if tt := toastText(t, h); !strings.Contains(tt, "삭제할 권한이 없어요") {
		t.Fatalf("toast %q", tt)
	}
	setRole(h, api.RoleOwner)
	h.Press("d")
	if overlays(h) != 1 {
		t.Fatal("the owner may delete anyone's message")
	}
}

func TestDeleteFailureToasts(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := fixtureMsg(t, h, "@minji 점심 먹고 ㄱㄱ")
	h.API.Error("DELETE /v1/messages/"+m.ID, http.StatusForbidden, "forbidden")
	h.Send(runCmdMsg(deleteMessageCmd(h.Context(), m)))
	if got, _ := chatOf(h.Context()).FindMessage(m.ID); got.Deleted {
		t.Fatal("a failed delete keeps the message")
	}
	if tt := toastText(t, h); !strings.Contains(tt, deleteFailed) {
		t.Fatalf("toast %q", tt)
	}
}

func TestEmptyEditConfirmsDelete(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, "@minji 점심 먹고 ㄱㄱ")
	h.Press("e")
	cc := channelOf(t, h).composer
	cc.c.Reset()
	h.Press("enter")
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "이 메시지를 삭제할까요?") {
		t.Fatalf("an emptied edit asks to delete:\n%s", h.Screen())
	}
}

func TestPinPermission(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	setRole(h, api.RoleMember)
	m := selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("p")
	if tt := toastText(t, h); !strings.Contains(tt, "고정할 권한이 없어요") {
		t.Fatalf("toast %q", tt)
	}
	if n := len(reqs(h.API, "PUT", "/v1/messages/"+m.ID+"/pin")); n != 0 {
		t.Fatal("no request without permission")
	}

	h = devChannel(t, uitest.Size{W: 120, H: 40})
	m = selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("p")
	if n := len(reqs(h.API, "PUT", "/v1/messages/"+m.ID+"/pin")); n != 1 {
		t.Fatalf("owner pins (%d)", n)
	}
	h.Event("message.pinned", map[string]any{"channel_id": m.ChannelID, "message_id": m.ID, "pinned_by": fixtures.UserSam})
	row, _ := lineWith(t, h.Screen(), "민지 11:02")
	if l := strings.Split(h.Screen(), "\n")[row]; !strings.Contains(l, "📌") {
		t.Fatalf("pinned header %q", l)
	}
	h.Press("p") // pinned: p unpins
	if n := len(reqs(h.API, "DELETE", "/v1/messages/"+m.ID+"/pin")); n != 1 {
		t.Fatal("p on a pinned message unpins")
	}

	// a DM: anyone may pin
	h = open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}, uitest.WithSeed(bootstrappedSeed))
	setRole(h, api.RoleMember)
	m = selectText(t, h, "점심 국밥 어때요?")
	h.Press("p")
	if n := len(reqs(h.API, "PUT", "/v1/messages/"+m.ID+"/pin")); n != 1 {
		t.Fatal("DM pin")
	}
}

func TestCopyKey(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("y")
	if !hasClipboard(h.TeaMsgs(), "오늘 배포 몇 시에 해요?") {
		t.Fatal("tea.SetClipboard")
	}
	if tt := toastText(t, h); !strings.Contains(tt, "복사했어요") {
		t.Fatalf("toast %q", tt)
	}
}

func TestDeletedMessageHasNoActions(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
	h.Event("message.deleted", realtime.MessageDeleted{ChannelID: m.ChannelID, MessageID: m.ID, Tombstone: true})
	v := channelOf(t, h)
	v.pane.selectID(m.ID)
	h.Press("r", "y", "p", "e")
	if overlays(h) != 0 || hasClipboard(h.TeaMsgs(), "") || len(reqs(h.API, "PUT", "/v1/messages/"+m.ID+"/pin")) != 0 || v.composer.editing != nil {
		t.Fatal("a deleted message takes no react / copy / pin / edit")
	}
}

func TestActionsMenu(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	setRole(h, api.RoleMember)
	selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("m")
	if overlays(h) != 1 {
		t.Fatal("m opens the menu")
	}
	want := []string{"스레드로 답글", "리액션", "리액션 누른 사람", "고정", "복사", "작성자 · 민지", "신고", "작성자 차단"}
	if labels := menuLabels(t, h); !slices.Equal(labels, want) {
		t.Fatalf("items %q, want %q", labels, want)
	}
	h.Press("p") // 고정 is disabled for a member: its hint does nothing
	if overlays(h) != 1 || len(reqs(h.API, "PUT", "/v1/messages/"+fixtureMsg(t, h, "오늘 배포 몇 시에 해요?").ID+"/pin")) != 0 {
		t.Fatal("고정 is disabled for a member")
	}
	clickOn(t, h, "신고", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "신고 · 민지의 메시지") {
		t.Fatalf("신고 morphs into the report dialog (depth 1):\n%s", h.Screen())
	}
	h.Press("esc")

	// 작성자 · 민지: the author menu (CA 6.1) reports the author as a user
	selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("m")
	clickOn(t, h, "작성자 · 민지", tea.MouseLeft)
	if want := []string{"DM 보내기", "프로필 보기", "사용자 신고", "차단"}; !slices.Equal(menuLabels(t, h), want) {
		t.Fatalf("author menu %q, want %q", menuLabels(t, h), want)
	}
	clickOn(t, h, "사용자 신고", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "신고 · 민지") || strings.Contains(h.Screen(), "민지의 메시지") {
		t.Fatalf("사용자 신고 morphs into the user report dialog:\n%s", h.Screen())
	}
	h.Press("enter") // reason → note
	h.Type("도배")
	h.Press("enter") // submit
	var report []byte
	for _, r := range h.API.Requests() {
		if r.Method == "POST" && r.Path == "/v1/reports" {
			report = r.Body
		}
	}
	if jsonField(report, "target_type") != "user" || jsonField(report, "target_id") != fixtures.UserMinji {
		t.Fatalf("the author is reported as a user: %s", report)
	}
	if overlays(h) != 0 {
		h.Press("esc")
	}

	selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.Press("m")
	clickOn(t, h, "작성자 차단", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "차단할까요?") {
		t.Fatalf("작성자 차단 asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "PUT", "/v1/blocks/@minji")); n != 1 {
		t.Fatalf("PUT /v1/blocks/@minji (%d): %+v", n, h.API.Requests())
	}
	if !h.Context().Store.IsBlocked(fixtures.UserMinji) || !strings.Contains(h.Screen(), blockedText) {
		t.Fatal("blocked at once: her messages fold")
	}
}

func TestActionsMenuMine(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, "@minji 점심 먹고 ㄱㄱ")
	h.Press("m")
	want := []string{"스레드로 답글", "리액션", "수정", "고정", "복사", "삭제"}
	if labels := menuLabels(t, h); !slices.Equal(labels, want) {
		t.Fatalf("items %q, want %q", labels, want)
	}
	h.Press("e") // the hint key runs 수정
	cc := channelOf(t, h).composer
	if overlays(h) != 0 || cc.editing == nil || !cc.focused() {
		t.Fatal("수정 from the menu starts the edit")
	}
}

func TestActionsMenuOverlayArgs(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
	h.Send(ui.OpenOverlayMsg{ID: "chat.message_actions", Args: m.ChannelID + " " + m.ID})
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "스레드로 답글") {
		t.Fatalf("chat.message_actions opens by args:\n%s", h.Screen())
	}
	h.Press("t")
	if r, ok := threadRoute(h); !ok || r.ID != m.ID || r.Arg != m.ChannelID {
		t.Fatalf("스레드로 답글 opens the thread: %+v", h.Model.Frame().Routes)
	}
	h.Send(ui.OpenOverlayMsg{ID: "chat.message_actions", Args: "nope"})
	if overlays(h) != 0 {
		t.Fatal("unknown message: no menu")
	}
}

func TestReactionUsersSubmenu(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := selectText(t, h, "오늘 배포 몇 시에 해요?")
	h.API.JSON("GET /v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👀"), 200, map[string]any{"items": []api.UserSummary{userBy(t, h.Context().Store, "hana")}, "next_cursor": nil})
	h.Press("m")
	clickOn(t, h, "리액션 누른 사람", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "👍 2") || !strings.Contains(h.Screen(), "👀 1") {
		t.Fatalf("several emoji: pick one first:\n%s", h.Screen())
	}
	clickOn(t, h, "👀 1", tea.MouseLeft)
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "👀 누른 사람") || !strings.Contains(h.Screen(), "@hana") {
		t.Fatalf("then the list:\n%s", h.Screen())
	}
}

func TestReactionUsersPaging(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
	s := h.Context().Store
	path := "GET /v1/messages/" + m.ID + "/reactions/" + url.PathEscape("👍")
	h.API.Handle(path, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "c2" {
			writeJSON(w, 200, map[string]any{"items": []api.UserSummary{userBy(t, s, "hana")}, "next_cursor": nil})
			return
		}
		writeJSON(w, 200, map[string]any{"items": []api.UserSummary{userBy(t, s, "junho"), userBy(t, s, "minji")}, "next_cursor": "c2"})
	})
	h.Send(ui.OpenOverlayMsg{ID: "chat.reaction_users", Args: m.ID + " 👍"})
	if !strings.Contains(h.Screen(), "@junho") || strings.Contains(h.Screen(), "@hana") {
		t.Fatalf("first page:\n%s", h.Screen())
	}
	h.Press("j") // the last item of the page: the next page is fetched
	if !strings.Contains(h.Screen(), "@hana") {
		t.Fatalf("next page appended:\n%s", h.Screen())
	}
	if q := lastQuery(h.API, "/v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👍")); !strings.Contains(q, "cursor=c2") {
		t.Fatalf("cursor query %q", q)
	}
	h.Press("j", "j")
	if n := len(reqs(h.API, "GET", "/v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👍"))); n != 2 {
		t.Fatalf("no request past the last page (%d)", n)
	}
}

func TestActionsMenuGolden(t *testing.T) {
	forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) {
		setRole(h, api.RoleMember)
		selectText(t, h, "오늘 배포")
		h.Press("m")
		h.Golden("message_actions")
	})
}

func TestReactionUsersGolden(t *testing.T) {
	forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) {
		m := fixtureMsg(t, h, "오늘 배포 몇 시에 해요?")
		h.API.JSON("GET /v1/messages/"+m.ID+"/reactions/"+url.PathEscape("👍"), 200, map[string]any{"items": []api.UserSummary{userBy(t, h.Context().Store, "junho")}, "next_cursor": nil})
		h.Send(ui.OpenOverlayMsg{ID: "chat.reaction_users", Args: m.ID + " 👍"})
		h.Golden("reaction_users")
	})
}

func TestPollVoteViaMenuAndClick(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := selectText(t, h, "점심 메뉴")
	pid := m.Poll.ID
	h.Press("m")
	labels := menuLabels(t, h)
	mustContainInOrder(t, strings.Join(labels, "|"), "리액션", "투표: 국밥", "투표: 돈까스", "고정")
	if slices.Contains(labels, "투표 취소") || slices.Contains(labels, "투표 마감") {
		t.Fatalf("no retract before voting, no close for a non-creator: %q", labels)
	}
	clickOn(t, h, "투표: 국밥", tea.MouseLeft)
	r := reqs(h.API, "PUT", "/v1/polls/"+pid+"/vote")
	if len(r) != 1 {
		t.Fatalf("PUT vote (%d)", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"option_idxs":[0]}`)
	got, _ := chatOf(h.Context()).FindMessage(m.ID)
	if got.Poll == nil || !slices.Equal(got.Poll.MyVotes, []int{0}) || got.Poll.Options[0].VoteCount != 5 {
		t.Fatalf("SetPoll authoritative: %+v", got.Poll)
	}

	clickText(t, h, layout.SlotMain, "돈까스")
	r = reqs(h.API, "PUT", "/v1/polls/"+pid+"/vote")
	if len(r) != 2 {
		t.Fatalf("click on a bar votes (%d)", len(r))
	}
	mustJSONObject(t, r[1].Body, `{"option_idxs":[1]}`)
	got, _ = chatOf(h.Context()).FindMessage(m.ID)
	if !slices.Equal(got.Poll.MyVotes, []int{1}) {
		t.Fatalf("my vote moved: %+v", got.Poll.MyVotes)
	}

	selectText(t, h, "점심 메뉴")
	h.Press("m")
	clickOn(t, h, "투표 취소", tea.MouseLeft)
	if n := len(reqs(h.API, "DELETE", "/v1/polls/"+pid+"/vote")); n != 1 {
		t.Fatal("투표 취소 retracts")
	}
	got, _ = chatOf(h.Context()).FindMessage(m.ID)
	if len(got.Poll.MyVotes) != 0 {
		t.Fatalf("retracted: %+v", got.Poll.MyVotes)
	}
}

func TestPollMultiAndClose(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := fixtureMsg(t, h, "점심 메뉴")
	p := *m.Poll
	p.Multi, p.CreatorID, p.MyVotes = true, h.Context().Store.Me.ID, []int{0}
	chatOf(h.Context()).SetPoll(p, true)
	h.Send(runCmdMsg(voteCmd(h.Context(), p, []int{0, 1})))
	r := reqs(h.API, "PUT", "/v1/polls/"+p.ID+"/vote")
	if len(r) != 1 {
		t.Fatal("voteCmd")
	}
	mustJSONObject(t, r[0].Body, `{"option_idxs":[0,1]}`)

	got, _ := chatOf(h.Context()).FindMessage(m.ID)
	if !slices.Equal(got.Poll.MyVotes, []int{0, 1}) {
		t.Fatalf("multi vote stored: %+v", got.Poll.MyVotes)
	}
	mine := *got.Poll // the canned server answer is 민지's single-choice poll: make it my multi poll again
	mine.CreatorID, mine.Multi = h.Context().Store.Me.ID, true
	chatOf(h.Context()).SetPoll(mine, true)
	channelOf(t, h).pane.refresh(h.Context())
	if v := pollToggle(mine, 1); !slices.Equal(v, []int{0}) {
		t.Fatalf("multi toggle off: %v", v)
	}
	selectText(t, h, "점심 메뉴")
	h.Press("m")
	labels := menuLabels(t, h)
	if !slices.Contains(labels, "투표 마감") {
		t.Fatalf("the creator may close: %q", labels)
	}
	clickOn(t, h, "투표 마감", tea.MouseLeft)
	if n := len(reqs(h.API, "POST", "/v1/polls/"+p.ID+"/close")); n != 1 {
		t.Fatal("투표 마감 closes")
	}
	got, _ = chatOf(h.Context()).FindMessage(m.ID)
	if !got.Poll.Closed {
		t.Fatal("closed poll stored")
	}
	clickText(t, h, layout.SlotMain, "돈까스")
	if n := len(reqs(h.API, "PUT", "/v1/polls/"+p.ID+"/vote")); n != 1 {
		t.Fatal("a closed poll takes no votes")
	}
}

// menuLabels are the item labels of the top overlay (a menu) as drawn: the
// "› " cursor marker and the right-aligned one-key hint dropped.
func menuLabels(t testing.TB, h *uitest.Harness) []string {
	t.Helper()
	scr := h.Screen()
	ov := h.Model.Frame().Overlays
	if len(ov) == 0 {
		t.Fatalf("no overlay:\n%s", scr)
	}
	b := ov[len(ov)-1]
	r := b.Rect
	if b.Bordered {
		r = r.Inner()
	}
	var out []string
	for _, l := range cropRect(scr, r) {
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "› "))
		if i := strings.LastIndex(l, " "); i >= 0 && len(l)-i == 2 && l[i+1] < 0x80 {
			l = strings.TrimSpace(l[:i])
		}
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
