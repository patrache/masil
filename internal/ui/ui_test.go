package ui_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

func TestRouteKey(t *testing.T) {
	cases := []struct {
		r    ui.Route
		want string
	}{
		{ui.Route{Kind: ui.RouteChannel, ID: "C", Anchor: "M", Query: map[string]string{"a": "b"}}, "channel/C/"},
		{ui.Route{Kind: ui.RouteFeed, Arg: "following"}, "feed//following"},
		{ui.Route{Kind: ui.RouteThread, ID: "R", Arg: "C"}, "thread/R/C"},
		{ui.Route{Kind: ui.RouteNotifications}, "notifications//"},
	}
	for _, c := range cases {
		if got := c.r.Key(); got != c.want {
			t.Errorf("%+v.Key() = %q, want %q", c.r, got, c.want)
		}
	}
	// Anchor/Query never change the key.
	a := ui.Route{Kind: ui.RoutePost, ID: "P", Anchor: "x"}
	b := ui.Route{Kind: ui.RoutePost, ID: "P", Query: map[string]string{"mode": "quote"}}
	if a.Key() != b.Key() {
		t.Errorf("Anchor/Query changed key: %q vs %q", a.Key(), b.Key())
	}
}

type fakeOverlay struct{ ui.Overlay }

func run(t *testing.T, name string, c tea.Cmd) tea.Msg {
	t.Helper()
	if c == nil {
		t.Fatalf("%s: cmd is nil", name)
	}
	return c()
}

func TestHelpersEmitMsgs(t *testing.T) {
	r := ui.Route{Kind: ui.RouteChannel, ID: "C1"}
	o := &fakeOverlay{}
	cases := []struct {
		name string
		cmd  tea.Cmd
		want tea.Msg
	}{
		{"Navigate", ui.Navigate(r), ui.NavigateMsg{Route: r}},
		{"NavigateTo", ui.NavigateTo(r, ui.TargetSide), ui.NavigateMsg{Route: r, Target: ui.TargetSide}},
		{"Back", ui.Back(), ui.BackMsg{}},
		{"Toast", ui.Toast("x", ui.ToastError), ui.ToastMsg{Text: "x", Kind: ui.ToastError}},
		{"RunCommand", ui.RunCommand("core.theme", "amber"), ui.RunCommandMsg{ID: "core.theme", Args: "amber"}},
		{"OpenOverlay", ui.OpenOverlay(o), ui.OpenOverlayMsg{Overlay: o}},
		{"OpenRegisteredOverlay", ui.OpenRegisteredOverlay("fun.x", "a"), ui.OpenOverlayMsg{ID: "fun.x", Args: "a"}},
	}
	for _, c := range cases {
		got := run(t, c.name, c.cmd)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s() = %#v, want %#v", c.name, got, c.want)
		}
	}
}

type afterCall struct {
	d   time.Duration
	msg tea.Msg
}

// recTimers records After calls and returns a marker cmd.
type recTimers struct {
	calls []afterCall
}

type markerMsg struct{ n int }

func (r *recTimers) After(d time.Duration, msg tea.Msg) tea.Cmd {
	r.calls = append(r.calls, afterCall{d, msg})
	n := len(r.calls)
	return func() tea.Msg { return markerMsg{n} }
}

type pingMsg struct{}

func TestAfterUsesContextTimers(t *testing.T) {
	rec := &recTimers{}
	ctx := &ui.Context{Timers: rec}
	c := ui.After(ctx, 3*time.Second, pingMsg{})
	if got := run(t, "After", c); got != (markerMsg{1}) {
		t.Fatalf("After did not return the Timers' cmd: got %#v", got)
	}
	if len(rec.calls) != 1 || rec.calls[0].d != 3*time.Second || rec.calls[0].msg != (pingMsg{}) {
		t.Fatalf("recorded calls = %#v", rec.calls)
	}

	rt := ui.RealTimers()
	if rt == nil {
		t.Fatal("RealTimers() is nil")
	}
	start := time.Now()
	if got := run(t, "RealTimers.After", rt.After(time.Millisecond, pingMsg{})); got != (pingMsg{}) {
		t.Fatalf("RealTimers().After()() = %#v, want pingMsg{}", got)
	}
	if time.Since(start) < time.Millisecond {
		t.Fatal("RealTimers().After returned before d elapsed")
	}
}

func TestDoRunsWithContextAPI(t *testing.T) {
	srv := apitest.New(t)
	srv.JSON("GET /v1/meta", 200, map[string]any{
		"api_version":        1,
		"server_time":        "2026-11-05T02:05:00Z",
		"min_client_version": "0.1.0",
		"limits":             map[string]any{"message_max_chars": 4000},
	})
	ctx := &ui.Context{API: srv.Client()}
	c := ui.Do(ctx, "core.x", func(c context.Context, a *api.Client) (*api.MetaInfo, error) {
		return a.Meta(c)
	})
	msg := run(t, "Do", c)
	res, ok := msg.(ui.ResultMsg[*api.MetaInfo])
	if !ok {
		t.Fatalf("Do()() = %T, want ui.ResultMsg[*api.MetaInfo]", msg)
	}
	if res.Tag != "core.x" || res.Err != nil || res.Value == nil {
		t.Fatalf("result = %+v", res)
	}
	if res.Value.APIVersion != 1 || res.Value.Limits.MessageMaxChars != 4000 {
		t.Fatalf("value = %+v", *res.Value)
	}

	// Errors are carried, not swallowed.
	srv.Error("GET /v1/me", 401, "unauthorized")
	msg = run(t, "Do err", ui.Do(ctx, "core.me", func(c context.Context, a *api.Client) (*api.Me, error) {
		return a.GetMe(c)
	}))
	er := msg.(ui.ResultMsg[*api.Me])
	if er.Tag != "core.me" || er.Err == nil || er.Value != nil {
		t.Fatalf("error result = %+v", er)
	}
}

func TestDensity(t *testing.T) {
	cases := map[layout.Mode]ui.Density{
		layout.ModeCommand:  ui.DensityFull,
		layout.ModeStandard: ui.DensityFull,
		layout.ModeCompact:  ui.DensityLine,
		layout.ModeWidget:   ui.DensityBubble,
		layout.ModeTiny:     ui.DensityMinimal,
		layout.ModeTooSmall: ui.DensityMinimal,
	}
	for m, want := range cases {
		ctx := &ui.Context{Mode: m}
		if got := ctx.Density(); got != want {
			t.Errorf("Density(%v) = %v, want %v", m.ID(), got, want)
		}
	}
}

func TestMe(t *testing.T) {
	if (&ui.Context{}).Me() != nil {
		t.Fatal("Me() without store must be nil")
	}
	st := store.New()
	ctx := &ui.Context{Store: st}
	if ctx.Me() != nil {
		t.Fatal("Me() before login must be nil")
	}
	me := &api.Me{}
	me.ID = "U1"
	st.Me = me
	if ctx.Me() != me {
		t.Fatal("Me() must return Store.Me")
	}
}

type completingProbe struct{}

func (completingProbe) Completing() bool { return true }

type notCompleter struct{}

func TestCompleterShape(t *testing.T) {
	var _ ui.Completer = completingProbe{}
	if c, ok := any(completingProbe{}).(ui.Completer); !ok || !c.Completing() {
		t.Fatal("completingProbe must satisfy ui.Completer")
	}
	if _, ok := any(notCompleter{}).(ui.Completer); ok {
		t.Fatal("a type without Completing() must not satisfy ui.Completer")
	}
}
