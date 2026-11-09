package feed

import (
	"fmt"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// paletteOpts is the canned sns server with the default home tab (no route).
func paletteOpts(t *testing.T) []uitest.Option {
	t.Helper()
	srv := snsAPI(t, false)
	composeHandlers(srv)
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed)}
}

func TestPaletteCommandsRun(t *testing.T) {
	feedTo := func(r ui.Route) func(t *testing.T, h *uitest.Harness) {
		return func(t *testing.T, h *uitest.Harness) {
			t.Helper()
			if top := feedTop(h); top.Key() != r.Key() {
				t.Fatalf("feed top = %+v, want %+v", top, r)
			}
			if f := h.Nav().Front; f != app.FrontFeed {
				t.Fatalf("Front = %v, want FrontFeed", f)
			}
			if len(h.Model.Frame().Overlays) != 0 {
				t.Fatalf("overlays = %+v, want none", h.Model.Frame().Overlays)
			}
		}
	}
	overlay := func(title string) func(t *testing.T, h *uitest.Harness) {
		return func(t *testing.T, h *uitest.Harness) {
			t.Helper()
			if n := len(h.Model.Frame().Overlays); n != 1 {
				t.Fatalf("overlays = %d, want 1", n)
			}
			if s := h.Screen(); !strings.Contains(s, title) {
				t.Fatalf("overlay %q not shown:\n%s", title, s)
			}
			if f := h.Nav().Front; f != app.FrontTab {
				t.Fatalf("Front = %v, want FrontTab (an overlay command does not navigate)", f)
			}
		}
	}
	rows := []struct {
		id    string
		check func(t *testing.T, h *uitest.Harness)
	}{
		{"post.new", feedTo(ui.Route{Kind: ui.RouteCompose, Arg: "post"})},
		{"story.new", feedTo(ui.Route{Kind: ui.RouteCompose, Arg: "story"})},
		{"feed.following", feedTo(ui.Route{Kind: ui.RouteFeed, Arg: "following"})},
		{"feed.discover", feedTo(ui.Route{Kind: ui.RouteFeed, Arg: "discover"})},
		{"trends", feedTo(ui.Route{Kind: ui.RouteTrends})},
		{"notifications", func(t *testing.T, h *uitest.Harness) {
			if f := h.Nav().Front; f != app.FrontNotifications {
				t.Fatalf("Front = %v, want FrontNotifications", f)
			}
			if inst := h.Instance(app.NotifRoute.Key()); inst == nil {
				t.Fatal("no notifications instance")
			}
		}},
		{"profile.me", feedTo(ui.Route{Kind: ui.RouteProfile, ID: fixtures.UserSam})},
		{"profile.edit", overlay(textEditTitle)},
		{"follow_requests", overlay(textFollowReqsTitle)},
	}
	for _, r := range rows {
		t.Run(r.id, func(t *testing.T) {
			if _, ok := palette.Lookup(r.id); !ok {
				t.Fatalf("palette command %q not registered", r.id)
			}
			h := uitest.New(t, paletteOpts(t)...)
			h.Send(ui.RunCommandMsg{ID: r.id})
			r.check(t, h)
			h.Check()
		})
	}
}

func TestPaletteCommandMeta(t *testing.T) {
	want := map[string]struct{ title, group, hint string }{
		"post.new":        {"새 글 쓰기", palette.GroupCommand, "c"},
		"story.new":       {"새 스토리 올리기", palette.GroupCommand, ""},
		"feed.following":  {"피드: 팔로잉", palette.GroupNav, "f"},
		"feed.discover":   {"피드: 전체(발견)", palette.GroupNav, ""},
		"trends":          {"트렌드 Top 10", palette.GroupNav, ""},
		"notifications":   {"알림함", palette.GroupNav, "ctrl+n"},
		"profile.me":      {"내 프로필", palette.GroupNav, ""},
		"profile.edit":    {"프로필 편집", palette.GroupSettings, ""},
		"follow_requests": {"팔로우 요청", palette.GroupCommand, ""},
	}
	for id, w := range want {
		c, ok := palette.Lookup(id)
		if !ok {
			t.Fatalf("%s not registered", id)
		}
		if c.Title != w.title || c.Group != w.group || c.KeyHint != w.hint || c.Run == nil {
			t.Errorf("%s = {%q %q %q run=%v}, want {%q %q %q run}", id, c.Title, c.Group, c.KeyHint, c.Run != nil, w.title, w.group, w.hint)
		}
	}
}

