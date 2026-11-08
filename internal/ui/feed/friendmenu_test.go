package feed

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// relWith sets hana's friendship: friend, or a pending request in dir
// ("incoming" | "outgoing"), or neither ("").
func relWith(friend bool, dir string) func(*api.Profile) {
	return func(p *api.Profile) {
		if p.Relationship == nil {
			return
		}
		p.Relationship.IsFriend = friend
		p.Relationship.FriendRequest = nil
		if dir != "" {
			p.Relationship.FriendRequest = &api.FriendRequestRef{ID: "01JFREQ0000000000000000077", Direction: dir}
		}
	}
}

// menuLabels are the item labels of the profile menu ("m"); the menu is closed again.
func menuLabels(h *uitest.Harness) []string {
	h.Press("m")
	s := overlayText(h)
	h.Press("esc")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.Trim(l, " │║╭╮╰╯╔╗╚╝─═")
		l = strings.TrimSpace(strings.TrimPrefix(l, "›"))
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// C2 (v0.1.1): the profile menu offers the friend action of the relationship —
// 친구 요청 (none), 친구 요청 취소 (sent), 친구 요청 수락 / 거절 (received), 친구
// 끊기 (friends) — over the 5.3 endpoints, and follows the answer at once.
func TestProfileFriendMenu(t *testing.T) {
	const reqID = "01JFREQ0000000000000000077"
	hana := userOf("hana")
	friendItems := []string{"친구 요청", "친구 요청 취소", "친구 요청 수락", "친구 요청 거절", "친구 끊기"}
	only := func(t *testing.T, h *uitest.Harness, want ...string) {
		t.Helper()
		got := menuLabels(h)
		for _, it := range friendItems {
			if slices.Contains(got, it) != slices.Contains(want, it) {
				t.Fatalf("menu item %q present=%v, want the friend items %v; menu %q", it, slices.Contains(got, it), want, got)
			}
		}
	}

	t.Run("none sends a request", func(t *testing.T) {
		srv := profileSrv(t, false, relWith(false, ""))
		srv.JSON("POST /v1/friends/requests", http.StatusCreated, map[string]any{
			"request": api.FriendRequest{ID: reqID, From: userOf("sam"), To: hana, Status: "pending", CreatedAt: fixtureNow}, "friend": nil})
		h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
		h.Resize(120, 40)
		only(t, h, "친구 요청")
		menuPick(t, h, "친구 요청")
		r, ok := lastReq(h, "POST", "/v1/friends/requests")
		if !ok || !strings.Contains(string(r.Body), `"user":"`) {
			t.Fatalf("no POST /v1/friends/requests (%+v)", r)
		}
		only(t, h, "친구 요청 취소")
	})

	t.Run("none auto-accepted", func(t *testing.T) {
		srv := profileSrv(t, false, relWith(false, ""))
		srv.JSON("POST /v1/friends/requests", http.StatusOK, map[string]any{
			"request": api.FriendRequest{ID: reqID, From: hana, To: userOf("sam"), Status: "accepted", CreatedAt: fixtureNow},
			"friend":  api.Friend{User: hana, Since: fixtureNow, Presence: api.PresenceOnline}})
		h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
		h.Resize(120, 40)
		menuPick(t, h, "친구 요청")
		only(t, h, "친구 끊기")
	})

	t.Run("outgoing cancels", func(t *testing.T) {
		srv := profileSrv(t, false, relWith(false, "outgoing"))
		srv.JSON("DELETE /v1/friends/requests/{id}", http.StatusNoContent, nil)
		h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
		h.Resize(120, 40)
		only(t, h, "친구 요청 취소")
		menuPick(t, h, "친구 요청 취소")
		if _, ok := lastReq(h, "DELETE", "/v1/friends/requests/"+reqID); !ok {
			t.Fatal("no DELETE /v1/friends/requests/<id>")
		}
		only(t, h, "친구 요청")
	})

	t.Run("incoming accepts", func(t *testing.T) {
		srv := profileSrv(t, false, relWith(false, "incoming"))
		srv.JSON("POST /v1/friends/requests/{id}/accept", http.StatusOK, map[string]any{"friend": api.Friend{User: hana, Since: fixtureNow}})
		h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
		h.Resize(120, 40)
		only(t, h, "친구 요청 수락", "친구 요청 거절")
		menuPick(t, h, "친구 요청 수락")
		if _, ok := lastReq(h, "POST", "/v1/friends/requests/"+reqID+"/accept"); !ok {
			t.Fatal("no POST .../accept")
		}
		only(t, h, "친구 끊기")
	})

	t.Run("incoming declines", func(t *testing.T) {
		srv := profileSrv(t, false, relWith(false, "incoming"))
		srv.JSON("POST /v1/friends/requests/{id}/decline", http.StatusNoContent, nil)
		h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
		h.Resize(120, 40)
		menuPick(t, h, "친구 요청 거절")
		if _, ok := lastReq(h, "POST", "/v1/friends/requests/"+reqID+"/decline"); !ok {
			t.Fatal("no POST .../decline")
		}
		only(t, h, "친구 요청")
	})

	t.Run("friend unfriends after a confirm", func(t *testing.T) {
		srv := profileSrv(t, false, relWith(true, ""))
		srv.JSON("DELETE /v1/friends/{user}", http.StatusNoContent, nil)
		h := uitest.New(t, profileOptsWith(srv, "@hana", false)...)
		h.Resize(120, 40)
		only(t, h, "친구 끊기")
		menuPick(t, h, "친구 끊기")
		if s := overlayText(h); !strings.Contains(s, "하나님과 친구를 끊을까요?") {
			t.Fatalf("no unfriend confirm:\n%s", s)
		}
		if len(reqsTo(h, "DELETE", "/v1/friends/"+hana.ID)) != 0 {
			t.Fatal("unfriended before the confirm")
		}
		h.Press("y")
		if _, ok := lastReq(h, "DELETE", "/v1/friends/"+hana.ID); !ok {
			t.Fatalf("y sent no DELETE /v1/friends/<hana>: %+v", h.API.Requests())
		}
		only(t, h, "친구 요청")
	})

	t.Run("live request events", func(t *testing.T) {
		h := uitest.New(t, profileOpts(t, "@hana", false, relWith(false, ""))...)
		h.Resize(120, 40)
		h.Event("friend.request_received", map[string]any{"request": api.FriendRequest{ID: reqID, From: hana, To: userOf("sam"), Status: "pending", CreatedAt: fixtureNow}})
		only(t, h, "친구 요청 수락", "친구 요청 거절")
		h.Event("friend.request_removed", map[string]any{"request_id": reqID, "reason": "canceled"})
		only(t, h, "친구 요청")
	})

	t.Run("not on my profile or a blocked user", func(t *testing.T) {
		h := uitest.New(t, profileOpts(t, "@me", false, nil)...)
		h.Resize(120, 40)
		only(t, h)
		h = uitest.New(t, profileOpts(t, "@hana", false, func(p *api.Profile) { p.Relationship.Blocked = true; p.Relationship.IsFriend = false })...)
		h.Resize(120, 40)
		only(t, h)
	})
}

// C4 (v0.1.1): the "+" help names what it does now — 팔로우 (none), 요청 취소
// (pending), 언팔로우 (following) — and follows the answer.
func TestProfileFollowHelpFollowsState(t *testing.T) {
	plus := func(t *testing.T, h *uitest.Harness) string {
		t.Helper()
		h.Screen()
		p, ok := h.Instance(ui.Route{Kind: ui.RouteProfile, ID: "@hana"}.Key()).(*profile)
		if !ok {
			t.Fatal("no profile instance")
		}
		for _, b := range p.Help(h.Context()) {
			if b.Help().Key == "+" {
				return b.Help().Desc
			}
		}
		return ""
	}
	h := uitest.New(t, profileOpts(t, "@hana", false, nil)...) // following: active
	h.Resize(120, 40)
	if got := plus(t, h); got != "언팔로우" {
		t.Fatalf("following: + help %q, want 언팔로우", got)
	}

	srv := profileSrv(t, false, notFollowing)
	srv.JSON("PUT /v1/users/{user}/follow", http.StatusOK, map[string]string{"state": "pending"})
	h = uitest.New(t, profileOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	if got := plus(t, h); got != "팔로우" {
		t.Fatalf("not following: + help %q, want 팔로우", got)
	}
	h.Press("+")
	if got := plus(t, h); got != "요청 취소" {
		t.Fatalf("pending: + help %q, want 요청 취소", got)
	}
	if s := h.Screen(); !strings.Contains(s, "+ 요청 취소") {
		t.Fatalf("help line:\n%s", s)
	}
}

// C4 (v0.1.1): no "+" help where "+" does nothing — a user I blocked (the
// relationship still says Following=none) and a profile still loading.
func TestProfileFollowHelpHiddenWhenPlusDoesNothing(t *testing.T) {
	hasPlus := func(ctx *ui.Context, p ui.Component) bool {
		for _, b := range p.(ui.Helper).Help(ctx) {
			if b.Help().Key == "+" {
				return true
			}
		}
		return false
	}
	h := uitest.New(t, profileOpts(t, "@hana", false, func(p *api.Profile) {
		p.Relationship.Blocked, p.Relationship.IsFriend, p.Relationship.Following = true, false, "none"
	})...)
	h.Resize(120, 40)
	s := h.Screen()
	p := h.Instance(ui.Route{Kind: ui.RouteProfile, ID: "@hana"}.Key())
	if hasPlus(h.Context(), p) || strings.Contains(s, "+ 팔로우") {
		t.Fatalf("blocked: + help shown:\n%s", s)
	}
	h.Press("+")
	for _, r := range h.API.Requests() {
		if strings.HasSuffix(r.Path, "/follow") {
			t.Fatalf("blocked: + sent %s %s", r.Method, r.Path)
		}
	}

	loading := newProfile(h.Context(), ui.Route{Kind: ui.RouteProfile, ID: "@hana"})
	if hasPlus(h.Context(), loading) {
		t.Fatal("loading: + help shown before the profile arrives")
	}
}
