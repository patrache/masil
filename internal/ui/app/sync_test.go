package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
	"github.com/patrache/masil/internal/ui/widgets"
)

// ---- test doubles ----

// recRT is a ui.Realtime that records SetIdle, Watch and MarkProcessed.
type recRT struct {
	mu     sync.Mutex
	st     realtime.State
	idle   []bool
	watch  [][]string
	marked []int64
}

func liveRT() *recRT { return &recRT{st: realtime.State{Phase: realtime.PhaseLive}} }

func (r *recRT) Typing(string, *string) {}
func (r *recRT) SetIdle(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.idle = append(r.idle, v)
}
func (r *recRT) Watch(topics []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.watch = append(r.watch, slices.Clone(topics))
}
func (r *recRT) State() realtime.State { return r.st }
func (r *recRT) MarkProcessed(seq int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.marked = append(r.marked, seq)
}
func (r *recRT) idles() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.idle)
}
func (r *recRT) watches() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.watch)
}

// probeReducer counts Store.Reset calls (registered for every Store of this binary).
type probeReducer struct{ resets int }

const probeReducerName = "core.testprobe.sync"

func (p *probeReducer) Name() string                       { return probeReducerName }
func (p *probeReducer) Apply(*store.Store, realtime.Event) {}
func (p *probeReducer) Reset()                             { p.resets++ }

func init() {
	store.RegisterReducer(func(*store.Store) store.Reducer { return &probeReducer{} })
}

// watchProbe is the RouteTrends component registered in TestMain: a placeholder
// that implements ui.Watcher with settable topics.
type watchProbe struct {
	ui.Component
	Topics []string
}

func (w *watchProbe) WatchTopics() []string { return w.Topics }

func newWatchProbe(*ui.Context, ui.Route) ui.Component {
	return &watchProbe{Component: widgets.Placeholder("trends"), Topics: []string{"post:A", "post:B"}}
}

// ---- helpers ----

func channelRoute(id string) ui.Route { return ui.Route{Kind: ui.RouteChannel, ID: id} }
func channelKey(id string) string     { return channelRoute(id).Key() }

func readyEvent(resumed, resync bool) map[string]any {
	return map[string]any{"resumed": resumed, "resync_required": resync, "replayed": 0, "seq": 0, "epoch": "E"}
}

// bootstrapPaths are the events.md 5.3 step-1 requests.
var bootstrapPaths = []string{
	"/v1/me", "/v1/spaces", "/v1/dms", "/v1/me/unreads", "/v1/notifications/unread-count",
	"/v1/friends", "/v1/friends/requests", "/v1/me/direct-invites", "/v1/blocks",
}

