package chat

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- invites / channel creation test data (chatRouteTable handlers) ----

// Invite codes: the two listed for every space (spaceInvites) and the one POST
// /v1/spaces/{id}/invites creates (echoCreateInvite); newPrivChannelID /
// newPubChannelID are the channels POST /v1/spaces/{id}/channels creates.
const (
	listedInviteCode  = "AB12CD34" // 3/10, expires in 7 days, by 민지
	listedInviteCode2 = "ZZ99YY88" // 3, no limit, no expiry, by me
	newInviteCode     = "NW12CD34"
	newPubChannelID   = "01JB00000000000000NEWPUB01"
	newPrivChannelID  = "01JB00000000000000NEWPRV01"
)

// listedInvites are the invites GET /v1/spaces/{id}/invites lists for space.
func listedInvites(space string) []api.Invite {
	s := seeded()
	sp := api.SpacePreview{ID: space}
	if sd := s.Spaces[space]; sd != nil {
		sp = sd.SpacePreview
	}
	exp := routeNow().Add(7 * 24 * time.Hour)
	return []api.Invite{
		{Code: listedInviteCode, Space: sp, CreatedBy: *s.User(fixtures.UserMinji), MaxUses: ptrTo(10), UseCount: 3, ExpiresAt: &exp, CreatedAt: routeNow().Add(-time.Hour)},
		{Code: listedInviteCode2, Space: sp, CreatedBy: s.Me.UserSummary, UseCount: 3, CreatedAt: routeNow().Add(-48 * time.Hour)},
	}
}

// spaceInvites answers GET /v1/spaces/{id}/invites with listedInvites.
func spaceInvites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": listedInvites(r.PathValue("id"))})
}

// echoCreateInvite answers POST /v1/spaces/{id}/invites with newInviteCode, its
// max_uses and expiry taken from the body (omitted expiry = 7 days, null = none).
func echoCreateInvite(w http.ResponseWriter, r *http.Request) {
	var in map[string]json.RawMessage
	json.NewDecoder(r.Body).Decode(&in)
	s := seeded()
	inv := api.Invite{Code: newInviteCode, Space: api.SpacePreview{ID: r.PathValue("id")}, CreatedBy: s.Me.UserSummary, CreatedAt: routeNow()}
	if sd := s.Spaces[r.PathValue("id")]; sd != nil {
		inv.Space = sd.SpacePreview
	}
	var n int
	if raw, ok := in["max_uses"]; ok && json.Unmarshal(raw, &n) == nil && n > 0 {
		inv.MaxUses = &n
	}
	sec := 604800
	if raw, ok := in["expires_in_sec"]; ok {
		sec = 0
		json.Unmarshal(raw, &sec)
	}
	if sec > 0 {
		exp := routeNow().Add(time.Duration(sec) * time.Second)
		inv.ExpiresAt = &exp
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invite": inv})
}

// echoDirectInvite answers POST /v1/spaces/{id}/direct-invites: the posted user, invited by me.
func echoDirectInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		User string `json:"user"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	s := seeded()
	sp := api.SpacePreview{ID: r.PathValue("id")}
	if sd := s.Spaces[r.PathValue("id")]; sd != nil {
		sp = sd.SpacePreview
	}
	writeJSON(w, http.StatusCreated, map[string]any{"invite": api.DirectInvite{
		ID: "01JK" + echoID()[4:], Space: sp, Inviter: s.Me.UserSummary, Invitee: userByRef(in.User), Status: "pending", CreatedAt: routeNow(),
	}})
}

// echoCreateChannel answers POST /v1/spaces/{id}/channels with the posted text
// channel (newPrivChannelID when private, else newPubChannelID), last in position.
func echoCreateChannel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string   `json:"name"`
		Topic     string   `json:"topic"`
		IsPrivate bool     `json:"is_private"`
		Members   []string `json:"members"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	space := r.PathValue("id")
	id := newPubChannelID
	if in.IsPrivate {
		id = newPrivChannelID
	}
	name := in.Name
	ch := api.Channel{ID: id, Kind: api.ChannelText, SpaceID: &space, Name: &name, Topic: in.Topic, IsPrivate: in.IsPrivate,
		Position: 99, MemberCount: 1 + len(in.Members), CreatedAt: routeNow(), My: api.ChannelMyState{Notify: "all"}}
	writeJSON(w, http.StatusCreated, map[string]any{"channel": ch})
}

