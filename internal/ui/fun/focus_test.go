package fun

// Focus mode commands and the missed-items summary overlay (plan 05 Task 17).
// The overlay is opened only by the app for alert{kind:"focus_summary"} (CA 9.1);
// focus.stop and focus.ended only change the Store.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// summary is a 25-minute focus session (02:05Z–02:30Z = 11:05–11:30 Seoul) with
// three missed alerts, newest first: a DM, a thread reply and a #개발 mention.
func summary() api.FocusSummary {
	ch := fixtures.ChDev
	root, reply, mention := sid(70), sid(71), sid(72)
	dm := fixtures.DMJunho
	dmMsg := sid(73)
	return api.FocusSummary{
		StartedAt: fixtureNow,
		EndedAt:   fixtureNow.Add(25 * time.Minute),
		Counts:    api.FocusCounts{Mention: 3, DM: 2, ThreadReply: 1, Other: 1},
		Items: []api.Alert{
			{ID: sid(80), Kind: api.AlertDM, Title: "준호", Body: "점심 국밥 어때요?", Actor: ptr(userOf("junho")),
				Target:    api.AlertTarget{Type: "message", ID: dmMsg, ChannelID: &dm, MessageID: &dmMsg},
				CreatedAt: fixtureNow.Add(20 * time.Minute), Suppressed: true, SuppressedReason: ptr("focus")},
			{ID: sid(81), Kind: api.AlertThreadReply, Title: "민지 · #개발 스레드", Body: "넵 2시에 할게요", Actor: ptr(userOf("minji")),
				Target:    api.AlertTarget{Type: "message", ID: reply, ChannelID: &ch, MessageID: &reply, ThreadRootID: &root},
				CreatedAt: fixtureNow.Add(13 * time.Minute), Suppressed: true, SuppressedReason: ptr("focus")},
			{ID: sid(82), Kind: api.AlertMention, Title: "민지 · #개발", Body: "@sam 배포 언제?", Actor: ptr(userOf("minji")),
				Target:    api.AlertTarget{Type: "message", ID: mention, ChannelID: &ch, MessageID: &mention},
				CreatedAt: fixtureNow.Add(7 * time.Minute), Suppressed: true, SuppressedReason: ptr("focus")},
		},
	}
}

// focusAlert is the alert{kind:focus_summary} the server sends when focus ends.
func focusAlert(id string) api.Alert {
	return api.Alert{
		ID: id, Kind: api.AlertFocusSummary, Title: "집중 모드 끝 — 멘션 3, DM 2", Body: "놓친 알림 7개",
		Target: api.AlertTarget{Type: "notification", ID: id}, CreatedAt: fixtureNow,
	}
}

// focusAPI is funAPI plus POST /v1/me/focus (echoes duration_sec, default 25
// minutes) and DELETE /v1/me/focus (summary()).
func focusAPI(t *testing.T) *apitest.Server {
	t.Helper()
	srv := funAPI(t)
	srv.Handle("POST /v1/me/focus", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DurationSec *int `json:"duration_sec"`
		}
		if err := decodeBody(r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sec := 25 * 60
		if body.DurationSec != nil {
			sec = *body.DurationSec
		}
		writeJSON(w, map[string]any{"focus": api.Focus{StartedAt: fixtureNow, EndsAt: fixtureNow.Add(time.Duration(sec) * time.Second)}})
	})
	srv.JSON("DELETE /v1/me/focus", http.StatusOK, map[string]any{"summary": summary()})
	return srv
}

// paletteCommand is the registered palette command id (fatal when missing).
func paletteCommand(t *testing.T, id string) palette.Command {
	t.Helper()
	c, ok := palette.Lookup(id)
	if !ok {
		t.Fatalf("palette command %q not registered", id)
	}
	return c
}

func focusHarness(t *testing.T, srv *apitest.Server) *uitest.Harness {
	t.Helper()
	if srv == nil {
		srv = focusAPI(t)
	}
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed))
	h.Resize(120, 40)
	return h
}

// endFocus delivers focus.ended(summary()) and then alert(focusAlert(id)).
func endFocus(h *uitest.Harness, id string) {
	h.Event("focus.ended", map[string]any{"summary": summary()})
	h.Event("alert", map[string]any{"alert": focusAlert(id)})
}

