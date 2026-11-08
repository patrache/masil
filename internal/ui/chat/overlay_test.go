package chat

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store/chatstore"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func TestMenuOverlayGolden(t *testing.T) {
	forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.OpenOverlayMsg{Overlay: newMenu("메시지", []menuItem{
			{Label: "스레드로 답글", Hint: "t"}, {Label: "리액션", Hint: "r"},
			{Label: "고정", Hint: "p", Disabled: true}, {Label: "삭제", Hint: "d", Danger: true},
		})})
		h.Golden("menu")
	})
}

func TestMenuRunsAndMorphs(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	ran := false
	h.Send(ui.OpenOverlayMsg{Overlay: newMenu("t", []menuItem{
		{Label: "A", Disabled: true},
		{Label: "B", Run: func(*ui.Context) (tea.Cmd, ui.Overlay) {
			ran = true
			return nil, newMenu("다음", []menuItem{{Label: "C"}})
		}},
	})})
	h.Press("enter") // disabled A is skipped: cursor starts on B
	if !ran || len(h.Model.Frame().Overlays) != 1 || !strings.Contains(h.Screen(), "다음") {
		t.Fatal("menu must morph in place, stack depth 1")
	}
	h.Press("esc")
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("esc closes")
	}
}

func TestMenuKeysSkipDisabledAndClose(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	var got []string
	run := func(name string) func(*ui.Context) (tea.Cmd, ui.Overlay) {
		return func(*ui.Context) (tea.Cmd, ui.Overlay) { got = append(got, name); return nil, nil }
	}
	h.Send(ui.OpenOverlayMsg{Overlay: newMenu("t", []menuItem{
		{Label: "A", Run: run("A")}, {Label: "B", Disabled: true, Run: run("B")}, {Label: "C", Run: run("C")},
	})})
	h.Press("j", "enter") // B is skipped
	if !slices.Equal(got, []string{"C"}) || len(h.Model.Frame().Overlays) != 0 {
		t.Fatalf("ran %v, overlays %d (a plain Run closes the menu)", got, len(h.Model.Frame().Overlays))
	}
}

func TestMenuClickRuns(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	ran := ""
	h.Send(ui.OpenOverlayMsg{Overlay: newMenu("t", []menuItem{
		{Label: "첫째", Run: func(*ui.Context) (tea.Cmd, ui.Overlay) { ran = "첫째"; return nil, nil }},
		{Label: "둘째", Run: func(*ui.Context) (tea.Cmd, ui.Overlay) { ran = "둘째"; return nil, nil }},
	})})
	row, col := lineWith(t, h.Screen(), "둘째")
	h.Click(col, row)
	if ran != "둘째" || len(h.Model.Frame().Overlays) != 0 {
		t.Fatalf("click ran %q", ran)
	}
}

func TestFormGoldenAndCursor(t *testing.T) {
	spec := formSpec{Title: "방 만들기", Submit: "만들기", Fields: []field{
		{Key: "name", Label: "이름", Max: 50, Validate: func(v string) string {
			if v == "" {
				return "이름을 입력해 주세요"
			}
			return ""
		}},
		{Key: "icon", Label: "아이콘", Kind: fieldChoice, Choices: []string{"없음", "🚀", "🍚"}},
		{Key: "private", Label: "비공개", Kind: fieldToggle},
	}}
	forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.OpenOverlayMsg{Overlay: newForm(spec)})
		h.Type("노바 ❄️")
		h.Golden("form")
		row, col := lineWith(t, h.Screen(), "노바 ❄")
		if c := h.Cursor(); c == nil || c.Y != row || c.X != col+textwidth.Width("노바 ❄") {
			t.Fatalf("cursor %+v", c)
		}
	})
}

func TestFormFieldsEdit(t *testing.T) {
	var vals map[string]string
	spec := formSpec{Title: "t", Submit: "확인", Fields: []field{
		{Key: "name", Label: "이름"},
		{Key: "icon", Label: "아이콘", Kind: fieldChoice, Choices: []string{"없음", "🚀", "🍚"}},
		{Key: "private", Label: "비공개", Kind: fieldToggle},
	}, OnSubmit: func(_ *ui.Context, v map[string]string) (string, tea.Cmd) { vals = v; return "", nil }}
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.OpenOverlayMsg{Overlay: newForm(spec)})
	h.Type("ab")
	h.Paste("c\nd")
	h.Press("tab", "l", "right", "h", "tab", "space", "enter")
	if vals == nil || vals["name"] != "abc d" || vals["icon"] != "🚀" || vals["private"] != "true" {
		t.Fatalf("vals %v", vals)
	}
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("untagged submit closes the form")
	}
}