func requested(srv *apitest.Server, method, path string) int {
	n := 0
	for _, r := range srv.Requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// lastRow is the status line.
func lastRow(h *uitest.Harness) string {
	lines := strings.Split(h.Screen(), "\n")
	return lines[len(lines)-1]
}

// observe records every broadcast msg of the harness's app.
func observe(h *uitest.Harness) *[]tea.Msg {
	var got []tea.Msg
	app.SetObserverForTest(h.Model, func(msg tea.Msg) { got = append(got, msg) })
	return &got
}

func loggedOutReasons(msgs []tea.Msg) []string {
	var out []string
	for _, m := range msgs {
		if lo, ok := m.(ui.LoggedOutMsg); ok {
			out = append(out, lo.Reason)
		}
	}
	return out
}

// saveCreds writes credentials.json so the logout flow has a file to remove.
func saveCreds(t *testing.T, h *uitest.Harness) string {
	t.Helper()
	p := h.Context().Paths
	if err := auth.Save(p, &auth.Credentials{Version: 1, Server: h.API.URL, Token: "msl_test", UserID: fixtures.UserSam, Handle: "sam"}); err != nil {
		t.Fatal(err)
	}
	return p.Credentials
}

// assertLoggedOut checks the common logout outcome (CA 3.4, plan 03 Task 30).
func assertLoggedOut(t *testing.T, h *uitest.Harness, credFile, notice string) {
	t.Helper()
	if _, err := os.Stat(credFile); !os.IsNotExist(err) {
		t.Errorf("credentials.json must be removed, stat err = %v", err)
	}
	if h.Context().Store.Me != nil {
		t.Errorf("Store must be reset, Me = %+v", h.Context().Store.Me)
	}
	if p, ok := h.Context().Store.Reducer(probeReducerName).(*probeReducer); !ok || p.resets == 0 {
		t.Errorf("reducers must be Reset")
	}
	if tok := h.Context().API.Token(); tok != "" {
		t.Errorf("API token must be cleared after logout")
	}
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteLogin {
		t.Errorf("Main shows %+v, want the login screen", r)
	}
	if h.Instance(channelKey(fixtures.ChDev)) != nil {
		t.Errorf("route instances must be dropped")
	}
	if n := len(app.OverlaysForTest(h.Model)); n != 0 {
		t.Errorf("overlays must be closed, %d open", n)
	}
	if notice != "" && !strings.Contains(h.Screen(), notice) {
		t.Errorf("screen must show %q:\n%s", notice, h.Screen())
	}
	if strings.Contains(h.Screen(), errtext.Describe(i18n.KoT(), &api.Error{Status: 401, Code: "unauthorized"})) {
		t.Errorf("no unauthorized error toast on the login screen:\n%s", h.Screen())
	}
	for _, sz := range uitest.Required {
		h.Resize(sz.W, sz.H)
		h.Check()
	}
}

const unauthorizedNotice = "다른 곳에서 로그아웃됐어요. 다시 로그인해 주세요"

// ---- tests ----

func TestFirstReadyBootstraps(t *testing.T) {
	h := uitest.New(t, uitest.WithSeed(func(*store.Store) {}), uitest.WithRoute(channelRoute(fixtures.ChDev)))
	var seen []string
	h.Instance(channelKey(fixtures.ChDev)).(*stub.Chat).OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		switch m := msg.(type) {
		case ui.ResyncStartedMsg:
			seen = append(seen, "started")
		case ui.ResyncDoneMsg:
			seen = append(seen, fmt.Sprintf("done:%v", m.Err))
		}
	}
	if strings.Contains(h.Screen(), "#개발") {
		t.Fatalf("empty store must not know #개발 yet:\n%s", h.Screen())
	}

	h.Event("ready", readyEvent(false, false))
	for _, p := range bootstrapPaths {
		if requested(h.API, http.MethodGet, p) != 1 {
			t.Errorf("first ready: GET %s requested %d times, want 1", p, requested(h.API, http.MethodGet, p))
		}
	}
	if s := h.Context().Store.Sync; !s.Bootstrapped || !s.Ready || s.Err != nil {
		t.Errorf("sync = %+v, want ready + bootstrapped", s)
	}
	if !slices.Equal(seen, []string{"started", "done:<nil>"}) {
		t.Errorf("broadcasts = %v, want [started done:<nil>]", seen)
	}
	if !strings.Contains(h.Screen(), "#개발") {
		t.Errorf("bootstrapped channels must show:\n%s", h.Screen())
	}

	n := len(h.API.Requests())
	h.Event("ready", readyEvent(true, false))
	if got := len(h.API.Requests()); got != n {
		t.Errorf("resumed ready made %d requests, want none", got-n)
	}
	h.Event("ready", readyEvent(true, true))
	if got := requested(h.API, http.MethodGet, "/v1/me"); got != 2 {
		t.Errorf("resync_required ready: GET /v1/me = %d, want 2", got)
	}
	if len(seen) != 4 {
		t.Errorf("resync broadcasts = %v, want two rounds", seen)
	}
}

