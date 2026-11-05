package store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// cannedBootstrap registers every events.md 5.3 step-1 endpoint.
func cannedBootstrap(srv *apitest.Server) {
	me := api.Me{UserSummary: uMe, Settings: api.Settings{DMPolicy: "friends_and_spaces", FocusDefaultMin: 25},
		Presence: api.PresenceState{Status: api.PresenceOnline, Mode: "auto"}}
	srv.JSON("GET /v1/me", 200, map[string]any{"user": me})
	srv.JSON("GET /v1/spaces", 200, map[string]any{"items": []api.Space{
		{SpacePreview: api.SpacePreview{ID: "S1", Name: "노바"}, MyRole: api.RoleOwner},
		{SpacePreview: api.SpacePreview{ID: "S2", Name: "점심"}, MyRole: api.RoleMember},
	}})
	srv.Handle("GET /v1/spaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		sd := api.SpaceDetail{Space: api.Space{SpacePreview: api.SpacePreview{ID: id, Name: "방 " + id}, MyRole: api.RoleMember},
			Channels: []api.Channel{textChannel("C"+id+"a", id, "일반", 0), textChannel("C"+id+"b", id, "개발", 1)}}
		writeJSON(w, map[string]any{"space": sd})
	})
	srv.JSON("GET /v1/dms", 200, map[string]any{"items": []api.Channel{dmChannel("D1MJ", uMe, uMinji)}, "next_cursor": nil})
	srv.JSON("GET /v1/me/unreads", 200, map[string]any{"items": []api.UnreadState{
		{ChannelID: "CS1b", UnreadCount: 4, MentionCount: 1, LastMessageID: sp("M9"), LastReadMessageID: sp("M5")},
		{ChannelID: "D1MJ", UnreadCount: 2},
	}})
	srv.JSON("GET /v1/notifications/unread-count", 200, map[string]any{"unread_count": 6})
	srv.JSON("GET /v1/friends", 200, map[string]any{"items": []api.Friend{{User: uMinji, Since: testTS, Presence: api.PresenceOnline}}, "next_cursor": nil})
	srv.JSON("GET /v1/friends/requests", 200, map[string]any{"items": []api.FriendRequest{{ID: "FR1", From: uJunho, To: uMe, Status: "pending", CreatedAt: testTS}}})
	srv.JSON("GET /v1/me/direct-invites", 200, map[string]any{"items": []api.DirectInvite{{ID: "DI1", Space: api.SpacePreview{ID: "S9"}, Inviter: uHana, Invitee: uMe, CreatedAt: testTS}}})
	srv.JSON("GET /v1/blocks", 200, map[string]any{"items": []api.BlockEntry{{User: api.UserSummary{ID: "U9BAD", DisplayName: "악당"}, CreatedAt: testTS}}})
}

func runBootstrap(t *testing.T, c *api.Client) BootstrapMsg {
	t.Helper()
	cmd := Bootstrap(c)
	if cmd == nil {
		t.Fatal("Bootstrap returned nil cmd")
	}
	m, ok := cmd().(BootstrapMsg)
	if !ok {
		t.Fatalf("Bootstrap msg type %T", cmd())
	}
	return m
}

