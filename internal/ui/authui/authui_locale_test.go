package authui_test

import (
	"strings"
	"testing"
	"unicode"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

// TestAuthLabelsBudget: button and field-name keys carry a width budget (max)
// and en fits it; the en login and signup screens keep their borders at
// 50x20 and 40x14 and show no Korean of their own.
func TestAuthLabelsBudget(t *testing.T) {
	n := 0
	for _, e := range i18n.Entries() {
		if e.Area != "auth" || !(strings.HasPrefix(e.Key, "button_") || strings.HasPrefix(e.Key, "field_") && !strings.HasSuffix(e.Key, "_placeholder")) {
			continue
		}
		n++
		if e.Max == 0 {
			t.Errorf("auth.%s: button/field label without max", e.Key)
			continue
		}
		en, ok := e.Text[i18n.En]
		if !ok || en.Other == "" {
			t.Errorf("auth.%s: no en", e.Key)
			continue
		}
		if w := textwidth.Width(en.Other); w > e.Max {
			t.Errorf("auth.%s: en %q is %d cells, max %d", e.Key, en.Other, w, e.Max)
		}
	}
	if n < 6 {
		t.Fatalf("auth area has %d button/field keys, want at least 6", n)
	}

	for _, mode := range []string{"login", "signup"} {
		for _, sz := range []uitest.Size{{W: 50, H: 20}, uitest.Tiny} {
			h := uitest.New(t, uitest.WithCredentials(nil), uitest.WithLocale(i18n.En))
			h.Resize(sz.W, sz.H)
			h.Context().API = api.New("https://masil.example")
			h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteLogin, Arg: mode}})
			h.Check()
			if h.Model.Frame().Layout.Mode == layout.ModeTooSmall {
				continue
			}
			body := strings.Join(bodyRows(h), "\n")
			for _, r := range body {
				if unicode.Is(unicode.Hangul, r) {
					t.Fatalf("%s %dx%d en: Korean on the screen:\n%s", mode, sz.W, sz.H, body)
				}
			}
			if !strings.Contains(body, "Password") {
				t.Fatalf("%s %dx%d en: no Password field:\n%s", mode, sz.W, sz.H, body)
			}
		}
	}
}
