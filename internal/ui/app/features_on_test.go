package app_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// With every tool flag on but no tool open, the shell draws exactly what it
// draws with the flags off (A8.4) — the TestShellGoldens screens, every size,
// with every tool and the tool routes registered in both draws — except the
// status line, which adds "ctrl+t 도구" after the ctrl+k hint (0.1.3 patch:
// nothing said how to reach the tools) and is otherwise the same.
// The help and palette screens are left out: they list the new keys on purpose
// (JX9, the release PR).
func TestFeaturesOnShellUnchanged(t *testing.T) {
	onlyMe := func(s *store.Store) { full := store.New(); fixtures.Seed(full); s.Me = full.Me }
	cases := []struct {
		name string
		opts []uitest.Option
	}{
		{"empty_home", []uitest.Option{uitest.WithSeed(onlyMe)}},
		{"fixtures_home", nil},
		{"dev_channel", []uitest.Option{uitest.WithRoute(devRoute)}},
	}
	// draw is the styled screen above the status line, and the plain status line.
	draw := func(t *testing.T, flags string, opts []uitest.Option, s uitest.Size) (body, status string) {
		installStubTools(t, flags) // every tool and tool route registered, as in cmd/masil
		h := uitest.New(t, opts...)
		h.Resize(s.W, s.H)
		styled := strings.Split(h.Styled(), "\n")
		plain := strings.Split(h.Screen(), "\n")
		return strings.Join(styled[:len(styled)-1], "\n"), plain[len(plain)-1]
	}
	for _, tc := range cases {
		for _, s := range uitest.AllSizes() {
			t.Run(fmt.Sprintf("%s/%dx%d", tc.name, s.W, s.H), func(t *testing.T) {
				off, offStatus := draw(t, "", tc.opts, s)
				on, onStatus := draw(t, "all", tc.opts, s)
				if on != off {
					t.Fatalf("MASIL_FEATURES=all changed the screen:\n--- off\n%s\n--- on\n%s", off, on)
				}
				switch {
				case strings.Contains(offStatus, "ctrl+t"):
					t.Fatalf("flags off: status = %q", offStatus)
				case !strings.Contains(offStatus, "ctrl+k"): // no ctrl+k hint: no tool hint, the same line
					if onStatus != offStatus {
						t.Fatalf("flags on changed the status line:\n--- off\n%q\n--- on\n%q", offStatus, onStatus)
					}
				case !strings.Contains(onStatus, "ctrl+t 도구"):
					t.Fatalf("flags on: status = %q, want the ctrl+t 도구 hint", onStatus)
				default: // exactly the 0.1.2 line with the tool hint added (a hint line too long is cut with …)
					got := strings.Fields(strings.Replace(onStatus, " · ctrl+t 도구", "", 1)) // the gap before the right side shrinks
					if !strings.Contains(onStatus, "…") && strings.Join(got, " ") != strings.Join(strings.Fields(offStatus), " ") {
						t.Fatalf("flags on changed more than the tool hint:\n--- off\n%q\n--- on\n%q", offStatus, onStatus)
					}
				}
			})
		}
	}
}
