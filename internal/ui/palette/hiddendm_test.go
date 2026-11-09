package palette_test

import (
	"slices"
	"testing"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// TestHiddenDMNotListed: a 1:1 DM whose peer I blocked (Store.HiddenDM) is listed
// neither in the empty query's unread items nor by a typed query, matching the DM
// list, DMUnread and the 알림함 (release v0.1.2 review: the palette opened it and
// the DM surface folded it back on the next event).
func TestHiddenDMNotListed(t *testing.T) {
	t.Cleanup(palette.SnapshotForTest())
	t.Cleanup(palette.ResetRecentForTest())
	ctx := uitest.New(t, uitest.WithSeed(func(s *store.Store) {
		fixtures.Seed(s)
		s.Channels[fixtures.DMJunho].My.UnreadCount = 5
		s.Channels[fixtures.DMMinji].My.UnreadCount = 2
		s.Blocks[fixtures.UserJunho] = uitest.Start
	})).Context()
	has := func(items []palette.Item, id string) bool {
		return slices.ContainsFunc(items, func(it palette.Item) bool { return it.Route != nil && it.Route.ID == id })
	}
	// Chosen before the block: the recent list must not bring it back either.
	palette.RememberForTest(palette.Item{Kind: palette.ItemDM, Title: "준호", Route: &ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMJunho}})
	empty := palette.ItemsForTest(ctx, "")
	if has(empty, fixtures.DMJunho) {
		t.Errorf("empty query lists the hidden DM (%q)", titles(empty))
	}
	if !has(empty, fixtures.DMMinji) {
		t.Errorf("empty query: unread DM 민지 missing (%q)", titles(empty))
	}
	if q := palette.ItemsForTest(ctx, "준호"); has(q, fixtures.DMJunho) {
		t.Errorf("query 준호 lists the hidden DM (%q)", titles(q))
	}
}
