package chat

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// strip removes ANSI escapes from every line.
func strip(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

// mustContain fails unless s contains every sub.
func mustContain(t testing.TB, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Fatalf("%q does not contain %q", s, sub)
		}
	}
}

var allDensities = []ui.Density{ui.DensityFull, ui.DensityLine, ui.DensityBubble, ui.DensityMinimal}

func renderCtx(t *testing.T) *ui.Context {
	t.Helper()
	return open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}).Context()
}

func msgRow(m *api.Message) row { return row{Kind: rowMessage, ID: m.ID, Msg: m} }

func TestRenderDensities(t *testing.T) {
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome})
	ctx := h.Context()
	s := ctx.Store
	minji := userBy(t, s, "minji")
	me := *s.User(s.Me.ID)
	at := time.Date(2026, 11, 5, 2, 2, 0, 0, time.UTC) // 11:02 KST
	m := mkMsg("C", 1, minji, "오늘 배포 몇 시에 해요?", at)
	m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 2, Me: true}}
	m.Thread = &api.ThreadSummary{ReplyCount: 3}
	full := strip(renderRow(ctx, row{Kind: rowMessage, ID: m.ID, Msg: &m}, msgStyle{Density: ui.DensityFull, Width: 60}))
	mustContain(t, full[0], "민지", "11:02")
	mustContain(t, strings.Join(full, "\n"), "오늘 배포 몇 시에 해요?", "답글 3개")
	line := strip(renderRow(ctx, row{Kind: rowMessage, ID: m.ID, Msg: &m}, msgStyle{Density: ui.DensityLine, Width: 60}))
	if len(line) != 1 || !strings.HasPrefix(line[0], "11:02 민지 오늘 배포 몇 시에 해요?") || !strings.Contains(line[0], "💬3") {
		t.Fatalf("%q", line)
	}
	mine := mkMsg("C", 2, me, "점심 먹고 ㄱㄱ", at.Add(3*time.Minute))
	bub := strip(renderRow(ctx, row{Kind: rowMessage, ID: mine.ID, Msg: &mine}, msgStyle{Density: ui.DensityBubble, Width: 40}))
	if strings.TrimSpace(bub[0]) != "11:05 나" || textwidth.Width(strings.TrimRight(bub[0], " ")) != 40 {
		t.Fatalf("mine must be right-aligned: %q", bub)
	}
	min := strip(renderRow(ctx, row{Kind: rowMessage, ID: m.ID, Msg: &m}, msgStyle{Density: ui.DensityMinimal, Width: 20}))
	if len(min) != 1 || !strings.HasPrefix(min[0], "민지 오늘") {
		t.Fatalf("%q", min)
	}
}

func TestRenderFullDetails(t *testing.T) {
	ctx := renderCtx(t)
	s := ctx.Store
	minji := userBy(t, s, "minji")
	at := time.Date(2026, 11, 5, 2, 2, 0, 0, time.UTC)
	m := mkMsg("C", 1, minji, "본문", at)
	m.Pinned = true
	m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 2}}
	m.Thread = &api.ThreadSummary{ReplyCount: 3}
	full := strip(renderRow(ctx, msgRow(&m), msgStyle{Density: ui.DensityFull, Width: 60}))
	if len(full) != 3 || !strings.HasPrefix(full[0], "민지 11:02") || !strings.Contains(full[0], "📌") {
		t.Fatalf("header: %q", full)
	}
	if full[1] != "  본문" {
		t.Fatalf("body is indented 2: %q", full[1])
	}
	if !strings.HasPrefix(full[2], "  👍 2 · 💬 답글 3개") {
		t.Fatalf("chips and replies share a line: %q", full[2])
	}
	// continuation: no header line.
	cont := strip(renderRow(ctx, msgRow(&m), msgStyle{Density: ui.DensityFull, Width: 60, Continuation: true}))
	if len(cont) != 2 || cont[0] != "  본문" {
		t.Fatalf("continuation: %q", cont)
	}
	// in a thread the reply count is not shown.
	th := strip(renderRow(ctx, msgRow(&m), msgStyle{Density: ui.DensityFull, Width: 60, InThread: true}))
	if strings.Contains(strings.Join(th, "\n"), "답글") {
		t.Fatalf("thread rows hide the reply count: %q", th)
	}
	// a long body wraps inside Width-2 and stays indented.
	long := mkMsg("C", 2, minji, strings.Repeat("가나다라 ", 20), at)
	for _, l := range strip(renderRow(ctx, msgRow(&long), msgStyle{Density: ui.DensityFull, Width: 30}))[1:] {
		if !strings.HasPrefix(l, "  ") || textwidth.Width(l) > 30 {
			t.Fatalf("wrapped body line %q", l)
		}
	}
}

