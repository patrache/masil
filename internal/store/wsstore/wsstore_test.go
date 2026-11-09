package wsstore_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/wsstore"
)

var base = time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC)

var (
	me   = api.UserSummary{ID: "U_ME", Handle: "sam", DisplayName: "샘"}
	hana = api.UserSummary{ID: "U_HANA", Handle: "hana", DisplayName: "하나"}
)

func ev(t *testing.T, typ string, data any) realtime.Event {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return realtime.Event{Type: typ, Data: raw, TS: base}
}

func newState(t *testing.T) (*store.Store, *wsstore.State) {
	t.Helper()
	s := store.New()
	s.Me = &api.Me{UserSummary: me}
	st := wsstore.Get(s)
	if st == nil {
		t.Fatal("workspace reducer not registered")
	}
	return s, st
}

// share is a live share owned by owner with one member per user, created min minutes after base.
func share(id string, owner api.UserSummary, min int, members ...api.UserSummary) api.WorkspaceShare {
	sh := api.WorkspaceShare{ID: id, Owner: owner, Title: "공유 " + id, Status: "live", CreatedAt: base.Add(time.Duration(min) * time.Minute),
		Members: []api.WorkspaceShareMember{}}
	for i, u := range members {
		sh.Members = append(sh.Members, api.WorkspaceShareMember{User: u, Seat: i + 1, InvitedAt: sh.CreatedAt})
	}
	return sh
}

