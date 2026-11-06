package chat

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// TestWindowTitleHostileNames (pending-fixes 0-b): the window title (OSC 0/2)
// built from a channel or DM name carries no control character, so a hostile
// name cannot end the title sequence and inject its own escapes.
func TestWindowTitleHostileNames(t *testing.T) {
	s := seeded()
	fixtures.Hostile(s)
	seed := func(s *store.Store) {
		bootstrappedSeed(s)
		fixtures.Hostile(s)
	}
	var routes []ui.Route
	for _, ch := range s.SpaceChannels(fixtures.SpaceHostile) {
		routes = append(routes, ui.Route{Kind: ui.RouteChannel, ID: ch.ID})
	}
	if len(routes) < 2 {
		t.Fatalf("hostile space has %d channels, want the control-name one too", len(routes))
	}
	sawCtrl := false
	for _, r := range routes {
		h := open(t, uitest.Size{W: 120, H: 40}, r, uitest.WithSeed(seed))
		title := h.Model.View().WindowTitle
		if !utf8.ValidString(title) {
			t.Errorf("%s: title %q is not valid UTF-8", r.ID, title)
		}
		for _, c := range title {
			if c < 0x20 || c >= 0x7f && c <= 0x9f {
				t.Errorf("%s: title %q carries control %U", r.ID, title, c)
			}
		}
		if strings.Contains(title, "제어") && strings.Contains(title, "채널") {
			sawCtrl = true // the control-character name reached the title, cleaned
		}
		h.Check()
	}
	if !sawCtrl {
		t.Error("no title showed the hostile control-character channel name")
	}
}
