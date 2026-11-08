package palette_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func TestChosung(t *testing.T) {
	for in, want := range map[string]string{
		"개발":     "ㄱㅂ",
		"노바 개발방": "ㄴㅂ ㄱㅂㅂ",
		"abc가":   "abcㄱ",
		"":       "",
		"#일반":    "#ㅇㅂ",
		"까치 힣":   "ㄲㅊ ㅎ",
	} {
		if got := palette.Chosung(in); got != want {
			t.Errorf("Chosung(%q) = %q, want %q", in, got, want)
		}
	}
}

func titles(items []palette.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

func TestRanking(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	t.Cleanup(palette.ResetRecentForTest())
	h := uitest.New(t)
	ctx := h.Context()

	for _, q := range []string{"ㄱㅂ", "개발"} {
		items := palette.ItemsForTest(ctx, q)
		if len(items) == 0 || items[0].Title != "#개발" || items[0].Kind != palette.ItemChannel {
			t.Fatalf("query %q: items %q, want #개발 first", q, titles(items))
		}
		if r := items[0].Route; r == nil || r.Kind != ui.RouteChannel || r.ID != fixtures.ChDev {
			t.Fatalf("query %q: #개발 route = %+v", q, r)
		}
		if items[0].Subtitle != "노바 개발방" {
			t.Fatalf("#개발 subtitle = %q, want the space name", items[0].Subtitle)
		}
	}

	items := palette.ItemsForTest(ctx, "민지")
	dm, person := -1, -1
	for i, it := range items {
		if it.Title == "민지" && it.Kind == palette.ItemDM && dm < 0 {
			dm = i
		}
		if it.Title == "민지" && it.Kind == palette.ItemPerson && person < 0 {
			person = i
		}
	}
	if dm < 0 || person < 0 || dm > person {
		t.Fatalf("query 민지: items %q (dm %d person %d), want DM before person", titles(items), dm, person)
	}
	if r := items[person].Route; r == nil || r.Kind != ui.RouteProfile || r.ID != "@minji" {
		t.Fatalf("friend route = %+v, want profile @minji", r)
	}

	items = palette.ItemsForTest(ctx, "theme")
	if len(items) == 0 || items[0].Kind != palette.ItemCommand || !strings.HasPrefix(items[0].Title, "테마") {
		t.Fatalf("query theme: %q, want theme commands first", titles(items))
	}
	var themeCmds int
	for _, it := range items {
		if strings.HasPrefix(it.Title, "테마: ") {
			themeCmds++
			if it.Command == nil || !strings.HasPrefix(it.Command.ID, "theme.set.") {
				t.Fatalf("%q: Command = %+v", it.Title, it.Command)
			}
		}
	}
	if themeCmds != 4 {
		t.Fatalf("query theme: %d 테마: … commands, want 4 (%q)", themeCmds, titles(items))
	}

	avail := false
	palette.Register(palette.Command{ID: "t.hidden", Title: "숨은 명령", Group: "명령",
		Available: func(*ui.Context) bool { return avail }})
	if got := titles(palette.ItemsForTest(ctx, "숨은")); len(got) != 0 {
		t.Fatalf("Available false must exclude the command: %q", got)
	}
	avail = true
	if got := titles(palette.ItemsForTest(ctx, "숨은")); len(got) != 1 || got[0] != "숨은 명령" {
		t.Fatalf("Available true: %q", got)
	}

	// Empty query: unread channels first, then recently chosen routes (newest first, max 8),
	// then 홈 (widget/tiny/compact have no other way back to it).
	items = palette.ItemsForTest(ctx, "")
	if len(items) != 2 || items[0].Title != "#일반" || items[0].Badge.Count != 3 ||
		items[1].Route == nil || items[1].Route.Kind != ui.RouteHome {
		t.Fatalf("empty query: %+v, want #일반 with badge 3 then 홈", items)
	}
	if got := palette.ItemsForTest(ctx, "홈"); len(got) == 0 || got[0].Route == nil || got[0].Route.Kind != ui.RouteHome {
		t.Fatalf("query 홈: %+v, want the home route first", got)
	}
	for i := 1; i <= 9; i++ {
		palette.RememberForTest(palette.Item{Kind: palette.ItemRoute, Title: fmt.Sprintf("최근 %d", i),
			Route: &ui.Route{Kind: ui.RouteHashtag, ID: strconv.Itoa(i)}})
	}
	palette.RememberForTest(palette.Item{Kind: palette.ItemRoute, Title: "최근 3",
		Route: &ui.Route{Kind: ui.RouteHashtag, ID: "3"}}) // chosen again: moves to the front, no duplicate
	got := titles(palette.ItemsForTest(ctx, ""))
	want := []string{"#일반", "최근 3", "최근 9", "최근 8", "최근 7", "최근 6", "최근 5", "최근 4", "최근 2", "홈"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("empty query = %q, want %q", got, want)
	}
}

func usersSearches(h *uitest.Harness) []string {
	var out []string
	for _, r := range h.API.Requests() {
		if r.Method == "GET" && r.Path == "/v1/users/search" {
			out = append(out, r.Query)
		}
	}
	return out
}

func TestPeopleSearchDebounced(t *testing.T) {
	t.Cleanup(palette.ResetRecentForTest())
	h := uitest.New(t)
	h.Press("ctrl+k")
	h.Type("mi")
	h.Advance(299 * time.Millisecond)
	if got := usersSearches(h); len(got) != 0 {
		t.Fatalf("searched before the 300ms debounce: %q", got)
	}
	h.Advance(time.Millisecond)
	if got := usersSearches(h); len(got) != 1 || got[0] != "limit=8&q=mi" {
		t.Fatalf("after 300ms: searches %q, want one limit=8&q=mi", got)
	}

	h.Type("n")
	h.Advance(200 * time.Millisecond)
	h.Type("j")
	h.Advance(299 * time.Millisecond)
	if got := usersSearches(h); len(got) != 1 {
		t.Fatalf("superseded query searched: %q", got)
	}
	h.Advance(time.Millisecond)
	if got := usersSearches(h); len(got) != 2 || got[1] != "limit=8&q=minj" {
		t.Fatalf("searches %q, want only the latest query (minj) added", got)
	}
	if s := h.Screen(); !strings.Contains(s, "민지") || !strings.Contains(s, "@minji") {
		t.Fatalf("people result not shown:\n%s", s)
	}
	h.Check()

	h.Press("enter")
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("enter must close the palette, %d overlays", n)
	}
	feed := h.Nav().Feed
	if top := feed[len(feed)-1]; top.Kind != ui.RouteProfile || top.ID != "@minji" {
		t.Fatalf("enter on the person: feed top = %+v, want profile @minji", top)
	}

	// One rune never searches.
	h.Press("ctrl+k")
	h.Type("x")
	h.Advance(time.Second)
	if got := usersSearches(h); len(got) != 2 {
		t.Fatalf("one-rune query searched: %q", got)
	}
}

