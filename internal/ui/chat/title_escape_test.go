package chat

import (
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// TestWindowTitleHostileNames (pending-fixes 0-b, 1): the window title (OSC
// 0/2) built from a channel or DM name carries no control character, so a
// hostile name cannot end the title sequence and inject its own escapes, and
// no bidi, zero-width, separator, variation-selector or filler rune: the OS
// GUI draws the title with full bidi, and a DM title is the other user's
// display name.
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
	for id, ch := range s.Channels {
		if ch.Kind == api.ChannelDM && slices.ContainsFunc(ch.Members, func(u api.UserSummary) bool { return u.Handle == "ctrl_user" }) {
			routes = append(routes, ui.Route{Kind: ui.RouteChannel, ID: id})
		}
	}
	if len(routes) < 3 {
		t.Fatalf("got %d routes, want both hostile channels and the hostile DM", len(routes))
	}
	sawCtrl, sawDM := false, false
	for _, r := range routes {
		h := open(t, uitest.Size{W: 120, H: 40}, r, uitest.WithSeed(seed))
		title := h.Model.View().WindowTitle
		uitest.AssertTitle(t, r.ID, title)
		if strings.Contains(title, "제어") && strings.Contains(title, "채널") {
			sawCtrl = true // the control-character name reached the title, cleaned
		}
		if strings.Contains(title, "제어") && strings.Contains(title, "이름") && strings.Contains(title, "역") {
			sawDM = true // the hostile display name reached the DM title, cleaned
		}
		h.Check()
	}
	if !sawCtrl || !sawDM {
		t.Errorf("hostile names did not reach the title: channel=%v dm=%v", sawCtrl, sawDM)
	}
}