func TestBootstrapNetworkFailureRetries(t *testing.T) {
	srv := apitest.New(t)
	fixtures.RegisterAPI(srv)
	full := store.New()
	fixtures.Seed(full)
	var fail atomic.Bool
	fail.Store(true)
	srv.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprintf(w, "%s", mustMarshal(t, map[string]any{"user": full.Me}))
	})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(func(*store.Store) {}))

	h.Event("ready", readyEvent(false, false))
	if !strings.Contains(h.Screen(), "서버에 연결할 수 없어요") {
		t.Errorf("network failure must toast:\n%s", h.Screen())
	}
	if s := lastRow(h); !strings.Contains(s, "✕ 오프라인") {
		t.Errorf("status = %q, want ✕ 오프라인", s)
	}
	if !slices.Contains(h.Clock().Pending(), 10*time.Second) {
		t.Errorf("a 10s retry must be armed, pending = %v", h.Clock().Pending())
	}
	for _, sz := range uitest.Required {
		h.Resize(sz.W, sz.H)
		h.Check()
	}
	h.Resize(120, 40)

	fail.Store(false)
	h.Advance(10 * time.Second)
	if s := h.Context().Store.Sync; !s.Bootstrapped || s.Err != nil {
		t.Fatalf("retry: sync = %+v, want bootstrapped", s)
	}
	if h.Context().Store.Me == nil {
		t.Errorf("retry must fill Store.Me")
	}
	if s := lastRow(h); !strings.Contains(s, "● 온라인") {
		t.Errorf("status = %q, want ● 온라인", s)
	}
	for _, sz := range uitest.Required {
		h.Resize(sz.W, sz.H)
		h.Check()
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPruneUnknownTabsAfterBootstrap(t *testing.T) {
	const gone = "01JB000000000000000000GONE"
	h := uitest.New(t)
	h.Send(ui.NavigateMsg{Route: channelRoute(gone)})
	h.Send(ui.NavigateMsg{Route: channelRoute(fixtures.ChDev)})
	h.Send(ui.NavigateMsg{Route: channelRoute(gone)}) // the unknown tab is active
	if n := h.Nav(); len(n.Tabs) != 3 || n.Tabs[n.Active].Stack[0].ID != gone {
		t.Fatalf("setup nav = %+v", n)
	}

	h.Event("ready", readyEvent(false, false))
	n := h.Nav()
	var roots []string
	for _, tab := range n.Tabs {
		roots = append(roots, tab.Stack[0].Key())
	}
	if !slices.Equal(roots, []string{"home//", channelKey(fixtures.ChDev)}) {
		t.Fatalf("tabs after bootstrap = %v, want home and #개발 only", roots)
	}
	if n.Active < 0 || n.Active >= len(n.Tabs) {
		t.Fatalf("active %d out of range", n.Active)
	}
	h.Check()
}

// events.md 5.3 step 5: an event applied while the bootstrap is in flight
// (here from inside its last REST request) survives the older snapshot, and
// the tab of a channel it created is not pruned.
func TestBootstrapKeepsEventsDuringFlight(t *testing.T) {
	const fresh = "01JB00000000000000000FRESH"
	srv := apitest.New(t)
	fixtures.RegisterAPI(srv)
	var h *uitest.Harness
	var once sync.Once
	srv.Handle("GET /v1/blocks", func(w http.ResponseWriter, r *http.Request) {
		// The Update goroutine is waiting on the bootstrap Cmd, so touching the
		// Store here stands in for events applied during the flight.
		once.Do(func() {
			st := h.Context().Store
			name := "새채널"
			sid := fixtures.SpaceNova
			seq := int64(1)
			st.Apply(realtime.Event{Type: "channel.created", Seq: &seq, TS: h.Clock().Now(),
				Data: json.RawMessage(mustMarshal(t, map[string]any{"channel": api.Channel{
					ID: fresh, Kind: api.ChannelText, SpaceID: &sid, Name: &name, Position: 9, CreatedAt: h.Clock().Now()}}))})
			seq2 := int64(2)
			st.Apply(realtime.Event{Type: "channel.deleted", Seq: &seq2, TS: h.Clock().Now(),
				Data: json.RawMessage(mustMarshal(t, map[string]any{"channel_id": fixtures.ChDeploy, "space_id": fixtures.SpaceNova, "reason": "deleted"}))})
		})
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprint(w, `{"items":[]}`)
	})
	h = uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(func(*store.Store) {}))
	h.Send(ui.NavigateMsg{Route: channelRoute(fresh)})
	h.Send(ui.NavigateMsg{Route: channelRoute(fixtures.ChDev)})

	h.Event("ready", readyEvent(false, false))
	st := h.Context().Store
	if s := st.Sync; !s.Bootstrapped || s.Err != nil {
		t.Fatalf("sync = %+v", s)
	}
	if st.Channels[fresh] == nil {
		t.Error("channel.created during the flight was undone by the snapshot")
	}
	if st.Channels[fixtures.ChDeploy] != nil {
		t.Error("channel.deleted during the flight was undone by the snapshot")
	}
	var roots []string
	for _, tab := range h.Nav().Tabs {
		roots = append(roots, tab.Stack[0].Key())
	}
	if !slices.Contains(roots, channelKey(fresh)) || !slices.Contains(roots, channelKey(fixtures.ChDev)) {
		t.Errorf("tabs after bootstrap = %v, want the fresh channel's tab kept", roots)
	}
	h.Check()
}