func TestRenderBubbleOthers(t *testing.T) {
	ctx := renderCtx(t)
	minji := userBy(t, ctx.Store, "minji")
	at := time.Date(2026, 11, 5, 2, 2, 0, 0, time.UTC)
	m := mkMsg("C", 1, minji, "안녕", at)
	m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 2}}
	m.Thread = &api.ThreadSummary{ReplyCount: 3}
	b := strip(renderRow(ctx, msgRow(&m), msgStyle{Density: ui.DensityBubble, Width: 40}))
	if len(b) != 3 || b[0] != "민지 11:02" || strings.TrimSpace(b[1]) != "안녕" || !strings.HasPrefix(b[1], " ") {
		t.Fatalf("bubble: %q", b)
	}
	if !strings.HasPrefix(b[2], "💬3") || !strings.Contains(b[2], "👍 2") {
		t.Fatalf("bubble meta: %q", b)
	}
	// the bubble is at most 80% of the width.
	long := mkMsg("C", 2, minji, strings.Repeat("가나다라 ", 30), at)
	for _, l := range strip(renderRow(ctx, msgRow(&long), msgStyle{Density: ui.DensityBubble, Width: 50}))[1:] {
		if textwidth.Width(l) > 40 {
			t.Fatalf("bubble wider than 80%%: %q", l)
		}
	}
	// my bubble ends at the right edge.
	me := *ctx.Store.User(ctx.Store.Me.ID)
	mine := mkMsg("C", 3, me, "ㅇㅋ", at)
	mb := renderRow(ctx, msgRow(&mine), msgStyle{Density: ui.DensityBubble, Width: 40})
	if got := strip(mb)[1]; !strings.HasSuffix(got, "ㅇㅋ ") || textwidth.Width(got) != 40 {
		t.Fatalf("my bubble must be right-aligned: %q", got)
	}
	// the bubble has the chip background (not the base background).
	if !strings.Contains(mb[1], bgSGR(ctx.Theme.P.Chip)) {
		t.Fatalf("bubble background missing: %q", mb[1])
	}
}

