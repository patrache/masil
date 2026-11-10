package e2e

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// sweepStates are the five shell states plus the open palette (CA 10.5).
var sweepStates = append(append([]struct {
	name string
	nav  func(h *uitest.Harness)
}{}, productStates...), struct {
	name string
	nav  func(h *uitest.Harness)
}{"palette", openPalette})

// sweepStep is the sampling step: every value in a full run, every 3rd with -short.
func sweepStep() int {
	if testing.Short() {
		return 3
	}
	return 1
}

// sweep opens every sweep state once (fixtures + Hostile) and resizes that one
// harness through sizes, checking integrity after each resize.
func sweep(t *testing.T, sizes []uitest.Size) {
	t.Helper()
	for _, st := range sweepStates {
		t.Run(st.name, func(t *testing.T) {
			h := newHarness(t, uitest.Size{W: 200, H: 60}, fullSeed)
			st.nav(h)
			before := h.Nav()
			for _, sz := range sizes {
				h.Resize(sz.W, sz.H)
				h.Check()
				if t.Failed() {
					t.Fatalf("%s at %dx%d", st.name, sz.W, sz.H)
				}
			}
			if st.name != "palette" && len(h.Nav().Tabs) != len(before.Tabs) {
				t.Fatalf("resizing changed the navigation: %+v -> %+v", before, h.Nav())
			}
		})
	}
}

func TestSweepWidths(t *testing.T) {
	var sizes []uitest.Size
	for w := 1; w <= 260; w += sweepStep() {
		for _, hh := range []int{6, 15, 16, 23, 24, 39, 40, 49, 50, 60} {
			sizes = append(sizes, uitest.Size{W: w, H: hh})
		}
	}
	sweep(t, sizes)
}

func TestSweepHeights(t *testing.T) {
	var sizes []uitest.Size
	for hh := 1; hh <= 90; hh += sweepStep() {
		for _, w := range []int{20, 43, 44, 99, 100, 159, 160, 199, 200} {
			sizes = append(sizes, uitest.Size{W: w, H: hh})
		}
	}
	sweep(t, sizes)
}

// TestHostileScreens opens the hostile channel, thread, post detail and
// profile at every size (fresh harness per size) and checks integrity.
func TestHostileScreens(t *testing.T) {
	screens := []struct {
		name  string
		route ui.Route
		want  string // shown at 120x40: the hostile data really is on screen
	}{
		{"channel", hostileRoute, strings.Repeat("x", 60)}, // the long message's tail (its head scrolls above the top tab bar row)
		{"thread", ui.Route{Kind: ui.RouteThread, ID: hostileThreadRoot(t), Arg: fixtures.ChHostile}, "리액션 폭탄"},
		{"post", ui.Route{Kind: ui.RoutePost, ID: fixtures.HostileTimeline()[0].Post.ID}, "오백 자짜리 글"},
		{"profile", ui.Route{Kind: ui.RouteProfile, ID: "@" + hostileHandle}, "팔로워 1.2억"},
	}
	for _, sc := range screens {
		t.Run(sc.name, func(t *testing.T) {
			h := newHarness(t, uitest.Size{W: 120, H: 40}, fullSeed, uitest.WithRoute(sc.route))
			if s := h.Screen(); !strings.Contains(s, sc.want) || strings.Contains(s, "불러오지 못했어요") {
				t.Fatalf("%s at 120x40 lacks %q or failed to load:\n%s", sc.name, sc.want, s)
			}
			for _, sz := range uitest.AllSizes() {
				t.Run(sizeName(sz), func(t *testing.T) {
					h := newHarness(t, sz, fullSeed, uitest.WithRoute(sc.route))
					h.Check()
				})
			}
		})
	}
}