func TestParseFocusArgs(t *testing.T) {
	for _, tc := range []struct {
		in  string
		set bool
		sec int
		err bool
	}{
		{in: ""},
		{in: "   "},
		{in: "25m", set: true, sec: 1500},
		{in: " 1h30m ", set: true, sec: 5400},
		{in: "90", set: true, sec: 5400},
		{in: "1m", set: true, sec: 60},
		{in: "12h", set: true, sec: 43200},
		{in: "720", set: true, sec: 43200},
		{in: "30s", err: true},
		{in: "13h", err: true},
		{in: "0", err: true},
		{in: "721", err: true},
		{in: "-5m", err: true},
		{in: "abc", err: true},
		{in: "25분", err: true},
	} {
		got, err := parseFocusArgs(tc.in)
		if (err != nil) != tc.err {
			t.Fatalf("parseFocusArgs(%q) err = %v, want err %v", tc.in, err, tc.err)
		}
		if tc.err {
			continue
		}
		if got.Set != tc.set || got.Value != tc.sec {
			t.Fatalf("parseFocusArgs(%q) = %+v, want set=%v %d", tc.in, got, tc.set, tc.sec)
		}
	}
}

func TestFocusStartArgs(t *testing.T) {
	for _, tc := range []struct {
		args, body string
		ends       time.Duration
		toast      string
	}{
		{args: "25m", body: `{"duration_sec":1500}`, ends: 25 * time.Minute, toast: "🎯 집중 모드 — 25분 동안 알림을 모아 둘게요"},
		{args: "", body: `{}`, ends: 25 * time.Minute, toast: "🎯 집중 모드 — 25분 동안 알림을 모아 둘게요"},
		{args: "90", body: `{"duration_sec":5400}`, ends: 90 * time.Minute, toast: "🎯 집중 모드 — 1시간 30분 동안 알림을 모아 둘게요"},
	} {
		h := focusHarness(t, nil)
		runCommand(h, "focus.start", tc.args)
		r, ok := lastReq(h, "POST", "/v1/me/focus")
		if !ok {
			t.Fatalf("focus.start %q sent no POST /v1/me/focus", tc.args)
		}
		if got := strings.TrimSpace(string(r.Body)); got != tc.body {
			t.Fatalf("focus.start %q body = %s, want %s", tc.args, got, tc.body)
		}
		f := h.Context().Store.Focus
		if f == nil || !f.EndsAt.Equal(fixtureNow.Add(tc.ends)) {
			t.Fatalf("focus.start %q: Store.Focus = %+v, want ends %v", tc.args, f, fixtureNow.Add(tc.ends))
		}
		if !strings.Contains(toastText(h), squash(tc.toast)) {
			t.Fatalf("focus.start %q: no %q toast:\n%s", tc.args, tc.toast, h.Screen())
		}
		if n := len(h.Model.Frame().Overlays); n != 0 {
			t.Fatalf("focus.start %q opened %d overlays", tc.args, n)
		}
	}
}

func TestFocusStartInvalid(t *testing.T) {
	for _, args := range []string{"30s", "13h"} {
		h := focusHarness(t, nil)
		runCommand(h, "focus.start", args)
		if _, ok := lastReq(h, "POST", "/v1/me/focus"); ok {
			t.Fatalf("focus.start %q sent a request", args)
		}
		if !strings.Contains(toastText(h), squash("집중 시간은 1분~12시간이에요")) {
			t.Fatalf("focus.start %q: no range toast:\n%s", args, h.Screen())
		}
		if h.Context().Store.Focus != nil {
			t.Fatalf("focus.start %q set Store.Focus", args)
		}
	}
}

func TestFocusCommandsAvailable(t *testing.T) {
	h := focusHarness(t, nil)
	ctx := h.Context()
	start, stop := paletteCommand(t, "focus.start"), paletteCommand(t, "focus.stop")
	if start.Title != "집중 모드 시작" || stop.Title != "집중 모드 끝내기" {
		t.Fatalf("titles %q / %q", start.Title, stop.Title)
	}
	if !start.Available(ctx) || stop.Available(ctx) {
		t.Fatal("not focusing: want focus.start available and focus.stop not")
	}
	ctx.Store.Focus = &api.Focus{StartedAt: fixtureNow, EndsAt: fixtureNow.Add(time.Hour)}
	if start.Available(ctx) || !stop.Available(ctx) {
		t.Fatal("focusing: want focus.stop available and focus.start not")
	}
}

