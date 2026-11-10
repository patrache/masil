package leet_test

import (
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// Every new screen keeps the frame whole at every size (Global Constraints:
// uitest.AllSizes, not only the golden sizes).

// The overlays: first run, settings, login, ranking.
func TestLeetOverlaysIntegrityAllSizes(t *testing.T) {
	setup(t)
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
		boardAPI(h)
		leetMe(h, api.LeetMe{})
		h.Press("ctrl+t", "1") // the first-run notice
		h.Check()
		h.Send(ui.CloseOverlayMsg{})
		for _, id := range []string{leet.OverlaySettings, leet.OverlayLogin} {
			h.Send(ui.OpenOverlayMsg{ID: id})
			h.Check()
			h.Send(ui.CloseOverlayMsg{})
		}
		openBoard(h)
		h.Check()
		h.Press("right", "right", "right") // every tab, the friends one too
		h.Check()
	})
}

// The problem with a result, the editor and the card dialog after an
// Accepted submit.
func TestLeetJudgeIntegrityAllSizes(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	withNvim(t, false)
	uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
		leetMe(h, api.LeetMe{DailySpaces: []api.LeetDailySpace{{SpaceID: fixtures.SpaceNova, ChannelID: "01JB0000000000000000000DLY"}}})
		solveOK(h)
		signIn(t, e, h)
		e.lc.QueueCheck(runID, "check_run_success.json")
		e.lc.QueueCheck(submitID, "check_submit_accepted.json")
		openToday(t, h)
		h.Press("r")
		h.Advance(time.Second)
		h.Check()
		h.Press("3")
		h.Check()
		h.Press("e")
		h.Check()
		h.Press("esc")
		h.Advance(10 * time.Second)
		h.Press("s")
		h.Advance(time.Second)
		h.Check()
		h.Press("c")
		h.Check()
	})
}

// The #daily cards with hostile data, folded and unfolded.
func TestLeetCardsIntegrityAllSizes(t *testing.T) {
	setup(t)
	evil := "\x1b[31m\u202eEvil\u202c\x07 Title\x1b]8;;http://x\x07link"
	root := cardMessage(msgDailyRoot, person(fixtures.UserMinji, "\x1b[2Jminji", evil), dailyBody, ui.CardLeetDaily, map[string]any{
		"date": "2026-11-05", "frontend_id": "16\x1b[5m06", "slug": slug1606,
		"title": evil + " Find Servers That Handled Most Number of Requests and Then Some More Words", "difficulty": "Ha\u202erd",
		"tags": []string{"Arr\x1b[1may"}, "is_paid_only": false,
	}, time.Date(2026, 11, 5, 0, 1, 0, 0, time.UTC))
	root.Thread = &api.ThreadSummary{ReplyCount: 2, ParticipantIDs: []string{fixtures.UserMinji, fixtures.UserJunho}}
	for _, sz := range uitest.AllSizes() {
		t.Run(sizeName(sz), func(t *testing.T) {
			h := dailyHarness(t, sz, root)
			showChannel(h)
			h.Check()
			expandFirstSolve(h)
			h.Check()
			h.Press("enter")
			h.Check()
		})
	}
}