func TestBootstrapRequests(t *testing.T) {
	srv := apitest.New(t)
	cannedBootstrap(srv)
	m := runBootstrap(t, srv.Client())
	if m.Err != nil {
		t.Fatalf("Err = %v", m.Err)
	}
	if m.Me == nil || m.Me.ID != uMe.ID {
		t.Errorf("Me = %+v", m.Me)
	}
	if len(m.Spaces) != 2 || m.Spaces[0].ID != "S1" || m.Spaces[1].ID != "S2" || len(m.Spaces[0].Channels) != 2 {
		t.Errorf("Spaces = %+v", m.Spaces)
	}
	if len(m.DMs) != 1 || len(m.Unreads) != 2 || m.NotifUnread != 6 || len(m.Friends) != 1 ||
		len(m.FriendReqIn) != 1 || len(m.DirectInvites) != 1 || len(m.Blocks) != 1 {
		t.Errorf("msg = %+v", m)
	}

	var paths []string
	queries := map[string]string{}
	for _, r := range srv.Requests() {
		if r.Method != "GET" {
			t.Errorf("unexpected %s %s", r.Method, r.Path)
		}
		paths = append(paths, r.Path)
		queries[r.Path] = r.Query
	}
	sort.Strings(paths)
	want := []string{"/v1/blocks", "/v1/dms", "/v1/friends", "/v1/friends/requests", "/v1/me", "/v1/me/direct-invites",
		"/v1/me/unreads", "/v1/notifications/unread-count", "/v1/spaces", "/v1/spaces/S1", "/v1/spaces/S2"}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Errorf("paths = %v\nwant    %v", paths, want)
	}
	if queries["/v1/friends/requests"] != "direction=incoming" {
		t.Errorf("friend requests query = %q", queries["/v1/friends/requests"])
	}
	for _, p := range []string{"/v1/dms", "/v1/friends"} {
		if queries[p] != "limit=100" {
			t.Errorf("%s query = %q, want limit=100", p, queries[p])
		}
	}

	// one endpoint 500 => Err set, other fields still filled.
	srv2 := apitest.New(t)
	cannedBootstrap(srv2)
	srv2.Error("GET /v1/blocks", 500, "internal_error")
	m2 := runBootstrap(t, srv2.Client())
	if m2.Err == nil {
		t.Fatal("expected Err")
	}
	if m2.Me == nil || len(m2.Spaces) != 2 || len(m2.DMs) != 1 || m2.NotifUnread != 6 || len(m2.Friends) != 1 || m2.Blocks != nil {
		t.Errorf("partial msg = %+v", m2)
	}
}

// pagedHandler serves pages of n items; next(cursor) returns the next cursor ("" => null).
func pagedHandler(t *testing.T, mk func(i int) any, perPage func(cursor string) int, next func(cursor string) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cur := r.URL.Query().Get("cursor")
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("%s: limit = %q", r.URL.Path, r.URL.Query().Get("limit"))
		}
		page := 0
		if cur != "" {
			page, _ = strconv.Atoi(cur[1:])
			page--
		}
		n := perPage(cur)
		items := make([]any, n)
		for i := range items {
			items[i] = mk(page*100 + i)
		}
		var nc any
		if c := next(cur); c != "" {
			nc = c
		}
		writeJSON(w, map[string]any{"items": items, "next_cursor": nc})
	}
}

