package chat

import (
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// jumpUnread is the route the 알림함's 안 읽은 대화 rows open.
func jumpUnread(ch, anchor string) ui.Route {
	return ui.Route{Kind: ui.RouteChannel, ID: ch, Anchor: anchor, Query: map[string]string{ui.QueryJump: ui.JumpUnread}}
}

// jump=unread loads the page around my read pointer and selects the first
// message after it that someone else wrote, not the pointer itself.
func TestChannelJumpUnreadSelectsFirstUnread(t *testing.T) {
	ms := fixtures.Messages(fixtures.ChGeneral) // read pointer = ms[0] in the seed
	h := open(t, uitest.Size{W: 120, H: 40}, jumpUnread(fixtures.ChGeneral, ms[0].ID), uitest.WithSeed(bootstrappedSeed))
	if q := lastQuery(h.API, "/v1/channels/"+fixtures.ChGeneral+"/messages"); q != "around="+ms[0].ID+"&limit=50" {
		t.Fatalf("query %q", q)
	}
	if got := selectedID(t, h); got != ms[1].ID {
		t.Fatalf("selected %q, want the first unread %q", got, ms[1].ID)
	}
	mustContainInOrder(t, h.Screen(), "다들 좋은 아침이에요", "새 메시지", ms[1].Body)
}

// My own messages after the pointer are never "new"; with nothing newer from
// anyone else the anchor itself stays selected.
func TestChannelJumpUnreadSkipsMine(t *testing.T) {
	ms := fixtures.Messages(fixtures.ChChat) // minji, then sam (me)
	h := open(t, uitest.Size{W: 120, H: 40}, jumpUnread(fixtures.ChChat, ms[0].ID), uitest.WithSeed(bootstrappedSeed))
	if got := selectedID(t, h); got != ms[0].ID {
		t.Fatalf("selected %q, want the anchor %q (only my message follows)", got, ms[0].ID)
	}
}

// A cached channel view reopened from the 알림함 takes the newer read pointer
// (divider and jump) instead of the one frozen when it was first built.
func TestChannelJumpUnreadRefreshesReadPointer(t *testing.T) {
	ms := fixtures.Messages(fixtures.ChGeneral)
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}, uitest.WithSeed(bootstrappedSeed))
	id := ms[1].ID
	h.Context().Store.Channels[fixtures.ChGeneral].My.LastReadMessageID = &id
	h.Send(ui.RouteUpdatedMsg{Route: jumpUnread(fixtures.ChGeneral, id)})
	if got := selectedID(t, h); got != ms[2].ID {
		t.Fatalf("selected %q, want %q", got, ms[2].ID)
	}
	if lr := channelOf(t, h).pane.lastRead; lr != id {
		t.Fatalf("divider pointer %q, want %q", lr, id)
	}
	mustContainInOrder(t, h.Screen(), ms[1].Body, "새 메시지", ms[2].Body)
}
