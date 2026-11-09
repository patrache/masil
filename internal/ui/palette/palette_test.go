package palette_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
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
	// The available commands follow (TestEmptyQueryListsCommands).
	items = routeItems(palette.ItemsForTest(ctx, ""))
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
	got := titles(routeItems(palette.ItemsForTest(ctx, "")))
	want := []string{"#일반", "최근 3", "최근 9", "최근 8", "최근 7", "최근 6", "최근 5", "최근 4", "최근 2", "홈"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("empty query = %q, want %q", got, want)
	}
}

// routeItems are the items that are not commands, in order.
func routeItems(items []palette.Item) []palette.Item {
	var out []palette.Item
	for _, it := range items {
		if it.Kind != palette.ItemCommand {
			out = append(out, it)
		}
	}
	return out
}

// commandIDs are the ids of the command items, in order.
func commandIDs(items []palette.Item) []string {
	var out []string
	for _, it := range items {
		if it.Command != nil {
			out = append(out, it.Command.ID)
		}
	}
	return out
}

// TestEmptyQueryListsCommands: an empty query lists, after the unread channels, the
// recent routes and 홈, every available command grouped 이동 → 명령 → 설정 and by
// title within a group (owner report 2026-11-09: only 홈 was listed, so commands
// could not be found without knowing their names). Chat's commands: ui/chat
// TestPaletteEmptyQueryChatCommands.
func TestEmptyQueryListsCommands(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	t.Cleanup(palette.ResetRecentForTest())
	ctx := uitest.New(t).Context()
	palette.Register(palette.Command{ID: "t.on", Title: "시험 켜짐", Group: "명령",
		Available: func(*ui.Context) bool { return true }})
	palette.Register(palette.Command{ID: "t.off", Title: "시험 꺼짐", Group: "명령",
		Available: func(*ui.Context) bool { return false }})

	items := palette.ItemsForTest(ctx, "")
	home := slices.IndexFunc(items, func(it palette.Item) bool { return it.Route != nil && it.Route.Kind == ui.RouteHome })
	if home < 0 {
		t.Fatalf("empty query: no 홈 in %q", titles(items))
	}
	for i, it := range items {
		if (it.Kind == palette.ItemCommand) != (i > home) {
			t.Fatalf("empty query: %q at %d, want routes up to 홈 then only commands (%q)", it.Title, i, titles(items))
		}
	}
	cmds := items[home+1:]
	rank := map[string]int{"이동": 0, "명령": 1, "설정": 2}
	for i := 1; i < len(cmds); i++ {
		a, b := cmds[i-1].Command, cmds[i].Command
		ra, rb := rank[a.Group], rank[b.Group]
		if ra > rb || (ra == rb && a.Title > b.Title) {
			t.Fatalf("empty query: %q (%s) before %q (%s), want 이동→명령→설정 then title order", a.Title, a.Group, b.Title, b.Group)
		}
	}
	ids := commandIDs(cmds)
	for _, id := range []string{"help", "settings", "blocks", "logout", "quit", "reconnect", "theme.cycle", "layout.auto", "t.on"} {
		if !slices.Contains(ids, id) {
			t.Errorf("empty query: %s missing from %q", id, ids)
		}
	}
	if slices.Contains(ids, "t.off") {
		t.Errorf("empty query: Available false t.off listed (%q)", ids)
	}
	if len(ids) == 0 || ids[0] != "help" { // 도움말 is the first 이동 command by title
		t.Errorf("empty query: commands %q, want help (도움말) first", ids)
	}
	if again := commandIDs(palette.ItemsForTest(ctx, "")); !slices.Equal(again, ids) {
		t.Fatalf("empty query not deterministic: %q then %q", ids, again)
	}
}

// TestCommandBadgeEveryQuery: a command's Badge (the 알림함's unread count) is drawn
// both when a query finds it and in the empty query's command list (release v0.1.2:
// the empty-query listing built its items without the badge).
func TestCommandBadgeEveryQuery(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	t.Cleanup(palette.ResetRecentForTest())
	ctx := uitest.New(t).Context()
	palette.Register(palette.Command{ID: "t.badged", Title: "시험 배지", Group: "이동",
		Badge: func(*ui.Context) ui.Badge { return ui.Badge{Count: 3} }})
	for _, q := range []string{"", "시험 배지"} {
		i := slices.IndexFunc(palette.ItemsForTest(ctx, q), func(it palette.Item) bool {
			return it.Command != nil && it.Command.ID == "t.badged"
		})
		if i < 0 {
			t.Fatalf("query %q: t.badged not listed", q)
		}
		if it := palette.ItemsForTest(ctx, q)[i]; it.Badge.Count != 3 {
			t.Errorf("query %q: t.badged badge = %+v, want Count 3", q, it.Badge)
		}
	}
}

// subseq: the runes of q appear in s in order (case-insensitive) — a fuzzy title match.
func subseq(s, q string) bool {
	rs := []rune(strings.ToLower(s))
	i := 0
	for _, r := range strings.ToLower(q) {
		for i < len(rs) && rs[i] != r {
			i++
		}
		if i == len(rs) {
			return false
		}
		i++
	}
	return true
}