func TestFocusStopNoOverlay(t *testing.T) {
	h := focusHarness(t, nil)
	h.Context().Store.Focus = &api.Focus{StartedAt: fixtureNow, EndsAt: fixtureNow.Add(time.Hour)}
	runCommand(h, "focus.stop", "")
	if _, ok := lastReq(h, "DELETE", "/v1/me/focus"); !ok {
		t.Fatal("focus.stop sent no DELETE /v1/me/focus")
	}
	st := h.Context().Store
	if st.LastFocusSummary == nil || st.LastFocusSummary.Counts.Mention != 3 {
		t.Fatalf("Store.LastFocusSummary = %+v, want the DELETE summary", st.LastFocusSummary)
	}
	if st.Focus != nil {
		t.Fatalf("Store.Focus = %+v, want nil", st.Focus)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("focus.stop opened %d overlays (only alert{focus_summary} may)", n)
	}
}

func TestFocusStopNotFocusing(t *testing.T) {
	conflictAPI := func() *apitest.Server {
		srv := funAPI(t)
		srv.Handle("DELETE /v1/me/focus", func(w http.ResponseWriter, r *http.Request) {
			writeStatus(w, http.StatusConflict, map[string]any{"error": map[string]any{
				"code": "conflict", "message": "not focusing", "details": map[string]any{"reason": "not_focusing"}}})
		})
		return srv
	}
	// Store.Focus is stale (the session already ended elsewhere): the app's
	// dispatch runs the command (Available) and the server answers 409.
	h := focusHarness(t, conflictAPI())
	h.Context().Store.Focus = &api.Focus{StartedAt: fixtureNow, EndsAt: fixtureNow.Add(time.Hour)}
	runCommand(h, "focus.stop", "")
	if !strings.Contains(toastText(h), squash("집중 모드가 아니에요")) {
		t.Fatalf("no not-focusing toast:\n%s", h.Screen())
	}
	if st := h.Context().Store; st.LastFocusSummary != nil || st.Focus != nil {
		t.Fatalf("after 409: LastFocusSummary=%+v Focus=%+v, want nil, nil", st.LastFocusSummary, st.Focus)
	}

	// Not focusing locally: the app's dispatch (CA 5.3.4 7 ③) refuses the
	// unavailable command before Run, so nothing is sent.
	h = focusHarness(t, conflictAPI())
	runCommand(h, "focus.stop", "")
	if n := len(reqsTo(h, "DELETE", "/v1/me/focus")); n != 0 {
		t.Fatalf("unavailable focus.stop sent %d DELETEs", n)
	}
	if !strings.Contains(toastText(h), squash("지금은 쓸 수 없는 명령이에요")) {
		t.Fatalf("no unavailable toast:\n%s", h.Screen())
	}
}

func TestFocusSummaryExactlyOnce(t *testing.T) {
	h := focusHarness(t, nil)
	h.Context().Store.Focus = &api.Focus{StartedAt: fixtureNow, EndsAt: fixtureNow.Add(time.Hour)}
	runCommand(h, "focus.stop", "") // early end
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("focus.stop opened %d overlays", n)
	}
	endFocus(h, "A1")
	h.Event("alert", map[string]any{"alert": focusAlert("A1")}) // replay of the same id
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want exactly 1", n)
	}
	if s := overlayText(h); !strings.Contains(s, "멘션 3 · DM 2 · 스레드 1 · 초대 0 · 친구 요청 0 · 기타 1") {
		t.Fatalf("summary counts missing:\n%s", h.Screen())
	}
	h.Press("esc")
	h.Event("alert", map[string]any{"alert": focusAlert("A1")})
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("overlays after esc + replay = %d, want 0", n)
	}
}

func TestFocusSummaryNaturalEnd(t *testing.T) {
	h := focusHarness(t, nil)
	h.Context().Store.Focus = &api.Focus{StartedAt: fixtureNow, EndsAt: fixtureNow.Add(25 * time.Minute)}
	endFocus(h, "A2")
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want exactly 1", n)
	}
	if h.Context().Store.Focus != nil {
		t.Fatal("focus.ended left Store.Focus set")
	}
	s := overlayText(h)
	for _, want := range []string{"집중 모드 끝", "25분 집중했어요 (11:05–11:30)", "@ 민지 · #개발  @sam 배포 언제?", "11:12", "✉ 준호", "↩ 민지 · #개발 스레드"} {
		if !strings.Contains(s, want) {
			t.Fatalf("overlay lacks %q:\n%s", want, s)
		}
	}
	if len(reqsTo(h, "DELETE", "/v1/me/focus")) != 0 {
		t.Fatal("a natural end sent DELETE /v1/me/focus")
	}
}

