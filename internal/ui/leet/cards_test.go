package leet_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// The #daily fixture: a leet.daily root in #카드 (fixtures.Cards) first
// opened by 민지, and two leet.solve replies in its thread — 준호's with his
// Go code (a spoiler), 민지's without code.
const (
	msgDailyRoot = "01JDLEET00000000000000DLY1"
	msgSolveCode = "01JDLEET00000000000000SLV1"
	msgSolvePlan = "01JDLEET00000000000000SLV2"
	codeSnippet  = "func busiestServers(k int, arrival []int, load []int) []int {\n\treturn nil\n}"
	dailyBody    = "🧩 오늘의 문제 1606 · https://leetcode.com/problems/" + slug1606 + "/"
)

func person(id, handle, name string) api.UserSummary {
	return api.UserSummary{ID: id, Handle: handle, DisplayName: name}
}

func cardMessage(id string, author api.UserSummary, body, typ string, data any, at time.Time) api.Message {
	raw, _ := json.Marshal(data)
	return api.Message{ID: id, ChannelID: fixtures.ChCards, Author: author, Kind: api.MessageKindCard, Body: body,
		Card: &api.Card{Type: typ, V: 1, Data: raw}, Mentions: []api.MentionRef{}, Reactions: []api.ReactionSummary{}, CreatedAt: at}
}

// dailyRoot is the root card; slug is its problem.
func dailyRoot(slug string) api.Message {
	m := cardMessage(msgDailyRoot, person(fixtures.UserMinji, "minji", "민지"), dailyBody, ui.CardLeetDaily, map[string]any{
		"date": "2026-11-05", "frontend_id": "1606", "slug": slug,
		"title": "Find Servers That Handled Most Number of Requests", "difficulty": "Hard",
		"tags": []string{"Array", "Heap (Priority Queue)"}, "is_paid_only": false,
	}, time.Date(2026, 11, 5, 0, 1, 0, 0, time.UTC))
	m.Thread = &api.ThreadSummary{ReplyCount: 2, ParticipantIDs: []string{fixtures.UserMinji, fixtures.UserJunho}}
	return m
}

func solveReplies() []api.Message {
	root := msgDailyRoot
	withCode := cardMessage(msgSolveCode, person(fixtures.UserJunho, "junho", "준호"), "✅ 준호 · Go · 1606", ui.CardLeetSolve, map[string]any{
		"date": "2026-11-05", "frontend_id": "1606", "slug": slug1606, "lang": "golang", "runtime_percentile": 85.31,
		"code": map[string]any{"lang": "golang", "text": codeSnippet}, "spoiler": true,
	}, time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC))
	plain := cardMessage(msgSolvePlan, person(fixtures.UserMinji, "minji", "민지"), "✅ 민지 · Python3 · 1606", ui.CardLeetSolve, map[string]any{
		"date": "2026-11-05", "frontend_id": "1606", "slug": slug1606, "lang": "python3", "runtime_percentile": nil, "code": nil, "spoiler": false,
	}, time.Date(2026, 11, 5, 1, 30, 0, 0, time.UTC))
	withCode.ThreadRootID, plain.ThreadRootID = &root, &root
	return []api.Message{withCode, plain}
}

// cardsSeed is the bootstrapped fixture seed plus #카드.
func cardsSeed(s *store.Store) {
	fixtures.Seed(s)
	s.Sync.Bootstrapped = true
	fixtures.Cards(s)
}

// serveDaily makes masil answer #카드 with root and its thread with the solves.
func serveDaily(h *uitest.Harness, root api.Message) {
	h.API.Handle("GET /v1/channels/{channel_id}/messages", func(w http.ResponseWriter, r *http.Request) {
		items := fixtures.Messages(r.PathValue("channel_id"))
		if r.PathValue("channel_id") == fixtures.ChCards {
			items = []api.Message{root}
		}
		if items == nil {
			items = []api.Message{}
		}
		writeJSON(w, api.MessageWindow{Items: items})
	})
	h.API.Handle("GET /v1/messages/{message_id}/thread", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, api.ThreadWindow{Root: root, Items: solveReplies()})
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// dailyHarness is a harness with #카드 serving root.
func dailyHarness(t *testing.T, sz uitest.Size, root api.Message) *uitest.Harness {
	t.Helper()
	h := uitest.New(t, uitest.WithSeed(cardsSeed))
	h.Resize(sz.W, sz.H)
	serveDaily(h, root)
	firstRunDone(t, h)
	return h
}

// showChannel opens #카드.
func showChannel(h *uitest.Harness) {
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChCards}})
}

// showThread opens the root's thread with anchor (a reply id, "" = none) selected.
func showThread(h *uitest.Harness, anchor string) {
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteThread, ID: msgDailyRoot, Arg: fixtures.ChCards, Anchor: anchor}})
}

