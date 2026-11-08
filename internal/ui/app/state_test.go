package app_test

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// ---- helpers (state.json) ----

func stChannel(id string) ui.Route { return ui.Route{Kind: ui.RouteChannel, ID: id} }

// statePaths points MASIL_CONFIG_DIR/MASIL_CACHE_DIR at fresh temp dirs.
func statePaths(t *testing.T) config.Paths {
	t.Helper()
	t.Setenv("MASIL_CONFIG_DIR", t.TempDir())
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if err := config.EnsureDir(p); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	return p
}

func credsFor(userID, handle string) *auth.Credentials {
	return &auth.Credentials{Version: 1, Server: "http://127.0.0.1:1", Token: "msl_test", UserID: userID, Handle: handle}
}

// newStateModel builds an app.Model on p directly (the harness always uses a fresh
// config dir, so a "restart" on the same dir is built here), seeded with the fixtures.
func newStateModel(t *testing.T, p config.Paths, cfg *config.Config, creds *auth.Credentials) *app.Model {
	t.Helper()
	if cfg == nil {
		cfg = config.Defaults()
	}
	m := app.New(app.Options{
		Paths:           p,
		Config:          cfg,
		Credentials:     creds,
		Now:             func() time.Time { return uitest.Start },
		Loc:             time.UTC,
		DisableRealtime: true,
		Notifier:        notify.New(&notify.Recorder{}, notify.OpenLedger(p), slog.New(slog.DiscardHandler)),
		Timers:          uitest.NewFakeClock(uitest.Start),
	})
	fixtures.Seed(m.Context().Store)
	return m
}

// drain runs cmd (expanding batches) and returns every msg it produced, without
// feeding them back.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, drain(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

func readStateMap(t *testing.T, p config.Paths) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p.State)
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("state.json is not JSON: %v\n%s", err, b)
	}
	return got
}

func writeRaw(t *testing.T, p config.Paths, s string) {
	t.Helper()
	if err := config.WriteFileAtomic(p.State, []byte(s), 0o600); err != nil {
		t.Fatalf("write state.json: %v", err)
	}
}

func navKeys(n app.NavState) []string {
	var out []string
	for _, tb := range n.Tabs {
		out = append(out, tb.Stack[0].Key())
	}
	return out
}

// ---- tests ----

func TestSavedStateShape(t *testing.T) {
	h := uitest.New(t)
	h.Send(ui.NavigateMsg{Route: stChannel(fixtures.ChGeneral)})
	h.Send(ui.NavigateMsg{Route: stChannel(fixtures.ChDev)})
	// Stack tops and threads are not remembered (CA 3.5: roots only).
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteThread, ID: "01JDDEV0000000000000000001", Arg: fixtures.ChDev}})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteSearch, Arg: "배포"}})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "following"}})
	h.Send(ui.RunCommandMsg{ID: "layout.pin.compact"})
	if h.Nav().Front != app.FrontFeed || h.Context().Mode != layout.ModeCompact {
		t.Fatalf("setup: front %v mode %v", h.Nav().Front, h.Context().Mode)
	}
	if err := app.WriteStateForTest(h.Model); err != nil {
		t.Fatalf("WriteStateForTest: %v", err)
	}
	p := h.Context().Paths
	got := readStateMap(t, p)
	want := map[string]any{
		"version": 1.0,
		"user_id": fixtures.UserSam,
		"open_tabs": []any{
			map[string]any{"kind": "home"},
			map[string]any{"kind": "channel", "id": fixtures.ChGeneral},
			map[string]any{"kind": "channel", "id": fixtures.ChDev},
		},
		"active_tab":    2.0,
		"front":         "feed",
		"last_space_id": fixtures.SpaceNova,
		"feed_tab":      "following",
		"pinned_mode":   "compact",
	}
	if !reflect.DeepEqual(got, want) {
		gb, _ := json.Marshal(got)
		wb, _ := json.Marshal(want)
		t.Fatalf("state.json =\n%s\nwant\n%s", gb, wb)
	}
	if fi, err := os.Stat(p.State); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) { // Windows has no POSIX modes
		t.Fatalf("state.json perm = %v (%v), want 0600", fi.Mode().Perm(), err)
	}

	// Unpinned, notifications in front, discover feed.
	h.Send(ui.RunCommandMsg{ID: "layout.auto"})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "discover"}})
	h.Send(ui.NavigateMsg{Route: app.NotifRoute})
	if err := app.WriteStateForTest(h.Model); err != nil {
		t.Fatalf("WriteStateForTest: %v", err)
	}
	got = readStateMap(t, p)
	if got["pinned_mode"] != "auto" || got["front"] != "notifications" || got["feed_tab"] != "discover" {
		t.Fatalf("pinned_mode %v front %v feed_tab %v", got["pinned_mode"], got["front"], got["feed_tab"])
	}
}

