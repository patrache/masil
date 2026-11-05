package uitest

import (
	"fmt"
	"testing"

	"github.com/patrache/masil/internal/ui/layout"
)

// Size is a terminal size.
type Size struct{ W, H int }

// Required lists the sizes every screen golden MUST cover.
var Required = []Size{{200, 60}, {120, 40}, {80, 24}, {50, 20}}

// ModeSizes lists representative sizes per layout mode.
var ModeSizes = map[layout.Mode][]Size{
	layout.ModeCommand:  {{200, 60}, {200, 50}, {240, 70}, {320, 90}},
	layout.ModeStandard: {{120, 40}, {100, 40}, {199, 50}, {200, 49}, {160, 45}},
	layout.ModeCompact:  {{120, 30}, {100, 39}, {199, 39}, {160, 16}},
	layout.ModeWidget:   {{80, 24}, {99, 60}, {60, 60}, {44, 24}},
	layout.ModeTiny:     {{50, 20}, {99, 23}, {43, 40}, {120, 15}, {44, 16}},
	layout.ModeTooSmall: {{19, 10}, {30, 5}},
}

// modeOrder is the stable order AllSizes walks ModeSizes in (largest mode first).
var modeOrder = []layout.Mode{
	layout.ModeCommand, layout.ModeStandard, layout.ModeCompact,
	layout.ModeWidget, layout.ModeTiny, layout.ModeTooSmall,
}

// AllSizes returns Required ∪ every ModeSizes entry, deduped, in a stable order:
// Required first, then ModeSizes by mode (Command..TooSmall). The slice is fresh.
func AllSizes() []Size {
	seen := make(map[Size]bool)
	var out []Size
	add := func(s Size) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range Required {
		add(s)
	}
	for _, m := range modeOrder {
		for _, s := range ModeSizes[m] {
			add(s)
		}
	}
	return out
}

// ForEachSize runs fn in a subtest per "WxH", with a fresh harness each.
// An empty size list fails the test (a golden loop must never pass vacuously).
func ForEachSize(t *testing.T, sizes []Size, fn func(t *testing.T, h *Harness)) {
	t.Helper()
	if len(sizes) == 0 {
		t.Fatal("uitest.ForEachSize: no sizes")
	}
	for _, s := range sizes {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			h := New(t)
			h.Resize(s.W, s.H)
			fn(t, h)
		})
	}
}
