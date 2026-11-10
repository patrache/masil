package palette_test

import (
	"slices"
	"testing"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
)

// testLabel is a catalog-backed label: ko "영어에도 있음", en "Only in English".
func testLabel(t *i18n.T) string { return t.Testonly.EnOnly() }

func findCommand(items []palette.Item, id string) (palette.Item, bool) {
	i := slices.IndexFunc(items, func(it palette.Item) bool { return it.Command != nil && it.Command.ID == id })
	if i < 0 {
		return palette.Item{}, false
	}
	return items[i], true
}

func TestPaletteLabelUsesContextLocale(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	t.Cleanup(palette.ResetRecentForTest())
	palette.Register(palette.Command{ID: "t.label", Title: "무시되는 제목", Label: testLabel, Group: palette.GroupCommand,
		Available: func(*ui.Context) bool { return true }})

	ko := uitest.New(t).Context()
	it, ok := findCommand(palette.ItemsForTest(ko, ""), "t.label")
	if !ok || it.Title != testLabel(i18n.KoT()) {
		t.Fatalf("ko palette title = %q (found %v), want %q", it.Title, ok, testLabel(i18n.KoT()))
	}
	en := uitest.New(t, uitest.WithLocale(i18n.En)).Context()
	it, ok = findCommand(palette.ItemsForTest(en, ""), "t.label")
	if !ok || it.Title != testLabel(i18n.For(i18n.En)) {
		t.Fatalf("en palette title = %q (found %v), want %q", it.Title, ok, testLabel(i18n.For(i18n.En)))
	}
	if _, ok := findCommand(palette.ItemsForTest(ko, "무시되는"), "t.label"); ok {
		t.Fatal("Title must be ignored when Label is set")
	}
}

func TestPaletteSearchesEnglishLabel(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	t.Cleanup(palette.ResetRecentForTest())
	palette.Register(palette.Command{ID: "t.english", Label: testLabel, Group: palette.GroupNav})
	ko := uitest.New(t).Context()
	for _, q := range []string{"English", "only", "영어에도"} {
		if _, ok := findCommand(palette.ItemsForTest(ko, q), "t.english"); !ok {
			t.Errorf("ko screen, query %q: t.english not found", q)
		}
	}
}

func TestPaletteGroupConstants(t *testing.T) {
	ko := i18n.KoT()
	for g, want := range map[string]string{
		palette.GroupNav: "이동", palette.GroupCommand: "명령", palette.GroupSettings: "설정",
	} {
		if got := palette.GroupTitle(ko, g); got != want {
			t.Errorf("GroupTitle(ko, %q) = %q, want %q", g, got, want)
		}
	}
	if palette.GroupTitle(i18n.For(i18n.En), palette.GroupSettings) == "설정" {
		t.Error("en group header must come from the catalog")
	}
	t.Cleanup(palette.SnapshotForTest())
	defer func() {
		if recover() == nil {
			t.Fatal("unknown Group must panic at Register")
		}
	}()
	palette.Register(palette.Command{ID: "t.badgroup", Title: "x", Group: "명령"}) // a 0.1.2 header string, not a group id
}

// TestPaletteKeywordsInFollowLocale: KeywordsIn adds the screen language's
// search words (P5 batch 2 — Korean keywords moved out of the Go sources).
func TestPaletteKeywordsInFollowLocale(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	t.Cleanup(palette.ResetRecentForTest())
	palette.Register(palette.Command{ID: "t.kw", Label: testLabel, Group: palette.GroupCommand,
		Keywords: []string{"static"},
		KeywordsIn: func(t *i18n.T) []string {
			if t.Locale() == i18n.Ko {
				return []string{"방해금지", "자리 비움"}
			}
			return []string{"donotdisturb"}
		}})
	ko := uitest.New(t).Context()
	for _, q := range []string{"방해금지", "자리 비움", "static"} {
		if _, ok := findCommand(palette.ItemsForTest(ko, q), "t.kw"); !ok {
			t.Errorf("ko screen, query %q: t.kw not found", q)
		}
	}
	if _, ok := findCommand(palette.ItemsForTest(ko, "donotdisturb"), "t.kw"); ok {
		t.Error("ko screen must not search the en keywords")
	}
	en := uitest.New(t, uitest.WithLocale(i18n.En)).Context()
	if _, ok := findCommand(palette.ItemsForTest(en, "donotdisturb"), "t.kw"); !ok {
		t.Error("en screen: KeywordsIn(en) not searched")
	}
	if got := palette.Words("친구, 친구 요청,  , 초대"); !slices.Equal(got, []string{"친구", "친구 요청", "초대"}) {
		t.Errorf("Words = %q", got)
	}
}