func ids(shares []api.WorkspaceShare) []string {
	out := []string{}
	for _, s := range shares {
		out = append(out, s.ID)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWSStoreTracksInvitedAndEnded(t *testing.T) {
	s, st := newState(t)
	s.Apply(ev(t, "workspace_share.invited", map[string]any{"share": share("W1", hana, 0, me)}))
	if got := ids(st.Invited()); !eq(got, []string{"W1"}) {
		t.Fatalf("Invited() = %v, want [W1]", got)
	}
	if got := st.Mine(); len(got) != 0 {
		t.Errorf("Mine() = %v, want none", ids(got))
	}
	s.Apply(ev(t, "workspace_share.ended", map[string]any{"share_id": "W1", "reason": "timeout"}))
	if got := st.Invited(); len(got) != 0 {
		t.Fatalf("after ended Invited() = %v, want none", ids(got))
	}
	if sh, ok := st.Share("W1"); !ok || sh.Status != "ended" || sh.EndReason == nil || *sh.EndReason != "timeout" {
		t.Errorf("Share(W1) = %+v, %v; want ended/timeout", sh, ok)
	}
	s.Apply(ev(t, "workspace_share.invited", map[string]any{"share": share("W2", hana, 1, me)}))
	s.Reset()
	if len(st.Invited()) != 0 || len(st.Mine()) != 0 {
		t.Errorf("after Reset: invited %v mine %v", ids(st.Invited()), ids(st.Mine()))
	}
	if _, ok := st.Share("W2"); ok {
		t.Error("Share(W2) survived Reset")
	}
}

// Newest first; my own shares (owner = me, from updated) are Mine, not Invited.
func TestWSStoreOrderAndRoles(t *testing.T) {
	s, st := newState(t)
	s.Apply(ev(t, "workspace_share.invited", map[string]any{"share": share("W1", hana, 0, me)}))
	s.Apply(ev(t, "workspace_share.invited", map[string]any{"share": share("W3", hana, 2, me)}))
	s.Apply(ev(t, "workspace_share.invited", map[string]any{"share": share("W2", hana, 1, me)}))
	s.Apply(ev(t, "workspace_share.updated", map[string]any{"share": share("M1", me, 3, hana)}))
	if got := ids(st.Invited()); !eq(got, []string{"W3", "W2", "W1"}) {
		t.Errorf("Invited() = %v, want newest first", got)
	}
	if got := ids(st.Mine()); !eq(got, []string{"M1"}) {
		t.Errorf("Mine() = %v, want [M1]", got)
	}
}

// updated replaces the share; one that no longer lists me (I was removed or left) is dropped.
func TestWSStoreUpdatedDropsRemoved(t *testing.T) {
	s, st := newState(t)
	s.Apply(ev(t, "workspace_share.invited", map[string]any{"share": share("W1", hana, 0, me)}))
	up := share("W1", hana, 0, me)
	lvl := "viewer"
	up.Members[0].MaxLevel = &lvl
	s.Apply(ev(t, "workspace_share.updated", map[string]any{"share": up}))
	if sh, ok := st.Share("W1"); !ok || sh.Members[0].MaxLevel == nil || *sh.Members[0].MaxLevel != "viewer" {
		t.Fatalf("Share(W1) after updated = %+v", sh)
	}
	s.Apply(ev(t, "workspace_share.updated", map[string]any{"share": share("W1", hana, 0)}))
	if _, ok := st.Share("W1"); ok || len(st.Invited()) != 0 {
		t.Errorf("removed share still listed: %v", ids(st.Invited()))
	}
	// An updated share with status ended is no longer live.
	s.Apply(ev(t, "workspace_share.updated", map[string]any{"share": share("M1", me, 1, hana)}))
	ended := share("M1", me, 1, hana)
	ended.Status = "ended"
	s.Apply(ev(t, "workspace_share.updated", map[string]any{"share": ended}))
	if len(st.Mine()) != 0 {
		t.Errorf("Mine() = %v after ended update", ids(st.Mine()))
	}
}

// Resync asks for (owner, live) and (invited, live) and ApplyResync replaces
// the lists, keeping what events changed after the resync started; a result
// from before Reset is dropped.
func TestWSStoreResync(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "workspace")
	srv := apitest.New(t)
	srv.Handle("GET /v1/workspace-shares", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		var items []api.WorkspaceShare
		switch r.URL.Query().Get("role") + "/" + r.URL.Query().Get("status") {
		case "owner/live":
			items = []api.WorkspaceShare{share("M1", me, 5, hana)}
		case "invited/live":
			items = []api.WorkspaceShare{share("W1", hana, 1, me), share("W2", hana, 2, me)}
		default:
			t.Errorf("unexpected query %s", r.URL.RawQuery)
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	})
	c := srv.Client()

	s, st := newState(t)
	s.Apply(ev(t, "workspace_share.invited", map[string]any{"share": share("OLD", hana, 0, me)}))
	cmd := st.Resync(s, c)
	if cmd == nil {
		t.Fatal("Resync returned nil")
	}
	// An event between the request and its result is kept.
	s.Apply(ev(t, "workspace_share.ended", map[string]any{"share_id": "W2", "reason": "ended"}))
	msg, ok := cmd().(wsstore.ResyncMsg)
	if !ok || msg.Err != nil {
		t.Fatalf("Resync msg = %#v", msg)
	}
	st.ApplyResync(msg)
	if got := ids(st.Invited()); !eq(got, []string{"W1"}) {
		t.Errorf("Invited() after resync = %v, want [W1] (OLD gone, W2 ended)", got)
	}
	if got := ids(st.Mine()); !eq(got, []string{"M1"}) {
		t.Errorf("Mine() after resync = %v", got)
	}

	stale := st.Resync(s, c)
	s.Reset()
	st.ApplyResync(stale().(wsstore.ResyncMsg))
	if len(st.Invited()) != 0 || len(st.Mine()) != 0 {
		t.Errorf("a pre-Reset result was applied: %v %v", ids(st.Invited()), ids(st.Mine()))
	}

	// A failed fetch leaves the lists as they are.
	s.Apply(ev(t, "workspace_share.invited", map[string]any{"share": share("W9", hana, 9, me)}))
	bad := apitest.New(t)
	bad.Error("GET /v1/workspace-shares", 500, "internal_error")
	m := st.Resync(s, bad.Client())().(wsstore.ResyncMsg)
	if m.Err == nil {
		t.Fatal("want an error")
	}
	st.ApplyResync(m)
	if got := ids(st.Invited()); !eq(got, []string{"W9"}) {
		t.Errorf("Invited() after failed resync = %v", got)
	}
}

// With the workspace feature off nothing is fetched.
func TestWSStoreResyncFeatureOff(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "")
	s, st := newState(t)
	if cmd := st.Resync(s, apitest.New(t).Client()); cmd != nil {
		t.Error("Resync with the feature off returned a command")
	}
	if cmd := st.Backfill(s, apitest.New(t).Client()); cmd != nil {
		t.Error("Backfill with the feature off returned a command")
	}
}
