package app_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// checkTitle fails unless title is valid UTF-8 without C0/DEL/C1 (so no ESC,
// BEL or 8-bit ST/CSI that could end the OSC 0/2 title sequence).
func checkTitle(t *testing.T, what, title string) {
	t.Helper()
	if !utf8.ValidString(title) {
		t.Errorf("%s: title %q is not valid UTF-8", what, title)
	}
	for _, c := range title {
		if c < 0x20 || c >= 0x7f && c <= 0x9f {
			t.Errorf("%s: title %q carries control %U", what, title, c)
		}
	}
}

// TestWindowTitleSanitizedCentrally (pending-fixes 0-b): currentTitle cleans
// whatever the focused component's Title() or the route's default title
// returns. stub.Chat returns Store.ChannelLabel unsanitized (as routeTitle
// does), so the hostile channel name reaches WindowTitle only through
// currentTitle's own Sanitize.
func TestWindowTitleSanitizedCentrally(t *testing.T) {
	seed := func(s *store.Store) {
		fixtures.Seed(s)
		fixtures.Hostile(s)
	}
	probe := store.New()
	seed(probe)
	sawCtrl := false
	for _, ch := range probe.SpaceChannels(fixtures.SpaceHostile) {
		raw := probe.ChannelLabel(ch.ID)
		h := uitest.New(t, uitest.WithSeed(seed), uitest.WithRoute(ui.Route{Kind: ui.RouteChannel, ID: ch.ID}))
		title := h.Model.View().WindowTitle
		checkTitle(t, ch.ID, title)
		if !strings.HasPrefix(title, "masil · #") {
			t.Errorf("%s: title %q, want the channel label", ch.ID, title)
		}
		if strings.ContainsAny(raw, "\x1b\x07\u009b") && strings.Contains(title, "제어") && strings.Contains(title, "채널") {
			sawCtrl = true // the raw label carried escapes; the title kept its text only
		}
		h.Check()
	}
	if !sawCtrl {
		t.Error("no title showed the hostile control-character channel name")
	}
}
