package chat

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- DM / channel settings test data (this file only) ----

const (
	gdmID  = "01JB000000000000000000GDM1" // group DM 점심팟 (me, 민지, 준호)
	privID = "01JB00000000000000000PRV01" // private text channel 비밀 in 노바 개발방
)

// groupDM is the group DM 점심팟 with me, 민지 and 준호, created by owner.
func groupDM(owner string) api.Channel {
	s := seeded()
	name := "점심팟"
	return api.Channel{
		ID: gdmID, Kind: api.ChannelGroupDM, Name: &name, OwnerID: &owner, MemberCount: 3,
		Members:   []api.UserSummary{s.Me.UserSummary, *s.User(fixtures.UserMinji), *s.User(fixtures.UserJunho)},
		CreatedAt: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC),
		My:        api.ChannelMyState{Notify: "all"},
	}
}

// privateChannel is the private text channel 비밀 of 노바 개발방 (I am a member).
func privateChannel() api.Channel {
	space, name := fixtures.SpaceNova, "비밀"
	return api.Channel{
		ID: privID, Kind: api.ChannelText, SpaceID: &space, Name: &name, IsPrivate: true, Position: 8, MemberCount: 2,
		CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		My:        api.ChannelMyState{Notify: "all"},
	}
}

// knownChannel is a channel the test API echoes: a seeded one or one of this file's.
func knownChannel(id string) (api.Channel, bool) {
	if ch := seeded().Channels[id]; ch != nil {
		return *ch, true
	}
	for _, ch := range []api.Channel{groupDM(fixtures.UserSam), privateChannel()} {
		if ch.ID == id {
			return ch, true
		}
	}
	return api.Channel{}, false
}

// withChannels is the bootstrapped fixture seed plus chs.
func withChannels(chs ...api.Channel) func(*store.Store) {
	return func(s *store.Store) {
		bootstrappedSeed(s)
		for _, ch := range chs {
			s.ObserveChannel(ch)
		}
	}
}

// openChannelWith opens ch (added to the seed) on srv.
func openChannelWith(t *testing.T, sz uitest.Size, ch api.Channel, srv *apitest.Server) *uitest.Harness {
	t.Helper()
	return openWith(t, sz, ui.Route{Kind: ui.RouteChannel, ID: ch.ID}, srv, uitest.WithSeed(withChannels(ch)))
}

// prefsMenu opens channel.prefs and returns the item labels (hints dropped).
func prefsMenu(t testing.TB, h *uitest.Harness) []string {
	t.Helper()
	h.Send(ui.RunCommandMsg{ID: "channel.prefs"})
	if overlays(h) != 1 {
		t.Fatalf("channel.prefs did not open:\n%s", h.Screen())
	}
	var out []string
	for _, l := range menuLabels(t, h) {
		out = append(out, strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(l), "✓")))
	}
	return out
}

// overlayRow is the overlay row holding s (trimmed).
func overlayRow(t testing.TB, h *uitest.Harness, s string) string {
	t.Helper()
	for _, l := range strings.Split(overlayText(t, h), "\n") {
		if strings.Contains(l, s) {
			return strings.Trim(l, "║│ ") // the border, if any
		}
	}
	t.Fatalf("%q not in the overlay:\n%s", s, overlayText(t, h))
	return ""
}

// itemNamed is the item labeled label in items.
func itemNamed(t testing.TB, items []menuItem, label string) menuItem {
	t.Helper()
	for _, it := range items {
		if it.Label == label {
			return it
		}
	}
	t.Fatalf("no item %q in %q", label, labelsOf(items))
	return menuItem{}
}