func TestFormValidationAndDone(t *testing.T) {
	srv := newAPI(t)
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, srv)
	submits, dones := 0, 0
	var done tea.Msg
	spec := formSpec{Title: "방 만들기", Submit: "만들기", Fields: []field{
		{Key: "name", Label: "이름", Validate: func(v string) string {
			if v == "" {
				return "이름을 입력해 주세요"
			}
			return ""
		}},
	}, OnSubmit: func(ctx *ui.Context, vals map[string]string) (string, tea.Cmd) {
		submits++
		return "chat.test:x", ui.Do(ctx, "chat.test:x", func(c context.Context, cl *api.Client) (*api.SpaceDetail, error) {
			return cl.CreateSpace(c, api.CreateSpaceRequest{Name: vals["name"]})
		})
	}, OnDone: func(_ *ui.Context, result tea.Msg) (tea.Cmd, ui.Overlay) {
		dones++
		done = result
		return nil, nil
	}}
	srv.Error("POST /v1/spaces", http.StatusConflict, "conflict")

	h.Send(ui.OpenOverlayMsg{Overlay: newForm(spec)})
	h.Press("enter")
	if !strings.Contains(h.Screen(), "이름을 입력해 주세요") || submits != 0 {
		t.Fatalf("validation: submits=%d\n%s", submits, h.Screen())
	}
	h.Type("a")
	h.Press("enter")
	if submits != 1 || !strings.Contains(h.Screen(), api.Describe(&api.Error{Code: "conflict"})) || len(h.Model.Frame().Overlays) != 1 || dones != 0 {
		t.Fatalf("409: submits=%d dones=%d\n%s", submits, dones, h.Screen())
	}
	srv.JSON("POST /v1/spaces", http.StatusCreated, map[string]any{"space": api.SpaceDetail{Space: api.Space{SpacePreview: api.SpacePreview{ID: "01JBNEW0000000000000000001", Name: "a"}}}})
	h.Press("enter")
	if dones != 1 || len(h.Model.Frame().Overlays) != 0 {
		t.Fatalf("success: dones=%d overlays=%d", dones, len(h.Model.Frame().Overlays))
	}
	if r, ok := done.(ui.ResultMsg[*api.SpaceDetail]); !ok || r.Tag != "chat.test:x" || r.Err != nil || r.Value.ID != "01JBNEW0000000000000000001" {
		t.Fatalf("OnDone got %#v", done)
	}
	if n := len(reqs(srv, "POST", "/v1/spaces")); n != 2 {
		t.Fatalf("POST /v1/spaces sent %d times", n)
	}
}

func TestFormDoneMorphs(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	spec := formSpec{Title: "t", Submit: "확인", Fields: []field{{Key: "name", Label: "이름"}},
		OnSubmit: func(ctx *ui.Context, _ map[string]string) (string, tea.Cmd) {
			return "chat.test:m", ui.Do(ctx, "chat.test:m", func(context.Context, *api.Client) (int, error) { return 7, nil })
		},
		OnDone: func(*ui.Context, tea.Msg) (tea.Cmd, ui.Overlay) {
			return nil, newMenu("완료됨", []menuItem{{Label: "닫기"}})
		}}
	h.Send(ui.OpenOverlayMsg{Overlay: newForm(spec)})
	h.Type("x")
	h.Press("enter")
	if len(h.Model.Frame().Overlays) != 1 || !strings.Contains(h.Screen(), "완료됨") {
		t.Fatalf("form must morph into the OnDone overlay\n%s", h.Screen())
	}
}

func TestResultOf(t *testing.T) {
	e := errors.New("boom")
	if tag, err, ok := resultOf(ui.ResultMsg[int]{Tag: "a", Err: e}); !ok || tag != "a" || err != e {
		t.Fatalf("got %q %v %v", tag, err, ok)
	}
	if tag, err, ok := resultOf(ui.ResultMsg[*api.Message]{Tag: "b"}); !ok || tag != "b" || err != nil {
		t.Fatalf("got %q %v %v", tag, err, ok)
	}
	if _, _, ok := resultOf("string"); ok {
		t.Fatal("a string is not a result")
	}
	if _, _, ok := resultOf(nil); ok {
		t.Fatal("nil is not a result")
	}
}

