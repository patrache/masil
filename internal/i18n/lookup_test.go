package i18n_test

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

func TestPseudoWraps(t *testing.T) {
	got := i18n.For(i18n.Pseudo).Testonly.Members(2)
	en := i18n.For(i18n.En).Testonly.Members(2)
	if !strings.HasPrefix(got, "⟦") || !strings.HasSuffix(got, "⟧") {
		t.Fatalf("pseudo = %q, want ⟦…⟧", got)
	}
	if w, min := textwidth.Width(got), float64(textwidth.Width(en))*1.4; float64(w) < min {
		t.Fatalf("pseudo width %d < en width ×1.4 (%.1f): %q", w, min, got)
	}
	if !strings.Contains(got, "2") {
		t.Fatalf("pseudo lost the argument: %q", got)
	}
	// Arguments (user data) are not transformed.
	if g := i18n.For(i18n.Pseudo).Testonly.Greet("Alice"); !strings.Contains(g, "Alice") {
		t.Fatalf("pseudo changed the argument: %q", g)
	}
	// No en text => ko text, still wrapped.
	if g := i18n.For(i18n.Pseudo).Testonly.KoOnly(); !strings.HasPrefix(g, "⟦") || !strings.Contains(g, "한국어만") {
		t.Fatalf("pseudo ko fallback = %q", g)
	}
}

func TestLookup(t *testing.T) {
	ko := i18n.KoT()
	if got, ok := ko.Lookup("testonly", "members", map[string]any{"n": 3}); !ok || got != "멤버 3명" {
		t.Fatalf("Lookup members = %q,%v", got, ok)
	}
	// JSON numbers arrive as float64.
	if got, ok := i18n.For(i18n.En).Lookup("testonly", "members", map[string]any{"n": float64(1)}); !ok || got != "1 member" {
		t.Fatalf("Lookup float64 = %q,%v", got, ok)
	}
	if got, ok := ko.Lookup("testonly", "greet", map[string]any{"name": "하나"}); !ok || got != "하나님, 안녕하세요" {
		t.Fatalf("Lookup greet = %q,%v", got, ok)
	}
	if got, ok := ko.Lookup("testonly", "greet", nil); !ok || got != "님, 안녕하세요" {
		t.Fatalf("missing arg = %q,%v", got, ok)
	}
	for _, c := range [][2]string{{"testonly", "nope"}, {"nope", "members"}, {"", ""}} {
		if got, ok := ko.Lookup(c[0], c[1], nil); ok || got != "" {
			t.Fatalf("Lookup(%q,%q) = %q,%v; want false", c[0], c[1], got, ok)
		}
	}
}

func TestEntriesMeta(t *testing.T) {
	es := i18n.Entries()
	for i := 1; i < len(es); i++ {
		a, b := es[i-1], es[i]
		if a.Area > b.Area || (a.Area == b.Area && a.Key >= b.Key) {
			t.Fatalf("Entries not sorted at %d: %s.%s then %s.%s", i, a.Area, a.Key, b.Area, b.Key)
		}
	}
	var m *i18n.Entry
	for i := range es {
		if es[i].Area == "testonly" && es[i].Key == "members" {
			m = &es[i]
		}
	}
	if m == nil {
		t.Fatal("testonly.members missing from Entries")
	}
	if m.Count != "n" || len(m.Params) != 1 || m.Params[0] != (i18n.Param{Name: "n", Type: "int"}) {
		t.Fatalf("meta = %+v", *m)
	}
	if m.Max != 14 || m.Desc == "" || m.NoTruncate || m.Legal {
		t.Fatalf("meta = %+v", *m)
	}
	if m.Text[i18n.En] != (i18n.Forms{One: "{n} member", Other: "{n} members", Short: "{n}"}) {
		t.Fatalf("en text = %+v", m.Text[i18n.En])
	}
	if _, ok := m.Text[i18n.ZhHans]; !ok {
		t.Fatal("zh-Hans text missing")
	}
	for _, e := range es {
		if e.Area == "testonly" && e.Key == "ko_only" {
			if _, ok := e.Text[i18n.En]; ok {
				t.Fatal("ko_only must have no en text")
			}
		}
	}
}