func TestEventAppliedAndBroadcast(t *testing.T) {
	h := uitest.New(t) // home first, then #개발: two live instances
	h.Send(ui.NavigateMsg{Route: channelRoute(fixtures.ChDev)})
	rt := liveRT()
	app.SetRealtimeForTest(h.Model, rt)
	var devGot, homeGot []string
	h.Instance(channelKey(fixtures.ChDev)).(*stub.Chat).OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		if e, ok := msg.(ui.EventMsg); ok {
			devGot = append(devGot, e.Event.Type)
		}
	}
	h.Instance("home//").(*stub.List).OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		if e, ok := msg.(ui.EventMsg); ok {
			homeGot = append(homeGot, e.Event.Type)
		}
	}
	st := h.Context().Store
	if got := st.Channels[fixtures.ChGeneral].My.UnreadCount; got != 3 {
		t.Fatalf("fixture #일반 unread = %d, want 3", got)
	}
	minji := *st.User(fixtures.UserMinji)
	h.Event("message.created", realtime.MessageEvent{Message: api.Message{
		ID: "01JDGEN0000000000000000099", ChannelID: fixtures.ChGeneral, Author: minji,
		Kind: "text", Body: "점심 뭐 먹어요?", CreatedAt: h.Clock().Now(),
	}})
	if got := st.Channels[fixtures.ChGeneral].My.UnreadCount; got != 4 {
		t.Errorf("#일반 unread = %d, want 4", got)
	}
	if !slices.Equal(devGot, []string{"message.created"}) || !slices.Equal(homeGot, []string{"message.created"}) {
		t.Errorf("broadcast: dev %v, home %v", devGot, homeGot)
	}
	if !slices.Equal(rt.marked, []int64{1}) {
		t.Errorf("MarkProcessed = %v, want [1]", rt.marked)
	}
	h.Event("typing", map[string]any{"channel_id": fixtures.ChGeneral, "user_id": fixtures.UserMinji, "expires_in_ms": 5000})
	if !slices.Equal(rt.marked, []int64{1}) {
		t.Errorf("ephemeral events are not marked: %v", rt.marked)
	}
}

func TestUnauthorizedMidSessionReturnsToLogin(t *testing.T) {
	t.Run("invalid_credentials is not a logout", func(t *testing.T) {
		h := uitest.New(t)
		cred := saveCreds(t, h)
		got := observe(h)
		h.API.Error("GET /v1/me", http.StatusUnauthorized, "invalid_credentials")
		_, err := h.Context().API.GetMe(context.Background())
		if api.CodeOf(err) != "invalid_credentials" {
			t.Fatalf("GetMe err = %v", err)
		}
		h.Send(ui.ErrorMsg{Err: err})
		if !strings.Contains(h.Screen(), errtext.Describe(i18n.KoT(), err)) {
			t.Errorf("error toast missing:\n%s", h.Screen())
		}
		if _, err := os.Stat(cred); err != nil {
			t.Errorf("credentials must be kept: %v", err)
		}
		if r := loggedOutReasons(*got); len(r) != 0 {
			t.Errorf("LoggedOutMsg %v, want none", r)
		}
		if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind == ui.RouteLogin {
			t.Errorf("must stay in the shell")
		}
	})

	t.Run("401 unauthorized on a ui.Do call", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(channelRoute(fixtures.ChDev)))
		cred := saveCreds(t, h)
		got := observe(h)
		h.Press("i")
		h.Type("보내는 중")
		if v := h.Instance(channelKey(fixtures.ChDev)).(*stub.Chat).Composer.Value(); v != "보내는 중" {
			t.Fatalf("draft = %q", v)
		}
		h.API.Error("GET /v1/users/search", http.StatusUnauthorized, "unauthorized")
		h.Press("ctrl+k")
		h.Type("민지")
		h.Advance(300 * time.Millisecond) // people search debounce => ui.Do => 401
		if requested(h.API, http.MethodGet, "/v1/users/search") == 0 {
			t.Fatal("the palette people search did not run")
		}
		assertLoggedOut(t, h, cred, unauthorizedNotice)
		if r := loggedOutReasons(*got); !slices.Equal(r, []string{"unauthorized"}) {
			t.Errorf("LoggedOutMsg reasons = %v, want [unauthorized]", r)
		}
		if h.Nav().Tabs[0].Stack[0].Kind != ui.RouteHome || len(h.Nav().Tabs) != 1 {
			t.Errorf("nav must be reset: %+v", h.Nav())
		}
	})

	t.Run("socket 4001", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(channelRoute(fixtures.ChDev)))
		cred := saveCreds(t, h)
		got := observe(h)
		h.Send(ui.RealtimeStateMsg{State: realtime.State{Phase: realtime.PhaseStopped, Err: realtime.ErrUnauthorized, LastClose: 4001}})
		assertLoggedOut(t, h, cred, unauthorizedNotice)
		if r := loggedOutReasons(*got); !slices.Equal(r, []string{"unauthorized"}) {
			t.Errorf("LoggedOutMsg reasons = %v, want [unauthorized]", r)
		}
		// Late events of the old session are dropped.
		h.Event("message.created", realtime.MessageEvent{Message: api.Message{ID: "01JDGEN0000000000000000099", ChannelID: fixtures.ChGeneral}})
		if len(h.Context().Store.Channels) != 0 {
			t.Errorf("events after logout must not repopulate the store")
		}
	})
}