// ---- helpers ----

// toInvites opens the invites tab of the settings (from the overview).
func toInvites(h *uitest.Harness) { h.Press("l", "l") }

// toChannels opens the channels tab of the settings (from the overview).
func toChannels(h *uitest.Harness) { h.Press("l", "l", "l") }

// novaDialog opens #개발 and the registered overlay id with 노바 개발방 as args.
func novaDialog(t *testing.T, sz uitest.Size, id string) *uitest.Harness {
	t.Helper()
	h := devChannel(t, sz)
	h.Send(ui.OpenOverlayMsg{ID: id, Args: fixtures.SpaceNova})
	if overlays(h) != 1 {
		t.Fatalf("%s did not open:\n%s", id, h.Screen())
	}
	return h
}

// inviteSelect moves the invites-tab cursor onto the row holding text.
func inviteSelect(t testing.TB, h *uitest.Harness, text string) {
	t.Helper()
	v := settingsOf(t, h)
	for range 10 {
		h.Press("k")
	}
	for range 10 {
		if it, ok := v.invites.list.Selected(); ok && strings.Contains(it.label, text) {
			return
		}
		h.Press("j")
	}
	t.Fatalf("no invites row %q:\n%s", text, paneText(t, h, layout.SlotMain))
}

// channelSelect moves the channels-tab cursor onto the row holding text.
func channelSelect(t testing.TB, h *uitest.Harness, text string) {
	t.Helper()
	v := settingsOf(t, h)
	for range 10 {
		h.Press("k")
	}
	for range 10 {
		if it, ok := v.channels.list.Selected(); ok && strings.Contains(it.label, text) {
			return
		}
		h.Press("j")
	}
	t.Fatalf("no channels row %q:\n%s", text, paneText(t, h, layout.SlotMain))
}

// ---- tests ----

func TestInvitesTabGolden(t *testing.T) {
	forEachSize(t, uitest.AllSizes(), settingsAs("owner"), func(t *testing.T, h *uitest.Harness) { toInvites(h); h.Golden("invites") })
}

func TestInviteRow(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	ctx := h.Context()
	invs := listedInvites(fixtures.SpaceNova)
	if got := inviteRow(ctx, invs[0], 80); got != "AB12CD34 · 3/10회 · 11월 12일 만료 · 민지" {
		t.Fatalf("limited row %q", got)
	}
	if got := inviteRow(ctx, invs[1], 80); got != "ZZ99YY88 · 3회 · 만료 없음 · "+seeded().Me.DisplayName {
		t.Fatalf("unlimited row %q", got)
	}
	if got := inviteRow(ctx, invs[0], 12); got != "AB12CD34 · …" || len([]rune(got)) > 12 {
		t.Fatalf("narrow row %q", got)
	}
}

