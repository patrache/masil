package feed

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// hostileProfileHandle is the 32-char hostile fixture handle (fixtures.Hostile).
const hostileProfileHandle = "hostile_abcdefghijklmnopqrstuvwx"

// profileUsers is every seeded user by handle (+ the hostile set).
func profileUsers(hostile bool) map[string]api.UserSummary {
	out := map[string]api.UserSummary{}
	for _, u := range seedStore(hostile).Users {
		out[u.Handle] = *u
	}
	return out
}

// profileFor is profileOf for a fixture handle; any other (hostile) user gets
// a long control-laden bio, huge counts and dnd presence.
func profileFor(users map[string]api.UserSummary, handle string) api.Profile {
	if _, ok := fixtureUsers()[handle]; ok {
		return profileOf(handle, relFollowing())
	}
	rel := relFollowing()
	rel.IsFriend = true
	return api.Profile{
		UserSummary:    users[handle],
		Bio:            "소개\x1b[31m빨강\x1b[0m\t탭\r\u0085 " + strings.Repeat("공백없는아주긴소개문장", 20) + "\n둘째 줄 ❄️ 👨‍👩‍👧",
		CreatedAt:      fixtureNow,
		FollowerCount:  123456789,
		FollowingCount: 98765,
		PostCount:      4321,
		Presence:       ptr(string(api.PresenceDND)),
		Relationship:   &rel,
	}
}