func labelsOf(items []menuItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

// ---- dm.new ----

func TestDMNewOneToOne(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "dm.new"})
	if title := pickerTitle(t, h); !strings.Contains(title, "새 대화") {
		t.Fatalf("dm.new title row %q", title)
	}
	h.Type("minji")
	h.Press("enter") // pick 민지
	if box := overlayText(t, h); strings.Contains(box, "대화 이름(선택)") {
		t.Fatalf("one person: no name field:\n%s", box)
	}
	h.Press("enter") // submit
	r := reqs(h.API, "POST", "/v1/dms")
	if len(r) != 1 {
		t.Fatalf("one POST /v1/dms, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"users":["@minji"]}`)
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != fixtures.DMMinji {
		t.Fatalf("opens the existing DM: %+v", tr)
	}
	if overlays(h) != 0 {
		t.Fatal("the dialog closes on success")
	}
}

func TestDMNewGroup(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "dm.new"})
	h.Type("minji")
	h.Press("enter")
	h.Type("junho")
	h.Press("enter")
	mustContain(t, overlayText(t, h), "대화 이름(선택)")
	h.Press("tab")
	h.Type("점심팟")
	h.Press("enter")
	r := reqs(h.API, "POST", "/v1/dms")
	if len(r) != 1 {
		t.Fatalf("one POST /v1/dms, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"users":["@minji","@junho"],"name":"점심팟"}`)
	tr := top(h)
	ch := h.Context().Store.Channels[tr.ID]
	if tr.Kind != ui.RouteChannel || ch == nil || ch.Kind != api.ChannelGroupDM {
		t.Fatalf("opens the new group DM (observed): %+v %+v", tr, ch)
	}
	mustContain(t, paneText(t, h, layout.SlotSidebar), "◇ 점심팟")
}

func TestDMNewGroupWithoutName(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "dm.new"})
	h.Type("minji")
	h.Press("enter")
	h.Type("hana")
	h.Press("enter", "enter") // pick, submit with an empty name
	r := reqs(h.API, "POST", "/v1/dms")
	if len(r) != 1 {
		t.Fatalf("one POST /v1/dms, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"users":["@minji","@hana"]}`)
}

func TestDMNewCandidates(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	p := openPicker(t, h, newDMDialog(h.Context(), ""))
	var got []string
	for _, u := range p.cands {
		got = append(got, u.Handle)
	}
	if want := []string{"minji", "hana", "junho"}; !slices.Equal(got, want) {
		t.Fatalf("friends, online first: %q, want %q", got, want)
	}
	if p.spec.Max != 9 || !p.spec.Search {
		t.Fatalf("max 9 with search: %+v", p.spec)
	}

	// blocked users are never offered
	h = openHome(t, uitest.Size{W: 120, H: 40})
	h.Context().Store.Blocks[fixtures.UserHana] = h.Context().Now()
	p = openPicker(t, h, newDMDialog(h.Context(), ""))
	if slices.ContainsFunc(p.cands, func(u api.UserSummary) bool { return u.ID == fixtures.UserHana }) {
		t.Fatalf("blocked 하나 offered: %+v", p.cands)
	}
}

func TestDMNewLimit(t *testing.T) {
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		for i := 1; i <= 10; i++ {
			u := api.UserSummary{ID: fmt.Sprintf("01JA0000000000000000PAL%03d", i), Handle: fmt.Sprintf("pal%02d", i), DisplayName: fmt.Sprintf("친구%02d", i)}
			s.ObserveUser(u)
			s.Friends[u.ID] = api.Friend{User: u, Since: uitest.Start, Presence: api.PresenceOffline}
		}
	}
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(seed))
	p := openPicker(t, h, newDMDialog(h.Context(), ""))
	for range 9 {
		h.Press("down", "enter")
	}
	if len(p.chips) != 9 {
		t.Fatalf("9 picks, got %d", len(p.chips))
	}
	h.Press("down", "enter") // the 10th
	if len(p.chips) != 9 {
		t.Fatalf("a 10th pick is refused: %d", len(p.chips))
	}
	mustContain(t, h.Screen(), "최대 9명까지 고를 수 있어요")
}

func TestDMNewNotAllowed(t *testing.T) {
	srv := newAPI(t)
	srv.Error("POST /v1/dms", http.StatusForbidden, "dm_not_allowed") // same shape: replaces the echo for this test
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, srv)
	h.Send(ui.RunCommandMsg{ID: "dm.new"})
	h.Type("minji")
	h.Press("enter", "enter")
	if overlays(h) != 1 {
		t.Fatal("the dialog stays open on an error")
	}
	mustContain(t, overlayText(t, h), api.Describe(&api.Error{Code: "dm_not_allowed"}))
	if h.Model.Frame().Toast != nil {
		t.Fatalf("the error shows in the dialog only, not as a toast too:\n%s", h.Screen())
	}
	if top(h).Kind != ui.RouteHome {
		t.Fatalf("no navigation on an error: %+v", top(h))
	}
}