func TestInvitesTab(t *testing.T) {
	h := settingsAs("owner")(t, uitest.Size{W: 120, H: 40})
	path := "/v1/spaces/" + fixtures.SpaceNova + "/invites"
	if n := len(reqs(h.API, "GET", path)); n != 0 {
		t.Fatalf("invites are fetched on the tab only, got %d", n)
	}
	toInvites(h)
	if n := len(reqs(h.API, "GET", path)); n != 1 {
		t.Fatalf("GET invites on the tab, got %d", n)
	}
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "＋ 새 초대 링크", "＋ 친구 직접 초대", listedInviteCode, listedInviteCode2)

	// enter on an invite shows it
	inviteSelect(t, h, listedInviteCode)
	h.Press("enter")
	mustContain(t, overlayText(t, h), listedInviteCode, "masil join "+listedInviteCode)
	h.Press("esc")

	// ＋ 새 초대 링크 creates one: the new code is shown and joins the list
	inviteSelect(t, h, "새 초대 링크")
	h.Press("enter")
	mustContain(t, overlayText(t, h), "만료", "최대 사용")
	h.Press("enter")
	if r := reqs(h.API, "POST", path); len(r) != 1 {
		t.Fatalf("one POST invites, got %d", len(r))
	}
	mustContain(t, overlayText(t, h), newInviteCode, "masil join "+newInviteCode)
	h.Press("esc")
	if overlays(h) != 0 {
		t.Fatalf("esc closes the invite:\n%s", h.Screen())
	}
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "＋ 친구 직접 초대", newInviteCode, listedInviteCode)

	// ＋ 친구 직접 초대 opens the direct invite picker
	inviteSelect(t, h, "친구 직접 초대")
	h.Press("enter")
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "친구 초대") {
		t.Fatalf("direct invite picker:\n%s", h.Screen())
	}
	h.Press("esc")

	// re-entering the tab fetches again; a failed fetch says so
	h.Press("h", "l")
	if n := len(reqs(h.API, "GET", path)); n != 2 {
		t.Fatalf("GET invites again on re-entry, got %d", n)
	}
	srv := settingsServer(t, api.RoleOwner)
	srv.Error("GET "+path, http.StatusInternalServerError, "internal")
	f := openWith(t, uitest.Size{W: 120, H: 40}, novaSettings(), srv, uitest.WithSeed(settingsSeed(api.RoleOwner)))
	toInvites(f)
	mustContain(t, paneText(t, f, layout.SlotMain), "＋ 새 초대 링크", "초대 링크를 불러오지 못했어요")
}

func TestInviteCreateChoices(t *testing.T) {
	cases := []struct {
		expires, uses int
		want          string
	}{
		{0, 0, `{"expires_in_sec":604800}`}, // 7일 (the default choice), 무제한
		{1, 3, `{"max_uses":10,"expires_in_sec":86400}`},
		{2, 1, `{"max_uses":1,"expires_in_sec":3600}`},
		{3, 5, `{"max_uses":100,"expires_in_sec":2592000}`},
		{4, 0, `{"expires_in_sec":null}`}, // 무기한
	}
	path := "/v1/spaces/" + fixtures.SpaceNova + "/invites"
	for _, c := range cases {
		h := novaDialog(t, uitest.Size{W: 120, H: 40}, "chat.invite_create")
		mustContain(t, overlayText(t, h), "< 7일 >", "< 무제한 >")
		for range c.expires {
			h.Press("l")
		}
		h.Press("tab")
		for range c.uses {
			h.Press("l")
		}
		h.Press("enter")
		r := reqs(h.API, "POST", path)
		if len(r) != 1 {
			t.Fatalf("%+v: one POST, got %d", c, len(r))
		}
		mustJSONObject(t, r[0].Body, c.want)
		if overlays(h) != 1 {
			t.Fatalf("%+v: the form morphs into the invite:\n%s", c, h.Screen())
		}
		mustContain(t, overlayText(t, h), newInviteCode, "masil join "+newInviteCode)
	}

	// a failure stays in the form with the reason
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.API.Error("POST "+path, http.StatusForbidden, "action_not_allowed")
	h.Send(ui.OpenOverlayMsg{ID: "chat.invite_create", Args: fixtures.SpaceNova})
	h.Press("enter")
	if overlays(h) != 1 || strings.Contains(overlayText(t, h), "masil join") {
		t.Fatalf("failure keeps the form:\n%s", h.Screen())
	}
	mustContain(t, overlayText(t, h), "✕")

	// an unknown space opens nothing
	g := devChannel(t, uitest.Size{W: 120, H: 40})
	g.Send(ui.OpenOverlayMsg{ID: "chat.invite_create", Args: "01JB0000000000000000NSPACE"})
	if overlays(g) != 0 {
		t.Fatalf("unknown space:\n%s", g.Screen())
	}
}