func TestAdoptSpaceIdempotent(t *testing.T) {
	s := seeded()
	sid, cid := "01JBNEW00000000000000000S1", "01JBNEW00000000000000000C1"
	name := "일반"
	mk := func() *api.SpaceDetail {
		return &api.SpaceDetail{Space: api.Space{SpacePreview: api.SpacePreview{ID: sid, Name: "새 방"}, MyRole: api.RoleOwner},
			Channels: []api.Channel{{ID: cid, Kind: api.ChannelText, SpaceID: &sid, Name: &name}}}
	}
	adoptSpace(s, mk())
	adoptSpace(s, mk())
	n := 0
	for _, id := range s.SpaceOrder {
		if id == sid {
			n++
		}
	}
	if n != 1 || s.Spaces[sid] == nil || s.Channels[cid] == nil || len(s.SpaceChannels(sid)) != 1 || len(s.Spaces[sid].Channels) != 1 {
		t.Fatalf("order count %d, space %v, channel %v", n, s.Spaces[sid], s.Channels[cid])
	}
	if s.SpaceOrder[0] != fixtures.SpaceNova {
		t.Fatal("existing order kept, new space appended")
	}
}

func TestPendingForScopes(t *testing.T) {
	s := seeded()
	ctx := &ui.Context{Store: s, Now: func() time.Time { return uitest.Start }}
	ch, root := fixtures.ChDev, "R"
	c := chatOf(ctx)
	c.AddPending(ch, chatstore.PendingMessage{Nonce: "N1", Body: "a"})
	c.AddPending(ch, chatstore.PendingMessage{Nonce: "N2", Body: "b", ThreadRootID: &root})
	nonces := func(ps []chatstore.PendingMessage) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Nonce)
		}
		return out
	}
	if got := nonces(pendingFor(ctx, ch, "")); !slices.Equal(got, []string{"N1"}) {
		t.Fatalf("channel scope %v", got)
	}
	if got := nonces(pendingFor(ctx, ch, "R")); !slices.Equal(got, []string{"N2"}) {
		t.Fatalf("thread scope %v", got)
	}
	if got := pendingFor(ctx, ch, "X"); len(got) != 0 {
		t.Fatalf("other thread %v", got)
	}
}

func TestVisibleDMsHidesBlocked(t *testing.T) {
	s := seeded()
	ctx := &ui.Context{Store: s}
	gname := "점심팟"
	group := api.Channel{ID: "01JB000000000000000000GDM1", Kind: api.ChannelGroupDM, Name: &gname, MemberCount: 3,
		Members:   []api.UserSummary{userBy(t, s, "sam"), userBy(t, s, "minji"), userBy(t, s, "junho")},
		CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}
	s.ObserveChannel(group)
	minjiDM, junhoDM := dmWith(t, s, "minji"), dmWith(t, s, "junho")
	ids := func() []string {
		var out []string
		for _, ch := range visibleDMs(ctx) {
			out = append(out, ch.ID)
		}
		return out
	}
	if got := ids(); !slices.Contains(got, minjiDM) || !slices.Contains(got, junhoDM) || !slices.Contains(got, group.ID) {
		t.Fatalf("before block %v", got)
	}
	s.Blocks[userBy(t, s, "junho").ID] = uitest.Start
	got := ids()
	if slices.Contains(got, junhoDM) || !slices.Contains(got, minjiDM) || !slices.Contains(got, group.ID) {
		t.Fatalf("after block %v", got)
	}
	if want := slices.DeleteFunc(slices.Clone(s.DMOrder), func(id string) bool { return id == junhoDM }); !slices.Equal(got, want) {
		t.Fatalf("order %v, want DMOrder %v", got, want)
	}
}

func TestChatOfSetsClock(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Advance(72 * time.Hour) // far from the wall clock: a time.Now receipt would fail below
	ctx := h.Context()
	c := chatOf(ctx)
	if c == nil || chatOf(ctx) != c {
		t.Fatal("chatOf returns the store's chat reducer")
	}
	h.Event("typing", map[string]any{"channel_id": fixtures.ChDev, "thread_root_id": nil, "user_id": fixtures.UserMinji, "expires_in_ms": 6000})
	now := ctx.Now()
	if got := c.Typing(fixtures.ChDev, nil, now.Add(5*time.Second)); len(got) != 1 {
		t.Fatalf("+5s typers %v", got)
	}
	if got := c.Typing(fixtures.ChDev, nil, now.Add(6*time.Second)); len(got) != 0 {
		t.Fatalf("+6s typers %v", got)
	}
}