func TestRenderKinds(t *testing.T) {
	ctx := renderCtx(t)
	s := ctx.Store
	minji := userBy(t, s, "minji")
	junho := userBy(t, s, "junho")
	at := time.Date(2026, 11, 5, 2, 2, 0, 0, time.UTC)
	full := func(m api.Message) []string {
		return strip(renderRow(ctx, msgRow(&m), msgStyle{Density: ui.DensityFull, Width: 60}))
	}

	me := mkMsg("C", 1, minji, "커피 마시는 중", at)
	me.Kind = "me"
	for _, d := range allDensities {
		got := strings.Join(strip(renderRow(ctx, msgRow(&me), msgStyle{Density: d, Width: 60})), "\n")
		mustContain(t, got, "* 민지 커피 마시는 중")
	}

	roll := api.RollResult{Expr: "2d6", Rolls: []int{3, 5}, Modifier: 2, Total: 10}
	if got := rollText(roll); got != "🎲 2d6 → 3 + 5 + 2 = 10" {
		t.Fatalf("rollText %q", got)
	}
	if got := rollText(api.RollResult{Expr: "1d20", Rolls: []int{17}, Total: 17}); got != "🎲 1d20 → 17" {
		t.Fatalf("single roll %q", got)
	}
	if got := rollText(api.RollResult{Expr: "2d6-1", Rolls: []int{1, 2}, Modifier: -1, Total: 2}); got != "🎲 2d6-1 → 1 + 2 - 1 = 2" {
		t.Fatalf("negative modifier %q", got)
	}
	rm := mkMsg("C", 2, minji, "/roll 2d6+2", at)
	rm.Kind, rm.Roll = "roll", &roll
	mustContain(t, strings.Join(full(rm), "\n"), "🎲 2d6 → 3 + 5 + 2 = 10")

	poll := api.Poll{ID: "P", Question: "점심 메뉴", VoterCount: 3, MyVotes: []int{0},
		Options: []api.PollOption{{Idx: 0, Text: "국밥", VoteCount: 2}, {Idx: 1, Text: "짜장", VoteCount: 1}}}
	pm := mkMsg("C", 3, minji, "점심 메뉴", at)
	pm.Kind, pm.Poll = "poll", &poll
	pl := full(pm)
	if strings.TrimSpace(pl[1]) != "📊 투표: 점심 메뉴" {
		t.Fatalf("poll header %q", pl)
	}
	all := strings.Join(pl, "\n")
	mustContain(t, all, "✓ 국밥", "짜장", "3명 참여")
	if strings.Contains(all, "(마감)") {
		t.Fatal("open poll is not closed")
	}
	closed := poll
	closed.Closed = true
	pm.Poll = &closed
	if pl := full(pm); !strings.Contains(pl[1], "📊 투표: 점심 메뉴") || !strings.Contains(pl[1], "(마감)") {
		t.Fatalf("closed poll %q", pl)
	}

	sys := mkMsg("C", 4, minji, "", at)
	sys.Kind = "system"
	sys.System = &api.SystemEvent{Event: "member_joined", ActorID: minji.ID}
	for _, d := range allDensities {
		got := strip(renderRow(ctx, msgRow(&sys), msgStyle{Density: d, Width: 60}))
		if len(got) != 1 || !strings.Contains(got[0], "── 민지님이 들어왔어요 ──") {
			t.Fatalf("system %v: %q", d, got)
		}
	}

	del := mkMsg("C", 5, minji, "지운 글", at)
	del.Deleted = true
	got := strings.Join(full(del), "\n")
	if !strings.Contains(got, "삭제된 메시지") || strings.Contains(got, "지운 글") {
		t.Fatalf("deleted %q", got)
	}

	ed := mkMsg("C", 6, minji, "고친 글", at)
	ed.EditedAt = &at
	if fl := full(ed); !strings.HasSuffix(fl[len(fl)-1], "고친 글 (수정됨)") {
		t.Fatalf("edited %q", fl)
	}

	code := mkMsg("C", 7, junho, "```sh\n$ wrangler deploy --env prod\n```", at)
	found := false
	for _, l := range full(code) {
		if i := strings.Index(l, "▎"); i >= 0 && strings.Contains(l[i:], "$ wrangler deploy --env prod") {
			found = true
		}
	}
	if !found {
		t.Fatalf("code block %q", full(code))
	}

	s.Blocks[minji.ID] = at
	bl := mkMsg("C", 8, minji, "안 보여야 함", at)
	bl.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 2}}
	for _, d := range allDensities {
		got := strip(renderRow(ctx, msgRow(&bl), msgStyle{Density: d, Width: 60}))
		if len(got) != 1 || strings.TrimSpace(got[0]) != "차단한 사용자의 메시지" {
			t.Fatalf("blocked %v: %q", d, got)
		}
	}
}

func TestSystemText(t *testing.T) {
	ctx := renderCtx(t)
	s := ctx.Store
	minji, junho := userBy(t, s, "minji"), userBy(t, s, "junho")
	jid := junho.ID
	cases := []struct {
		e    api.SystemEvent
		want string
	}{
		{api.SystemEvent{Event: "member_joined", ActorID: minji.ID}, "민지님이 들어왔어요"},
		{api.SystemEvent{Event: "member_left", ActorID: minji.ID}, "민지님이 나갔어요"},
		{api.SystemEvent{Event: "member_kicked", ActorID: minji.ID, TargetUserID: &jid}, "준호님이 내보내졌어요"},
		{api.SystemEvent{Event: "channel_created", ActorID: minji.ID, Data: api.SystemEventData{Name: "개발"}}, "#개발 채널을 만들었어요"},
		{api.SystemEvent{Event: "channel_renamed", ActorID: minji.ID, Data: api.SystemEventData{Name: "백엔드"}}, "채널 이름 변경: #백엔드"},
		{api.SystemEvent{Event: "channel_topic_changed", ActorID: minji.ID, Data: api.SystemEventData{Topic: "배포 얘기"}}, "주제 변경: 배포 얘기"},
		{api.SystemEvent{Event: "message_pinned", ActorID: minji.ID, Data: api.SystemEventData{MessageID: "M"}}, "메시지를 고정했어요"},
		{api.SystemEvent{Event: "group_renamed", ActorID: minji.ID, Data: api.SystemEventData{Name: "점심팟"}}, "대화 이름 변경: 점심팟"},
		{api.SystemEvent{Event: "group_member_added", ActorID: minji.ID, TargetUserID: &jid}, "준호님이 들어왔어요"},
		{api.SystemEvent{Event: "group_member_removed", ActorID: minji.ID, TargetUserID: &jid}, "준호님이 빠졌어요"},
		{api.SystemEvent{Event: "member_joined", ActorID: "01JA00000000000000000NOBODY"}, "알 수 없는 사용자님이 들어왔어요"},
		{api.SystemEvent{Event: "channel_renamed", ActorID: minji.ID, Data: api.SystemEventData{Name: "a\x1b[31mb"}}, "채널 이름 변경: #ab"},
	}
	for _, c := range cases {
		if got := systemText(ctx, c.e); got != c.want {
			t.Errorf("%s: %q, want %q", c.e.Event, got, c.want)
		}
	}
}