func TestFocusSummaryBeforeSummary(t *testing.T) {
	h := focusHarness(t, nil)
	h.Event("alert", map[string]any{"alert": focusAlert("A3")})
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want 1", n)
	}
	s := overlayText(h)
	for _, want := range []string{"집중 모드 끝 — 멘션 3, DM 2", "놓친 알림 7개", "요약을 불러오는 중…"} {
		if !strings.Contains(s, want) {
			t.Fatalf("loading overlay lacks %q:\n%s", want, s)
		}
	}
	h.Event("focus.ended", map[string]any{"summary": summary()})
	s = overlayText(h)
	if !strings.Contains(s, "멘션 3 · DM 2") || strings.Contains(s, "요약을 불러오는 중…") {
		t.Fatalf("overlay did not switch to the summary:\n%s", s)
	}
}

// A summary left from an earlier session (ended long before this alert) is not
// shown for the new alert; it waits for this session's focus.ended.
func TestFocusSummaryIgnoresOldSummary(t *testing.T) {
	h := focusHarness(t, nil)
	old := summary()
	old.StartedAt, old.EndedAt = fixtureNow.Add(-3*time.Hour), fixtureNow.Add(-2*time.Hour)
	old.Counts = api.FocusCounts{Other: 9}
	h.Context().Store.FocusEnded(old)
	h.Event("alert", map[string]any{"alert": focusAlert("A4")})
	s := overlayText(h)
	if !strings.Contains(s, "요약을 불러오는 중…") || strings.Contains(s, "기타 9") {
		t.Fatalf("old summary shown for a new alert:\n%s", s)
	}
}

func TestFocusSummaryEnterNavigates(t *testing.T) {
	h := focusHarness(t, nil)
	endFocus(h, "A5")
	h.Press("j", "j", "enter") // newest first: dm, thread reply, mention
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("overlay still open (%d)", n)
	}
	nav := h.Nav()
	stack := nav.Tabs[nav.Active].Stack
	top := stack[len(stack)-1]
	if top.Kind != ui.RouteChannel || top.ID != fixtures.ChDev || top.Anchor != sid(72) {
		t.Fatalf("enter opened %+v, want RouteChannel %s anchor %s", top, fixtures.ChDev, sid(72))
	}
}