func TestSuspendedAndUpgrade(t *testing.T) {
	t.Run("suspended", func(t *testing.T) {
		h := uitest.New(t)
		cred := saveCreds(t, h)
		got := observe(h)
		h.Send(ui.RealtimeStateMsg{State: realtime.State{Phase: realtime.PhaseStopped, Err: realtime.ErrSuspended}})
		assertLoggedOut(t, h, cred, "정지된 계정이에요")
		if r := loggedOutReasons(*got); !slices.Equal(r, []string{"suspended"}) {
			t.Errorf("LoggedOutMsg reasons = %v, want [suspended]", r)
		}
	})
	t.Run("upgrade required (socket)", func(t *testing.T) {
		h := uitest.New(t)
		cred := saveCreds(t, h)
		h.Send(ui.RealtimeStateMsg{State: realtime.State{Phase: realtime.PhaseStopped, Err: realtime.ErrUpgradeRequired}})
		if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind == ui.RouteLogin {
			t.Fatalf("upgrade required must stay in the shell")
		}
		if s := lastRow(h); !strings.Contains(s, "✕ 업데이트 필요") {
			t.Errorf("status = %q, want ✕ 업데이트 필요", s)
		}
		if !strings.Contains(h.Screen(), "masil을 업데이트해 주세요") {
			t.Errorf("upgrade toast missing:\n%s", h.Screen())
		}
		if _, err := os.Stat(cred); err != nil {
			t.Errorf("credentials must be kept: %v", err)
		}
		h.Check()
	})
	t.Run("upgrade required (REST 426)", func(t *testing.T) {
		h := uitest.New(t)
		h.API.Handle("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUpgradeRequired)
			w.Write([]byte(`{"error":{"code":"upgrade_required","message":"old","details":{"min_client_version":"9.9.9"}}}`))
		})
		if _, err := h.Context().API.GetMe(context.Background()); err == nil {
			t.Fatal("GetMe must fail with 426")
		}
		h.Resize(120, 40) // the next Update picks up the hook flag
		if s := lastRow(h); !strings.Contains(s, "✕ 업데이트 필요") {
			t.Errorf("status = %q, want ✕ 업데이트 필요", s)
		}
		if !strings.Contains(h.Screen(), "masil을 업데이트해 주세요") {
			t.Errorf("upgrade toast missing:\n%s", h.Screen())
		}
	})
}

