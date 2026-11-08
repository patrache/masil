package app_test

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// checkTitle fails unless title is valid UTF-8 without C0/DEL/C1 (so no ESC,
// BEL or 8-bit ST/CSI that could end the OSC 0/2 title sequence) and without
// bidi, zero-width, separator, variation-selector or filler runes: the OS GUI
// draws the title with full bidi, so a hostile DM name would reorder it
// (pending-fixes 0-b, 1).
func checkTitle(t *testing.T, what, title string) {
	t.Helper()
	uitest.AssertTitle(t, what, title)
}

// TestWindowTitleSanitizedCentrally (pending-fixes 0-b, 1): currentTitle
// cleans whatever the focused component's Title() or the route's default
// title returns. stub.Chat returns Store.ChannelLabel unsanitized (as
// routeTitle does), so the hostile channel and DM names reach WindowTitle
// only through currentTitle's own Sanitize.
func TestWindowTitleSanitizedCentrally(t *testing.T) {
	seed := func(s *store.Store) {
		fixtures.Seed(s)
		fixtures.Hostile(s)
	}
	probe := store.New()
	seed(probe)
	sawCtrl, sawHidden := false, false
	ids := []string{}
	for _, ch := range probe.SpaceChannels(fixtures.SpaceHostile) {
		ids = append(ids, ch.ID)
	}
	dm := hostileDM(probe)
	if dm == "" {
		t.Fatal("no DM with the hostile control-name user")
	}
	ids = append(ids, dm)
	for _, id := range ids {
		raw := probe.ChannelLabel(id)
		h := uitest.New(t, uitest.WithSeed(seed), uitest.WithRoute(ui.Route{Kind: ui.RouteChannel, ID: id}))
		title := h.Model.View().WindowTitle
		checkTitle(t, id, title)
		want := "masil · #"
		if id == dm {
			want = "masil · 제어"
		}
		if !strings.HasPrefix(title, want) {
			t.Errorf("%s: title %q, want prefix %q", id, title, want)
		}
		if strings.ContainsAny(raw, "\x1b\x07\u009b") && strings.Contains(title, "제어") {
			sawCtrl = true // the raw label carried escapes; the title kept its text only
		}
		if strings.IndexFunc(raw, uitest.Hidden) >= 0 && strings.Contains(title, "역") {
			sawHidden = true // the raw label carried bidi/zero-width runes; the title kept its text only
		}
		h.Check()
	}
	if !sawCtrl || !sawHidden {
		t.Errorf("hostile names did not reach the title: control=%v hidden=%v", sawCtrl, sawHidden)
	}
}

// hostileDM returns the DM channel with the hostile control-name user.
func hostileDM(s *store.Store) string {
	for id, ch := range s.Channels {
		if ch.Kind != api.ChannelDM {
			continue
		}
		for _, m := range ch.Members {
			if m.Handle == "ctrl_user" {
				return id
			}
		}
	}
	return ""
}