func TestChannelHelpers(t *testing.T) {
	s := seeded()
	ctx := &ui.Context{Store: s, Now: func() time.Time { return uitest.Start }}
	dev := s.Channels[chanID(t, s, "개발")]
	dev.Topic = "백엔드 얘기하는 곳"
	if got := channelTitle(ctx, dev, false); got != "# 개발" {
		t.Fatalf("title %q", got)
	}
	if got := channelTitle(ctx, dev, true); got != "# 개발 · 백엔드 얘기하는 곳" {
		t.Fatalf("title+topic %q", got)
	}
	dev.IsPrivate = true
	if got := channelTitle(ctx, dev, false); got != "🔒개발" {
		t.Fatalf("private %q", got)
	}
	if got := channelTitle(ctx, s.Channels[dmWith(t, s, "minji")], true); got != "민지 ●" {
		t.Fatalf("dm %q", got)
	}
	if got := channelTitle(ctx, s.Channels[dmWith(t, s, "junho")], true); got != "준호 ○" {
		t.Fatalf("dm offline %q", got)
	}
	// G2 gate: before any presence frame names the peer, the header falls back to
	// the friend entry exactly like the sidebar dot (it showed "○" beside "●").
	minjiDM := s.Channels[dmWith(t, s, "minji")]
	minji := dmPeer(s, minjiDM)
	delete(s.Presence, minji.ID)
	f, ok := s.Friends[minji.ID]
	if !ok {
		t.Fatal("fixture: minji is a friend")
	}
	f.Presence = api.PresenceOnline
	s.Friends[minji.ID] = f
	if got := channelTitle(ctx, minjiDM, true); got != "민지 ●" || dmPresence(ctx, minjiDM) != api.PresenceOnline {
		t.Fatalf("dm header from friend presence %q, sidebar %q", got, dmPresence(ctx, minjiDM))
	}

	nova := spaceByName(t, s, "노바 개발방")
	if spaceOf(ctx, dev) != s.Spaces[nova] || spaceOf(ctx, s.Channels[dmWith(t, s, "minji")]) != nil {
		t.Fatal("spaceOf")
	}
	if myRole(ctx, nova) != api.RoleOwner || !canManageSpace(ctx, nova) || canManageSpace(ctx, "01JBNOPE") {
		t.Fatal("role")
	}
	if !canPin(ctx, dev) || !canPin(ctx, s.Channels[dmWith(t, s, "minji")]) {
		t.Fatal("canPin owner / dm")
	}
	minjiMsg := mkMsg(dev.ID, 1, userBy(t, s, "minji"), "x", uitest.Start)
	if !canDelete(ctx, dev, minjiMsg) || canDelete(ctx, s.Channels[dmWith(t, s, "minji")], minjiMsg) {
		t.Fatal("canDelete owner in text, not others' DM messages")
	}
	s.Spaces[nova].MyRole = api.RoleMember
	if canPin(ctx, dev) || canDelete(ctx, dev, minjiMsg) || !canDelete(ctx, dev, mkMsg(dev.ID, 2, userBy(t, s, "sam"), "y", uitest.Start)) {
		t.Fatal("member: no pin, delete own only")
	}

	// viewedChannel / currentSpaceID
	if viewedChannel(ctx) != nil || currentSpaceID(ctx) != nova {
		t.Fatal("nothing viewed: nil channel, first space")
	}
	s.SetViewing(fixtures.DMMinji, fixtures.ChChat)
	if got := viewedChannel(ctx); got == nil || got.ID != fixtures.ChChat {
		t.Fatalf("smallest viewed id, got %v", got)
	}
	chatOf(ctx).SetActiveChannel(fixtures.DMMinji)
	if got := viewedChannel(ctx); got == nil || got.ID != fixtures.DMMinji {
		t.Fatalf("active+viewed wins, got %v", got)
	}
	s.SetViewing()
	chatOf(ctx).SetSelectedSpace(nova)
	if currentSpaceID(ctx) != nova {
		t.Fatal("selected space")
	}

	// markReadLocal
	gen := chanID(t, s, "일반")
	last := "01JDGEN0000000000000000004"
	markReadLocal(s, api.UnreadState{ChannelID: gen, LastReadMessageID: &last})
	if my := s.Channels[gen].My; my.UnreadCount != 0 || my.LastReadMessageID == nil || *my.LastReadMessageID != last {
		t.Fatalf("markReadLocal %+v", my)
	}
	markReadLocal(s, api.UnreadState{ChannelID: "01JBNOPE"}) // unknown: no-op, no panic
}