// hasWordPrefix: q (lowercased) prefixes the id, an id tail from a word boundary
// ("pin.command" of layout.pin.command), an English keyword, or one of their words.
func hasWordPrefix(c *palette.Command, q string) bool {
	q = strings.ToLower(q)
	cands := []string{c.ID}
	for i, r := range c.ID {
		if r == '.' || r == '_' || r == '-' {
			cands = append(cands, c.ID[i+1:])
		}
	}
	cands = append(cands, strings.FieldsFunc(c.ID, func(r rune) bool { return r == '.' || r == '_' || r == '-' })...)
	for _, k := range c.Keywords {
		cands = append(cands, k)
		cands = append(cands, strings.Fields(k)...)
	}
	for _, w := range cands {
		if strings.HasPrefix(strings.ToLower(w), q) {
			return true
		}
	}
	return false
}

// TestCommandMatching: titles and their chosung match fuzzily; English keywords and
// command ids only by word prefix (2+ letters), so one Latin letter no longer lists
// nearly every command (owner report 2026-11-09). Chat's commands: ui/chat
// TestPaletteCommandWordPrefix.
func TestCommandMatching(t *testing.T) {
	t.Cleanup(palette.ResetRecentForTest())
	ctx := uitest.New(t).Context()

	for _, q := range []string{"a", "o", "e", "co", "CO", "lay", "au", "theme", "sign o", "pin.command", "set.amber", "n.c", "t.a"} {
		for _, it := range palette.ItemsForTest(ctx, q) {
			if it.Command != nil && !subseq(it.Title, q) && (len([]rune(q)) < 2 || !hasWordPrefix(it.Command, q)) {
				t.Errorf("query %q: %s (%q) matches neither its title nor a word prefix", q, it.Command.ID, it.Title)
			}
		}
	}
	for q, want := range map[string][]string{
		"au":     {"layout.auto"},
		"co":     {"layout.pin.command", "theme.cycle", "reconnect"}, // command, color, connect
		"CO":     {"layout.pin.command", "theme.cycle", "reconnect"},
		"lay":    {"layout.auto", "layout.pin.widget", "layout.pin.tiny"},
		"sign o": {"logout"}, // a whole multi-word keyword prefix
		// a dotted id tail from a word boundary (fuzzy over the id matched these on main)
		"pin.command": {"layout.pin.command"},
		"pin.tiny":    {"layout.pin.tiny"},
		"set.amber":   {"theme.set.amber"},
		"ㄷㅇㅁ":         {"help"},
		"ㄹㅇㅇ ㅈㄷ":      {"layout.auto"},
		"단축키":         {"help"},   // Hangul keyword
		"차단해제":        {"blocks"}, // Hangul keyword "차단 해제" stays fuzzy
	} {
		ids := commandIDs(palette.ItemsForTest(ctx, q))
		for _, id := range want {
			if !slices.Contains(ids, id) {
				t.Errorf("query %q: %s missing from %q", q, id, ids)
			}
		}
	}
	for q, not := range map[string]string{"a": "layout.auto", "e": "help", "co": "help", "ut": "logout", "nf": "settings",
		"n.c": "layout.pin.command", "t.a": "layout.auto"} { // a tail starts at a word, not mid-word
		if ids := commandIDs(palette.ItemsForTest(ctx, q)); slices.Contains(ids, not) {
			t.Errorf("query %q: unrelated %s listed (%q)", q, not, ids)
		}
	}
	// 홈's English keyword is a word too.
	isHome := func(it palette.Item) bool { return it.Route != nil && it.Route.Kind == ui.RouteHome }
	if !slices.ContainsFunc(palette.ItemsForTest(ctx, "ho"), isHome) || slices.ContainsFunc(palette.ItemsForTest(ctx, "me"), isHome) {
		t.Errorf("홈: want matched by ho (home), not by me")
	}
}

// TestEmptyQueryScrollsSmall: the longer empty-query list scrolls in the small modes —
// moving down to the last command shows it, moving back up shows the first item, and
// the frame stays intact.
func TestEmptyQueryScrollsSmall(t *testing.T) {
	t.Cleanup(palette.ResetRecentForTest())
	sizes := []uitest.Size{{W: 44, H: 16}, {W: 50, H: 20}, {W: 80, H: 24}, {W: 120, H: 15}, {W: 160, H: 16}, {W: 44, H: 24}}
	uitest.ForEachSize(t, sizes, func(t *testing.T, h *uitest.Harness) {
		items := palette.ItemsForTest(h.Context(), "")
		first, last := items[0].Title, items[len(items)-1].Title
		h.Press("ctrl+k")
		if s := h.Screen(); !strings.Contains(s, first) || strings.Contains(s, last) {
			t.Fatalf("opened: want %q shown and %q scrolled off:\n%s", first, last, s)
		}
		for range items {
			h.Press("down")
		}
		if s := h.Screen(); !strings.Contains(s, last) {
			t.Fatalf("after down×%d: last command %q not shown:\n%s", len(items), last, s)
		}
		h.Check()
		h.Press("pgup", "pgup", "pgup", "pgup", "pgup", "pgup", "pgup", "pgup", "pgup", "pgup")
		if s := h.Screen(); !strings.Contains(s, first) || !strings.Contains(s, "이동") {
			t.Fatalf("after pgup: first item %q / 이동 header not shown:\n%s", first, s)
		}
		h.Check()
	})
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
