package widgets_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

func walkTabs() []widgets.Tab {
	return []widgets.Tab{
		{Label: "🧩 leetcode daily", Short: "🧩"},
		{Label: "🌿 lazygit", Short: "🌿"},
		{Label: "🧪 API test", Short: "🧪"},
		{Label: "#general", Badge: ui.Badge{Count: 3}},
		{Label: "DM"},
	}
}

// plainWalk renders the bar and returns its plain text, checking the width.
func plainWalk(t *testing.T, b *widgets.WalkBar, w int) string {
	t.Helper()
	out := b.View(th, w)
	if got := ansi.StringWidth(out); got != w {
		t.Fatalf("width %d: got %d cells: %q", w, got, ansi.Strip(out))
	}
	if strings.Contains(out, "\n") {
		t.Fatalf("width %d: the bar wraps: %q", w, ansi.Strip(out))
	}
	return ansi.Strip(out)
}

// hitAll reports, for every entry, whether some column of the bar maps to it.
func hitAll(b *widgets.WalkBar, w, n int) []bool {
	seen := make([]bool, n)
	for x := range w {
		if i, ok := b.Hit(x); ok && i >= 0 && i < n {
			seen[i] = true
		}
	}
	return seen
}

func TestWalkBarWideShowsEveryLabelAndMarks(t *testing.T) {
	b := widgets.NewWalkBar()
	b.Set(walkTabs(), 1)
	got := plainWalk(t, b, 100)
	for _, want := range []string{"[🧩 leetcode daily ", " 🌿 lazygit ", " 🧪 API test]", "#general", " DM "} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Count(got, "[") != 1 || strings.Count(got, "]") != 1 {
		t.Errorf("want one [ and one ]: %q", got)
	}
	out := b.View(th, 100)
	if !strings.Contains(out, th.S.TabActive.Render(" 🌿 lazygit ")) {
		t.Errorf("the current entry is not TabActive: %q", out)
	}
}

func TestWalkBarWrapsMarks(t *testing.T) {
	b := widgets.NewWalkBar()
	b.Set(walkTabs(), 0) // [ goes to the last entry, ] to the second
	got := plainWalk(t, b, 100)
	if !strings.Contains(got, "[DM ") || !strings.Contains(got, " 🌿 lazygit]") {
		t.Errorf("wrapped marks: %q", got)
	}
	two := widgets.NewWalkBar()
	two.Set(walkTabs()[:2], 0) // both keys go to the other one
	if got := plainWalk(t, two, 60); !strings.Contains(got, "[🌿 lazygit]") {
		t.Errorf("two entries: %q", got)
	}
	one := widgets.NewWalkBar()
	one.Set(walkTabs()[:1], 0)
	if got := plainWalk(t, one, 60); strings.ContainsAny(got, "[]") {
		t.Errorf("a single entry has no marks: %q", got)
	}
}

func TestWalkBarNarrowKeepsEveryEntry(t *testing.T) {
	tabs := walkTabs()
	for _, w := range []int{70, 50, 40, 30, 24, 20, 16} {
		for active := range tabs {
			b := widgets.NewWalkBar()
			b.Set(tabs, active)
			got := plainWalk(t, b, w)
			if !strings.Contains(got, "[") || !strings.Contains(got, "]") {
				t.Errorf("w=%d active=%d: the [ ] targets are gone: %q", w, active, got)
			}
			for i, ok := range hitAll(b, w, len(tabs)) {
				if !ok {
					t.Errorf("w=%d active=%d: entry %d has no clickable cell: %q", w, active, i, got)
				}
			}
		}
	}
	b := widgets.NewWalkBar()
	b.Set(tabs, 0)
	if got := plainWalk(t, b, 50); !strings.Contains(got, "…") {
		t.Errorf("w=50: labels are shortened first: %q", got)
	}
	if got := plainWalk(t, b, 24); !strings.Contains(got, "🧩") || !strings.Contains(got, "#g") {
		t.Errorf("w=24: icons/initials: %q", got)
	}
}

func TestWalkBarHitMapsTheLabel(t *testing.T) {
	b := widgets.NewWalkBar()
	b.Set(walkTabs(), 0)
	got := plainWalk(t, b, 100)
	at := strings.Index(got, "API")
	col := ansi.StringWidth(got[:at])
	if i, ok := b.Hit(col); !ok || i != 2 {
		t.Errorf("Hit(%d) = %d,%v, want 2", col, i, ok)
	}
	for _, x := range []int{-1, 99, 100} {
		if _, ok := b.Hit(x); ok {
			t.Errorf("Hit(%d) should miss", x)
		}
	}
	empty := widgets.NewWalkBar()
	if got := plainWalk(t, empty, 10); strings.TrimSpace(got) != "" {
		t.Errorf("empty bar = %q", got)
	}
	if v := empty.View(th, 0); v != "" {
		t.Errorf("zero width = %q", v)
	}
}