// TestPeopleSearchStaleResult: a result for an older query is ignored.
func TestPeopleSearchStaleResult(t *testing.T) {
	h := uitest.New(t)
	h.Press("ctrl+k")
	h.Type("ha")
	h.Advance(300 * time.Millisecond)
	if s := h.Screen(); !strings.Contains(s, "@hana") {
		t.Fatalf("ha: 하나 not found:\n%s", s)
	}
	h.Press("backspace", "backspace")
	h.Type("zz")
	h.Send(ui.ResultMsg[[]any]{Tag: "core.palette.people:ha"}) // wrong type: ignored, no panic
	if s := h.Screen(); strings.Contains(s, "@hana") {
		t.Fatalf("stale people result still shown for zz:\n%s", s)
	}
}

func TestLookup(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	palette.ResetForTest()
	palette.Register(palette.Command{ID: "t.echo", Title: "echo", Run: func(*ui.Context, string) tea.Cmd { return nil }})
	if c, ok := palette.Lookup("t.echo"); !ok || c.ID != "t.echo" || c.Run == nil {
		t.Fatalf("Lookup(t.echo) = %+v, %v", c, ok)
	}
	if _, ok := palette.Lookup("nope"); ok {
		t.Fatal("Lookup(nope) must be false")
	}
	for _, id := range []string{"theme.cycle", "logout", "quit"} {
		if _, ok := palette.Lookup(id); ok {
			t.Fatalf("core command %q visible after ResetForTest", id)
		}
	}
}

// TestNoAppImport: the palette package (non-test files) never imports ui/app
// (core commands are registered by ui/app's init, CA 2.2).
func TestNoAppImport(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(fset, f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range af.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); strings.HasSuffix(p, "/internal/ui/app") {
				t.Fatalf("%s imports %s", f, p)
			}
		}
	}
}