// selectLast leaves the composer and selects the newest message of the view.
func selectLast(h *uitest.Harness) { h.Press("esc", "G") }

// leetInstance is the open leet instance id (or "").
func leetInstance(h *uitest.Harness, id string) ui.Component {
	return h.Instance(ui.Route{Kind: ui.RouteTool, ID: id, Arg: "leet"}.Key())
}

func TestDailyCardGolden(t *testing.T) {
	setup(t)
	for _, sz := range leetSizes {
		t.Run(sizeName(sz), func(t *testing.T) {
			h := dailyHarness(t, sz, dailyRoot(slug1606))
			showChannel(h)
			s := h.Screen()
			full := h.Context().Mode >= layout.ModeCompact
			if full && !strings.Contains(s, "처음 연 사람 민지") || !full && !strings.Contains(s, "🧩 1606 Hard · 풀이 2") {
				t.Fatalf("no daily card:\n%s", s)
			}
			if !strings.Contains(s, "🧩") || strings.Contains(s, "leetcode.com/problems") {
				t.Fatalf("daily card drawn from its body:\n%s", s)
			}
			h.Check()
			h.Golden("leet_daily_card")
		})
	}
}

func TestSolveCardGolden(t *testing.T) {
	setup(t)
	for _, sz := range leetSizes {
		t.Run(sizeName(sz), func(t *testing.T) {
			h := dailyHarness(t, sz, dailyRoot(slug1606))
			showThread(h, "")
			s := h.Screen()
			if !strings.Contains(s, "✅ 준호 · Go") {
				t.Fatalf("no solve card:\n%s", s)
			}
			if strings.Contains(s, "busiestServers") {
				t.Fatalf("spoiler shown folded:\n%s", s)
			}
			h.Check()
			h.Golden("leet_solve_card")
		})
	}
}

// An older client (no card renderers) draws the cards from their bodies and
// never shows the code.
func TestOldClientFallback(t *testing.T) {
	setup(t)
	t.Cleanup(ui.SnapshotRegistryForTest())
	saved := ui.Registered()
	ui.ResetRegistryForTest()
	for slot, f := range saved.Panes {
		ui.RegisterPane(slot, f)
	}
	for kind, f := range saved.Routes {
		ui.RegisterRoute(kind, f)
	}
	for id, f := range saved.Overlays {
		ui.RegisterOverlay(id, f)
	}
	for typ, f := range saved.AlertTargets {
		ui.RegisterAlertTarget(typ, f)
	}
	for _, s := range saved.Tools {
		ui.RegisterTool(s)
	}
	for _, k := range saved.KillSwitches {
		ui.RegisterKillSwitch(k)
	}
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot(slug1606))
	showChannel(h)
	if s := h.Screen(); !strings.Contains(s, "leetcode.com/problems/"+slug1606) || strings.Contains(s, "처음 연 사람") {
		t.Fatalf("old client does not show the body link:\n%s", s)
	}
	showThread(h, msgSolveCode)
	h.Press("enter")
	if s := h.Screen(); strings.Contains(s, "func busiestServers") || !strings.Contains(s, "✅ 준호 · Go") {
		t.Fatalf("old client thread:\n%s", s)
	}
}

// expandFirstSolve selects 준호's solve card (the older reply) and presses enter.
func expandFirstSolve(h *uitest.Harness) {
	showThread(h, msgSolveCode)
	h.Press("enter")
}

func TestSpoilerConfirmWhenUnsolved(t *testing.T) {
	setup(t)
	const ask = "아직 안 풀었어요. 그래도 볼까요?"
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot(slug1606))
	expandFirstSolve(h)
	if s := h.Screen(); !strings.Contains(s, ask) || strings.Contains(s, "busiestServers") {
		t.Fatalf("no spoiler confirm (or the code before it):\n%s", s)
	}
	h.Press("esc") // keeps it folded; the thread stays
	if s := h.Screen(); strings.Contains(s, ask) || strings.Contains(s, "busiestServers") || !strings.Contains(s, "▸ 코드 보기") {
		t.Fatalf("esc did not cancel:\n%s", s)
	}
	h.Press("enter", "enter")
	if s := h.Screen(); !strings.Contains(s, "busiestServers") || strings.Contains(s, ask) {
		t.Fatalf("code not shown after yes:\n%s", s)
	}
	h.Check()
	// Folding it again needs no confirm.
	h.Press("enter")
	if s := h.Screen(); strings.Contains(s, "busiestServers") || strings.Contains(s, ask) {
		t.Fatalf("not folded:\n%s", s)
	}
}

func TestSpoilerNoConfirmWhenSolved(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot(slug1606))
	leetMe(h, api.LeetMe{Current: 1, Recent: []api.LeetSolve{{Date: "2026-11-05", Slug: slug1606, Lang: "golang"}}})
	dailyReport(h, "2026-11-05", slug1606)
	openToday(t, h) // the tool asks masil for my solves
	expandFirstSolve(h)
	if s := h.Screen(); !strings.Contains(s, "busiestServers") || strings.Contains(s, "아직 안 풀었어요") {
		t.Fatalf("code not shown:\n%s", s)
	}
}