func TestBootstrapFollowsCursors(t *testing.T) {
	mkDM := func(i int) any { return dmChannel(fmt.Sprintf("D%05d", i), uMe, uMinji) }
	mkFriend := func(i int) any {
		return api.Friend{User: api.UserSummary{ID: fmt.Sprintf("U%05d", i), DisplayName: "f"}, Since: testTS}
	}
	twoPages := func(cur string) int {
		if cur == "" {
			return 100
		}
		return 30
	}
	nextTwo := func(cur string) string {
		if cur == "" {
			return "c2"
		}
		return ""
	}

	srv := apitest.New(t)
	cannedBootstrap(srv)
	srv.Handle("GET /v1/dms", pagedHandler(t, mkDM, twoPages, nextTwo))
	srv.Handle("GET /v1/friends", pagedHandler(t, mkFriend, twoPages, nextTwo))
	m := runBootstrap(t, srv.Client())
	if m.Err != nil {
		t.Fatalf("Err = %v", m.Err)
	}
	if len(m.DMs) != 130 || len(m.Friends) != 130 {
		t.Fatalf("DMs %d Friends %d, want 130", len(m.DMs), len(m.Friends))
	}
	for i, d := range m.DMs {
		if d.ID != fmt.Sprintf("D%05d", i) {
			t.Fatalf("DMs[%d] = %s: server order lost", i, d.ID)
		}
	}
	for _, p := range []string{"/v1/dms", "/v1/friends"} {
		var qs []string
		for _, r := range srv.Requests() {
			if r.Path == p {
				qs = append(qs, r.Query)
			}
		}
		if len(qs) != 2 || !sameQuery(qs[0], "limit=100") || !sameQuery(qs[1], "cursor=c2&limit=100") {
			t.Errorf("%s queries = %v", p, qs)
		}
	}

	// a server that always returns next_cursor => 20 pages, no Err.
	srv2 := apitest.New(t)
	cannedBootstrap(srv2)
	full := func(string) int { return 100 }
	always := func(cur string) string {
		if cur == "" {
			return "c2"
		}
		n, _ := strconv.Atoi(cur[1:])
		return "c" + strconv.Itoa(n+1)
	}
	srv2.Handle("GET /v1/dms", pagedHandler(t, mkDM, full, always))
	m2 := runBootstrap(t, srv2.Client())
	if m2.Err != nil {
		t.Fatalf("capped Err = %v", m2.Err)
	}
	if len(m2.DMs) != 2000 {
		t.Errorf("capped DMs = %d, want 2000", len(m2.DMs))
	}
	n := 0
	for _, r := range srv2.Requests() {
		if r.Path == "/v1/dms" {
			n++
		}
	}
	if n != 20 {
		t.Errorf("GET /v1/dms count = %d, want 20", n)
	}

	// page 2 returns 500 => Err set, Friends nil (a partial list would drop
	// every friend after page 1 from the store).
	srv3 := apitest.New(t)
	cannedBootstrap(srv3)
	ok := pagedHandler(t, mkFriend, twoPages, nextTwo)
	srv3.Handle("GET /v1/friends", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"x","details":{}}}`))
			return
		}
		ok(w, r)
	})
	m3 := runBootstrap(t, srv3.Client())
	if m3.Err == nil {
		t.Fatal("page-2 failure: expected Err")
	}
	if m3.Friends != nil {
		t.Errorf("partial friends = %d, want nil (failed request)", len(m3.Friends))
	}
	st := New()
	st.Friends = map[string]api.Friend{"U1": {User: api.UserSummary{ID: "U1"}}, "U150": {User: api.UserSummary{ID: "U150"}}}
	st.ApplyBootstrap(m3)
	if len(st.Friends) != 2 {
		t.Errorf("failed friends request replaced the store: %d friends", len(st.Friends))
	}
}

// One failed GET /v1/spaces/{id} must not drop that space (and its channels
// and unread counts) from the store: the whole Spaces result counts as failed.
func TestBootstrapSpaceDetailFailureKeepsStore(t *testing.T) {
	srv := apitest.New(t)
	cannedBootstrap(srv)
	full := runBootstrap(t, srv.Client())
	if full.Err != nil || len(full.Spaces) != 2 {
		t.Fatalf("canned bootstrap: %v %d", full.Err, len(full.Spaces))
	}
	s := New()
	s.ApplyBootstrap(full)

	srv2 := apitest.New(t)
	cannedBootstrap(srv2)
	srv2.Handle("GET /v1/spaces/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "S2" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"x","details":{}}}`))
			return
		}
		sd := api.SpaceDetail{Space: api.Space{SpacePreview: api.SpacePreview{ID: id, Name: "방 " + id}, MyRole: api.RoleMember},
			Channels: []api.Channel{textChannel("C"+id+"a", id, "일반", 0), textChannel("C"+id+"b", id, "개발", 1)}}
		writeJSON(w, map[string]any{"space": sd})
	})
	m := runBootstrap(t, srv2.Client())
	if m.Err == nil {
		t.Fatal("expected Err")
	}
	if m.Spaces != nil {
		t.Errorf("partial Spaces = %d, want nil (failed request)", len(m.Spaces))
	}
	s.ApplyBootstrap(m)
	if s.Spaces["S2"] == nil || s.Channels["CS2a"] == nil || s.Channels["CS2b"] == nil || len(s.SpaceChannels("S2")) != 2 {
		t.Errorf("space S2 lost after a failed GET /v1/spaces/S2: spaces=%v", s.SpaceOrder)
	}
	if fmt.Sprint(s.SpaceOrder) != "[S1 S2]" {
		t.Errorf("SpaceOrder = %v", s.SpaceOrder)
	}
	if s.Channels["D1MJ"] == nil {
		t.Error("DMs lost")
	}
}

func sameQuery(a, b string) bool {
	va, err1 := url.ParseQuery(a)
	vb, err2 := url.ParseQuery(b)
	return err1 == nil && err2 == nil && fmt.Sprint(va) == fmt.Sprint(vb)
}

type resyncMsg struct{ name string }

type resyncReducer struct {
	recReducer
	name   string
	sawMy  int
	gotCli *api.Client
	called int
	nilCmd bool
}

func (r *resyncReducer) Name() string { return r.name }
func (r *resyncReducer) Resync(s *Store, c *api.Client) tea.Cmd {
	r.called++
	r.gotCli = c
	if ch := s.Channels["CS1b"]; ch != nil {
		r.sawMy = ch.My.UnreadCount
	}
	if r.nilCmd {
		return nil
	}
	return func() tea.Msg { return resyncMsg{r.name} }
}

