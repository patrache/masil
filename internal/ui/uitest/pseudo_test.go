package uitest_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// errTB records Errorf instead of failing (to test an assertion's failures).
type errTB struct {
	testing.TB
	errs []string
}

func (e *errTB) Helper() {}
func (e *errTB) Errorf(format string, args ...any) {
	e.errs = append(e.errs, fmt.Sprintf(format, args...))
}

func TestAssertPseudoWrapped(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		allow      []string
		fail       string // "" = passes; else a word the report names
	}{
		{"wrapped plus user data", "⟦Ŝéñð⟧ │ sam", []string{"sam"}, ""},
		{"hard-coded Korean", "⟦Ŝéñð⟧ 보내기", nil, "보내기"},
		{"truncated wrap", "⟦Ŝéñ…", nil, ""},
		{"unclosed to the border", "│⟦Ŝéñð ẃîţĥ│ok", nil, "ok"},
		{"digits, punctuation, borders", "╭─ 12:05 · 3/4 (99+) ─╮\n│ #  ● ▸ │", nil, ""},
		{"single letters", "j k · N · ㄱ", nil, ""},
		{"wrapped over two lines", "⟦Ŝéñð ýôü\nẃîţĥ⟧ x", nil, ""},
		{"English left over", "⟦Ŝéñð⟧ Send", nil, "Send"},
		{"allow is word-exact", "sammy", []string{"sam"}, "sammy"},
		{"allow with punctuation", "#개발 ctrl+k", []string{"#개발", "ctrl+k"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &errTB{TB: t}
			uitest.AssertPseudoWrapped(rec, "pane/main/channel", tc.text, tc.allow)
			switch {
			case tc.fail == "" && len(rec.errs) > 0:
				t.Fatalf("%q failed: %v", tc.text, rec.errs)
			case tc.fail != "" && len(rec.errs) == 0:
				t.Fatalf("%q passed, want a failure naming %q", tc.text, tc.fail)
			case tc.fail != "":
				msg := strings.Join(rec.errs, "\n")
				if !strings.Contains(msg, "pane/main/channel") || !strings.Contains(msg, tc.fail) {
					t.Fatalf("report %q does not name the region and %q", msg, tc.fail)
				}
			}
		})
	}
}

func TestPseudoAllowHasFixtures(t *testing.T) {
	data := fixtures.UserData()
	for _, want := range []string{"민지", "준호", "하나", "김개발", "#개발", "노바 개발방", "sam", "@minji"} {
		if !slices.Contains(data, want) {
			t.Errorf("fixtures.UserData() lacks %q", want)
		}
	}
	h := uitest.New(t, uitest.WithLocale(i18n.Pseudo))
	allow := uitest.PseudoAllow(h)
	for _, want := range []string{"민지", "#개발", "노바 개발방", "ctrl+k", "enter", "esc", "/poll", "/shrug", "masil"} {
		if !slices.Contains(allow, want) {
			t.Errorf("PseudoAllow lacks %q", want)
		}
	}
}

func TestPseudoRegions(t *testing.T) {
	h := uitest.New(t, uitest.WithLocale(i18n.Pseudo))
	h.Resize(120, 40)
	regions := uitest.PseudoRegions(h)
	if _, ok := regions["status"]; !ok {
		t.Fatalf("no status region: %v", keysOf(regions))
	}
	var pane bool
	for k := range regions {
		pane = pane || strings.HasPrefix(k, "pane/main/")
	}
	if !pane {
		t.Fatalf("no pane/main/<kind> region: %v", keysOf(regions))
	}
	h.Press("ctrl+k") // the palette overlay
	regions = uitest.PseudoRegions(h)
	var overlay string
	for k := range regions {
		if strings.HasPrefix(k, "overlay/") {
			overlay = k
		}
	}
	if overlay == "" {
		t.Fatalf("no overlay region with the palette open: %v", keysOf(regions))
	}
	// Every screen cell lands in exactly one region: the texts hold as many
	// non-space characters as the screen.
	var screen, split int
	for _, r := range h.Screen() {
		if r != ' ' && r != '\n' {
			screen++
		}
	}
	for _, text := range regions {
		for _, r := range text {
			if r != ' ' && r != '\n' {
				split++
			}
		}
	}
	if screen != split {
		t.Fatalf("regions hold %d non-space runes, the screen %d", split, screen)
	}
}

func keysOf(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