func TestInviteRevokeAndCopy(t *testing.T) {
	h := settingsAs("owner")(t, uitest.Size{W: 120, H: 40})
	toInvites(h)
	inviteSelect(t, h, listedInviteCode)
	h.Press("y")
	if c := clipboards(h); !slices.Equal(c, []string{"masil join " + listedInviteCode}) {
		t.Fatalf("clipboard %q", c)
	}
	mustContain(t, toastText(t, h), "복사했어요")

	// y on an action row copies nothing
	inviteSelect(t, h, "새 초대 링크")
	h.Press("y")
	if c := clipboards(h); len(c) != 1 {
		t.Fatalf("action row copied: %q", c)
	}

	// m → 복사 / 폐기; 폐기 asks, then DELETE; the row leaves
	inviteSelect(t, h, listedInviteCode)
	h.Press("m")
	mustContain(t, overlayText(t, h), "복사", "폐기")
	h.Press("j", "enter") // 복사 → 폐기
	if !strings.Contains(h.Screen(), "폐기할까요?") {
		t.Fatalf("revoke asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", "/v1/invites/"+listedInviteCode)); n != 1 {
		t.Fatalf("one revoke, got %d", n)
	}
	main := paneText(t, h, layout.SlotMain)
	if strings.Contains(main, listedInviteCode) || !strings.Contains(main, listedInviteCode2) {
		t.Fatalf("revoked row gone, the other stays:\n%s", main)
	}

	// a failed revoke keeps the row
	h.API.Error("DELETE /v1/invites/"+listedInviteCode2, http.StatusForbidden, "action_not_allowed")
	inviteSelect(t, h, listedInviteCode2)
	h.Press("m")
	h.Press("j", "enter") // 복사 → 폐기
	h.Press("y")
	mustContain(t, paneText(t, h, layout.SlotMain), listedInviteCode2)
	mustContain(t, toastText(t, h), "초대 링크를 폐기하지 못했어요")
}

