package chat

import (
	"slices"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// paletteCommandIDs are the command ids palette query q lists, in order.
func paletteCommandIDs(ctx *ui.Context, q string) []string {
	var out []string
	for _, it := range palette.ItemsForTest(ctx, q) {
		if it.Command != nil {
			out = append(out, it.Command.ID)
		}
	}
	return out
}

// TestPaletteEmptyQueryChatCommands (owner report 2026-11-09): ctrl+k with nothing
// typed lists the available chat commands, so 채널 만들기 and 초대 링크 만들기 are
// found without knowing their names; 방 설정 (이동) comes before the 명령 group;
// commands whose Available is false (no current space) are not listed.
func TestPaletteEmptyQueryChatCommands(t *testing.T) {
	ctx := uitest.New(t).Context()
	ids := paletteCommandIDs(ctx, "")
	for _, id := range []string{"channel.create", "invite.create", "invite.direct", "space.create", "space.join", "space.settings", "friend.add", "dm.new", "friends"} {
		if !slices.Contains(ids, id) {
			t.Errorf("empty query: %s missing from %q", id, ids)
		}
	}
	if s, c := slices.Index(ids, "space.settings"), slices.Index(ids, "channel.create"); s < 0 || c < 0 || s > c {
		t.Errorf("empty query: 방 설정 (이동) at %d, 채널 만들기 (명령) at %d, want 이동 first (%q)", s, c, ids)
	}

	noSpace := func(s *store.Store) {
		fixtures.Seed(s)
		s.SpaceOrder = nil
		s.Spaces = map[string]*api.SpaceDetail{}
	}
	ids = paletteCommandIDs(uitest.New(t, uitest.WithSeed(noSpace)).Context(), "")
	for _, id := range []string{"channel.create", "invite.create", "invite.direct", "space.settings", "space.leave"} {
		if slices.Contains(ids, id) {
			t.Errorf("no space: unavailable %s listed (%q)", id, ids)
		}
	}
	for _, id := range []string{"space.create", "space.join", "friend.add"} {
		if !slices.Contains(ids, id) {
			t.Errorf("no space: %s missing from %q", id, ids)
		}
	}
}

// TestPaletteCommandWordPrefix (owner report 2026-11-09): English keywords and ids
// match by word prefix only (2+ letters); Korean titles, their chosung and Hangul
// keywords stay fuzzy.
func TestPaletteCommandWordPrefix(t *testing.T) {
	ctx := devChannel(t, uitest.Size{W: 120, H: 40}).Context() // #개발 viewed: 채널 설정 is available
	for q, want := range map[string][]string{
		"cre":      {"channel.create", "space.create", "invite.create"},
		"Cre":      {"channel.create", "space.create", "invite.create"},
		"ch":       {"channel.create", "channel.prefs"},
		"ad":       {"friend.add"},
		"direct m": {"dm.new"},
		"ㅊㄴ":       {"channel.create"},
		"ㅊㄴ ㅁㄷㄱ":   {"channel.create"},
		"새채널":      {"channel.create"},
		"만들기":      {"channel.create", "space.create", "invite.create"},
		"친구 추가":    {"friend.add"},
	} {
		ids := paletteCommandIDs(ctx, q)
		for _, id := range want {
			if !slices.Contains(ids, id) {
				t.Errorf("query %q: %s missing from %q", q, id, ids)
			}
		}
	}
	for q, not := range map[string][]string{
		"a":   {"friend.add", "space.leave", "channel.create", "invite.create", "search.messages"},
		"c":   {"channel.create", "invite.create"},
		"cre": {"friend.add", "search.messages", "space.join", "invite.direct"},
		"ch":  {"search.messages", "space.create"},
		"ate": {"channel.create", "space.create", "invite.create"},
	} {
		ids := paletteCommandIDs(ctx, q)
		for _, id := range not {
			if slices.Contains(ids, id) {
				t.Errorf("query %q: unrelated %s listed (%q)", q, id, ids)
			}
		}
	}
}
