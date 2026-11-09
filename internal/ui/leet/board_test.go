package leet_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// boardAPI answers the space board (per period) and the friends board.
func boardAPI(h *uitest.Harness) {
	h.API.Handle("GET /v1/spaces/{space_id}/leet/board", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("period")
		solved := map[string]int{"week": 1, "month": 4, "all": 12}[p]
		writeJSON(w, api.LeetBoard{Period: p, Items: []api.LeetBoardEntry{
			{User: person(fixtures.UserMinji, "minji", "민지"), Solved: solved + 2, Streak: 3},
			{User: person(fixtures.UserSam, "sam", "sam"), Solved: solved + 1, Streak: 2},
			{User: person(fixtures.UserJunho, "junho", "준호"), Solved: solved, Streak: 0},
		}})
	})
	h.API.JSON("GET /v1/leet/friends/board", http.StatusOK, api.LeetFriendsBoard{Items: []api.LeetFriendStreak{
		{User: person(fixtures.UserJunho, "junho", "준호"), Current: 9, Best: 21},
		{User: person(fixtures.UserSam, "sam", "sam"), Current: 2, Best: 5},
	}})
}

// boardQueries are the board requests in order: "period=…" or "friends".
func boardQueries(h *uitest.Harness) []string {
	var out []string
	for _, r := range h.API.Requests() {
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.Path, "/v1/spaces/") && strings.HasSuffix(r.Path, "/leet/board"):
			out = append(out, r.Query)
		case r.Method == "GET" && r.Path == "/v1/leet/friends/board":
			out = append(out, "friends")
		}
	}
	return out
}

// openBoard runs the palette's leet.board in a 노바 개발방 channel.
func openBoard(h *uitest.Harness) {
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}})
	h.Send(ui.RunCommandMsg{ID: "leet.board"})
}

func TestBoardTabs(t *testing.T) {
	setup(t)
	h := uitest.New(t)
	boardAPI(h)
	openBoard(h)
	if s := overlayText(t, h); !strings.Contains(s, compact("1. 민지 · 3일 · 🔥3")) {
		t.Fatalf("week board:\n%s", h.Screen())
	}
	h.Press("right")
	if s := overlayText(t, h); !strings.Contains(s, compact("1. 민지 · 6일")) {
		t.Fatalf("month board:\n%s", h.Screen())
	}
	h.Press("right")
	h.Press("right")
	if s := overlayText(t, h); !strings.Contains(s, compact("1. 준호 · 🔥9")) {
		t.Fatalf("friends board:\n%s", h.Screen())
	}
	want := []string{"period=week", "period=month", "period=all", "friends"}
	if got := boardQueries(h); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	// Going back shows what was loaded; no second request.
	h.Press("left", "left", "left")
	if got := boardQueries(h); len(got) != 4 {
		t.Fatalf("requests = %v", got)
	}
	h.Press("esc")
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("esc left %d overlays", n)
	}
}

func TestBoardGolden(t *testing.T) {
	setup(t)
	uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
		boardAPI(h)
		openBoard(h)
		h.Check()
		h.Golden("leet_board")
	})
}
