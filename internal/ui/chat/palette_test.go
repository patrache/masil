package chat

import (
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// chatOverlayIDs are the chat overlays every one of which must open by its
// registered id (ui.OpenOverlayMsg{ID, Args}).
var chatOverlayIDs = []string{
	"chat.reaction_users", "chat.message_actions", "chat.space_add", "chat.space_switch", "chat.space_create",
	"chat.space_join", "chat.dm_new", "chat.channel_prefs", "chat.channel_create", "chat.invite_create",
	"chat.invite_created", "chat.invite_direct", "chat.pins", "chat.friend_add",
}

// argsFor is the OpenOverlayMsg args of overlay id in h (#개발 open): a channel id,
// "<message id> 👍", "<channel id> <message id>", a space id or an invite code.
func argsFor(t testing.TB, h *uitest.Harness, id string) string {
	t.Helper()
	switch id {
	case "chat.reaction_users":
		return fixtureMsg(t, h, "오늘 배포 몇 시에 해요?").ID + " 👍"
	case "chat.message_actions":
		return devID(t, h) + " " + fixtureMsg(t, h, "오늘 배포 몇 시에 해요?").ID
	case "chat.channel_prefs", "chat.pins":
		return devID(t, h)
	case "chat.channel_create", "chat.invite_create", "chat.invite_direct":
		return fixtures.SpaceNova
	case "chat.invite_created":
		return testInviteCode
	}
	return ""
}

func TestChatPaletteCommandsRegistered(t *testing.T) {
	want := []string{"space.create", "space.join", "space.leave", "space.settings", "channel.create", "channel.prefs", "dm.new", "friends", "friend.add", "invite.create", "invite.direct", "invites.inbox", "search.messages", "pins.show"}
	if !slices.Equal(chatCommandIDs, want) {
		t.Fatalf("chatCommandIDs = %q, want the CA 5.6 chat row %q", chatCommandIDs, want)
	}
	for _, id := range chatCommandIDs {
		c, ok := palette.Lookup(id)
		if !ok {
			t.Errorf("%s not registered by ui/chat", id)
			continue
		}
		if c.TitleIn(i18n.KoT()) == "" || c.Run == nil || (c.Group != palette.GroupNav && c.Group != palette.GroupCommand && c.Group != palette.GroupSettings) {
			t.Errorf("%s: incomplete command %+v", id, c)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s not registered by ui/chat", id)
				}
			}()
			palette.Register(palette.Command{ID: id}) // duplicate id must panic
		}()
	}
}

func TestChatOverlaysRegistered(t *testing.T) {
	for _, id := range chatOverlayIDs {
		if _, ok := ui.Registered().Overlays[id]; !ok {
			t.Errorf("%s is not a registered overlay", id)
			continue
		}
		h := devChannel(t, uitest.Size{W: 120, H: 40})
		h.Send(ui.OpenOverlayMsg{ID: id, Args: argsFor(t, h, id)})
		if n := overlays(h); n != 1 {
			t.Errorf("%s did not open (%d overlays)", id, n)
		}
	}
}

func TestPaletteRunTargets(t *testing.T) {
	sz := uitest.Size{W: 120, H: 40}
	cases := []struct {
		id, args string
		want     ui.Route
	}{
		{"friends", "", ui.Route{Kind: ui.RouteFriends}},
		{"invites.inbox", "", ui.Route{Kind: ui.RouteInvite, Arg: "direct"}},
		{"search.messages", "배포", ui.Route{Kind: ui.RouteSearch, Arg: "배포"}},
		{"space.settings", "", ui.Route{Kind: ui.RouteSpaceSettings, ID: fixtures.SpaceNova}},
	}
	for _, c := range cases {
		h := devChannel(t, sz)
		h.Send(ui.RunCommandMsg{ID: c.id, Args: c.args})
		if got := top(h); got.Key() != c.want.Key() {
			t.Errorf("%s → %+v, want %+v", c.id, got, c.want)
		}
	}

	// space.leave → Confirm overlay (a member; the owner is told to transfer first)
	h := devChannel(t, sz)
	setRole(h, api.RoleMember)
	h.Send(ui.RunCommandMsg{ID: "space.leave"})
	if overlays(h) != 1 || !strings.Contains(h.Screen(), "나가기") {
		t.Fatalf("space.leave opens a confirmation:\n%s", h.Screen())
	}
	if n := len(reqs(h.API, "DELETE", "/v1/spaces/"+fixtures.SpaceNova+"/members/@me")); n != 0 {
		t.Fatal("nothing is left before confirming")
	}

	// pins.show → chat.pins of the viewed channel
	h = devChannel(t, sz)
	h.Send(ui.RunCommandMsg{ID: "pins.show"})
	if overlays(h) != 1 || len(reqs(h.API, "GET", pinsPath(devID(t, h)))) != 1 {
		t.Fatalf("pins.show opens the viewed channel's pins:\n%s", h.Screen())
	}
}
