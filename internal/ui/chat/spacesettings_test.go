package chat

import (
	"cmp"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- space settings test data (this file only) ----

const userBanned = "01JA00000000000000000BAN01" // 도배꾼 @spammer: banned from 노바 개발방

func bannedUser() api.UserSummary {
	return api.UserSummary{ID: userBanned, Handle: "spammer", DisplayName: "도배꾼"}
}

var memberJoined = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

// roleRank orders owner → admin → member (the server's member order, api.md 5.7).
func roleRank(r api.SpaceRole) int {
	switch r {
	case api.RoleOwner:
		return 0
	case api.RoleAdmin:
		return 1
	}
	return 2
}

// sortMembers sorts like GET /v1/spaces/{id}/members: role, then display name.
func sortMembers(ms []api.SpaceMember) {
	slices.SortFunc(ms, func(a, b api.SpaceMember) int {
		return cmp.Or(cmp.Compare(roleRank(a.Role), roleRank(b.Role)), strings.Compare(a.User.DisplayName, b.User.DisplayName))
	})
}

// novaMembers is the 노바 개발방 member list when my role is role: as owner, 민지
// is admin; as admin or member, 민지 is the owner and 하나 an admin.
func novaMembers(role api.SpaceRole) []api.SpaceMember {
	s := seeded()
	roles := map[string]api.SpaceRole{"sam": api.RoleOwner, "minji": api.RoleAdmin}
	if role != api.RoleOwner {
		roles = map[string]api.SpaceRole{"sam": role, "minji": api.RoleOwner, "hana": api.RoleAdmin}
	}
	var out []api.SpaceMember
	for _, h := range memberHandles {
		var u api.UserSummary
		for _, cu := range s.Users {
			if cu.Handle == h {
				u = *cu
			}
		}
		r, ok := roles[h]
		if !ok {
			r = api.RoleMember
		}
		out = append(out, api.SpaceMember{User: u, Role: r, JoinedAt: memberJoined})
	}
	sortMembers(out)
	return out
}

// membersHandler answers GET /v1/spaces/{id}/members from ms like the server: q
// matches a handle / display-name substring, role filters by role.
func membersHandler(ms []api.SpaceMember) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("q"))
		role := r.URL.Query().Get("role")
		items := []api.SpaceMember{}
		for _, m := range ms {
			if q != "" && !strings.Contains(strings.ToLower(m.User.Handle), q) && !strings.Contains(strings.ToLower(m.User.DisplayName), q) {
				continue
			}
			if role != "" && string(m.Role) != role {
				continue
			}
			items = append(items, m)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
	}
}

// novaDetailAs is the 노바 개발방 detail with my role role (민지 owns it unless I do).
func novaDetailAs(role api.SpaceRole) api.SpaceDetail {
	s := seeded()
	d := *s.Spaces[fixtures.SpaceNova]
	d.MyRole = role
	if role != api.RoleOwner {
		d.OwnerID = fixtures.UserMinji
	}
	d.Channels = []api.Channel{}
	for _, ch := range s.SpaceChannels(fixtures.SpaceNova) {
		d.Channels = append(d.Channels, *ch)
	}
	return d
}

// novaBan is 도배꾼, banned by me for 도배.
func novaBan() api.SpaceBan {
	return api.SpaceBan{User: bannedUser(), BannedBy: seeded().Me.UserSummary, Reason: "도배", CreatedAt: uitest.Start.Add(-24 * time.Hour)}
}

// settingsServer is newAPI with 노바 개발방 answered as role: its detail, its
// members (novaMembers) and one ban (concrete paths: other spaces keep the
// default routes).
func settingsServer(t testing.TB, role api.SpaceRole) *apitest.Server {
	t.Helper()
	srv := newAPI(t)
	nova := fixtures.SpaceNova
	srv.JSON("GET /v1/spaces/"+nova, http.StatusOK, map[string]any{"space": novaDetailAs(role)})
	srv.Handle("GET /v1/spaces/"+nova+"/members", membersHandler(novaMembers(role)))
	srv.JSON("GET /v1/spaces/"+nova+"/bans", http.StatusOK, map[string]any{"items": []api.SpaceBan{novaBan()}})
	return srv
}

// settingsSeed is the bootstrapped seed with my role in 노바 개발방 set to role.
func settingsSeed(role api.SpaceRole) func(*store.Store) {
	return func(s *store.Store) {
		bootstrappedSeed(s)
		d := novaDetailAs(role)
		s.Spaces[fixtures.SpaceNova].MyRole = d.MyRole
		s.Spaces[fixtures.SpaceNova].OwnerID = d.OwnerID
	}
}

// novaSettings is RouteSpaceSettings of 노바 개발방.
func novaSettings() ui.Route { return ui.Route{Kind: ui.RouteSpaceSettings, ID: fixtures.SpaceNova} }

// settingsAs opens 노바 개발방's settings with my role role ("owner"|"admin"|"member").
func settingsAs(role string) func(t *testing.T, sz uitest.Size) *uitest.Harness {
	return func(t *testing.T, sz uitest.Size) *uitest.Harness {
		t.Helper()
		r := api.SpaceRole(role)
		return openWith(t, sz, novaSettings(), settingsServer(t, r), uitest.WithSeed(settingsSeed(r)))
	}
}

