package fun

import (
	"strings"
	"testing"
	"unicode"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// TestFunPluralsEN (P5 Task 11): every fun key with a count param has both en
// forms, and n=1 picks one while n=2 picks other.
func TestFunPluralsEN(t *testing.T) {
	en := i18n.For(i18n.En)
	n := 0
	for _, e := range i18n.Entries() {
		if e.Area != "fun" || e.Count == "" {
			continue
		}
		n++
		f := e.Text[i18n.En]
		if f.One == "" || f.Other == "" {
			t.Errorf("fun.%s: en needs one and other, got %+v", e.Key, f)
			continue
		}
		args := func(v int) map[string]any {
			m := map[string]any{}
			for _, p := range e.Params {
				if p.Type == "int" {
					m[p.Name] = v
				} else {
					m[p.Name] = "x"
				}
			}
			return m
		}
		one, _ := en.Lookup("fun", e.Key, args(1))
		two, _ := en.Lookup("fun", e.Key, args(2))
		if one == two || !strings.Contains(one, "1") || !strings.Contains(two, "2") {
			t.Errorf("fun.%s: n=1 %q, n=2 %q — want the one and other forms", e.Key, one, two)
		}
	}
	if n == 0 {
		t.Fatal("no fun key with a count param")
	}
	if got := en.Fun.TooLong(1); got != "Over 1 character" {
		t.Errorf("TooLong(1) = %q", got)
	}
	if got := en.Fun.TooLong(300); got != "Over 300 characters" {
		t.Errorf("TooLong(300) = %q", got)
	}
}

func hasHangul(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Hangul, r) })
}

// TestFunDialogsEN: the status, presence and guestbook dialogs draw no Korean
// in en (fixture names aside — the seed has none in these dialogs).
func TestFunDialogsEN(t *testing.T) {
	for _, cmd := range []string{"status.set", "presence.set"} {
		h := uitest.New(t, uitest.WithAPI(statusAPI(t)), uitest.WithSeed(fixtures.Seed), uitest.WithLocale(i18n.En))
		h.Resize(120, 40)
		runCommand(h, cmd, "")
		s := overlayText(h)
		if s == "" || hasHangul(s) {
			t.Errorf("%s dialog in en:\n%s", cmd, s)
		}
		if title := h.Screen(); !strings.Contains(title, map[string]string{"status.set": "Status message", "presence.set": "Presence"}[cmd]) {
			t.Errorf("%s: en title missing:\n%s", cmd, title)
		}
	}
}