func TestDailyCardHostileSlug(t *testing.T) {
	setup(t)
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot("../../x"))
	showChannel(h)
	if s := h.Screen(); !strings.Contains(s, "leetcode.com/problems") || strings.Contains(s, "처음 연 사람") {
		t.Fatalf("a bad slug is not drawn as the body:\n%s", s)
	}
	selectLast(h)
	h.Press("enter")
	for _, id := range []string{"leet:2026-11-05/../../x", "leet:today"} {
		if leetInstance(h, id) != nil {
			t.Fatalf("enter opened %s", id)
		}
	}
	if strings.Contains(h.Screen(), "../../x") {
		t.Fatalf("hostile slug on screen:\n%s", h.Screen())
	}
}

func TestMismatchWarning(t *testing.T) {
	setup(t)
	const warn = "내가 받은 오늘의 문제와 달라요"
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot("two-sum"))
	other := q1606()
	if err := leetcode.OpenCache(h.Context().Paths.Cache).PutDaily("2026-11-05", other); err != nil {
		t.Fatal(err)
	}
	showChannel(h)
	if s := h.Screen(); !strings.Contains(s, warn) {
		t.Fatalf("no mismatch warning:\n%s", s)
	}
	h2 := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot(slug1606))
	if err := leetcode.OpenCache(h2.Context().Paths.Cache).PutDaily("2026-11-05", other); err != nil {
		t.Fatal(err)
	}
	showChannel(h2)
	if s := h2.Screen(); strings.Contains(s, warn) {
		t.Fatalf("warning for the same problem:\n%s", s)
	}
}

func TestEnterOpensDate(t *testing.T) {
	setup(t)
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot(slug1606))
	showChannel(h)
	selectLast(h)
	h.Press("enter")
	if leetInstance(h, "leet:2026-11-05/"+slug1606) == nil {
		t.Fatalf("enter did not open the day's problem:\n%s", h.Screen())
	}
}

// b on the root card opens the space's ranking.
func TestDailyCardBoardKey(t *testing.T) {
	setup(t)
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot(slug1606))
	boardAPI(h)
	showChannel(h)
	selectLast(h)
	h.Press("b")
	if got := boardQueries(h); len(got) != 1 || got[0] != "period=week" {
		t.Fatalf("board requests = %v\n%s", got, h.Screen())
	}
}

// With the flag off the cards are drawn from their bodies and enter opens
// the thread as for any message.
func TestCardsFlagOff(t *testing.T) {
	setup(t)
	t.Setenv("MASIL_FEATURES", "")
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot(slug1606))
	showChannel(h)
	if s := h.Screen(); !strings.Contains(s, "leetcode.com/problems/"+slug1606) || strings.Contains(s, "처음 연 사람") {
		t.Fatalf("flag off draws the card:\n%s", s)
	}
	selectLast(h)
	h.Press("enter")
	if leetInstance(h, "leet:2026-11-05/"+slug1606) != nil {
		t.Fatal("flag off opened the tool")
	}
}

// A10.3: when the root card's problem differs from mine, enter opens mine
// (today's: the today instance) and not the card's slug.
func TestMismatchEnterOpensMine(t *testing.T) {
	setup(t)
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, dailyRoot("two-sum"))
	if err := leetcode.OpenCache(h.Context().Paths.Cache).PutDaily("2026-11-05", q1606()); err != nil {
		t.Fatal(err)
	}
	showChannel(h)
	selectLast(h)
	h.Press("enter")
	if leetInstance(h, "leet:2026-11-05/two-sum") != nil {
		t.Fatalf("enter opened the card's problem, not mine:\n%s", h.Screen())
	}
	if leetInstance(h, "leet:today") == nil {
		t.Fatalf("enter did not open my problem of the day:\n%s", h.Screen())
	}
}

// A past day: mine is opened by its own slug.
func TestMismatchEnterOpensMinePastDay(t *testing.T) {
	setup(t)
	root := dailyRoot("two-sum")
	var data map[string]any
	_ = json.Unmarshal(root.Card.Data, &data)
	data["date"] = "2026-11-04"
	root.Card.Data, _ = json.Marshal(data)
	h := dailyHarness(t, uitest.Size{W: 120, H: 40}, root)
	if err := leetcode.OpenCache(h.Context().Paths.Cache).PutDaily("2026-11-04", q1606()); err != nil {
		t.Fatal(err)
	}
	showChannel(h)
	selectLast(h)
	h.Press("enter")
	if leetInstance(h, "leet:2026-11-04/"+slug1606) == nil {
		t.Fatalf("enter did not open my problem of that day:\n%s", h.Screen())
	}
}