// D2 (owner decision 2026-11-06): a group DM refused over a block between two
// participants comes back as 403 dm_not_allowed with details {} (nobody named);
// the dialog says so in its own words. A can_dm failure (details.user_ids) and a
// 1:1 refusal keep the usual dm_not_allowed line.
func dmNotAllowedWith(details string) []byte {
	return []byte(`{"error":{"code":"dm_not_allowed","message":"x","details":` + details + `}}`)
}

func TestDMNewGroupBlockedPair(t *testing.T) {
	for _, tc := range []struct {
		name, details, want string
	}{
		{"block pair", `{}`, groupDMRefusedText},
		{"can_dm failure", `{"user_ids":["` + fixtures.UserJunho + `"]}`, api.Describe(&api.Error{Code: "dm_not_allowed"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newAPI(t)
			srv.JSON("POST /v1/dms", http.StatusForbidden, dmNotAllowedWith(tc.details))
			h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, srv)
			h.Send(ui.RunCommandMsg{ID: "dm.new"})
			h.Type("minji")
			h.Press("enter")
			h.Type("junho")
			h.Press("enter", "enter")
			if r := reqs(h.API, "POST", "/v1/dms"); len(r) != 1 {
				t.Fatalf("one POST /v1/dms, got %d", len(r))
			}
			if overlays(h) != 1 {
				t.Fatal("the dialog stays open on an error")
			}
			box := overlayText(t, h)
			mustContain(t, box, tc.want)
			if tc.want != groupDMRefusedText && strings.Contains(box, groupDMRefusedText) {
				t.Fatalf("a can_dm failure is not a block pair:\n%s", box)
			}
		})
	}
}

func TestDMNewOneToOneNotAllowedKeepsText(t *testing.T) {
	srv := newAPI(t)
	srv.JSON("POST /v1/dms", http.StatusForbidden, dmNotAllowedWith(`{}`)) // 1:1 refusals carry no details either
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, srv)
	h.Send(ui.RunCommandMsg{ID: "dm.new"})
	h.Type("minji")
	h.Press("enter", "enter")
	box := overlayText(t, h)
	mustContain(t, box, api.Describe(&api.Error{Code: "dm_not_allowed"}))
	if strings.Contains(box, groupDMRefusedText) {
		t.Fatalf("a 1:1 refusal is not about participants:\n%s", box)
	}
}

func TestGroupAddBlockedPair(t *testing.T) {
	srv := newAPI(t)
	srv.JSON("POST /v1/channels/"+gdmID+"/members", http.StatusForbidden, dmNotAllowedWith(`{}`))
	h := openChannelWith(t, uitest.Size{W: 120, H: 40}, groupDM(fixtures.UserSam), srv)
	prefsMenu(t, h)
	clickOn(t, h, "멤버 추가", tea.MouseLeft)
	h.Type("hana")
	h.Press("enter", "enter")
	if r := reqs(h.API, "POST", "/v1/channels/"+gdmID+"/members"); len(r) != 1 {
		t.Fatalf("one POST members, got %d", len(r))
	}
	if overlays(h) != 1 {
		t.Fatal("the picker stays open on an error")
	}
	mustContain(t, overlayText(t, h), groupDMRefusedText)
}

func TestDMNewGolden(t *testing.T) {
	forEachSize(t, overlaySizes, openHome, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.RunCommandMsg{ID: "dm.new"})
		h.Golden("dm_new")
	})
}

// ---- channel.prefs ----