func TestRestoreOnStart(t *testing.T) {
	h := uitest.New(t)
	h.Send(ui.NavigateMsg{Route: stChannel(fixtures.ChGeneral)})
	h.Send(ui.NavigateMsg{Route: stChannel(fixtures.ChDev)})
	h.Send(ui.NavigateMsg{Route: stChannel(fixtures.ChGeneral)}) // active = #일반 (index 1)
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteThread, ID: "01JDGEN0000000000000000001", Arg: fixtures.ChGeneral}})
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "discover"}})
	h.Send(ui.RunCommandMsg{ID: "layout.pin.compact"})
	if err := app.WriteStateForTest(h.Model); err != nil {
		t.Fatalf("WriteStateForTest: %v", err)
	}
	p := h.Context().Paths

	m := newStateModel(t, p, nil, credsFor(fixtures.UserSam, "sam"))
	n := m.Nav()
	wantTabs := []string{"home//", stChannel(fixtures.ChGeneral).Key(), stChannel(fixtures.ChDev).Key()}
	if !reflect.DeepEqual(navKeys(n), wantTabs) || n.Active != 1 {
		t.Fatalf("tabs %v active %d, want %v active 1", navKeys(n), n.Active, wantTabs)
	}
	for i, tb := range n.Tabs {
		if len(tb.Stack) != 1 || tb.Thread != nil {
			t.Fatalf("tab %d restored with stack %v thread %v, want root only", i, tb.Stack, tb.Thread)
		}
	}
	if n.Front != app.FrontFeed || len(n.Feed) != 1 || n.Feed[0].Key() != (ui.Route{Kind: ui.RouteFeed, Arg: "discover"}).Key() {
		t.Fatalf("front %v feed %v", n.Front, n.Feed)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if got := m.Context().Mode; got != layout.ModeCompact {
		t.Fatalf("mode at 120x40 = %v, want the restored pin compact", got)
	}

	// The restored state is written back unchanged.
	before := readStateMap(t, p)
	if err := app.WriteStateForTest(m); err != nil {
		t.Fatalf("WriteStateForTest: %v", err)
	}
	if after := readStateMap(t, p); !reflect.DeepEqual(before, after) {
		t.Fatalf("round trip changed state.json:\n%v\n%v", before, after)
	}

	// An initial route (masil join) still applies on top of the restored tabs.
	inv := ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDeploy}
	m2 := app.New(app.Options{
		Paths: p, Config: config.Defaults(), Credentials: credsFor(fixtures.UserSam, "sam"), InitialRoute: &inv,
		DisableRealtime: true, Timers: uitest.NewFakeClock(uitest.Start),
		Notifier: notify.New(&notify.Recorder{}, notify.OpenLedger(p), slog.New(slog.DiscardHandler)),
	})
	if got := navKeys(m2.Nav()); !reflect.DeepEqual(got, append(wantTabs, inv.Key())) || m2.Nav().Active != 3 {
		t.Fatalf("with initial route: tabs %v active %d", got, m2.Nav().Active)
	}

	// `masil join <code>` (a non-root route) opens on the home tab, never on top of the
	// restored active channel: otherwise that channel later shows the stale invite card
	// (G2 gate: switching to the restored DM showed "방 초대").
	join := ui.Route{Kind: ui.RouteInvite, ID: "AbC12345"}
	m3 := app.New(app.Options{
		Paths: p, Config: config.Defaults(), Credentials: credsFor(fixtures.UserSam, "sam"), InitialRoute: &join,
		DisableRealtime: true, Timers: uitest.NewFakeClock(uitest.Start),
		Notifier: notify.New(&notify.Recorder{}, notify.OpenLedger(p), slog.New(slog.DiscardHandler)),
	})
	n3 := m3.Nav()
	if got := navKeys(n3); !reflect.DeepEqual(got, wantTabs) || n3.Active != 0 || n3.Front != app.FrontTab {
		t.Fatalf("join: tabs %v active %d front %v, want %v active 0 (home) front tab", got, n3.Active, n3.Front, wantTabs)
	}
	if st := n3.Tabs[0].Stack; len(st) != 2 || st[1].Key() != join.Key() {
		t.Fatalf("join: home stack %v, want home then the invite", st)
	}
	for i, tb := range n3.Tabs[1:] {
		if len(tb.Stack) != 1 {
			t.Fatalf("join: tab %d stack %v, want the restored root only", i+1, tb.Stack)
		}
	}
}