func TestLogoutCommand(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		h := uitest.New(t, uitest.WithRoute(channelRoute(fixtures.ChDev)))
		cred := saveCreds(t, h)
		got := observe(h)
		h.API.Handle("POST /v1/auth/logout", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
		h.Send(ui.RunCommandMsg{ID: "logout"})
		if requested(h.API, http.MethodPost, "/v1/auth/logout") != 0 {
			t.Fatal("logout must wait for the confirmation")
		}
		h.Press("y")
		if requested(h.API, http.MethodPost, "/v1/auth/logout") != 1 {
			t.Fatalf("POST /v1/auth/logout not sent: %+v", h.API.Requests())
		}
		assertLoggedOut(t, h, cred, "")
		if r := loggedOutReasons(*got); !slices.Equal(r, []string{"logout"}) {
			t.Errorf("LoggedOutMsg reasons = %v, want [logout]", r)
		}
		if strings.Contains(h.Screen(), unauthorizedNotice) {
			t.Errorf("a plain logout has no notice")
		}
	})
	t.Run("network error", func(t *testing.T) {
		h := uitest.New(t)
		cred := saveCreds(t, h)
		got := observe(h)
		h.API.Handle("POST /v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
		})
		h.Send(ui.RunCommandMsg{ID: "logout"})
		h.Press("y")
		assertLoggedOut(t, h, cred, "")
		if r := loggedOutReasons(*got); !slices.Equal(r, []string{"logout"}) {
			t.Errorf("LoggedOutMsg reasons = %v, want [logout]", r)
		}
	})
	t.Run("token already revoked", func(t *testing.T) {
		h := uitest.New(t)
		cred := saveCreds(t, h)
		got := observe(h)
		h.API.Error("POST /v1/auth/logout", http.StatusUnauthorized, "unauthorized")
		h.Send(ui.RunCommandMsg{ID: "logout"})
		h.Press("y")
		assertLoggedOut(t, h, cred, "")
		if r := loggedOutReasons(*got); !slices.Equal(r, []string{"logout"}) {
			t.Errorf("LoggedOutMsg reasons = %v, want [logout] once", r)
		}
	})
	t.Run("socket 4001 before the 204", func(t *testing.T) {
		// The server revokes the session and closes the socket with 4001 before it
		// answers POST /v1/auth/logout, so the realtime stop usually arrives first.
		h := uitest.New(t, uitest.WithRoute(channelRoute(fixtures.ChDev)))
		cred := saveCreds(t, h)
		got := observe(h)
		h.API.Handle("POST /v1/auth/logout", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
		post := app.ConfirmLogoutForTest(h.Model)
		if post == nil {
			t.Fatal("the confirmed logout must return the POST Cmd")
		}
		h.Send(ui.RealtimeStateMsg{State: realtime.State{Phase: realtime.PhaseStopped, Err: realtime.ErrUnauthorized, LastClose: 4001}})
		h.Send(post())
		if r := h.Model.Frame().Routes[layout.SlotMain]; len(r.Query) != 0 {
			t.Errorf("login route %+v: the user's own logout has no notice", r)
		}
		if strings.Contains(h.Screen(), unauthorizedNotice) {
			t.Errorf("the user's own logout must not say \"logged out elsewhere\":\n%s", h.Screen())
		}
		assertLoggedOut(t, h, cred, "")
		if r := loggedOutReasons(*got); !slices.Equal(r, []string{"logout"}) {
			t.Errorf("LoggedOutMsg reasons = %v, want [logout] once", r)
		}
	})
}

func TestIdleDetection(t *testing.T) {
	h := uitest.New(t)
	rt := liveRT()
	app.SetRealtimeForTest(h.Model, rt)
	h.Resize(120, 40) // first Update with live realtime arms the idle timer

	h.Advance(10 * time.Minute)
	if got := rt.idles(); !slices.Equal(got, []bool{true}) {
		t.Fatalf("after 10m: SetIdle = %v, want [true]", got)
	}
	h.Press("j")
	if got := rt.idles(); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("input: SetIdle = %v, want [true false]", got)
	}
	h.Advance(9 * time.Minute)
	h.Press("j")
	h.Advance(9 * time.Minute)
	if got := rt.idles(); !slices.Equal(got, []bool{true, false}) {
		t.Fatalf("input within 10m: SetIdle = %v, want no new idle", got)
	}
	h.Advance(time.Minute)
	if got := rt.idles(); !slices.Equal(got, []bool{true, false, true}) {
		t.Fatalf("10m after the last input: SetIdle = %v", got)
	}
	if n := countDur(h.Clock().Pending(), 10*time.Minute) + countDur(h.Clock().Pending(), 9*time.Minute); n > 1 {
		t.Errorf("at most one idle timer pending, got %v", h.Clock().Pending())
	}
}

func countDur(ds []time.Duration, d time.Duration) int {
	n := 0
	for _, x := range ds {
		if x == d {
			n++
		}
	}
	return n
}

