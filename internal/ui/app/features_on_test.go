package app_test

import (
	"fmt"
	"testing"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// With every tool flag on but no tool open, the shell draws exactly what it
// draws with the flags off (A8.4) — the TestShellGoldens screens, every size,
// with every tool and the tool routes registered in both draws.
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
	draw := func(t *testing.T, flags string, opts []uitest.Option, s uitest.Size) string {
		installStubTools(t, flags) // every tool and tool route registered, as in cmd/masil
		h := uitest.New(t, opts...)
		h.Resize(s.W, s.H)
		return h.Styled()
	}
	for _, tc := range cases {
		for _, s := range uitest.AllSizes() {
			t.Run(fmt.Sprintf("%s/%dx%d", tc.name, s.W, s.H), func(t *testing.T) {
				off := draw(t, "", tc.opts, s)
				on := draw(t, "all", tc.opts, s)
				if on != off {
					t.Fatalf("MASIL_FEATURES=all changed the screen:\n--- off\n%s\n--- on\n%s", off, on)
				}
			})
		}
	}
}