func TestChannelPrefsNotifyMute(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	dev := devID(t, h)
	got := prefsMenu(t, h)
	if want := []string{"알림: 모두", "알림: 멘션만", "알림: 끄기", "음소거", "고정 메시지 보기"}; !slices.Equal(got[:len(want)], want) {
		t.Fatalf("common items %q, want %q first", got, want)
	}
	if row := overlayRow(t, h, "알림: 모두"); !strings.HasSuffix(row, "✓") {
		t.Fatalf("the current notify level is checked: %q", row)
	}
	if row := overlayRow(t, h, "알림: 멘션만"); strings.HasSuffix(row, "✓") {
		t.Fatalf("only the current level is checked: %q", row)
	}

	clickOn(t, h, "알림: 멘션만", tea.MouseLeft)
	r := reqs(h.API, "PUT", "/v1/channels/"+dev+"/prefs")
	if len(r) != 1 {
		t.Fatalf("one PUT prefs, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"notify":"mentions"}`)
	if n := h.Context().Store.Channels[dev].My.Notify; n != "mentions" {
		t.Fatalf("notify = %q", n)
	}
	if overlays(h) != 0 {
		t.Fatal("the menu closes")
	}
	prefsMenu(t, h)
	if row := overlayRow(t, h, "알림: 멘션만"); !strings.HasSuffix(row, "✓") {
		t.Fatalf("멘션만 checked now: %q", row)
	}

	clickOn(t, h, "음소거", tea.MouseLeft)
	r = reqs(h.API, "PUT", "/v1/channels/"+dev+"/prefs")
	if len(r) != 2 {
		t.Fatalf("second PUT prefs, got %d", len(r))
	}
	mustJSONObject(t, r[1].Body, `{"muted":true}`)
	if !h.Context().Store.Channels[dev].My.Muted {
		t.Fatal("muted at once")
	}
	if got := prefsMenu(t, h); !slices.Contains(got, "음소거 해제") || slices.Contains(got, "음소거") {
		t.Fatalf("muted: 음소거 해제 offered: %q", got)
	}
	clickOn(t, h, "음소거 해제", tea.MouseLeft)
	mustJSONObject(t, reqs(h.API, "PUT", "/v1/channels/"+dev+"/prefs")[2].Body, `{"muted":false}`)
	if h.Context().Store.Channels[dev].My.Muted {
		t.Fatal("unmuted")
	}
}

func TestChannelPrefsFailureReverts(t *testing.T) {
	srv := newAPI(t)
	srv.Error("PUT /v1/channels/"+fixtures.ChDev+"/prefs", http.StatusInternalServerError, "internal")
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}, srv, uitest.WithSeed(bootstrappedSeed))
	prefsMenu(t, h)
	clickOn(t, h, "알림: 끄기", tea.MouseLeft)
	if n := h.Context().Store.Channels[fixtures.ChDev].My.Notify; n != "all" {
		t.Fatalf("reverted on failure: %q", n)
	}
	mustContain(t, toastText(t, h), "알림 설정을 바꾸지 못했어요")
}

func TestChannelPrefsDM(t *testing.T) {
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}, uitest.WithSeed(bootstrappedSeed))
	got := prefsMenu(t, h)
	for _, no := range []string{"이름 바꾸기", "멤버 추가", "나가기", "채널 삭제", "이름·주제 바꾸기"} {
		if slices.Contains(got, no) {
			t.Fatalf("a 1:1 DM has no %q: %q", no, got)
		}
	}
}

