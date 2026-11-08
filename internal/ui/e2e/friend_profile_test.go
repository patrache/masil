package e2e

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// pickMenu opens the focused screen's menu (m) and runs the item label.
func pickMenu(t *testing.T, h *uitest.Harness, label string) {
	t.Helper()
	h.Press("m")
	for range 12 {
		if strings.Contains(h.Screen(), "› "+label) {
			h.Press("enter")
			return
		}
		h.Press("j")
	}
	t.Fatalf("menu item %q not reachable:\n%s", label, h.Screen())
}

// C2 (v0.1.1): a friend request sent from the profile menu shows on the cached
// friends screen's 보낸 요청 tab, as one sent from friend.add does.
func TestProfileFriendRequestReachesFriendsScreen(t *testing.T) {
	kim := api.UserSummary{ID: fixtures.UserDevKim, Handle: "dev_kim", DisplayName: "김개발"}
	sam := api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "sam"}
	srv := productAPI(t)
	var (
		mu  sync.Mutex
		out = []api.FriendRequest{}
	)
	srv.Handle("GET /v1/friends/requests", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Query().Get("direction") == "outgoing" {
			writeJSON(w, map[string]any{"items": out})
			return
		}
		writeJSON(w, map[string]any{"items": []api.FriendRequest{}})
	})
	srv.Handle("POST /v1/friends/requests", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		req := api.FriendRequest{ID: "01JFREQ0000000000000000K01", From: sam, To: kim, Status: "pending", CreatedAt: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)}
		out = append(out, req)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"request": req, "friend": nil})
	})
	srv.Handle("DELETE /v1/friends/requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		out = slices.DeleteFunc(out, func(q api.FriendRequest) bool { return q.ID == r.PathValue("id") })
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(productSeed))
	h.Resize(120, 40)

	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFriends, Anchor: ui.FriendsList}})
	h.Press("l", "l") // 보낸 요청
	if s := h.Screen(); !strings.Contains(s, "보낸 요청 0") {
		t.Fatalf("friends screen before:\n%s", s)
	}

	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteProfile, ID: "@dev_kim"}})
	pickMenu(t, h, "친구 요청")
	if !strings.Contains(h.Screen(), "님에게 친구 요청을 보냈어요") {
		t.Fatalf("no sent toast:\n%s", h.Screen())
	}

	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFriends}})
	if s := h.Screen(); !strings.Contains(s, "보낸 요청 1") || !strings.Contains(s, "김개발") {
		t.Fatalf("the cached friends screen did not refetch its sent requests:\n%s", s)
	}

	// and a cancel from the profile empties it again
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteProfile, ID: "@dev_kim"}})
	pickMenu(t, h, "친구 요청 취소")
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFriends}})
	if s := h.Screen(); !strings.Contains(s, "보낸 요청 0") {
		t.Fatalf("the cached friends screen kept the canceled request:\n%s", s)
	}
}

// C2 (v0.1.1): accepting a request from the profile menu makes the friend and
// drops the received request in the Store at once: the home 친구 요청 N row goes
// without waiting for friend.added.
func TestProfileFriendAcceptClearsHomeRequestRow(t *testing.T) {
	const reqID = "01JFREQ0000000000000000K02"
	kim := api.UserSummary{ID: fixtures.UserDevKim, Handle: "dev_kim", DisplayName: "김개발"}
	sam := api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "sam"}
	in := api.FriendRequest{ID: reqID, From: kim, To: sam, Status: "pending", CreatedAt: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)}
	srv := productAPI(t)
	srv.Handle("GET /v1/users/{user}", func(w http.ResponseWriter, r *http.Request) {
		p := profileOf(kim)
		p.Relationship.FriendRequest = &api.FriendRequestRef{ID: reqID, Direction: "incoming"}
		writeJSON(w, map[string]any{"user": p})
	})
	srv.Handle("POST /v1/friends/requests/{id}/accept", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"friend": api.Friend{User: kim, Since: in.CreatedAt, Presence: api.PresenceAway}})
	})
	seed := func(s *store.Store) {
		productSeed(s)
		s.FriendReqIn = []api.FriendRequest{in}
	}
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(seed))
	h.Resize(120, 40)
	if s := h.Screen(); !strings.Contains(s, "친구 요청 1") {
		t.Fatalf("home before:\n%s", s)
	}

	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteProfile, ID: "@dev_kim"}})
	pickMenu(t, h, "친구 요청 수락")
	st := h.Context().Store
	if _, ok := st.Friends[fixtures.UserDevKim]; !ok || len(st.FriendReqIn) != 0 {
		t.Fatalf("store after accept: friend=%v requests=%d", ok, len(st.FriendReqIn))
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteHome}})
	if s := h.Screen(); strings.Contains(s, "친구 요청 1") {
		t.Fatalf("home still shows the accepted request:\n%s", s)
	}
}