func TestBuildRowsDividersAndPending(t *testing.T) {
	ctx := renderCtx(t)
	if ctx.Density() != ui.DensityFull {
		t.Fatalf("120x40 must be full density, got %v", ctx.Density())
	}
	minji := userBy(t, ctx.Store, "minji")
	d1 := time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC)
	msgs := []api.Message{
		mkMsg("C", 1, minji, "a", d1), mkMsg("C", 2, minji, "b", d1.Add(time.Minute)),
		mkMsg("C", 3, minji, "c", d2), mkMsg("C", 4, minji, "d", d2.Add(time.Minute)),
	}
	failed := chatstore.PendingMessage{Nonce: "N2", Body: "실패한 글", Kind: "text", Failed: errors.New("offline"), SentAt: d2.Add(3 * time.Minute)}
	pending := []chatstore.PendingMessage{
		failed,
		{Nonce: "N1", Body: "보내는 글", Kind: "text", SentAt: d2.Add(2 * time.Minute)},
	}
	rows := buildRows(ctx, msgs, pending, rowOpts{LastRead: msgID(2)})
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	want := []string{"day:20261104", msgID(1), msgID(2), "unread", "day:20261105", msgID(3), msgID(4), "pending:N1", "pending:N2"}
	if strings.Join(ids, " ") != strings.Join(want, " ") {
		t.Fatalf("rows\n got %v\nwant %v", ids, want)
	}
	kinds := map[string]rowKind{"day:20261104": rowDay, "unread": rowUnread, "pending:N1": rowPending, msgID(1): rowMessage}
	for _, r := range rows {
		if k, ok := kinds[r.ID]; ok && r.Kind != k {
			t.Fatalf("%s kind %v", r.ID, r.Kind)
		}
	}
	day := strip(renderRow(ctx, rows[0], msgStyle{Density: ui.DensityFull, Width: 60}))
	if len(day) != 1 || !strings.Contains(day[0], "2026년 11월 4일 수요일") || !strings.HasPrefix(day[0], "─") || textwidth.Width(day[0]) != 60 {
		t.Fatalf("day divider %q", day)
	}
	for _, d := range allDensities {
		u := strip(renderRow(ctx, rows[3], msgStyle{Density: d, Width: 40}))
		if len(u) != 1 || !strings.Contains(u[0], "── 새 메시지 ──") {
			t.Fatalf("unread divider %v: %q", d, u)
		}
		if d != ui.DensityFull && len(renderRow(ctx, rows[0], msgStyle{Density: d, Width: 40})) != 0 {
			t.Fatalf("day divider is full density only (%v)", d)
		}
	}
	fl := strings.Join(strip(renderRow(ctx, rows[len(rows)-1], msgStyle{Density: ui.DensityFull, Width: 80})), "\n")
	mustContain(t, fl, "실패한 글", "전송 실패")
	sl := strings.Join(strip(renderRow(ctx, rows[len(rows)-2], msgStyle{Density: ui.DensityFull, Width: 80})), "\n")
	mustContain(t, sl, "보내는 글", "보내는 중…")
	for _, d := range allDensities {
		got := strings.Join(strip(renderRow(ctx, rows[len(rows)-1], msgStyle{Density: d, Width: 80})), "\n")
		mustContain(t, got, "실패한 글", "전송 실패")
	}

	// Loading puts a rowLoading first; no LastRead match means no unread divider.
	rows = buildRows(ctx, msgs, nil, rowOpts{Loading: true})
	if rows[0].Kind != rowLoading || rows[0].ID != "loading" {
		t.Fatalf("loading row: %+v", rows[0])
	}
	for _, r := range rows {
		if r.Kind == rowUnread {
			t.Fatal("no unread divider without LastRead")
		}
	}
	// the last read message is the newest one: no divider.
	for _, r := range buildRows(ctx, msgs, nil, rowOpts{LastRead: msgID(4)}) {
		if r.Kind == rowUnread {
			t.Fatal("nothing unread after the newest message")
		}
	}
	// the last read message is gone from the window: the divider goes before the first newer one.
	rows = buildRows(ctx, []api.Message{msgs[0], msgs[2]}, nil, rowOpts{LastRead: msgID(2)})
	ids = ids[:0]
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if got := strings.Join(ids, " "); got != "day:20261104 "+msgID(1)+" unread day:20261105 "+msgID(3) {
		t.Fatalf("divider after a deleted read pointer: %s", got)
	}
	// threads have no unread divider.
	for _, r := range buildRows(ctx, msgs, nil, rowOpts{LastRead: msgID(2), InThread: true}) {
		if r.Kind == rowUnread {
			t.Fatal("thread rows have no unread divider")
		}
	}
}