func TestChannelPrefsGroupDM(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}
	mine := groupDM(fixtures.UserSam)

	h := openChannelWith(t, sz, mine, newAPI(t))
	got := prefsMenu(t, h)
	for _, want := range []string{"이름 바꾸기", "멤버 추가", "멤버 내보내기", "나가기"} {
		if !slices.Contains(got, want) {
			t.Fatalf("owner's group DM menu %q lacks %q", got, want)
		}
	}

	// 이름 바꾸기: a form prefilled with the name -> PATCH {"name"}
	clickOn(t, h, "이름 바꾸기", tea.MouseLeft)
	mustContain(t, overlayText(t, h), "점심팟")
	h.Press("ctrl+u") // clears before the cursor (ctrl+c skips overlays)
	h.Type("새이름")
	h.Press("enter")
	r := reqs(h.API, "PATCH", "/v1/channels/"+gdmID)
	if len(r) != 1 {
		t.Fatalf("one PATCH, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"name":"새이름"}`)
	if ch := h.Context().Store.Channels[gdmID]; ch == nil || ch.Name == nil || *ch.Name != "새이름" {
		t.Fatalf("renamed in the Store: %+v", ch)
	}
	if overlays(h) != 0 {
		t.Fatal("the form closes on success")
	}

	// 멤버 추가: existing members are excluded; POST members {"users":["@hana"]}
	h = openChannelWith(t, sz, mine, newAPI(t))
	prefsMenu(t, h)
	if p, ok := groupAddPicker(h.Context(), h.Context().Store.Channels[gdmID]).(*userPicker); !ok || p.spec.Max != 7 || !p.spec.Search {
		t.Fatalf("max = 10 - 3 members, with search: %+v", p)
	}
	clickOn(t, h, "멤버 추가", tea.MouseLeft)
	if title := pickerTitle(t, h); !strings.Contains(title, "멤버 추가") {
		t.Fatalf("멤버 추가 opens a picker: %q", title)
	}
	box := overlayText(t, h)
	mustContain(t, box, "하나 @hana")
	if strings.Contains(box, "@minji") || strings.Contains(box, "@junho") {
		t.Fatalf("existing members are excluded:\n%s", box)
	}
	h.Type("hana")
	h.Press("enter", "enter")
	r = reqs(h.API, "POST", "/v1/channels/"+gdmID+"/members")
	if len(r) != 1 {
		t.Fatalf("one POST members, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"users":["@hana"]}`)
	ch := h.Context().Store.Channels[gdmID]
	if ch == nil || !slices.ContainsFunc(ch.Members, func(u api.UserSummary) bool { return u.ID == fixtures.UserHana }) {
		t.Fatalf("하나 added in the Store: %+v", ch)
	}

	// 멤버 내보내기: a member submenu (not me) -> Confirm -> DELETE members/@minji
	h = openChannelWith(t, sz, mine, newAPI(t))
	prefsMenu(t, h)
	clickOn(t, h, "멤버 내보내기", tea.MouseLeft)
	if got := menuLabels(t, h); !slices.Contains(got, "민지") || !slices.Contains(got, "준호") || slices.Contains(got, "sam") {
		t.Fatalf("member submenu %q", got)
	}
	clickOn(t, h, "민지", tea.MouseLeft)
	if !strings.Contains(h.Screen(), "민지님을 내보낼까요?") {
		t.Fatalf("asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", "/v1/channels/"+gdmID+"/members/@minji")); n != 1 {
		t.Fatalf("kick: %d", n)
	}
	if ch := h.Context().Store.Channels[gdmID]; slices.ContainsFunc(ch.Members, func(u api.UserSummary) bool { return u.ID == fixtures.UserMinji }) {
		t.Fatalf("민지 left the Store copy: %+v", ch.Members)
	}

	// 나가기: Confirm -> DELETE members/@me -> home, the channel is gone
	h = openChannelWith(t, sz, mine, newAPI(t))
	prefsMenu(t, h)
	clickOn(t, h, "나가기", tea.MouseLeft)
	if !strings.Contains(h.Screen(), "나갈까요?") {
		t.Fatalf("asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", "/v1/channels/"+gdmID+"/members/@me")); n != 1 {
		t.Fatalf("leave: %d", n)
	}
	if tr := top(h); tr.Kind != ui.RouteHome {
		t.Fatalf("leaving goes home: %+v", tr)
	}
	if h.Context().Store.Channels[gdmID] != nil {
		t.Fatal("the group DM is gone from the Store")
	}

	// not the owner: no 멤버 내보내기
	h = openChannelWith(t, sz, groupDM(fixtures.UserMinji), newAPI(t))
	if got := prefsMenu(t, h); slices.Contains(got, "멤버 내보내기") || !slices.Contains(got, "이름 바꾸기") || !slices.Contains(got, "나가기") {
		t.Fatalf("member's group DM menu %q", got)
	}
}

func TestChannelPrefsGroupDMFull(t *testing.T) {
	full := groupDM(fixtures.UserSam)
	for i := len(full.Members); i < 10; i++ {
		full.Members = append(full.Members, api.UserSummary{ID: fmt.Sprintf("01JA0000000000000000GM%04d", i), Handle: fmt.Sprintf("gm%d", i), DisplayName: fmt.Sprintf("멤버%d", i)})
	}
	full.MemberCount = 10
	h := openChannelWith(t, uitest.Size{W: 120, H: 40}, full, newAPI(t))
	ch := h.Context().Store.Channels[gdmID]
	if it := itemNamed(t, channelPrefItems(h.Context(), ch), "멤버 추가"); !it.Disabled {
		t.Fatalf("a full group DM cannot add: %+v", it)
	}
}

func TestChannelPrefsTextAdmin(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}

	// owner on #개발: 이름·주제 바꾸기 validates the name, then PATCH {"name","topic"}
	h := devChannel(t, sz)
	dev := devID(t, h)
	got := prefsMenu(t, h)
	for _, want := range []string{"이름·주제 바꾸기", "채널 삭제"} {
		if !slices.Contains(got, want) {
			t.Fatalf("owner's text menu %q lacks %q", got, want)
		}
	}
	if slices.Contains(got, "멤버 추가") {
		t.Fatalf("a public channel has no member list to add to: %q", got)
	}
	clickOn(t, h, "이름·주제 바꾸기", tea.MouseLeft)
	h.Press("ctrl+u") // clears before the cursor (ctrl+c skips overlays)
	h.Type("Bad Name")
	h.Press("enter")
	mustContain(t, overlayText(t, h), "채널 이름은")
	if n := len(reqs(h.API, "PATCH", "/v1/channels/"+dev)); n != 0 {
		t.Fatalf("an invalid name is not sent (%d)", n)
	}
	h.Press("ctrl+u") // clears before the cursor (ctrl+c skips overlays)
	h.Type("백엔드")
	h.Press("tab")
	h.Type("서버 이야기")
	h.Press("enter")
	r := reqs(h.API, "PATCH", "/v1/channels/"+dev)
	if len(r) != 1 {
		t.Fatalf("one PATCH, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"name":"백엔드","topic":"서버 이야기"}`)
	if ch := h.Context().Store.Channels[dev]; ch.Name == nil || *ch.Name != "백엔드" || ch.Topic != "서버 이야기" {
		t.Fatalf("updated in the Store: %+v", ch)
	}

	// 채널 삭제: Confirm -> DELETE, the channel is gone, the default channel opens
	h = devChannel(t, sz)
	prefsMenu(t, h)
	clickOn(t, h, "채널 삭제", tea.MouseLeft)
	if !strings.Contains(h.Screen(), "삭제할까요?") {
		t.Fatalf("asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", "/v1/channels/"+dev)); n != 1 {
		t.Fatalf("delete: %d", n)
	}
	if h.Context().Store.Channels[dev] != nil {
		t.Fatal("deleted at once")
	}
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != fixtures.ChGeneral {
		t.Fatalf("the default channel opens: %+v", tr)
	}

	// the default channel cannot be deleted
	h = open(t, sz, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}, uitest.WithSeed(bootstrappedSeed))
	del := itemNamed(t, channelPrefItems(h.Context(), h.Context().Store.Channels[fixtures.ChGeneral]), "채널 삭제")
	if !del.Disabled || del.Hint != "기본 채널" {
		t.Fatalf("default channel delete: %+v", del)
	}

	// a plain member manages nothing on a public channel
	h = devChannel(t, sz)
	setRole(h, api.RoleMember)
	items := labelsOf(channelPrefItems(h.Context(), h.Context().Store.Channels[dev]))
	for _, no := range []string{"이름·주제 바꾸기", "채널 삭제", "멤버 추가", "멤버 내보내기"} {
		if slices.Contains(items, no) {
			t.Fatalf("member on #개발 has %q: %q", no, items)
		}
	}

	// private text, role member & channel member: 멤버 추가 only
	h = openChannelWith(t, sz, privateChannel(), newAPI(t))
	setRole(h, api.RoleMember)
	items = labelsOf(channelPrefItems(h.Context(), h.Context().Store.Channels[privID]))
	if !slices.Contains(items, "멤버 추가") || slices.Contains(items, "멤버 내보내기") || slices.Contains(items, "채널 삭제") {
		t.Fatalf("member on a private channel: %q", items)
	}
	setRole(h, api.RoleAdmin)
	items = labelsOf(channelPrefItems(h.Context(), h.Context().Store.Channels[privID]))
	if !slices.Contains(items, "멤버 추가") || !slices.Contains(items, "멤버 내보내기") {
		t.Fatalf("admin on a private channel: %q", items)
	}
}

// privServer is newAPI with the private channel's members = me and 민지.
func privServer(t testing.TB) *apitest.Server {
	t.Helper()
	srv := newAPI(t)
	s := seeded()
	srv.JSON("GET /v1/channels/"+privID+"/members", http.StatusOK, map[string]any{"items": []api.ChannelMember{
		{User: s.Me.UserSummary, JoinedAt: uitest.Start},
		{User: *s.User(fixtures.UserMinji), JoinedAt: uitest.Start},
	}, "next_cursor": nil})
	return srv
}

func TestChannelPrefsPrivateMembers(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}

	// 멤버 추가: candidates = space members who are not channel members
	srv := privServer(t)
	h := openChannelWith(t, sz, privateChannel(), srv)
	setRole(h, api.RoleMember)
	prefsMenu(t, h)
	clickOn(t, h, "멤버 추가", tea.MouseLeft)
	if title := pickerTitle(t, h); !strings.Contains(title, "멤버 추가") {
		t.Fatalf("멤버 추가 opens a picker after loading: %q\n%s", title, h.Screen())
	}
	if n := len(reqs(srv, "GET", "/v1/spaces/"+fixtures.SpaceNova+"/members")); n != 1 {
		t.Fatalf("space members fetched once (%d)", n)
	}
	box := overlayText(t, h)
	mustContain(t, box, "@dev_kim", "@hana", "@junho")
	if strings.Contains(box, "@minji") || strings.Contains(box, "@sam") {
		t.Fatalf("channel members (and I) are excluded:\n%s", box)
	}
	h.Type("hana")
	h.Press("enter", "enter")
	r := reqs(srv, "POST", "/v1/channels/"+privID+"/members")
	if len(r) != 1 {
		t.Fatalf("one POST members, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"users":["@hana"]}`)
	if n := h.Context().Store.Channels[privID].MemberCount; n != 3 {
		t.Fatalf("member count %d", n)
	}

	// 멤버 내보내기 (admin): the channel members but me -> Confirm -> DELETE
	srv = privServer(t)
	h = openChannelWith(t, sz, privateChannel(), srv)
	setRole(h, api.RoleAdmin)
	prefsMenu(t, h)
	clickOn(t, h, "멤버 내보내기", tea.MouseLeft)
	if got := menuLabels(t, h); !slices.Contains(got, "민지") || slices.Contains(got, "sam") {
		t.Fatalf("member submenu %q", got)
	}
	clickOn(t, h, "민지", tea.MouseLeft)
	h.Press("y")
	if n := len(reqs(srv, "DELETE", "/v1/channels/"+privID+"/members/@minji")); n != 1 {
		t.Fatalf("kick: %d", n)
	}
}

func TestChannelPrefsLoadFailure(t *testing.T) {
	srv := newAPI(t)
	srv.Error("GET /v1/spaces/"+fixtures.SpaceNova+"/members", http.StatusInternalServerError, "internal")
	h := openChannelWith(t, uitest.Size{W: 120, H: 40}, privateChannel(), srv)
	prefsMenu(t, h)
	clickOn(t, h, "멤버 추가", tea.MouseLeft)
	if overlays(h) != 1 {
		t.Fatal("the loader stays open on an error")
	}
	mustContain(t, overlayText(t, h), "✕")
	h.Press("esc")
	if overlays(h) != 0 {
		t.Fatal("esc closes the loader")
	}
}

func TestChannelPrefsAvailability(t *testing.T) {
	h := openHome(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "channel.prefs"})
	if overlays(h) != 0 {
		t.Fatalf("no viewed channel: nothing to set:\n%s", h.Screen())
	}
	// an explicit channel id opens its settings from anywhere
	h.Send(ui.OpenOverlayMsg{ID: "chat.channel_prefs", Args: fixtures.ChDeploy})
	if overlays(h) != 1 {
		t.Fatalf("chat.channel_prefs with an id opens:\n%s", h.Screen())
	}
	if row := overlayRow(t, h, "알림: 멘션만"); !strings.HasSuffix(row, "✓") {
		t.Fatalf("#배포알림 is mentions-only: %q", row)
	}
	h.Press("esc")
	h.Send(ui.OpenOverlayMsg{ID: "chat.channel_prefs", Args: "01JB0000000000000000NOSUCH"})
	if overlays(h) != 0 {
		t.Fatal("an unknown channel opens nothing")
	}
}

func TestChannelPrefsGolden(t *testing.T) {
	forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.RunCommandMsg{ID: "channel.prefs"})
		h.Golden("channel_prefs")
	})
}

func TestChannelPrefsHostileIntegrity(t *testing.T) {
	forEachSize(t, overlaySizes, hostileOpen, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.RunCommandMsg{ID: "channel.prefs"})
		h.Check()
	})
}