func TestWatchDebounced(t *testing.T) {
	h := uitest.New(t)
	rt := liveRT()
	app.SetRealtimeForTest(h.Model, rt)
	h.Resize(200, 60)
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTrends}})
	if r := h.Model.Frame().Routes[layout.SlotFeed]; r.Kind != ui.RouteTrends {
		t.Fatalf("trends must sit in the feed pane, routes = %+v", h.Model.Frame().Routes)
	}
	h.Advance(299 * time.Millisecond)
	if w := rt.watches(); len(w) != 0 {
		t.Fatalf("watch before the 300ms debounce: %v", w)
	}
	h.Advance(time.Millisecond)
	if w := rt.watches(); len(w) != 1 || !slices.Equal(w[0], []string{"post:A", "post:B"}) {
		t.Fatalf("watch = %v, want [[post:A post:B]]", w)
	}
	h.Resize(200, 60)
	h.Advance(300 * time.Millisecond)
	if w := rt.watches(); len(w) != 1 {
		t.Fatalf("unchanged topics must not be re-sent: %v", w)
	}

	// Two watchers: the focused pane's topics first, deduplicated, capped at 50.
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTrends, ID: "main"}, Target: ui.TargetMain})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTrends}}) // focus back to the feed pane
	if f := h.Model.Focus(); f.Slot != layout.SlotFeed {
		t.Fatalf("focus = %+v, want the feed pane", f)
	}
	mainP := h.Instance(ui.Route{Kind: ui.RouteTrends, ID: "main"}.Key()).(*watchProbe)
	feedP := h.Instance(ui.Route{Kind: ui.RouteTrends}.Key()).(*watchProbe)
	mainP.Topics, feedP.Topics = nil, nil
	for i := range 30 {
		mainP.Topics = append(mainP.Topics, fmt.Sprintf("post:M%02d", i))
		feedP.Topics = append(feedP.Topics, fmt.Sprintf("post:F%02d", i))
	}
	feedP.Topics = append(feedP.Topics, "post:F00") // duplicate
	h.Resize(200, 60)
	h.Advance(300 * time.Millisecond)
	w := rt.watches()
	last := w[len(w)-1]
	if len(last) != 50 {
		t.Fatalf("watch len = %d, want 50 (cap)", len(last))
	}
	want := append(slices.Clone(feedP.Topics[:30]), mainP.Topics[:20]...)
	if !slices.Equal(last, want) {
		t.Fatalf("watch = %v\nwant    %v", last, want)
	}
}

func TestSetViewing(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(channelRoute(fixtures.ChDev)))
	st := h.Context().Store
	if !st.Viewing(fixtures.ChDev) || st.Viewing(fixtures.ChGeneral) {
		t.Fatalf("#개발 visible: Viewing(dev)=%v Viewing(general)=%v", st.Viewing(fixtures.ChDev), st.Viewing(fixtures.ChGeneral))
	}
	h.Resize(120, 30) // compact
	if !st.Viewing(fixtures.ChDev) {
		t.Fatalf("compact #개발: Viewing(dev) = false")
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteHome}})
	if st.Viewing(fixtures.ChDev) {
		t.Fatalf("compact home: Viewing(dev) must be false")
	}
	h.Resize(10, 4) // too small: nothing visible
	if st.Viewing(fixtures.ChDev) {
		t.Fatalf("too small: Viewing(dev) must be false")
	}
}