func TestInviteDirect(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}
	nova := fixtures.SpaceNova
	notHana := slices.DeleteFunc(novaMembers(api.RoleOwner), func(m api.SpaceMember) bool { return m.User.Handle == "hana" })

	// candidates: friends (민지, 준호, 하나) who are not members (하나 only)
	h := devChannel(t, sz)
	h.API.Handle("GET /v1/spaces/"+nova+"/members", membersHandler(notHana))
	h.Send(ui.OpenOverlayMsg{ID: "chat.invite_direct", Args: nova})
	txt := overlayText(t, h)
	if !strings.Contains(txt, "하나") || strings.Contains(txt, "민지") || strings.Contains(txt, "준호") {
		t.Fatalf("candidates = non-member friends:\n%s", txt)
	}
	h.Press("down", "enter", "enter")
	r := reqs(h.API, "POST", "/v1/spaces/"+nova+"/direct-invites")
	if len(r) != 1 {
		t.Fatalf("one direct invite, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"user":"@hana"}`)
	if overlays(h) != 0 {
		t.Fatalf("success closes the picker:\n%s", h.Screen())
	}
	mustContain(t, toastText(t, h), "하나님을 노바 개발방에 초대했어요")

	// 409 already_member stays in the picker with the reason
	h.API.Error("POST /v1/spaces/"+nova+"/direct-invites", http.StatusConflict, "already_member")
	h.Send(ui.OpenOverlayMsg{ID: "chat.invite_direct", Args: nova})
	h.Press("down", "enter", "enter")
	if overlays(h) != 1 {
		t.Fatalf("a failure keeps the picker:\n%s", h.Screen())
	}
	mustContain(t, overlayText(t, h), "이미 멤버예요")

	// the member list unknown (fetch failed): every friend is offered
	g := devChannel(t, sz)
	g.API.Error("GET /v1/spaces/"+nova+"/members", http.StatusInternalServerError, "internal")
	g.Send(ui.OpenOverlayMsg{ID: "chat.invite_direct", Args: nova})
	mustContain(t, overlayText(t, g), "민지", "준호", "하나")

	// everyone is a member already: says so
	e := novaDialog(t, sz, "chat.invite_direct")
	mustContain(t, overlayText(t, e), "초대할 수 있는 친구가 없어요")
}

func TestChannelCreatePublicPrivate(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}
	path := "/v1/spaces/" + fixtures.SpaceNova + "/channels"

	// a bad name is refused before any request
	h := novaDialog(t, sz, "chat.channel_create")
	mustContain(t, overlayText(t, h), "[ ] 비공개")
	h.Type("Bad Name")
	h.Press("enter")
	mustContain(t, overlayText(t, h), "영문 소문자·숫자·한글·_- 1~32자")
	if n := len(reqs(h.API, "POST", path)); n != 0 {
		t.Fatalf("bad name posted %d", n)
	}

	// public: {"name"} only; the new channel is stored and opened
	h.Press("ctrl+u")
	h.Type("배포알림")
	h.Press("enter")
	r := reqs(h.API, "POST", path)
	if len(r) != 1 {
		t.Fatalf("one POST channels, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"name":"배포알림"}`)
	if overlays(h) != 0 {
		t.Fatalf("success closes the form:\n%s", h.Screen())
	}
	if h.Context().Store.Channels[newPubChannelID] == nil {
		t.Fatal("the new channel is observed")
	}
	if tr := top(h); tr.Kind != ui.RouteChannel || tr.ID != newPubChannelID {
		t.Fatalf("opens the new channel: %+v", tr)
	}

	// a topic is sent when given
	p := novaDialog(t, sz, "chat.channel_create")
	p.Type("공지")
	p.Press("tab")
	p.Type("알림 모음")
	p.Press("enter")
	mustJSONObject(t, reqs(p.API, "POST", path)[0].Body, `{"name":"공지","topic":"알림 모음"}`)

	// private: the toggle, then a member picker (space members, me excluded)
	q := novaDialog(t, sz, "chat.channel_create")
	q.Type("비밀")
	q.Press("tab", "tab", "space")
	mustContain(t, overlayText(t, q), "[x] 비공개")
	q.Press("enter")
	if n := len(reqs(q.API, "POST", path)); n != 0 {
		t.Fatalf("private posts after the picker, got %d", n)
	}
	txt := overlayText(t, q)
	mustContain(t, txt, "민지", "준호", "하나")
	q.Type("민지")
	q.Press("enter", "enter")
	r = reqs(q.API, "POST", path)
	if len(r) != 1 {
		t.Fatalf("one private POST, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"name":"비밀","is_private":true,"members":["@minji"]}`)
	if tr := top(q); tr.ID != newPrivChannelID {
		t.Fatalf("opens the private channel: %+v", tr)
	}
	mustContain(t, paneText(t, q, layout.SlotSidebar), "🔒비밀")

	// private with nobody picked is fine: just me
	e := novaDialog(t, sz, "chat.channel_create")
	e.Type("혼자")
	e.Press("tab", "tab", "space", "enter")
	e.Press("enter")
	r = reqs(e.API, "POST", path)
	if len(r) != 1 {
		t.Fatalf("private with no members: one POST, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"name":"혼자","is_private":true}`)

	// 409 already_exists stays in the form with errtext.Describe
	f := devChannel(t, sz)
	f.API.Error("POST "+path, http.StatusConflict, "already_exists")
	f.Send(ui.OpenOverlayMsg{ID: "chat.channel_create", Args: fixtures.SpaceNova})
	f.Type("배포알림")
	f.Press("enter")
	if overlays(f) != 1 {
		t.Fatalf("409 keeps the form:\n%s", f.Screen())
	}
	mustContain(t, overlayText(t, f), errtext.Describe(i18n.KoT(), &api.Error{Status: http.StatusConflict, Code: "already_exists"}))
}

func TestChannelsTab(t *testing.T) {
	h := settingsAs("owner")(t, uitest.Size{W: 120, H: 40})
	toChannels(h)
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "＋ 새 채널", "# 일반", "# 잡담", "# 개발", "# 배포알림")

	// n opens chat.channel_create; so does enter on ＋ 새 채널
	h.Press("n")
	if overlays(h) != 1 || !strings.Contains(overlayText(t, h), "채널 이름") {
		t.Fatalf("n opens channel create:\n%s", h.Screen())
	}
	h.Press("esc")
	channelSelect(t, h, "새 채널")
	h.Press("enter")
	if overlays(h) != 1 || !strings.Contains(overlayText(t, h), "채널 이름") {
		t.Fatalf("enter on ＋ 새 채널:\n%s", h.Screen())
	}
	h.Press("esc")

	// enter on a channel opens its settings menu
	channelSelect(t, h, "개발")
	h.Press("enter")
	got := menuLabels(t, h)
	if !slices.Contains(got, "이름·주제 바꾸기") || !slices.Contains(got, "음소거") {
		t.Fatalf("channel prefs menu %q", got)
	}
	h.Press("esc")

	// a channel created elsewhere joins the list (Store-backed)
	h.Event("channel.created", map[string]any{"channel": api.Channel{ID: newPubChannelID, Kind: api.ChannelText, SpaceID: ptrTo(fixtures.SpaceNova),
		Name: ptrTo("새소식"), Topic: "소식 모음", Position: 9, CreatedAt: uitest.Start, My: api.ChannelMyState{Notify: "all"}}})
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "# 배포알림", "# 새소식 · 소식 모음")
}

func TestInviteChannelDialogsGolden(t *testing.T) {
	for _, id := range []string{"chat.invite_create", "chat.invite_direct", "chat.channel_create"} {
		t.Run(strings.TrimPrefix(id, "chat."), func(t *testing.T) {
			forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) {
				h.Send(ui.OpenOverlayMsg{ID: id, Args: spaceByName(t, h.Context().Store, "노바 개발방")})
				h.Golden(strings.TrimPrefix(id, "chat."))
			})
		})
	}
}

func TestCreatePaletteAvailability(t *testing.T) {
	for _, id := range []string{"invite.create", "invite.direct", "channel.create"} {
		if _, ok := palette.Lookup(id); !ok {
			t.Fatalf("%s not registered", id)
		}
	}
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	ctx := h.Context()
	if !canCreateInvite(ctx) || !canCreateChannel(ctx) || !canDirectInvite(ctx) {
		t.Fatal("owner: all available")
	}
	setRole(h, api.RoleAdmin)
	if !canCreateInvite(ctx) || !canCreateChannel(ctx) || !canDirectInvite(ctx) {
		t.Fatal("admin: all available")
	}
	setRole(h, api.RoleMember)
	if canCreateInvite(ctx) || canCreateChannel(ctx) || !canDirectInvite(ctx) {
		t.Fatalf("member: invite %v channel %v direct %v", canCreateInvite(ctx), canCreateChannel(ctx), canDirectInvite(ctx))
	}
	for id, avail := range map[string]func(*ui.Context) bool{"invite.create": canCreateInvite, "channel.create": canCreateChannel, "invite.direct": canDirectInvite} {
		cmd, _ := palette.Lookup(id)
		if cmd.Available == nil || cmd.Available(ctx) != avail(ctx) {
			t.Fatalf("%s: Available differs from its can* rule", id)
		}
	}

	// the commands open their dialogs for the current space
	setRole(h, api.RoleOwner)
	for id, want := range map[string]string{"invite.create": "최대 사용", "invite.direct": "친구 초대", "channel.create": "채널 이름"} {
		h.Send(ui.RunCommandMsg{ID: id})
		if overlays(h) != 1 || !strings.Contains(h.Screen(), want) {
			t.Fatalf("%s did not open its dialog:\n%s", id, h.Screen())
		}
		h.Press("esc")
	}

	// no space at hand: none
	empty := &ui.Context{Store: store.New()}
	if canCreateInvite(empty) || canCreateChannel(empty) || canDirectInvite(empty) {
		t.Fatal("no current space: none available")
	}
}