func TestBuildRowsNoDayDividerOutsideFull(t *testing.T) {
	h := open(t, uitest.Size{W: 60, H: 20}, ui.Route{Kind: ui.RouteHome})
	ctx := h.Context()
	if ctx.Density() == ui.DensityFull {
		t.Skip("60x20 is full density in this layout")
	}
	minji := userBy(t, ctx.Store, "minji")
	msgs := []api.Message{mkMsg("C", 1, minji, "a", time.Date(2026, 11, 4, 3, 0, 0, 0, time.UTC))}
	for _, r := range buildRows(ctx, msgs, nil, rowOpts{}) {
		if r.Kind == rowDay {
			t.Fatalf("day divider in %v", ctx.Density())
		}
	}
}

func TestIsContinuation(t *testing.T) {
	ctx := renderCtx(t)
	minji, junho := userBy(t, ctx.Store, "minji"), userBy(t, ctx.Store, "junho")
	at := time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC)
	a := mkMsg("C", 1, minji, "a", at)
	b := mkMsg("C", 2, minji, "b", at.Add(4*time.Minute))
	c := mkMsg("C", 3, minji, "c", at.Add(10*time.Minute))
	d := mkMsg("C", 4, junho, "d", at.Add(10*time.Minute))
	if !isContinuation(msgRow(&a), msgRow(&b)) {
		t.Fatal("same author within 5 minutes continues")
	}
	if isContinuation(msgRow(&b), msgRow(&c)) || isContinuation(msgRow(&c), msgRow(&d)) {
		t.Fatal("gap > 5 min or other author starts a new group")
	}
	if isContinuation(row{Kind: rowDay, ID: "day:x"}, msgRow(&b)) {
		t.Fatal("a divider breaks the group")
	}
}

func TestRenderSelectedAndFade(t *testing.T) {
	ctx := renderCtx(t)
	th := ctx.Theme
	minji := userBy(t, ctx.Store, "minji")
	m := mkMsg("C", 1, minji, "선택된 글 `code`", time.Date(2026, 11, 5, 2, 2, 0, 0, time.UTC))
	m.Reactions = []api.ReactionSummary{{Emoji: "👍", Count: 2}}
	fade := th.S.NewHighlight.Background(lipgloss.Color("#123456"))
	for _, d := range allDensities {
		for _, st := range []msgStyle{
			{Density: d, Width: 50, Selected: true},
			{Density: d, Width: 50, Fade: &fade},
		} {
			want := th.P.SelBG
			if st.Fade != nil {
				want = st.Fade.GetBackground()
			}
			for _, l := range renderRow(ctx, msgRow(&m), st) {
				if textwidth.Width(l) != 50 {
					t.Fatalf("%v highlighted line must span the width: %q", d, l)
				}
				if strings.Contains(l, bgSGR(th.P.BG)) {
					t.Fatalf("%v base background left in a highlighted line: %q", d, l)
				}
				if !strings.Contains(l, bgSGR(want)) {
					t.Fatalf("%v highlight background missing: %q", d, l)
				}
			}
		}
	}
}

