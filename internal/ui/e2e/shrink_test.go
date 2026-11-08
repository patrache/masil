package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// longDev is a 300-message #개발 history ("긴 글 000" … "긴 글 299", two authors
// taking turns, a minute apart).
func longDev() []api.Message {
	base := fixtures.Messages(fixtures.ChDev)
	a, b := base[0].Author, base[1].Author
	start := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	out := make([]api.Message, 300)
	for i := range out {
		au := a
		if i%2 == 1 {
			au = b
		}
		out[i] = api.Message{
			ID: fmt.Sprintf("01JDLONG%018d", i), ChannelID: fixtures.ChDev, Author: au, Kind: "text",
			Body: fmt.Sprintf("긴 글 %03d", i), CreatedAt: start.Add(time.Duration(i) * time.Minute),
		}
	}
	return out
}

// A selection 8 messages above the bottom, chosen while the view is still at the
// bottom, stays on screen when the window shrinks: the bottom pin must not leave
// the message r / t / edit / delete act on out of view.
func TestChatShrinkKeepsSelectionOnScreen(t *testing.T) {
	for _, sz := range []uitest.Size{{W: 80, H: 24}, {W: 61, H: 30}, {W: 50, H: 20}} {
		t.Run(sizeName(sz), func(t *testing.T) {
			srv := productAPI(t)
			msgs := longDev()
			srv.Handle("GET /v1/channels/{channel_id}/messages", func(w http.ResponseWriter, r *http.Request) {
				var ms []api.Message
				if r.PathValue("channel_id") == fixtures.ChDev {
					ms = msgs
				} else {
					ms = fixtures.Messages(r.PathValue("channel_id"))
				}
				if ms == nil {
					ms = []api.Message{}
				}
				writeJSON(w, api.MessageWindow{Items: ms})
			})
			h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(productSeed))
			h.Resize(120, 40)
			openDev(h)
			for range 8 {
				h.Press("k")
			}
			sel := probeSelection(t, h)
			want := ""
			for _, m := range msgs {
				if m.ID == sel {
					want = m.Body
				}
			}
			if want == "" || want == msgs[len(msgs)-1].Body {
				t.Fatalf("k selected %q, want a message above the last one", sel)
			}
			if !strings.Contains(h.Screen(), want) || !strings.Contains(h.Screen(), msgs[len(msgs)-1].Body) {
				t.Fatalf("precondition: %q and the last message on screen at 120x40:\n%s", want, h.Screen())
			}
			h.Resize(sz.W, sz.H)
			if s := h.Screen(); !strings.Contains(s, want) {
				t.Fatalf("selected message %q left the screen after shrinking:\n%s", want, s)
			}
			if got := probeSelection(t, h); got != sel {
				t.Fatalf("selection = %q, want %q", got, sel)
			}
			h.Check()
		})
	}
}