func TestAlertRoute(t *testing.T) {
	st := store.New()
	fixtures.Seed(st)
	st.DirectInvites = append(st.DirectInvites, api.DirectInvite{ID: "INV1", Status: "pending",
		Space: api.SpacePreview{ID: fixtures.SpaceNova, Name: "노바 개발방"}})
	ch, msg, root, post, space := fixtures.ChDev, "M1", "R1", "P1", fixtures.SpaceNova
	other := "S9"
	for _, tc := range []struct {
		name string
		a    api.Alert
		want ui.Route
		ok   bool
	}{
		{"message", api.Alert{Kind: api.AlertMention, Target: api.AlertTarget{Type: "message", ID: msg, ChannelID: &ch, MessageID: &msg}},
			ui.Route{Kind: ui.RouteChannel, ID: ch, Anchor: msg}, true},
		{"thread", api.Alert{Kind: api.AlertThreadReply, Target: api.AlertTarget{Type: "message", ID: msg, ChannelID: &ch, MessageID: &msg, ThreadRootID: &root}},
			ui.Route{Kind: ui.RouteThread, ID: root, Arg: ch, Anchor: msg}, true},
		{"channel", api.Alert{Kind: api.AlertChannelMessage, Target: api.AlertTarget{Type: "channel", ID: ch}},
			ui.Route{Kind: ui.RouteChannel, ID: ch}, true},
		{"post", api.Alert{Kind: api.AlertSocial, Target: api.AlertTarget{Type: "post", ID: post}},
			ui.Route{Kind: ui.RoutePost, ID: post}, true},
		{"post id field", api.Alert{Kind: api.AlertMention, Target: api.AlertTarget{Type: "post", ID: "X", PostID: &post}},
			ui.Route{Kind: ui.RoutePost, ID: post}, true},
		{"space pending", api.Alert{Kind: api.AlertInvite, Target: api.AlertTarget{Type: "space", ID: space, SpaceID: &space}},
			ui.Route{Kind: ui.RouteInvite, Arg: "direct", ID: "INV1"}, true},
		{"space unknown", api.Alert{Kind: api.AlertInvite, Target: api.AlertTarget{Type: "space", ID: other, SpaceID: &other}},
			ui.Route{Kind: ui.RouteInvite, Arg: "direct"}, true},
		{"user", api.Alert{Kind: api.AlertSocial, Target: api.AlertTarget{Type: "user", ID: fixtures.UserMinji}},
			ui.Route{Kind: ui.RouteProfile, ID: fixtures.UserMinji}, true},
		{"notification", api.Alert{Kind: api.AlertSocial, Target: api.AlertTarget{Type: "notification", ID: "N1"}},
			ui.Route{Kind: ui.RouteNotifications}, true},
		{"friend request", api.Alert{Kind: api.AlertFriendRequest, Target: api.AlertTarget{Type: "user", ID: fixtures.UserMinji}},
			ui.Route{Kind: ui.RouteFriends}, true},
		{"focus summary", api.Alert{Kind: api.AlertFocusSummary, Target: api.AlertTarget{Type: "notification", ID: "A"}},
			ui.Route{Kind: ui.RouteNotifications}, true},
		{"message without channel", api.Alert{Kind: api.AlertMention, Target: api.AlertTarget{Type: "message", ID: msg}},
			ui.Route{Kind: ui.RouteNotifications}, false},
		{"unknown type", api.Alert{Kind: api.AlertSocial, Target: api.AlertTarget{Type: "weird", ID: "Z"}},
			ui.Route{Kind: ui.RouteNotifications}, false},
	} {
		got, ok := alertRoute(st, tc.a)
		if ok != tc.ok || got.Kind != tc.want.Kind || got.ID != tc.want.ID || got.Arg != tc.want.Arg || got.Anchor != tc.want.Anchor {
			t.Fatalf("%s: alertRoute = %+v, %v; want %+v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func focusSummarySizes() []uitest.Size {
	return append(overlaySizes(), uitest.Size{W: 19, H: 10})
}

func TestFocusSummaryGolden(t *testing.T) {
	// testdata/TestFocusSummaryGolden/{summary,loading}/<WxH>/<name>.golden
	opts := []uitest.Option{uitest.WithAPI(focusAPI(t)), uitest.WithSeed(fixtures.Seed)}
	t.Run("summary", func(t *testing.T) {
		sweep(t, focusSummarySizes(), "summary", opts, func(h *uitest.Harness) {
			endFocus(h, "A1")
			h.Press("j")
		})
	})
	t.Run("loading", func(t *testing.T) {
		sweep(t, focusSummarySizes(), "loading", opts, func(h *uitest.Harness) {
			h.Event("alert", map[string]any{"alert": focusAlert("A1")})
		})
	})
}

func TestFocusSummaryHostile(t *testing.T) {
	seed := func(s *store.Store) {
		fixtures.Seed(s)
		fixtures.Hostile(s)
	}
	evil := "\x1b[31m빨강\x1b]8;;http://x\x07링크\r\t" + strings.Repeat("긴👨‍👩‍👧", 40)
	sum := summary()
	for i := range sum.Items {
		sum.Items[i].Title = evil
		sum.Items[i].Body = "**굵게** `code`\n" + evil
	}
	for i := 0; i < 20; i++ {
		sum.Items = append(sum.Items, api.Alert{ID: sid(90 + i), Kind: api.AlertKind("weird\x1b[2J"), Title: evil, Body: evil, CreatedAt: fixtureNow})
	}
	al := focusAlert("AH")
	al.Title, al.Body = evil, evil
	opts := []uitest.Option{uitest.WithAPI(focusAPI(t)), uitest.WithSeed(seed)}
	sizes := append(focusSummarySizes(), uitest.AllSizes()...)
	t.Run("summary", func(t *testing.T) {
		sweep(t, sizes, "", opts, func(h *uitest.Harness) {
			h.Event("focus.ended", map[string]any{"summary": sum})
			h.Event("alert", map[string]any{"alert": al})
			h.Press("j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j", "j")
		})
	})
	t.Run("loading", func(t *testing.T) {
		sweep(t, sizes, "", opts, func(h *uitest.Harness) {
			h.Event("alert", map[string]any{"alert": al})
		})
	})
}