func TestRestoreIgnoresForeignOrCorruptState(t *testing.T) {
	defaultNav := func(t *testing.T, m *app.Model) {
		t.Helper()
		n := m.Nav()
		if !reflect.DeepEqual(navKeys(n), []string{"home//"}) || n.Active != 0 || n.Front != app.FrontTab ||
			len(n.Feed) != 1 || n.Feed[0].Arg != "following" {
			t.Fatalf("nav = %+v, want the default", n)
		}
		for _, msg := range drain(m.Init()) {
			switch msg.(type) {
			case ui.ToastMsg, ui.ErrorMsg:
				t.Fatalf("Init produced %#v, want no toast", msg)
			}
		}
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		if m.Context().Mode != layout.ModeStandard {
			t.Fatalf("mode = %v, want auto standard (no restored pin)", m.Context().Mode)
		}
	}
	valid := func(user string, version int) string {
		s := app.SavedState{
			Version: version, UserID: user,
			OpenTabs:  []app.SavedTab{{Kind: "home"}, {Kind: "channel", ID: fixtures.ChDev}},
			ActiveTab: 1, Front: "feed", FeedTab: "discover", PinnedMode: "compact",
		}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	cases := []struct{ name, body string }{
		{"foreign user", valid(fixtures.UserMinji, 1)},
		{"garbage", "{{garbage"},
		{"version 2", valid(fixtures.UserSam, 2)},
		{"empty", ""},
		{"json null", "null"},
		{"wrong types", `{"version":"1","user_id":7,"open_tabs":"x"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := statePaths(t)
			writeRaw(t, p, c.body)
			defaultNav(t, newStateModel(t, p, nil, credsFor(fixtures.UserSam, "sam")))
		})
	}
	t.Run("missing", func(t *testing.T) {
		p := statePaths(t)
		defaultNav(t, newStateModel(t, p, nil, credsFor(fixtures.UserSam, "sam")))
	})
	t.Run("logged out ignores it", func(t *testing.T) {
		p := statePaths(t)
		writeRaw(t, p, valid(fixtures.UserSam, 1))
		m := newStateModel(t, p, nil, nil)
		if got := navKeys(m.Nav()); !reflect.DeepEqual(got, []string{"home//"}) {
			t.Fatalf("logged out restored tabs %v", got)
		}
	})
	t.Run("invalid entries dropped", func(t *testing.T) {
		p := statePaths(t)
		writeRaw(t, p, `{"version":1,"user_id":"`+fixtures.UserSam+`","open_tabs":[`+
			`{"kind":"thread","id":"`+fixtures.ChDev+`"},{"kind":"channel","id":"x\u001b[31m"},{"kind":"channel"},`+
			`{"kind":"channel","id":"`+fixtures.ChDev+`"},{"kind":"channel","id":"`+fixtures.ChDev+`"},{"kind":"home","id":"zzz"}],`+
			`"active_tab":3,"front":"sideways","feed_tab":"../etc","pinned_mode":"huge"}`)
		m := newStateModel(t, p, nil, credsFor(fixtures.UserSam, "sam"))
		n := m.Nav()
		if want := []string{stChannel(fixtures.ChDev).Key(), "home//"}; !reflect.DeepEqual(navKeys(n), want) || n.Active != 0 {
			t.Fatalf("tabs %v active %d, want %v active 0", navKeys(n), n.Active, want)
		}
		if n.Front != app.FrontTab || n.Feed[0].Arg != "following" {
			t.Fatalf("front %v feed %v", n.Front, n.Feed)
		}
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		if m.Context().Mode != layout.ModeStandard {
			t.Fatalf("mode = %v, want auto", m.Context().Mode)
		}
	})
	t.Run("active out of range", func(t *testing.T) {
		p := statePaths(t)
		writeRaw(t, p, `{"version":1,"user_id":"`+fixtures.UserSam+`","open_tabs":[{"kind":"home"},{"kind":"channel","id":"`+
			fixtures.ChDev+`"}],"active_tab":9,"front":"tab","feed_tab":"following","pinned_mode":"auto"}`)
		m := newStateModel(t, p, nil, credsFor(fixtures.UserSam, "sam"))
		if n := m.Nav(); len(n.Tabs) != 2 || n.Active != 0 {
			t.Fatalf("tabs %v active %d", navKeys(n), n.Active)
		}
	})
}

func TestConcurrentSaveStateStaysValid(t *testing.T) {
	p := statePaths(t)
	a := newStateModel(t, p, nil, credsFor(fixtures.UserSam, "sam"))
	b := newStateModel(t, p, nil, credsFor(fixtures.UserSam, "sam"))
	for _, m := range []*app.Model{a, b} {
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	}
	a.Update(ui.NavigateMsg{Route: stChannel(fixtures.ChGeneral)})
	b.Update(ui.NavigateMsg{Route: stChannel(fixtures.ChDev)})
	b.Update(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteFeed, Arg: "discover"}})

	snapshot := func(m *app.Model) map[string]any {
		if err := app.WriteStateForTest(m); err != nil {
			t.Fatalf("WriteStateForTest: %v", err)
		}
		return readStateMap(t, p)
	}
	wantA, wantB := snapshot(a), snapshot(b)
	if reflect.DeepEqual(wantA, wantB) {
		t.Fatal("setup: both models save the same state")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for _, m := range []*app.Model{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if err := app.WriteStateForTest(m); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent WriteStateForTest: %v", err)
	}
	got := readStateMap(t, p)
	if !reflect.DeepEqual(got, wantA) && !reflect.DeepEqual(got, wantB) {
		t.Fatalf("final state.json %v equals neither writer", got)
	}
	entries, err := os.ReadDir(filepath.Dir(p.State))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".state.json.tmp-") {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}

func TestPinStartupPrecedence(t *testing.T) {
	cases := []struct {
		name, statePin, cfgPin string
		want                   layout.Mode
	}{
		{"state auto, config widget", "auto", "widget", layout.ModeWidget},
		{"state compact, config widget", "compact", "widget", layout.ModeCompact},
		{"state compact, config auto", "compact", "auto", layout.ModeCompact},
		{"state auto, config auto", "auto", "auto", layout.ModeStandard},
		{"no state, config widget", "", "widget", layout.ModeWidget},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := statePaths(t)
			if c.statePin != "" {
				b, err := json.Marshal(app.SavedState{Version: 1, UserID: fixtures.UserSam,
					OpenTabs: []app.SavedTab{{Kind: "home"}}, Front: "tab", FeedTab: "following", PinnedMode: c.statePin})
				if err != nil {
					t.Fatal(err)
				}
				writeRaw(t, p, string(b))
			}
			cfg := config.Defaults()
			cfg.Layout.Pin = c.cfgPin
			m := newStateModel(t, p, cfg, credsFor(fixtures.UserSam, "sam"))
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			if got := m.Context().Mode; got != c.want {
				t.Fatalf("mode = %v, want %v", got, c.want)
			}
		})
	}
}