func TestPaletteFindsSnsCommands(t *testing.T) {
	h := uitest.New(t, paletteOpts(t)...)
	h.Press("ctrl+k")
	h.Type("새 글")
	if s := h.Screen(); !strings.Contains(s, "새 글 쓰기") {
		t.Fatalf("palette lacks 새 글 쓰기:\n%s", s)
	}
	h.Check()
	h.Press("esc")
	h.Press("ctrl+k")
	h.Type("ㅌㄹㄷ")
	if s := h.Screen(); !strings.Contains(s, "트렌드 Top 10") {
		t.Fatalf("chosung search lacks 트렌드 Top 10:\n%s", s)
	}
	h.Check()
}

func TestPaletteHashtagSource(t *testing.T) {
	h := uitest.New(t, paletteOpts(t)...)
	feedstore.Get(h.Context().Store).SetTrends(trendsTop10())
	before := len(h.API.Requests())
	h.Press("ctrl+k")
	h.Type("#첫")
	s := h.Screen()
	if !strings.Contains(s, "#첫눈") || !strings.Contains(s, "트렌드 1위") {
		t.Fatalf("palette lacks the trend #첫눈:\n%s", s)
	}
	if n := len(h.API.Requests()); n != before {
		t.Fatalf("typing a #tag made %d requests: %+v", n-before, h.API.Requests()[before:])
	}
	h.Check()
	h.Press("enter")
	if top := feedTop(h); top.Kind != ui.RouteHashtag || top.ID != "첫눈" {
		t.Fatalf("feed top = %+v, want hashtag 첫눈", top)
	}
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("palette still open after enter")
	}

	h.Press("ctrl+k")
	before = len(h.API.Requests())
	h.Type("#없는태그")
	s = h.Screen()
	if !strings.Contains(s, "#없는태그") || !strings.Contains(s, "태그 피드 열기") {
		t.Fatalf("palette lacks the open-tag item:\n%s", s)
	}
	for rank := 1; rank <= 10; rank++ {
		if w := fmt.Sprintf("트렌드 %d위", rank); strings.Contains(s, w) {
			t.Fatalf("an unrelated trend (%s) is listed:\n%s", w, s)
		}
	}
	if n := len(h.API.Requests()); n != before {
		t.Fatalf("typing a #tag made %d requests", n-before)
	}
	h.Check()
}

func TestHashtagSourceItems(t *testing.T) {
	h := uitest.New(t, paletteOpts(t)...)
	ctx := h.Context()
	feedstore.Get(ctx.Store).SetTrends(trendsTop10())
	src := hashtagSource{}

	got := src.Items(ctx, "#첫")
	if len(got) != 2 {
		t.Fatalf("#첫 = %+v, want trend + open item", got)
	}
	tr, open := got[0], got[1]
	if tr.Kind != palette.ItemRoute || tr.Title != "#첫눈" || tr.Subtitle != "트렌드 1위" ||
		tr.Route == nil || tr.Route.Kind != ui.RouteHashtag || tr.Route.ID != "첫눈" {
		t.Fatalf("trend item = %+v", tr)
	}
	if open.Title != "#첫" || open.Subtitle != "태그 피드 열기" || open.Route == nil || open.Route.ID != "첫" {
		t.Fatalf("open item = %+v", open)
	}
	if tr.Score <= open.Score {
		t.Fatalf("trend score %d must outrank the open item %d", tr.Score, open.Score)
	}

	if got := src.Items(ctx, "#첫눈"); len(got) != 1 || got[0].Title != "#첫눈" || got[0].Subtitle != "트렌드 1위" {
		t.Fatalf("#첫눈 = %+v, want the trend only (exact tag => no open item)", got)
	}
	if got := src.Items(ctx, "#GO"); len(got) != 2 || got[0].Title != "#golang" || got[0].Subtitle != "트렌드 2위" {
		t.Fatalf("#GO = %+v, want #golang (case-insensitive prefix) + open", got)
	}
	if got := src.Items(ctx, "#없는태그"); len(got) != 1 || got[0].Title != "#없는태그" || got[0].Subtitle != "태그 피드 열기" {
		t.Fatalf("#없는태그 = %+v", got)
	}
	for _, q := range []string{"", "#", "첫눈", "a#b", "#" + strings.Repeat("가", 51)} {
		if got := src.Items(ctx, q); len(got) != 0 {
			t.Errorf("%q = %+v, want none", q, got)
		}
	}
	if got := src.Items(ctx, "#"+strings.Repeat("가", 50)); len(got) != 1 {
		t.Errorf("50-rune tag = %+v, want the open item", got)
	}

	// No trends loaded: only the open item.
	feedstore.Get(ctx.Store).SetTrends(nil)
	if got := src.Items(ctx, "#첫"); len(got) != 1 || got[0].Title != "#첫" {
		t.Fatalf("no trends: %+v", got)
	}
	if got := src.Items(nil, "#첫"); len(got) != 1 {
		t.Fatalf("nil ctx: %+v", got)
	}
}
