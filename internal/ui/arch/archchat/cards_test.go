package archchat

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/collab/collabtest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	_ "github.com/patrache/masil/internal/ui/arch" // the arch.* card renderers
	_ "github.com/patrache/masil/internal/ui/chat" // the real chat
	_ "github.com/patrache/masil/internal/ui/tools"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

func TestMain(m *testing.M) {
	stub.Install() // only the routes chat does not register (feed, home, …)
	os.Exit(m.Run())
}

const (
	cardID   = "01JDCRD0000000000000000009"
	cardBody = "📐 결제 시스템 v3 · 노드 12 · p99 156ms · 약 $6,359/월(추정)"
	docID    = "D0001"
)

var mini = []string{"┌결제 API┐   ┌주 DB─┐", "│▊   28%◆━━▶│   15%│", "└────────┘   └──────┘"}

// designData is a valid arch.design v1 card of a room document in 노바 개발방.
func designData(over map[string]any) json.RawMessage {
	d := map[string]any{
		"title": "결제 시스템 v3", "space_id": fixtures.SpaceNova, "doc_id": docID, "version_no": 3,
		"node_count": 12, "cost_usd_month": 6359, "price_asof": "2026-11", "engine_version": 1,
		"mini": mini, "design": nil,
	}
	for k, v := range over {
		d[k] = v
	}
	b, _ := json.Marshal(d)
	return b
}

// card is a kind "card" message in #카드 by 준호.
func card(id string, typ string, v int, data json.RawMessage, at int) api.Message {
	return api.Message{
		ID: id, ChannelID: fixtures.ChCards, Kind: api.MessageKindCard, Body: cardBody,
		Card:      &api.Card{Type: typ, V: v, Data: data},
		Author:    api.UserSummary{ID: fixtures.UserJunho, Handle: "junho", DisplayName: "준호"},
		CreatedAt: time.Date(2026, 11, 9, 3, at, 0, 0, time.UTC),
	}
}

// open shows #카드 with extra (newest last) after the fixture's messages.
func open(t *testing.T, w, h int, extra ...api.Message) *uitest.Harness {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "arch")
	srv := apitest.New(t)
	fixtures.RegisterAPI(srv)
	srv.Handle("GET /v1/channels/{channel_id}/messages", func(rw http.ResponseWriter, r *http.Request) {
		ms := fixtures.Messages(r.PathValue("channel_id"))
		if r.PathValue("channel_id") == fixtures.ChCards {
			ms = append(slices.Clone(ms), extra...)
		}
		if ms == nil {
			ms = []api.Message{}
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(api.MessageWindow{Items: ms})
	})
	seed := func(s *store.Store) {
		fixtures.Seed(s)
		s.Sync.Bootstrapped = true
		fixtures.Cards(s)
	}
	hs := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(seed), uitest.WithCollab(collabtest.New(t)),
		uitest.WithRoute(ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChCards}))
	hs.Resize(w, h)
	return hs
}

func TestCardRendersInChat(t *testing.T) {
	h := open(t, 120, 40, card(cardID, ui.CardArchDesign, 1, designData(nil), 10))
	h.Check()
	scr := h.Screen()
	for _, want := range []string{"📐 결제 시스템 v3", mini[0], mini[1], "노드 12", "~$6,359/월 (추정 · 2026-11 가격표)", "⏎ 열기"} {
		if !strings.Contains(scr, want) {
			t.Errorf("the card lacks %q:\n%s", want, scr)
		}
	}
	if strings.Contains(scr, cardBody) {
		t.Errorf("a drawn card shows its fallback body:\n%s", scr)
	}
	h.Press("enter") // the newest message (the card) starts selected
	var got *ui.Route
	for _, r := range h.Nav().Tools {
		if r.Kind == ui.RouteTool && r.Arg == ui.ToolArch {
			got = &r
		}
	}
	if got == nil || got.Query["doc"] != docID || got.ID != "arch:doc:"+docID {
		t.Fatalf("enter opened %+v (tools %+v)", got, h.Nav().Tools)
	}
}

// reregister puts back every registration of saved except the arch.* cards:
// an older client (0.1.2) knows none of them.
func reregister(saved ui.Registry) {
	for slot, f := range saved.Panes {
		ui.RegisterPane(slot, f)
	}
	for k, f := range saved.Routes {
		ui.RegisterRoute(k, f)
	}
	for id, f := range saved.Overlays {
		ui.RegisterOverlay(id, f)
	}
	for typ, f := range saved.AlertTargets {
		ui.RegisterAlertTarget(typ, f)
	}
	for typ, r := range saved.Cards {
		if !strings.HasPrefix(typ, "arch.") {
			ui.RegisterCard(typ, r)
		}
	}
	for _, s := range saved.Tools {
		ui.RegisterTool(s)
	}
	for _, k := range saved.KillSwitches {
		ui.RegisterKillSwitch(k)
	}
}

// Acceptance #12: without the renderer the card is exactly its body.
func TestOldClientSeesBody(t *testing.T) {
	saved := ui.Registered()
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	reregister(saved)
	if _, ok := ui.CardFor(ui.CardArchDesign); ok {
		t.Fatal("arch.design is still registered")
	}
	h := open(t, 120, 40, card(cardID, ui.CardArchDesign, 1, designData(nil), 10))
	h.Check()
	scr := h.Screen()
	if !strings.Contains(scr, cardBody) {
		t.Errorf("an old client does not show the body %q:\n%s", cardBody, scr)
	}
	if strings.Contains(scr, mini[0]) || strings.Contains(scr, "⏎ 열기") {
		t.Errorf("an old client shows card data:\n%s", scr)
	}
}

func TestCardRendererHostileData(t *testing.T) {
	long := strings.Repeat("━", 200)
	var many []string
	for range 50 {
		many = append(many, "줄\x1b[31m빨강\u202Eevil"+long)
	}
	hostile := designData(map[string]any{"mini": many, "title": "제목\x1b]0;x\x07\u202E" + strings.Repeat("가", 70), "engine_version": 7})
	msgs := []api.Message{
		card("01JDCRD0000000000000000005", ui.CardArchDesign, 1, hostile, 5),
		card("01JDCRD0000000000000000006", ui.CardArchDesign, 2, designData(nil), 6),
		card("01JDCRD0000000000000000007", ui.CardArchSim, 1, json.RawMessage(`"not an object"`), 7), // (bytes that are not JSON never arrive: the message list would not parse)
		card("01JDCRD0000000000000000008", ui.CardArchSim, 1, json.RawMessage(`[1,2,3]`), 8),
	}
	for _, sz := range [][2]int{{80, 24}, {120, 40}} {
		h := open(t, sz[0], sz[1], msgs...)
		h.Check()
		scr := h.Screen()
		if strings.ContainsAny(scr, "\x1b\x07\u202e") {
			t.Errorf("%dx%d: control or bidi character on screen", sz[0], sz[1])
		}
		// malformed data and v=2 fall back to the body; the hostile card draws
		if n := strings.Count(scr, "📐 결제 시스템 v3 · 노드 12"); n < 1 {
			t.Errorf("%dx%d: no body fallback:\n%s", sz[0], sz[1], scr)
		}
		if !strings.Contains(scr, "📐 제목") {
			t.Errorf("%dx%d: the hostile card is not drawn:\n%s", sz[0], sz[1], scr)
		}
	}
}