func TestRecolorBG(t *testing.T) {
	th := renderCtx(t).Theme
	in := th.S.Base.Render("a") + th.S.InlineCode.Render("b") + th.S.Bold.Render("c")
	out := recolorBG(in, th.P.BG, th.P.SelBG)
	if ansi.Strip(out) != "abc" || strings.Contains(out, bgSGR(th.P.BG)) || strings.Count(out, bgSGR(th.P.SelBG)) != 2 || !strings.Contains(out, bgSGR(th.P.CodeBG)) {
		t.Fatalf("recolor: %q", out)
	}
}

func TestRenderWidthNeverExceeded(t *testing.T) {
	ctx := renderCtx(t)
	fixtures.Hostile(ctx.Store)
	ch := hostileChannel(t, ctx.Store)
	msgs := fixtures.HostileMessages(ch)
	edited := msgs[0]
	at := edited.CreatedAt
	edited.ID, edited.EditedAt, edited.Pinned = msgID(900), &at, true
	sys := msgs[1]
	sys.ID, sys.Kind, sys.System = msgID(901), "system", &api.SystemEvent{Event: "channel_renamed", ActorID: msgs[1].Author.ID, Data: api.SystemEventData{Name: msgs[1].Body}}
	roll := msgs[2]
	roll.ID, roll.Kind, roll.Roll = msgID(902), "roll", &api.RollResult{Expr: msgs[2].Body, Rolls: []int{1, 2, 3}, Modifier: -5, Total: 1}
	me := msgs[3]
	me.ID, me.Kind = msgID(903), "me"
	msgs = append(msgs, edited, sys, roll, me)
	pend := []chatstore.PendingMessage{
		{Nonce: "N1", Body: msgs[0].Body, Kind: "text", SentAt: uitest.Start},
		{Nonce: "N2", Body: msgs[1].Body, Kind: "me", Failed: errors.New("x"), SentAt: uitest.Start},
	}
	rows := buildRows(ctx, msgs, pend, rowOpts{LastRead: msgs[2].ID, Loading: true})
	rows = append(rows, row{Kind: rowNotice, ID: "notice", Text: msgs[1].Body})
	fade := ctx.Theme.S.NewHighlight
	for _, d := range allDensities {
		for _, w := range []int{1, 2, 3, 10, 33, 94} {
			for i, r := range rows {
				for _, st := range []msgStyle{
					{Density: d, Width: w},
					{Density: d, Width: w, Selected: true, Continuation: true},
					{Density: d, Width: w, InThread: true, Fade: &fade},
				} {
					for j, l := range renderRow(ctx, r, st) {
						sw, wc := ansi.StringWidth(l), ansi.StringWidthWc(l)
						if sw > w || sw != wc {
							t.Fatalf("density %v width %d row %d (%s) line %d: width %d wc %d: %q", d, w, i, r.ID, j, sw, wc, l)
						}
					}
				}
			}
		}
	}
}

func TestRenderPendingOneLineKeepsBody(t *testing.T) {
	ctx := renderCtx(t)
	p := chatstore.PendingMessage{Nonce: "N", Body: "꽤 길게 쓴 실패한 메시지 본문입니다", Kind: "text", Failed: errors.New("x"), SentAt: uitest.Start}
	r := row{Kind: rowPending, ID: "pending:N", Pending: &p}
	for _, d := range []ui.Density{ui.DensityLine, ui.DensityMinimal} {
		got := strip(renderRow(ctx, r, msgStyle{Density: d, Width: 50}))
		if len(got) != 1 || !strings.Contains(got[0], "꽤 길게") || !strings.Contains(got[0], "전송 실패") {
			t.Fatalf("%v: %q", d, got)
		}
	}
	full := strings.Join(strip(renderRow(ctx, r, msgStyle{Density: ui.DensityFull, Width: 80})), "\n")
	mustContain(t, full, "전송 실패 — enter 다시 보내기 · d 버리기")
}