func TestSetViewingBeforeBroadcast(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(channelRoute(fixtures.ChDev)))
	st := h.Context().Store
	h.Resize(120, 30) // compact
	h.Press("j")
	h.Press("t") // the thread replaces the channel in Main
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteThread {
		t.Fatalf("compact Main = %+v, want the thread", r)
	}
	if st.Viewing(fixtures.ChDev) {
		t.Fatalf("compact thread: Viewing(dev) must be false (a stub thread is not a ChannelViewer)")
	}

	type rec struct {
		msg     string
		viewing bool
	}
	var recs []rec
	record := func(id string) func(*ui.Context, tea.Msg) {
		return func(ctx *ui.Context, msg tea.Msg) {
			switch m := msg.(type) {
			case ui.ModeChangedMsg:
				recs = append(recs, rec{fmt.Sprintf("mode:%s->%s", m.From.ID(), m.To.ID()), ctx.Store.Viewing(id)})
			case ui.FocusChangedMsg:
				recs = append(recs, rec{"focus:" + m.RouteKey, ctx.Store.Viewing(id)})
			case ui.RouteUpdatedMsg:
				recs = append(recs, rec{"route:" + m.Route.Key(), ctx.Store.Viewing(id)})
			case ui.EventMsg:
				recs = append(recs, rec{"event:" + m.Event.Type, ctx.Store.Viewing(id)})
			}
		}
	}
	dev := h.Instance(channelKey(fixtures.ChDev)).(*stub.Chat)
	dev.OnUpdate = record(fixtures.ChDev)

	h.Resize(200, 60) // command: the channel is back in Main beside the thread pane
	if len(recs) == 0 || recs[0].msg != "mode:compact->command" {
		t.Fatalf("records = %+v, want ModeChangedMsg first", recs)
	}
	for _, r := range recs {
		if !r.viewing {
			t.Fatalf("command: %s saw Viewing(dev)=false (stale projection); all = %+v", r.msg, recs)
		}
	}

	recs = nil
	h.Resize(120, 30)
	if len(recs) == 0 || recs[0].msg != "mode:command->compact" || recs[0].viewing {
		t.Fatalf("back to compact: records = %+v, want ModeChangedMsg with Viewing(dev)=false", recs)
	}

	// Focus change at 200x60: general's instance records Viewing for itself and dev.
	h.Resize(200, 60)
	h.Send(ui.NavigateMsg{Route: channelRoute(fixtures.ChGeneral)})
	h.Send(ui.NavigateMsg{Route: channelRoute(fixtures.ChDev)})
	gen := h.Instance(channelKey(fixtures.ChGeneral)).(*stub.Chat)
	var genRecs []string
	gen.OnUpdate = func(ctx *ui.Context, msg tea.Msg) {
		if m, ok := msg.(ui.FocusChangedMsg); ok {
			genRecs = append(genRecs, fmt.Sprintf("%s general=%v dev=%v", m.RouteKey, ctx.Store.Viewing(fixtures.ChGeneral), ctx.Store.Viewing(fixtures.ChDev)))
		}
	}
	h.Send(ui.NavigateMsg{Route: channelRoute(fixtures.ChGeneral)})
	want := channelKey(fixtures.ChGeneral) + " general=true dev=false"
	if !slices.Equal(genRecs, []string{want}) {
		t.Fatalf("general FocusChangedMsg records = %v, want [%s]", genRecs, want)
	}

	// An event in a visible channel: Viewing is already this projection's set.
	h.Send(ui.NavigateMsg{Route: channelRoute(fixtures.ChDev)})
	recs = nil
	minji := *st.User(fixtures.UserMinji)
	h.Event("message.created", realtime.MessageEvent{Message: api.Message{
		ID: "01JDDEV0000000000000000099", ChannelID: fixtures.ChDev, Author: minji, Kind: "text", Body: "ㅇㅇ", CreatedAt: h.Clock().Now(),
	}})
	if !slices.Contains(recs, rec{"event:message.created", true}) {
		t.Fatalf("event records = %+v, want message.created with Viewing(dev)=true", recs)
	}
}

// TestLoggedOutInitialLoginRoute: without credentials an InitialRoute of kind
// RouteLogin picks the form (`masil signup` => Arg "signup"); any other initial
// route still shows the plain login screen.
func TestLoggedOutInitialLoginRoute(t *testing.T) {
	h := uitest.New(t, uitest.WithCredentials(nil), uitest.WithRoute(ui.Route{Kind: ui.RouteLogin, Arg: "signup"}))
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteLogin || r.Arg != "signup" {
		t.Fatalf("main route = %+v, want login/signup", r)
	}
	if !strings.Contains(h.Screen(), "비밀번호 확인") {
		t.Fatalf("signup form not shown:\n%s", h.Screen())
	}

	h = uitest.New(t, uitest.WithCredentials(nil), uitest.WithRoute(ui.Route{Kind: ui.RouteInvite, ID: "AbC12345"}))
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteLogin || r.Arg != "" || r.ID != "" {
		t.Fatalf("main route = %+v, want the plain login screen", r)
	}
}

// C1 (v0.1.1): navigating again to a route with the same Anchor still reaches the
// cached instance as a RouteUpdatedMsg (the jump is asked for again: the friends
// screen goes back to 받은 요청, a message link re-selects its message), while a
// relayout with no navigation (a resize) sends none.
func TestSameAnchorNavigateResendsRouteUpdated(t *testing.T) {
	h := uitest.New(t, uitest.WithRoute(channelRoute(fixtures.ChDev)))
	h.Resize(120, 40)
	anchored := channelRoute(fixtures.ChDev)
	anchored.Anchor = "M1"
	h.Send(ui.NavigateMsg{Route: anchored})
	dev := h.Instance(channelKey(fixtures.ChDev)).(*stub.Chat)
	var got []string
	dev.OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		if m, ok := msg.(ui.RouteUpdatedMsg); ok {
			got = append(got, m.Route.Anchor)
		}
	}
	h.Send(ui.NavigateMsg{Route: anchored})
	if !slices.Equal(got, []string{"M1"}) {
		t.Fatalf("same-Anchor navigate: RouteUpdatedMsg anchors = %v, want [M1]", got)
	}
	got = nil
	h.Resize(121, 40)
	h.Resize(120, 40)
	if len(got) != 0 {
		t.Fatalf("a resize re-sent RouteUpdatedMsg %v", got)
	}
}
