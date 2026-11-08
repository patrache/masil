package chat

import (
	"net/http"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

// systemChannel opens 개발 whose history is a system line (member_joined) and
// one text message; it returns the harness and the system message.
func systemChannel(t *testing.T) (*uitest.Harness, api.Message) {
	t.Helper()
	ch := chanID(t, seeded(), "개발")
	newcomer := api.UserSummary{ID: "01JA0000000000000000NEW001", Handle: "newbie", DisplayName: "새친구"}
	joined := api.Message{ID: msgID(900), ChannelID: ch, Author: newcomer, Kind: "system", CreatedAt: uitest.Start.Add(-time.Minute),
		System: &api.SystemEvent{Event: "member_joined", ActorID: newcomer.ID, TargetUserID: &newcomer.ID}}
	hello := mkMsg(ch, 901, newcomer, "안녕하세요", uitest.Start)
	srv := newAPI(t)
	srv.JSON("GET /v1/channels/"+ch+"/messages", http.StatusOK, api.MessageWindow{Items: []api.Message{joined, hello}})
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(bootstrappedSeed))
	h.Screen()
	return h, joined
}

// D7 (v0.1.1): a system message cannot be a thread root and takes no reaction.
// It stays selectable, but enter / t do not open a thread, r opens no picker,
// a second click does not open a thread, the actions menu has neither item and
// the help line shows neither key.
func TestSystemMessageNoThreadNoReaction(t *testing.T) {
	h, sys := systemChannel(t)
	v := channelOf(t, h)
	if !v.pane.selectID(sys.ID) {
		t.Fatal("a system message is selectable")
	}
	if v.composer.focused() {
		v.composer.blur()
	}
	for _, k := range []string{"enter", "t"} {
		h.Press(k)
		if r, ok := threadRoute(h); ok {
			t.Fatalf("%s opened a thread on a system message: %+v", k, r)
		}
	}
	h.Press("r")
	if n := overlays(h); n != 0 {
		t.Fatalf("r on a system message opens %d overlays", n)
	}
	if selectedID(t, h) != sys.ID {
		t.Fatalf("selection moved to %q", selectedID(t, h))
	}
	for _, b := range v.Help(h.Context()) {
		if d := b.Help().Desc; d == "스레드" || d == "리액션" {
			t.Fatalf("help line on a system message shows %q", d)
		}
	}
	for _, it := range messageActions(h.Context(), h.Context().Store.Channels[v.id()], sys) {
		if it.Label == itemThread || it.Label == itemReact {
			t.Fatalf("actions menu of a system message has %q", it.Label)
		}
	}

	// mouse: a click on the already selected system line does not open a thread
	clickText(t, h, layout.SlotMain, "새친구님이 들어왔어요")
	clickText(t, h, layout.SlotMain, "새친구님이 들어왔어요")
	if r, ok := threadRoute(h); ok {
		t.Fatalf("a second click opened a thread on a system message: %+v", r)
	}

	// a user message still shows (and runs) the thread key
	selectText(t, h, "안녕하세요")
	hs := v.Help(h.Context())
	if len(hs) == 0 || hs[0].Help().Desc != "스레드" {
		t.Fatalf("help on a user message %+v", hs)
	}
}