// settingsOf is the RouteSpaceSettings instance of r (default 노바 개발방).
func settingsOf(t testing.TB, h *uitest.Harness) *spaceSettingsView {
	t.Helper()
	v, ok := h.Instance(novaSettings().Key()).(*spaceSettingsView)
	if !ok || v == nil {
		t.Fatalf("no space settings instance (%T)", h.Instance(novaSettings().Key()))
	}
	return v
}

// settingsTabLabels are the tab labels of the view for its current role.
func settingsTabLabels(ctx *ui.Context, v *spaceSettingsView) []string {
	var out []string
	for _, tab := range v.tabSet(ctx) {
		out = append(out, tab.label())
	}
	return out
}

// memberSelect moves the members-tab cursor with j/k onto the row of handle.
func memberSelect(t testing.TB, h *uitest.Harness, handle string) {
	t.Helper()
	v := settingsOf(t, h)
	for range 20 {
		h.Press("k")
	}
	for range 20 {
		if it, ok := v.members.list.Selected(); ok && it.member.User.Handle == handle {
			return
		}
		h.Press("j")
	}
	t.Fatalf("no member row @%s:\n%s", handle, paneText(t, h, layout.SlotMain))
}

// toMembers opens the members tab (from the overview).
func toMembers(h *uitest.Harness) { h.Press("l") }

// ---- tests ----

func TestSpaceSettingsGolden(t *testing.T) {
	for _, role := range []string{"owner", "member"} {
		for i, name := range []string{"overview", "members"} {
			t.Run(role+"_"+name, func(t *testing.T) {
				forEachSize(t, uitest.AllSizes(), settingsAs(role), func(t *testing.T, h *uitest.Harness) {
					for range i {
						h.Press("l")
					}
					h.Golden(role + "_" + name)
				})
			})
		}
	}
}

func TestSpaceSettingsTabsByRole(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}
	all := []string{"개요", "멤버", "초대", "채널", "차단됨"}
	for role, want := range map[string][]string{"owner": all, "admin": all, "member": {"개요", "멤버"}} {
		h := settingsAs(role)(t, sz)
		v := settingsOf(t, h)
		if got := settingsTabLabels(h.Context(), v); !slices.Equal(got, want) {
			t.Fatalf("%s: tabs %q, want %q", role, got, want)
		}
		main := paneText(t, h, layout.SlotMain)
		mustContainInOrder(t, main, want...)
		if role == "member" && strings.Contains(main, "차단됨") {
			t.Fatalf("member sees no bans tab:\n%s", main)
		}
		if title, _ := v.Title(h.Context()); title != "노바 개발방 설정" {
			t.Fatalf("%s: title %q", role, title)
		}
		// l walks every tab and stops at the last; h goes back
		for range len(want) + 2 {
			h.Press("l")
		}
		if v.tab != v.tabSet(h.Context())[len(want)-1] {
			t.Fatalf("%s: l stops at the last tab, got %v", role, v.tab)
		}
		h.Press("left")
		if v.tab != v.tabSet(h.Context())[len(want)-2] {
			t.Fatalf("%s: left goes back, got %v", role, v.tab)
		}
	}

	// a click on a tab label switches too
	h := settingsAs("owner")(t, sz)
	clickText(t, h, layout.SlotMain, "차단됨")
	if v := settingsOf(t, h); v.tab != tabBans {
		t.Fatalf("click on 차단됨: tab %v", v.tab)
	}

	// demoted to member while on the bans tab: the tab set shrinks, the tab clamps
	h = settingsAs("admin")(t, sz)
	h.Press("l", "l", "l", "l")
	me := seeded().Me.UserSummary
	h.Event("space.member_updated", realtime.SpaceMemberEvent{SpaceID: fixtures.SpaceNova, Member: api.SpaceMember{User: me, Role: api.RoleMember}})
	main := paneText(t, h, layout.SlotMain)
	if strings.Contains(main, "차단됨") || strings.Contains(main, "초대") {
		t.Fatalf("demoted: only 개요/멤버 left:\n%s", main)
	}
	if v := settingsOf(t, h); v.tab != tabMembers {
		t.Fatalf("demoted: clamped to the last tab, got %v", v.tab)
	}
}

func TestSpaceSettingsOverview(t *testing.T) {
	h := settingsAs("owner")(t, uitest.Size{W: 120, H: 40})
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "🚀 노바 개발방", "노바 팀 개발 이야기", "멤버 5명", "내 역할: 방장", "방 정보 바꾸기", "방장 넘기기", "방 삭제")
	if strings.Contains(main, "방 나가기") || strings.Contains(main, "방 신고") {
		t.Fatalf("owner: no 방 나가기 / 방 신고:\n%s", main)
	}
	if n := len(reqs(h.API, "GET", "/v1/spaces/"+fixtures.SpaceNova)); n != 1 {
		t.Fatalf("GET space once on entry, got %d", n)
	}
	if n := len(reqs(h.API, "GET", "/v1/spaces/"+fixtures.SpaceNova+"/members")); n != 1 {
		t.Fatalf("GET members once on entry, got %d", n)
	}

	h = settingsAs("admin")(t, uitest.Size{W: 120, H: 40})
	main = paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "내 역할: 관리자", "방 정보 바꾸기", "방 나가기", "방 신고")
	if strings.Contains(main, "방장 넘기기") || strings.Contains(main, "방 삭제") {
		t.Fatalf("admin: no owner actions:\n%s", main)
	}

	h = settingsAs("member")(t, uitest.Size{W: 120, H: 40})
	main = paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "내 역할: 멤버", "방 나가기", "방 신고")
	if strings.Contains(main, "방 정보 바꾸기") {
		t.Fatalf("member: no 방 정보 바꾸기:\n%s", main)
	}

	// space.updated changes the overview at once
	h.Event("space.updated", realtime.SpaceUpdated{Space: api.Space{SpacePreview: api.SpacePreview{ID: fixtures.SpaceNova, Name: "새 이름", MemberCount: 7}}})
	mustContain(t, paneText(t, h, layout.SlotMain), "새 이름", "멤버 7명")
	if title, _ := settingsOf(t, h).Title(h.Context()); title != "새 이름 설정" {
		t.Fatalf("title follows the store: %q", title)
	}
}