// profileSrv is snsAPI with GET /v1/users/{user} (+ /posts) answering every
// seeded user (and the hostile set), the profile passed through mut.
func profileSrv(t *testing.T, hostile bool, mut func(*api.Profile)) *apitest.Server {
	t.Helper()
	srv := snsAPI(t, hostile)
	users := profileUsers(hostile)
	resolve := func(ref string) (string, bool) {
		switch {
		case ref == "@me":
			return "sam", true
		case strings.HasPrefix(ref, "@"):
			_, ok := users[ref[1:]]
			return ref[1:], ok
		}
		for h, u := range users {
			if u.ID == ref {
				return h, true
			}
		}
		return "", false
	}
	notFound := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"not_found","message":"not_found","details":{}}}`))
	}
	srv.Handle("GET /v1/users/{user}", func(w http.ResponseWriter, r *http.Request) {
		h, ok := resolve(r.PathValue("user"))
		if !ok {
			notFound(w)
			return
		}
		p := profileFor(users, h)
		if mut != nil {
			mut(&p)
		}
		writeJSON(w, map[string]any{"user": p})
	})
	srv.Handle("GET /v1/users/{user}/posts", func(w http.ResponseWriter, r *http.Request) {
		h, ok := resolve(r.PathValue("user"))
		if !ok {
			notFound(w)
			return
		}
		filter := r.URL.Query().Get("filter")
		if filter == "" {
			filter = "posts"
		}
		if _, known := fixtureUsers()[h]; known {
			writeJSON(w, pageOf(profilePosts(h, filter)))
			return
		}
		var items []api.TimelineItem
		for _, it := range fixtures.HostileTimeline() {
			if it.Post.Author.Handle == h && filter == "posts" {
				items = append(items, it)
			}
		}
		writeJSON(w, pageOf(items))
	})
	return srv
}

// profileOptsWith opens RouteProfile id on srv.
func profileOptsWith(srv *apitest.Server, id string, hostile bool) []uitest.Option {
	seed := fixtures.Seed
	if hostile {
		seed = func(s *store.Store) { fixtures.Seed(s); fixtures.Hostile(s) }
	}
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(seed), uitest.WithRoute(ui.Route{Kind: ui.RouteProfile, ID: id})}
}

func profileOpts(t *testing.T, id string, hostile bool, mut func(*api.Profile)) []uitest.Option {
	t.Helper()
	return profileOptsWith(profileSrv(t, hostile, mut), id, hostile)
}

func notFollowing(p *api.Profile) {
	if p.Relationship != nil {
		p.Relationship.Following = "none"
	}
}

func privateLocked(p *api.Profile) {
	p.IsPrivate = true
	p.Relationship.CanViewPosts = false
	p.Relationship.Following = "none"
}

func TestProfileGolden(t *testing.T) {
	// testdata/TestProfileGolden/<case>/<WxH>/<case>.golden
	t.Run("hana", func(t *testing.T) {
		sweep(t, uitest.AllSizes(), "hana", profileOpts(t, "@hana", false, nil), nil)
	})
	t.Run("private", func(t *testing.T) {
		sweep(t, uitest.Required, "private", profileOpts(t, "@hana", false, privateLocked), nil)
	})
	t.Run("me", func(t *testing.T) {
		sweep(t, uitest.Required, "me", profileOpts(t, "@me", false, nil), nil)
	})
}

func TestProfileHostile(t *testing.T) {
	sweep(t, uitest.AllSizes(), "", profileOpts(t, "@"+hostileProfileHandle, true, nil), nil)
}

func TestProfileShowsHeader(t *testing.T) {
	h := uitest.New(t, profileOpts(t, "@hana", false, nil)...)
	h.Resize(120, 40)
	s := h.Screen()
	for _, want := range []string{"하나", "@hana", "● 온라인", "🎧 듣는 중", "눈 오는 날 좋아함", "팔로워 120 · 팔로잉 80 · 글 42", "[팔로잉 ✓]", "나를 팔로우해요", "오늘 첫 눈"} {
		if !strings.Contains(s, want) {
			t.Fatalf("profile lacks %q:\n%s", want, s)
		}
	}
	for _, tab := range []string{"글", "답글", "미디어", "방명록"} {
		if !strings.Contains(s, " "+tab+" ") {
			t.Fatalf("profile lacks tab %q:\n%s", tab, s)
		}
	}
}

func TestProfilePrivateLocked(t *testing.T) {
	srv := profileSrv(t, false, privateLocked)
	h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	s := h.Screen()
	if !strings.Contains(s, "비공개 계정이에요") || !strings.Contains(s, "[팔로우]") {
		t.Fatalf("private profile:\n%s", s)
	}
	if _, ok := lastReq(h, "GET", "/v1/users/"+userOf("hana").ID+"/posts"); ok {
		t.Fatal("a locked private profile requested its posts")
	}
}

// suspendedSeen is a suspended account as the server shows it to others (D1,
// api.md 3.9) — except the stale bio/status/avatar a cache may still hold,
// which the screen must not draw either.
func suspendedSeen(p *api.Profile) {
	p.Suspended = true
	p.FollowerCount, p.FollowingCount, p.PostCount = 0, 0, 0
	p.Presence, p.LastSeenAt = nil, nil
	p.Relationship.CanViewPosts = false
	p.Relationship.CanDM = false
}

func TestProfileSuspendedHeader(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		srv := profileSrv(t, false, suspendedSeen)
		h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
		h.Resize(size[0], size[1])
		s := h.Screen()
		for _, want := range []string{"하나", "@hana", textSuspendedTitle} {
			if !strings.Contains(s, want) {
				t.Fatalf("%v: suspended profile lacks %q:\n%s", size, want, s)
			}
		}
		// the header says it too (in place of the counts), besides the locked state below
		if n := strings.Count(s, "정지된 계정"); n < 2 {
			t.Fatalf("%v: header lacks the 정지된 계정 label (%d occurrences):\n%s", size, n, s)
		}
		for _, bad := range []string{"팔로워 0", "팔로워", "🎧 듣는 중", "눈 오는 날 좋아함", "[팔로우]", "[팔로잉 ✓]"} {
			if strings.Contains(s, bad) {
				t.Fatalf("%v: suspended profile shows %q:\n%s", size, bad, s)
			}
		}
		if _, ok := lastReq(h, "GET", "/v1/users/"+userOf("hana").ID+"/posts"); ok {
			t.Fatalf("%v: a suspended profile requested its posts", size)
		}
	}
}

func TestProfileFollowToggle(t *testing.T) {
	srv := profileSrv(t, false, notFollowing)
	srv.JSON("PUT /v1/users/{user}/follow", http.StatusOK, map[string]string{"state": "pending"})
	h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	if s := h.Screen(); !strings.Contains(s, "[팔로우]") {
		t.Fatalf("not-following chip:\n%s", s)
	}
	h.Press("+")
	if _, ok := lastReq(h, "PUT", "/v1/users/"+userOf("hana").ID+"/follow"); !ok {
		t.Fatal("+ sent no PUT /v1/users/<hana>/follow")
	}
	if s := h.Screen(); !strings.Contains(s, "[요청됨]") {
		t.Fatalf("pending chip not shown:\n%s", s)
	}
}

func TestProfileFollowActiveCounts(t *testing.T) {
	srv := profileSrv(t, false, notFollowing)
	srv.JSON("PUT /v1/users/{user}/follow", http.StatusOK, map[string]string{"state": "active"})
	h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	h.Press("+")
	if s := h.Screen(); !strings.Contains(s, "[팔로잉 ✓]") || !strings.Contains(s, "팔로워 121") {
		t.Fatalf("active follow not shown:\n%s", s)
	}
}

func TestProfileUnfollowConfirms(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.JSON("DELETE /v1/users/{user}/follow", http.StatusNoContent, nil)
	h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	h.Press("+")
	if s := overlayText(h); !strings.Contains(s, "팔로우를 끊을까요?") {
		t.Fatalf("no unfollow confirm:\n%s", s)
	}
	if _, ok := lastReq(h, "DELETE", "/v1/users/"+userOf("hana").ID+"/follow"); ok {
		t.Fatal("unfollowed before the confirm")
	}
	h.Press("y")
	if _, ok := lastReq(h, "DELETE", "/v1/users/"+userOf("hana").ID+"/follow"); !ok {
		t.Fatal("y sent no DELETE /v1/users/<hana>/follow")
	}
	if s := h.Screen(); !strings.Contains(s, "[팔로우]") || !strings.Contains(s, "팔로워 119") {
		t.Fatalf("unfollowed state not shown:\n%s", s)
	}
}

func TestProfileTabsLoadFilters(t *testing.T) {
	h := uitest.New(t, profileOpts(t, "@hana", false, nil)...)
	h.Resize(120, 40)
	path := "/v1/users/" + userOf("hana").ID + "/posts"
	if r, ok := lastReq(h, "GET", path); !ok || !strings.Contains(r.Query, "filter=posts") {
		t.Fatalf("first tab request = %+v (found %v), want filter=posts", r, ok)
	}
	h.Press("2")
	if r, ok := lastReq(h, "GET", path); !ok || !strings.Contains(r.Query, "filter=replies") {
		t.Fatalf("2: last posts request = %+v, want filter=replies", r)
	}
	if s := h.Screen(); !strings.Contains(s, "아직 답글이 없어요") {
		t.Fatalf("empty replies tab:\n%s", s)
	}
	h.Press("3")
	if r, ok := lastReq(h, "GET", path); !ok || !strings.Contains(r.Query, "filter=media") {
		t.Fatalf("3: last posts request = %+v, want filter=media", r)
	}
	n := len(reqsTo(h, "GET", path))
	h.Press("1")
	if got := len(reqsTo(h, "GET", path)); got != n {
		t.Fatalf("1: an already loaded tab refetched (%d -> %d requests)", n, got)
	}
	if s := h.Screen(); !strings.Contains(s, "오늘 첫 눈") {
		t.Fatalf("posts tab:\n%s", s)
	}
}

func TestProfileBlockAndReport(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.Handle("PUT /v1/blocks/{user}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	hana := userOf("hana").ID
	menuPick(t, h, "차단")
	if s := overlayText(h); !strings.Contains(s, "차단할까요?") {
		t.Fatalf("no block confirm:\n%s", s)
	}
	h.Press("y")
	if _, ok := lastReq(h, "PUT", "/v1/blocks/"+hana); !ok {
		t.Fatal("no PUT /v1/blocks/<hana>")
	}
	s := h.Screen()
	if !strings.Contains(s, "차단한 사용자예요") || strings.Contains(s, "오늘 첫 눈") {
		t.Fatalf("blocked profile:\n%s", s)
	}
	menuPick(t, h, "사용자 신고")
	if s := h.Screen(); !strings.Contains(s, "신고") || !strings.Contains(s, "스팸") {
		t.Fatalf("no report dialog:\n%s", s)
	}
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want the report dialog only", n)
	}
}

func TestProfileMenuItems(t *testing.T) {
	h := uitest.New(t, profileOpts(t, "@hana", false, nil)...)
	h.Resize(120, 40)
	h.Press("m")
	s := overlayText(h)
	for _, w := range []string{"스토리 보기", "팔로워", "팔로잉", "차단", "사용자 신고"} {
		if !strings.Contains(s, w) {
			t.Fatalf("menu lacks %q:\n%s", w, s)
		}
	}
	if strings.Contains(s, "프로필 편집") {
		t.Fatalf("someone else's profile offers 프로필 편집:\n%s", s)
	}
	h.Press("esc")

	h = uitest.New(t, profileOpts(t, "@me", false, nil)...)
	h.Resize(120, 40)
	h.Press("m")
	s = overlayText(h)
	if !strings.Contains(s, "프로필 편집") || !strings.Contains(s, "팔로우 요청") || strings.Contains(s, "차단") || strings.Contains(s, "사용자 신고") {
		t.Fatalf("my profile menu:\n%s", s)
	}
}

func TestProfileMenuFollowers(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.JSON("GET /v1/users/{user}/followers", http.StatusOK, pageOf([]api.UserSummary{userOf("minji")}))
	h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	menuPick(t, h, "팔로워")
	if _, ok := lastReq(h, "GET", "/v1/users/"+userOf("hana").ID+"/followers"); !ok {
		t.Fatal("no GET followers")
	}
	if s := overlayText(h); !strings.Contains(s, "@minji") {
		t.Fatalf("followers list:\n%s", s)
	}
}

// mePatched is the seeded Me with fn applied (the PATCH /v1/me/profile answer).
func mePatched(fn func(*api.Me)) api.Me {
	me := *seedStore(false).Me
	fn(&me)
	return me
}

func TestProfileEditSave(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.JSON("PATCH /v1/me/profile", http.StatusOK, map[string]any{"user": mePatched(func(m *api.Me) { m.DisplayName = "새 이름" })})
	h := uitest.New(t, profileOptsWith(srv, "@me", false)...)
	h.Resize(120, 40)
	h.Press("e")
	if s := overlayText(h); !strings.Contains(s, "이름") || !strings.Contains(s, "소개") {
		t.Fatalf("no profile edit dialog:\n%s", s)
	}
	h.Press("backspace", "backspace", "backspace")
	h.Type("새 이름")
	h.Press("enter")
	r, ok := lastReq(h, "PATCH", "/v1/me/profile")
	if !ok {
		t.Fatal("enter sent no PATCH /v1/me/profile")
	}
	body := postBody(t, r)
	if body["display_name"] != "새 이름" {
		t.Fatalf("PATCH body %s: display_name", r.Body)
	}
	for _, k := range []string{"avatar_upload_id", "bio"} {
		if _, has := body[k]; has {
			t.Fatalf("PATCH body %s has unchanged %q", r.Body, k)
		}
	}
	if got := h.Context().Store.Me.DisplayName; got != "새 이름" {
		t.Fatalf("Store.Me.DisplayName = %q", got)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("edit dialog still open (%d)", n)
	}
	if !strings.Contains(toastText(h), squash("프로필을 저장했어요")) {
		t.Fatalf("no saved toast:\n%s", h.Screen())
	}
	if s := h.Screen(); !strings.Contains(s, "새 이름") {
		t.Fatalf("profile header not updated:\n%s", s)
	}
}

func TestProfileEditEmptyName(t *testing.T) {
	h := uitest.New(t, profileOpts(t, "@me", false, nil)...)
	h.Resize(120, 40)
	h.Press("e", "backspace", "backspace", "backspace", "enter")
	if !strings.Contains(toastText(h), squash("이름을 입력해 주세요")) {
		t.Fatalf("no empty-name toast:\n%s", h.Screen())
	}
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want the dialog kept", n)
	}
	for _, r := range h.API.Requests() {
		if r.Method == "PATCH" {
			t.Fatalf("sent %s %s", r.Method, r.Path)
		}
	}
}

func TestProfileEditClearAvatar(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.JSON("PATCH /v1/me/profile", http.StatusOK, map[string]any{"user": mePatched(func(m *api.Me) { m.AvatarURL = nil })})
	h := uitest.New(t, profileOptsWith(srv, "@me", false)...)
	h.Resize(120, 40)
	h.Press("e", "ctrl+x", "enter")
	r, ok := lastReq(h, "PATCH", "/v1/me/profile")
	if !ok {
		t.Fatal("no PATCH /v1/me/profile")
	}
	body := postBody(t, r)
	if v, has := body["avatar_upload_id"]; !has || v != nil {
		t.Fatalf("PATCH body %s, want avatar_upload_id null", r.Body)
	}
	if _, has := body["display_name"]; has {
		t.Fatalf("PATCH body %s has the unchanged display_name", r.Body)
	}
}

func TestProfileEditAvatarUpload(t *testing.T) {
	srv := profileSrv(t, false, nil)
	composeHandlers(srv)
	srv.JSON("PATCH /v1/me/profile", http.StatusOK, map[string]any{"user": mePatched(func(m *api.Me) {
		m.AvatarURL = ptr("/v1/media/" + fmt.Sprintf("01JDUPL%019d", 1))
	})})
	h := uitest.New(t, profileOptsWith(srv, "@me", false)...)
	h.Resize(120, 40)
	path := writePNG(t)
	h.Press("e", "tab", "tab")
	h.Type(path)
	h.Press("enter")
	var upIdx, patchIdx = -1, -1
	for i, r := range h.API.Requests() {
		switch {
		case r.Method == "POST" && r.Path == "/v1/uploads":
			upIdx = i
			if !strings.Contains(r.Query, "purpose=avatar") {
				t.Fatalf("upload query %q, want purpose=avatar", r.Query)
			}
		case r.Method == "PATCH" && r.Path == "/v1/me/profile":
			patchIdx = i
			if body := postBody(t, r); body["avatar_upload_id"] != fmt.Sprintf("01JDUPL%019d", 1) {
				t.Fatalf("PATCH body %s, want the upload id", r.Body)
			}
		}
	}
	if upIdx < 0 || patchIdx < 0 || upIdx > patchIdx {
		t.Fatalf("upload #%d, PATCH #%d: want the upload first, then the PATCH", upIdx, patchIdx)
	}
	if me := h.Context().Store.Me; me.AvatarURL == nil || !strings.HasSuffix(*me.AvatarURL, fmt.Sprintf("01JDUPL%019d", 1)) {
		t.Fatalf("Store.Me.AvatarURL = %v", me.AvatarURL)
	}
}

func TestFollowRequestsAcceptDecline(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.JSON("POST /v1/me/follow-requests/{user}/accept", http.StatusNoContent, nil)
	srv.JSON("POST /v1/me/follow-requests/{user}/decline", http.StatusNoContent, nil)
	h := uitest.New(t, profileOptsWith(srv, "@me", false)...)
	h.Resize(120, 40)
	h.Send(ui.OpenOverlayMsg{ID: "feed.follow_requests"})
	if _, ok := lastReq(h, "GET", "/v1/me/follow-requests"); !ok {
		t.Fatal("no GET /v1/me/follow-requests")
	}
	if s := overlayText(h); !strings.Contains(s, "@minji") || !strings.Contains(s, "@junho") {
		t.Fatalf("follow requests:\n%s", s)
	}
	h.Press("a")
	if _, ok := lastReq(h, "POST", "/v1/me/follow-requests/"+userOf("minji").ID+"/accept"); !ok {
		t.Fatal("a sent no accept")
	}
	if s := overlayText(h); strings.Contains(s, "@minji") || !strings.Contains(s, "@junho") {
		t.Fatalf("minji row not removed:\n%s", s)
	}
	h.Press("d")
	if _, ok := lastReq(h, "POST", "/v1/me/follow-requests/"+userOf("junho").ID+"/decline"); !ok {
		t.Fatal("d sent no decline")
	}
	if s := overlayText(h); strings.Contains(s, "@junho") {
		t.Fatalf("junho row not removed:\n%s", s)
	}
}

func TestProfileOpenStories(t *testing.T) {
	h := uitest.New(t, profileOpts(t, "@dev_kim", false, nil)...)
	h.Resize(120, 40)
	h.Press("s")
	want := ui.Route{Kind: ui.RouteStories, ID: userOf("dev_kim").ID}
	if top := feedTop(h); top.Kind != want.Kind || top.ID != want.ID {
		t.Fatalf("s: feed top = %+v, want %+v", top, want)
	}

	h = uitest.New(t, profileOpts(t, "@hana", false, privateLocked)...)
	h.Resize(120, 40)
	n := len(h.API.Requests())
	h.Press("s")
	if top := feedTop(h); top.Kind != ui.RouteProfile {
		t.Fatalf("s on a locked private profile: feed top = %+v", top)
	}
	if got := len(h.API.Requests()); got != n {
		t.Fatalf("s on a locked private profile sent %d requests", got-n)
	}
}

func TestProfileStoryRingMark(t *testing.T) {
	h := uitest.New(t, profileOpts(t, "@hana", false, nil)...)
	h.Resize(120, 40)
	if s := h.Screen(); !strings.Contains(s, "◉ 하나") {
		t.Fatalf("no unseen story mark before the name:\n%s", s)
	}
}

func TestProfileTabFromQuery(t *testing.T) {
	srv := profileSrv(t, false, nil)
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteProfile, ID: "@hana", Query: map[string]string{"tab": "guestbook"}}))
	p, ok := h.Instance(ui.Route{Kind: ui.RouteProfile, ID: "@hana"}.Key()).(*profile)
	if !ok {
		t.Fatal("no profile instance")
	}
	if p.tab != tabGuestbook {
		t.Fatalf("tab = %d, want the guestbook tab", p.tab)
	}
}

func TestProfileOverlaysGolden(t *testing.T) {
	// One "WxH" subtest per size holding both goldens
	// (testdata/TestProfileOverlaysGolden/<WxH>/{follow_requests,profile_edit}.golden).
	sizes := append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6})
	opts := profileOpts(t, "@me", false, nil)
	for _, s := range sizes {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			h := uitest.New(t, opts...)
			h.Resize(s.W, s.H)
			h.Send(ui.OpenOverlayMsg{ID: "feed.follow_requests"})
			h.Golden("follow_requests")

			h = uitest.New(t, opts...)
			h.Resize(s.W, s.H)
			h.Send(ui.OpenOverlayMsg{ID: "feed.profile_edit"})
			h.Golden("profile_edit")
		})
	}
}

func TestUserRef(t *testing.T) {
	for in, want := range map[string]api.UserRef{
		"@me":                        api.MeRef,
		"@Hana":                      api.Handle("hana"),
		"01JDUSER000000000000000004": api.UserID("01JDUSER000000000000000004"),
	} {
		if got := userRef(in); got != want {
			t.Fatalf("userRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProfileTabHidesBlocked(t *testing.T) {
	// Review Focus 3: dev_kim's 글 tab holds a repost of hana's post; blocking
	// hana drops it from this per-screen list at once.
	h := uitest.New(t, profileOpts(t, "@dev_kim", false, nil)...)
	h.Resize(120, 40)
	if s := h.Screen(); !strings.Contains(s, "오늘 첫 눈") {
		t.Fatalf("repost of hana's post not shown:\n%s", s)
	}
	h.Event("block.updated", map[string]any{"user_id": userOf("hana").ID, "blocked": true})
	if s := h.Screen(); strings.Contains(s, "오늘 첫 눈") || !strings.Contains(s, "@dev_kim") {
		t.Fatalf("blocked hana's post still listed:\n%s", s)
	}
}