func TestApplyBootstrapBatchesResyncers(t *testing.T) {
	withFactories(t)
	var a, b, quiet *resyncReducer
	RegisterReducer(func(s *Store) Reducer { a = &resyncReducer{name: "a"}; return a })
	RegisterReducer(func(s *Store) Reducer { return &recReducer{} }) // plain reducer, no Resyncer
	RegisterReducer(func(s *Store) Reducer { quiet = &resyncReducer{name: "q", nilCmd: true}; return quiet })

	srv := apitest.New(t)
	cannedBootstrap(srv)
	c := srv.Client()
	m := runBootstrap(t, c)

	s := New()
	s.Channels["STALE"] = &api.Channel{ID: "STALE"}
	s.SpaceOrder = []string{"OLD"}
	cmd := s.ApplyBootstrap(m)
	if cmd == nil {
		t.Fatal("ApplyBootstrap returned nil cmd")
	}
	if got, ok := cmd().(resyncMsg); !ok || got.name != "a" {
		t.Fatalf("cmd() = %#v, want resyncMsg{a}", cmd())
	}
	if a.called != 1 || quiet.called != 1 || a.gotCli != c {
		t.Errorf("Resync calls a=%d q=%d client=%v", a.called, quiet.called, a.gotCli == c)
	}
	if a.sawMy != 4 {
		t.Errorf("Resync ran before core data landed: saw unread %d", a.sawMy)
	}

	if !s.Sync.Bootstrapped || s.Sync.Err != nil {
		t.Errorf("Sync = %+v", s.Sync)
	}
	if s.Me == nil || s.Me.ID != uMe.ID || s.Settings.FocusDefaultMin != 25 || s.Self.Mode != "auto" {
		t.Errorf("Me/Settings/Self = %+v %+v %+v", s.Me, s.Settings, s.Self)
	}
	if s.Channels["STALE"] != nil {
		t.Error("ApplyBootstrap must replace channels")
	}
	if fmt.Sprint(s.SpaceOrder) != "[S1 S2]" || s.Spaces["S1"] == nil {
		t.Errorf("SpaceOrder = %v", s.SpaceOrder)
	}
	ch := s.Channels["CS1b"]
	if ch == nil || ch.My.UnreadCount != 4 || ch.My.MentionCount != 1 || ch.My.LastReadMessageID == nil || *ch.My.LastReadMessageID != "M5" {
		t.Errorf("unreads not applied: %+v", ch)
	}
	if s.Channels["D1MJ"] == nil || s.Channels["D1MJ"].My.UnreadCount != 2 || fmt.Sprint(s.DMOrder) != "[D1MJ]" {
		t.Errorf("DMs: %+v %v", s.Channels["D1MJ"], s.DMOrder)
	}
	if s.NotifUnread != 6 || len(s.Friends) != 1 || len(s.FriendReqIn) != 1 || len(s.DirectInvites) != 1 || !s.IsBlocked("U9BAD") {
		t.Errorf("social = %d %d %d %d", s.NotifUnread, len(s.Friends), len(s.FriendReqIn), len(s.DirectInvites))
	}
	for _, id := range []string{uMe.ID, uMinji.ID, uJunho.ID, uHana.ID, "U9BAD"} {
		if s.User(id) == nil {
			t.Errorf("embedded user %s not observed", id)
		}
	}
	if s.ChannelLabel("D1MJ") != "민지" || len(s.SpaceChannels("S2")) != 2 {
		t.Error("derived views after bootstrap")
	}

	// Two Resyncers returning commands => batched.
	withFactories(t)
	RegisterReducer(func(s *Store) Reducer { a = &resyncReducer{name: "a"}; return a })
	RegisterReducer(func(s *Store) Reducer { b = &resyncReducer{name: "b"}; return b })
	s2 := New()
	cmd2 := s2.ApplyBootstrap(m)
	batch, ok := cmd2().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("two resyncers => %T %v", cmd2(), cmd2())
	}
	got := []string{batch[0]().(resyncMsg).name, batch[1]().(resyncMsg).name}
	sort.Strings(got)
	if fmt.Sprint(got) != "[a b]" {
		t.Errorf("batched msgs = %v", got)
	}

	// No resyncers => nil cmd, still bootstrapped.
	withFactories(t)
	s3 := New()
	if s3.ApplyBootstrap(BootstrapMsg{Me: &api.Me{UserSummary: uMe}}) != nil {
		t.Error("no resyncers => nil cmd")
	}
	if !s3.Sync.Bootstrapped {
		t.Error("Bootstrapped not set")
	}
}