func TestOverviewActions(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}
	nova := fixtures.SpaceNova

	// owner: 방 정보 바꾸기 — the form is prefilled and only the changed field is sent
	h := settingsAs("owner")(t, sz)
	h.Press("enter")
	if overlays(h) != 1 {
		t.Fatalf("방 정보 바꾸기 opens a form:\n%s", h.Screen())
	}
	mustContain(t, overlayText(t, h), "노바 개발방", "노바 팀 개발 이야기", "🚀")
	h.Type("2")
	h.Press("enter")
	r := reqs(h.API, "PATCH", "/v1/spaces/"+nova)
	if len(r) != 1 {
		t.Fatalf("one PATCH, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"name":"노바 개발방2"}`)
	if overlays(h) != 0 {
		t.Fatalf("the form closes on success:\n%s", h.Screen())
	}
	if got := h.Context().Store.Spaces[nova].Name; got != "노바 개발방2" {
		t.Fatalf("store name %q", got)
	}
	if got := h.Context().Store.Spaces[nova].MyRole; got != api.RoleOwner {
		t.Fatalf("the update keeps my role, got %q", got)
	}
	mustContain(t, toastText(t, h), "방 정보를 바꿨어요")

	// the icon choice: 없음 sends null, nothing else
	h = settingsAs("owner")(t, sz)
	h.Press("enter", "tab", "tab", "left")
	h.Press("enter")
	r = reqs(h.API, "PATCH", "/v1/spaces/"+nova)
	if len(r) != 1 {
		t.Fatalf("icon: one PATCH, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"icon_emoji":null}`)

	// nothing changed: no request
	h = settingsAs("owner")(t, sz)
	h.Press("enter", "enter")
	if n := len(reqs(h.API, "PATCH", "/v1/spaces/"+nova)); n != 0 || overlays(h) != 0 {
		t.Fatalf("unchanged form: %d PATCH, %d overlays", n, overlays(h))
	}

	// owner: 방장 넘기기 — space members (not me) → Confirm → POST transfer
	h = settingsAs("owner")(t, sz)
	h.Press("j", "enter")
	if overlays(h) != 1 {
		t.Fatalf("방장 넘기기 opens a picker:\n%s", h.Screen())
	}
	mustContain(t, overlayText(t, h), "하나", "준호", "민지")
	h.Type("하나")
	h.Press("enter", "enter")
	if !strings.Contains(h.Screen(), "하나님에게 방장을 넘길까요?") {
		t.Fatalf("transfer asks first:\n%s", h.Screen())
	}
	if n := len(reqs(h.API, "POST", "/v1/spaces/"+nova+"/transfer")); n != 0 {
		t.Fatalf("no transfer before the confirm (%d)", n)
	}
	h.Press("y")
	r = reqs(h.API, "POST", "/v1/spaces/"+nova+"/transfer")
	if len(r) != 1 {
		t.Fatalf("one transfer, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"user":"@hana"}`)
	sd := h.Context().Store.Spaces[nova]
	if sd.MyRole != api.RoleAdmin || sd.OwnerID != fixtures.UserHana {
		t.Fatalf("after transfer: role %q owner %q", sd.MyRole, sd.OwnerID)
	}
	main := paneText(t, h, layout.SlotMain)
	mustContain(t, main, "내 역할: 관리자", "방 나가기")
	if strings.Contains(main, "방장 넘기기") {
		t.Fatalf("no more owner actions:\n%s", main)
	}
	toMembers(h)
	mustContain(t, rowOf(t, h, "@hana"), "방장")
	mustContain(t, rowOf(t, h, "@sam"), "관리자")

	// owner: 방 삭제 → Confirm → DELETE → home, the space is gone; no 방 나가기
	h = settingsAs("owner")(t, sz)
	h.Press("j", "j", "enter")
	if !strings.Contains(h.Screen(), "삭제할까요?") {
		t.Fatalf("방 삭제 asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", "/v1/spaces/"+nova)); n != 1 {
		t.Fatalf("one DELETE space, got %d", n)
	}
	if tr := top(h); tr.Kind != ui.RouteHome {
		t.Fatalf("home after delete: %+v", tr)
	}
	if h.Context().Store.Spaces[nova] != nil || slices.Contains(h.Context().Store.SpaceOrder, nova) {
		t.Fatal("the deleted space leaves the store at once")
	}

	// member: 방 나가기 → Confirm → DELETE members/@me → home
	h = settingsAs("member")(t, sz)
	h.Press("enter")
	if !strings.Contains(h.Screen(), "나갈까요?") {
		t.Fatalf("방 나가기 asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", "/v1/spaces/"+nova+"/members/@me")); n != 1 {
		t.Fatalf("one leave, got %d", n)
	}
	if tr := top(h); tr.Kind != ui.RouteHome {
		t.Fatalf("home after leave: %+v", tr)
	}
	if h.Context().Store.Spaces[nova] != nil {
		t.Fatal("the space I left leaves the store at once")
	}

	// member: 방 신고 → the report dialog
	h = settingsAs("member")(t, sz)
	h.Press("j", "enter")
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "신고 · 노바 개발방") {
		t.Fatalf("방 신고 → report dialog:\n%s", h.Screen())
	}

	// a click on an action runs it
	h = settingsAs("member")(t, sz)
	clickText(t, h, layout.SlotMain, "방 신고")
	clickText(t, h, layout.SlotMain, "방 신고")
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "신고 · 노바 개발방") {
		t.Fatalf("click 방 신고 → report dialog:\n%s", h.Screen())
	}
}

func TestOverviewLeaveFails(t *testing.T) {
	srv := settingsServer(t, api.RoleMember)
	srv.Error("DELETE /v1/spaces/"+fixtures.SpaceNova+"/members/@me", http.StatusInternalServerError, "internal")
	h := openWith(t, uitest.Size{W: 120, H: 40}, novaSettings(), srv, uitest.WithSeed(settingsSeed(api.RoleMember)))
	h.Press("enter", "y")
	if h.Context().Store.Spaces[fixtures.SpaceNova] == nil {
		t.Fatal("a failed leave keeps the space")
	}
	if tr := top(h); tr.Kind != ui.RouteSpaceSettings {
		t.Fatalf("a failed leave stays: %+v", tr)
	}
	mustContain(t, toastText(t, h), "방에서 나가지 못했어요")
}

// rowOf is the members-tab line holding text.
func rowOf(t testing.TB, h *uitest.Harness, text string) string {
	t.Helper()
	for _, l := range strings.Split(paneText(t, h, layout.SlotMain), "\n") {
		if strings.Contains(l, text) {
			return l
		}
	}
	t.Fatalf("no row with %q:\n%s", text, paneText(t, h, layout.SlotMain))
	return ""
}

func TestSpaceMembersRows(t *testing.T) {
	h := settingsAs("owner")(t, uitest.Size{W: 120, H: 40})
	toMembers(h)
	main := paneText(t, h, layout.SlotMain)
	mustContainInOrder(t, main, "sam @sam", "민지 @minji", "김개발 @dev_kim", "준호 @junho", "하나 @hana")
	if row := rowOf(t, h, "@sam"); !strings.HasSuffix(strings.TrimRight(row, " "), "방장") {
		t.Fatalf("the role label is right-aligned: %q", row)
	}
	mustContain(t, rowOf(t, h, "@minji"), "관리자")
	if row := rowOf(t, h, "@junho"); strings.Contains(row, "관리자") || strings.Contains(row, "방장") {
		t.Fatalf("a member row has no role label: %q", row)
	}
	mustContain(t, rowOf(t, h, "@minji"), "●")

	// narrow: the role label stays whole, the name gives way
	h = settingsAs("owner")(t, uitest.Size{W: 40, H: 20})
	toMembers(h)
	scr := h.Screen()
	if !strings.Contains(scr, "방장") || !strings.Contains(scr, "관리자") {
		t.Fatalf("narrow: role labels kept:\n%s", scr)
	}
}

func TestMemberRoleAndKick(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}
	nova := fixtures.SpaceNova

	// owner on a member row: 관리자로 지정 → PATCH {"role":"admin"}
	h := settingsAs("owner")(t, sz)
	toMembers(h)
	memberSelect(t, h, "junho")
	h.Press("m")
	if got, want := menuLabels(t, h), []string{"관리자로 지정", "내보내기", "차단(밴)", "DM 보내기", "사용자 신고", "차단"}; !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("owner menu on a member %q, want %q", got, want)
	}
	clickOn(t, h, "관리자로 지정", tea.MouseLeft)
	r := reqs(h.API, "PATCH", "/v1/spaces/"+nova+"/members/@junho")
	if len(r) != 1 {
		t.Fatalf("one role PATCH, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"role":"admin"}`)
	mustContain(t, rowOf(t, h, "@junho"), "관리자")

	// owner on an admin row: 관리자 해제 → PATCH {"role":"member"}
	h = settingsAs("owner")(t, sz)
	toMembers(h)
	memberSelect(t, h, "minji")
	h.Press("m")
	if got := menuLabels(t, h); !slices.Contains(got, "관리자 해제") || slices.Contains(got, "관리자로 지정") {
		t.Fatalf("owner menu on an admin %q", got)
	}
	clickOn(t, h, "관리자 해제", tea.MouseLeft)
	r = reqs(h.API, "PATCH", "/v1/spaces/"+nova+"/members/@minji")
	if len(r) != 1 {
		t.Fatalf("one role PATCH, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"role":"member"}`)

	// 내보내기 → Confirm → DELETE (no query); the row leaves
	h = settingsAs("owner")(t, sz)
	toMembers(h)
	memberSelect(t, h, "junho")
	h.Press("m")
	clickOn(t, h, "내보내기", tea.MouseLeft)
	if !strings.Contains(h.Screen(), "내보낼까요?") {
		t.Fatalf("내보내기 asks first:\n%s", h.Screen())
	}
	h.Press("y")
	r = reqs(h.API, "DELETE", "/v1/spaces/"+nova+"/members/@junho")
	if len(r) != 1 || r[0].Query != "" {
		t.Fatalf("one kick without a query, got %+v", r)
	}
	if strings.Contains(paneText(t, h, layout.SlotMain), "@junho") {
		t.Fatalf("the kicked member's row leaves:\n%s", paneText(t, h, layout.SlotMain))
	}

	// 차단(밴) → reason form → DELETE ?ban=true&reason=도배
	h = settingsAs("owner")(t, sz)
	toMembers(h)
	memberSelect(t, h, "junho")
	h.Press("m")
	clickOn(t, h, "차단(밴)", tea.MouseLeft)
	if overlays(h) != 1 {
		t.Fatalf("차단(밴) opens a form:\n%s", h.Screen())
	}
	h.Type("도배")
	h.Press("enter")
	r = reqs(h.API, "DELETE", "/v1/spaces/"+nova+"/members/@junho")
	if len(r) != 1 || r[0].Query != "ban=true&reason=%EB%8F%84%EB%B0%B0" {
		t.Fatalf("one ban with the reason, got %+v", r)
	}
	if overlays(h) != 0 || strings.Contains(paneText(t, h, layout.SlotMain), "@junho") {
		t.Fatalf("ban: form closed and row gone:\n%s", h.Screen())
	}

	// a ban without a reason sends ban=true only
	h = settingsAs("owner")(t, sz)
	toMembers(h)
	memberSelect(t, h, "junho")
	h.Press("m")
	clickOn(t, h, "차단(밴)", tea.MouseLeft)
	h.Press("enter")
	if r = reqs(h.API, "DELETE", "/v1/spaces/"+nova+"/members/@junho"); len(r) != 1 || r[0].Query != "ban=true" {
		t.Fatalf("ban without a reason, got %+v", r)
	}

	// admin: no actions on the owner or another admin; kick / ban on members
	h = settingsAs("admin")(t, sz)
	toMembers(h)
	for _, handle := range []string{"minji", "hana"} {
		memberSelect(t, h, handle)
		h.Press("m")
		got := menuLabels(t, h)
		for _, no := range []string{"내보내기", "차단(밴)", "관리자로 지정", "관리자 해제"} {
			if slices.Contains(got, no) {
				t.Fatalf("admin on @%s: %q has %q", handle, got, no)
			}
		}
		h.Press("esc")
	}
	memberSelect(t, h, "junho")
	h.Press("m")
	if got, want := menuLabels(t, h), []string{"내보내기", "차단(밴)", "DM 보내기", "사용자 신고", "차단"}; !slices.Equal(got[len(got)-len(want):], want) {
		t.Fatalf("admin menu on a member %q, want %q", got, want)
	}
	h.Press("esc")

	// member: only DM 보내기, 사용자 신고, 차단
	h = settingsAs("member")(t, sz)
	toMembers(h)
	memberSelect(t, h, "junho")
	h.Press("m")
	if got, want := menuLabels(t, h), []string{"DM 보내기", "사용자 신고", "차단"}; !slices.Equal(got[len(got)-len(want):], want) || slices.Contains(got, "내보내기") {
		t.Fatalf("member menu %q, want %q", got, want)
	}
	h.Press("esc")

	// my own row has no menu
	memberSelect(t, h, "sam")
	h.Press("m")
	if overlays(h) != 0 {
		t.Fatalf("no menu on my own row:\n%s", h.Screen())
	}
}

func TestMemberRowActions(t *testing.T) {
	h := settingsAs("owner")(t, uitest.Size{W: 120, H: 40})
	ctx := h.Context()
	s := ctx.Store
	junho := api.SpaceMember{User: userBy(t, s, "junho"), Role: api.RoleMember}
	s.Blocks[fixtures.UserJunho] = uitest.Start
	if got := labelsOf(memberRowActions(ctx, fixtures.SpaceNova, junho)); slices.Contains(got, "차단") {
		t.Fatalf("no 차단 for a user I blocked: %q", got)
	}
	me := api.SpaceMember{User: s.Me.UserSummary, Role: api.RoleOwner}
	if got := memberRowActions(ctx, fixtures.SpaceNova, me); len(got) != 0 {
		t.Fatalf("none for my own row: %q", labelsOf(got))
	}
	owner := api.SpaceMember{User: userBy(t, s, "minji"), Role: api.RoleOwner}
	setRole(h, api.RoleAdmin)
	if got := labelsOf(memberRowActions(ctx, fixtures.SpaceNova, owner)); slices.Contains(got, "내보내기") {
		t.Fatalf("admin cannot kick the owner: %q", got)
	}
}

func TestMemberFilter(t *testing.T) {
	h := settingsAs("owner")(t, uitest.Size{W: 120, H: 40})
	toMembers(h)
	path := "/v1/spaces/" + fixtures.SpaceNova + "/members"
	before := len(reqs(h.API, "GET", path))
	h.Press("/")
	v := settingsOf(t, h)
	if !v.CapturesInput() {
		t.Fatal("/ focuses the filter input")
	}
	h.Type("준")
	if n := len(reqs(h.API, "GET", path)); n != before {
		t.Fatalf("no fetch before the debounce (%d -> %d)", before, n)
	}
	if c := h.Cursor(); c == nil {
		t.Fatal("the filter input shows the real cursor")
	}
	h.Advance(299 * time.Millisecond)
	if n := len(reqs(h.API, "GET", path)); n != before {
		t.Fatalf("no fetch at 299ms (%d -> %d)", before, n)
	}
	h.Advance(time.Millisecond)
	if n := len(reqs(h.API, "GET", path)); n != before+1 {
		t.Fatalf("one fetch after 300ms (%d -> %d)", before, n)
	}
	if q := lastQuery(h.API, path); q != "q=%EC%A4%80" {
		t.Fatalf("query %q", q)
	}
	main := paneText(t, h, layout.SlotMain)
	if !strings.Contains(main, "@junho") || strings.Contains(main, "@minji") {
		t.Fatalf("filtered rows:\n%s", main)
	}

	// typing restarts the debounce: one fetch for the settled query
	h.Type("호")
	h.Advance(200 * time.Millisecond)
	h.Press("backspace")
	h.Advance(200 * time.Millisecond)
	h.Type("호")
	h.Advance(299 * time.Millisecond)
	if n := len(reqs(h.API, "GET", path)); n != before+1 {
		t.Fatalf("debounce restarts (%d)", n)
	}
	h.Advance(time.Millisecond)
	if n := len(reqs(h.API, "GET", path)); n != before+2 || lastQuery(h.API, path) != "q=%EC%A4%80%ED%98%B8" {
		t.Fatalf("one fetch for the settled query (%d, %q)", n, lastQuery(h.API, path))
	}

	// settling back on the applied query fetches nothing
	h.Press("backspace")
	h.Type("호")
	h.Advance(time.Second)
	if n := len(reqs(h.API, "GET", path)); n != before+2 {
		t.Fatalf("same query again: no fetch (%d)", n)
	}

	// esc leaves the input (the query stays); the list keys work again
	h.Press("esc")
	if v.CapturesInput() {
		t.Fatal("esc leaves the filter input")
	}
	if tr := top(h); tr.Kind != ui.RouteSpaceSettings {
		t.Fatalf("esc in the filter does not go back: %+v", tr)
	}
	mustContain(t, paneText(t, h, layout.SlotMain), "준호")
}

func TestMemberPaging(t *testing.T) {
	srv := settingsServer(t, api.RoleOwner)
	all := novaMembers(api.RoleOwner)
	// same shape as settingsServer's route: replaced for this test (two pages)
	srv.Handle("GET /v1/spaces/"+fixtures.SpaceNova+"/members", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			writeJSON(w, http.StatusOK, map[string]any{"items": all[:3], "next_cursor": "P2"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": all[3:], "next_cursor": nil})
	})
	h := openWith(t, uitest.Size{W: 120, H: 40}, novaSettings(), srv, uitest.WithSeed(settingsSeed(api.RoleOwner)))
	toMembers(h)
	path := "/v1/spaces/" + fixtures.SpaceNova + "/members"
	if strings.Contains(paneText(t, h, layout.SlotMain), "@junho") {
		t.Fatal("only the first page so far")
	}
	if n := len(reqs(h.API, "GET", path)); n != 1 {
		t.Fatalf("one page so far, got %d", n)
	}
	for range 5 {
		h.Press("j")
	}
	if n := len(reqs(h.API, "GET", path)); n != 2 || lastQuery(h.API, path) != "cursor=P2" {
		t.Fatalf("reaching the bottom fetches the next page: %d %q", n, lastQuery(h.API, path))
	}
	mustContainInOrder(t, paneText(t, h, layout.SlotMain), "@minji", "@dev_kim", "@junho", "@hana")
	for range 5 {
		h.Press("j")
	}
	if n := len(reqs(h.API, "GET", path)); n != 2 {
		t.Fatalf("no more pages, got %d", n)
	}
}

// TestTransferPickerLoadsAllPages: 방장 넘기기 offers every member, not just the
// first page — the target here is only on page 2.
func TestTransferPickerLoadsAllPages(t *testing.T) {
	srv := settingsServer(t, api.RoleOwner)
	var first, rest []api.SpaceMember
	for _, m := range novaMembers(api.RoleOwner) {
		if m.User.Handle == "hana" {
			rest = append(rest, m)
		} else {
			first = append(first, m)
		}
	}
	// same shape as settingsServer's route: replaced for this test (two pages)
	srv.Handle("GET /v1/spaces/"+fixtures.SpaceNova+"/members", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			writeJSON(w, http.StatusOK, map[string]any{"items": first, "next_cursor": "P2"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rest, "next_cursor": nil})
	})
	h := openWith(t, uitest.Size{W: 120, H: 40}, novaSettings(), srv, uitest.WithSeed(settingsSeed(api.RoleOwner)))
	h.Press("j", "enter")
	if overlays(h) != 1 {
		t.Fatalf("방장 넘기기 opens a picker:\n%s", h.Screen())
	}
	path := "/v1/spaces/" + fixtures.SpaceNova + "/members"
	if q := lastQuery(h.API, path); !strings.Contains(q, "cursor=P2") || !strings.Contains(q, "limit=100") {
		t.Fatalf("the picker reads the next page: %q", q)
	}
	mustContain(t, overlayText(t, h), "하나", "준호")
	h.Type("하나")
	h.Press("enter", "enter")
	if !strings.Contains(h.Screen(), "하나님에게 방장을 넘길까요?") {
		t.Fatalf("a page-2 member can be picked:\n%s", h.Screen())
	}
	h.Press("y")
	r := reqs(h.API, "POST", "/v1/spaces/"+fixtures.SpaceNova+"/transfer")
	if len(r) != 1 {
		t.Fatalf("one transfer, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"user":"@hana"}`)
}

func TestMemberUpdatedLive(t *testing.T) {
	h := settingsAs("owner")(t, uitest.Size{W: 120, H: 40})
	toMembers(h)
	nova := fixtures.SpaceNova
	junho := userBy(t, seeded(), "junho")
	h.Event("space.member_updated", realtime.SpaceMemberEvent{SpaceID: nova, Member: api.SpaceMember{User: junho, Role: api.RoleAdmin}})
	mustContain(t, rowOf(t, h, "@junho"), "관리자")

	// another space's event changes nothing
	h.Event("space.member_updated", realtime.SpaceMemberEvent{SpaceID: fixtures.SpaceHostile, Member: api.SpaceMember{User: userBy(t, seeded(), "hana"), Role: api.RoleAdmin}})
	if strings.Contains(rowOf(t, h, "@hana"), "관리자") {
		t.Fatal("an event of another space is ignored")
	}

	// joined adds a row, left removes it
	h.Event("space.member_joined", realtime.SpaceMemberEvent{SpaceID: nova, Member: api.SpaceMember{User: sunUser(), Role: api.RoleMember, JoinedAt: uitest.Start}})
	mustContain(t, rowOf(t, h, "@taeyang"), "태양")
	h.Event("space.member_left", realtime.SpaceMemberLeft{SpaceID: nova, UserID: fixtures.UserHana, Reason: "left"})
	if strings.Contains(paneText(t, h, layout.SlotMain), "@hana") {
		t.Fatalf("member_left removes the row:\n%s", paneText(t, h, layout.SlotMain))
	}
}

func TestSpaceSettingsGone(t *testing.T) {
	h := settingsAs("member")(t, uitest.Size{W: 120, H: 40})
	h.Event("space.removed", realtime.SpaceRemoved{SpaceID: fixtures.SpaceNova, Reason: "kicked"})
	mustContain(t, paneText(t, h, layout.SlotMain), "볼 수 없는 방이에요")
	h.Press("l", "enter", "m", "/")
	if overlays(h) != 0 {
		t.Fatalf("a gone space has no actions:\n%s", h.Screen())
	}
	h.Check()

	// a space unknown from the start (GET 404) shows the same notice
	srv := newAPI(t)
	srv.Error("GET /v1/spaces/01JB0000000000000000NSPACE", http.StatusNotFound, "not_found")
	g := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteSpaceSettings, ID: "01JB0000000000000000NSPACE"}, srv, uitest.WithSeed(bootstrappedSeed))
	mustContain(t, paneText(t, g, layout.SlotMain), "볼 수 없는 방이에요")
}

func TestBansTab(t *testing.T) {
	h := settingsAs("owner")(t, uitest.Size{W: 120, H: 40})
	nova := fixtures.SpaceNova
	bans := "/v1/spaces/" + nova + "/bans"
	if n := len(reqs(h.API, "GET", bans)); n != 0 {
		t.Fatalf("bans are fetched on the tab only, got %d", n)
	}
	h.Press("l", "l", "l", "l")
	if n := len(reqs(h.API, "GET", bans)); n != 1 {
		t.Fatalf("GET bans on the tab, got %d", n)
	}
	mustContain(t, rowOf(t, h, "@spammer"), "도배꾼", "도배", "sam")
	h.Press("enter")
	if !strings.Contains(h.Screen(), "차단을 풀까요?") {
		t.Fatalf("unban asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", bans+"/@spammer")); n != 1 {
		t.Fatalf("one unban, got %d", n)
	}
	mustContain(t, paneText(t, h, layout.SlotMain), "차단한 사람이 없어요")

	// leaving and re-entering the tab fetches again
	h.Press("h", "l")
	if n := len(reqs(h.API, "GET", bans)); n != 2 {
		t.Fatalf("GET bans again on re-entry, got %d", n)
	}

	// a failed fetch says so
	srv := settingsServer(t, api.RoleOwner)
	srv.Error("GET "+bans, http.StatusInternalServerError, "internal")
	f := openWith(t, uitest.Size{W: 120, H: 40}, novaSettings(), srv, uitest.WithSeed(settingsSeed(api.RoleOwner)))
	f.Press("l", "l", "l", "l")
	mustContain(t, paneText(t, f, layout.SlotMain), "차단 목록을 불러오지 못했어요")
}

func TestSpaceSettingsPalette(t *testing.T) {
	for _, id := range []string{"space.settings", "space.leave"} {
		if _, ok := palette.Lookup(id); !ok {
			t.Fatalf("%s not registered", id)
		}
	}
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "space.settings"})
	if tr := top(h); tr.Key() != novaSettings().Key() {
		t.Fatalf("space.settings opens the current space's settings: %+v", tr)
	}

	// space.leave: the owner is told to transfer first, a member is asked
	h = devChannel(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.RunCommandMsg{ID: "space.leave"})
	if overlays(h) != 0 {
		t.Fatalf("owner: no confirm:\n%s", h.Screen())
	}
	mustContain(t, toastText(t, h), "방장을 넘긴 뒤에 나갈 수 있어요")
	setRole(h, api.RoleMember)
	h.Send(ui.RunCommandMsg{ID: "space.leave"})
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "나갈까요?") {
		t.Fatalf("member: leave asks first:\n%s", h.Screen())
	}
	h.Press("y")
	if n := len(reqs(h.API, "DELETE", "/v1/spaces/"+fixtures.SpaceNova+"/members/@me")); n != 1 {
		t.Fatalf("one leave, got %d", n)
	}

	// no space at hand: not available
	cmd, _ := palette.Lookup("space.settings")
	s := store.New()
	if cmd.Available != nil && cmd.Available(&ui.Context{Store: s}) {
		t.Fatal("space.settings needs a current space")
	}
}

// hostileSettings opens the hostile space's settings as its owner: hostile users
// as members and a ban with a hostile reason.
func hostileSettings(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	hs := seeded()
	fixtures.Hostile(hs)
	d := *hs.Spaces[fixtures.SpaceHostile]
	d.MyRole = api.RoleOwner
	d.Channels = []api.Channel{}
	members := []api.SpaceMember{{User: hs.Me.UserSummary, Role: api.RoleOwner, JoinedAt: memberJoined}}
	var bans []api.SpaceBan
	for _, u := range hs.Users {
		if strings.HasPrefix(u.Handle, "hostile_") || u.Handle == "ctrl_user" {
			members = append(members, api.SpaceMember{User: *u, Role: api.RoleAdmin, JoinedAt: memberJoined})
			bans = append(bans, api.SpaceBan{User: *u, BannedBy: *u, Reason: "\x1b[31m빨강\x1b[0m\t탭\r" + strings.Repeat("긴사유👨‍👩‍👧", 40), CreatedAt: uitest.Start})
		}
	}
	srv := newAPI(t)
	srv.JSON("GET /v1/spaces/"+fixtures.SpaceHostile, http.StatusOK, map[string]any{"space": d})
	srv.JSON("GET /v1/spaces/"+fixtures.SpaceHostile+"/members", http.StatusOK, map[string]any{"items": members, "next_cursor": "MORE"})
	srv.JSON("GET /v1/spaces/"+fixtures.SpaceHostile+"/bans", http.StatusOK, map[string]any{"items": bans})
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		fixtures.Hostile(s)
		s.Spaces[fixtures.SpaceHostile].MyRole = api.RoleOwner
	}
	return openWith(t, sz, ui.Route{Kind: ui.RouteSpaceSettings, ID: fixtures.SpaceHostile}, srv, uitest.WithSeed(seed))
}

func TestSpaceSettingsHostileIntegrity(t *testing.T) {
	for i, name := range []string{"overview", "members", "invites", "channels", "bans"} {
		t.Run(name, func(t *testing.T) {
			forEachSize(t, uitest.AllSizes(), hostileSettings, func(t *testing.T, h *uitest.Harness) {
				for range i {
					h.Press("l")
				}
				h.Check()
				if i == 1 {
					h.Press("/")
					h.Paste("\x1b[31m준👨‍👩‍👧\t탭\n끝")
					h.Check()
				}
			})
		})
	}
}

// TestPaletteLeaveLandsAnywhere: space.leave from a route with no conversation,
// members pane or settings route alive still lands its result (store, toast,
// home), and a failure is toasted.
func TestPaletteLeaveLandsAnywhere(t *testing.T) {
	member := func(s *store.Store) {
		bootstrappedSeed(s)
		s.Spaces[fixtures.SpaceNova].MyRole = api.RoleMember
	}
	for _, sz := range []uitest.Size{{W: 120, H: 30}, {W: 80, H: 24}, {W: 120, H: 40}} {
		h := open(t, sz, ui.Route{Kind: ui.RouteHome}, uitest.WithSeed(member))
		h.Send(ui.RunCommandMsg{ID: "space.leave"})
		if !strings.Contains(h.Screen(), "나갈까요?") {
			t.Fatalf("%v: leave asks first:\n%s", sz, h.Screen())
		}
		h.Press("enter")
		if n := len(reqs(h.API, "DELETE", "/v1/spaces/"+fixtures.SpaceNova+"/members/@me")); n != 1 {
			t.Fatalf("%v: one leave, got %d", sz, n)
		}
		if h.Context().Store.Spaces[fixtures.SpaceNova] != nil || slices.Contains(h.Context().Store.SpaceOrder, fixtures.SpaceNova) {
			t.Fatalf("%v: the space I left leaves the store", sz)
		}
		mustContain(t, toastText(t, h), "방에서 나왔어요")
		if overlays(h) != 0 {
			t.Fatalf("%v: the confirmation is closed:\n%s", sz, h.Screen())
		}
		if tr := top(h); tr.Kind != ui.RouteHome {
			t.Fatalf("%v: home after leave: %+v", sz, tr)
		}
	}

	srv := newAPI(t)
	srv.Error("DELETE /v1/spaces/"+fixtures.SpaceNova+"/members/@me", http.StatusInternalServerError, "internal")
	h := openWith(t, uitest.Size{W: 120, H: 30}, ui.Route{Kind: ui.RouteHome}, srv, uitest.WithSeed(member))
	h.Send(ui.RunCommandMsg{ID: "space.leave"})
	h.Press("y")
	if h.Context().Store.Spaces[fixtures.SpaceNova] == nil {
		t.Fatal("a failed leave keeps the space")
	}
	mustContain(t, toastText(t, h), "방에서 나가지 못했어요")
	if overlays(h) != 0 {
		t.Fatalf("the confirmation is closed after a failure:\n%s", h.Screen())
	}
}
