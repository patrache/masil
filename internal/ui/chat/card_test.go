package chat

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// cardSeed is the bootstrapped fixture seed plus #카드 (fixtures.Cards).
func cardSeed(s *store.Store) {
	bootstrappedSeed(s)
	fixtures.Cards(s)
}

// cardChannel opens #카드.
func cardChannel(t *testing.T, sz uitest.Size) *uitest.Harness {
	t.Helper()
	return open(t, sz, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChCards}, uitest.WithSeed(cardSeed))
}

// designLines is the test renderer of arch.design: a title, a count line, and a
// line far wider than any pane (chat must cut it).
func designLines(ctx *ui.Context, m *api.Message, width int) []string {
	var d struct {
		Title        string
		Nodes, Edges int
	}
	_ = json.Unmarshal(m.Card.Data, &d)
	t := ctx.Theme
	return []string{
		t.S.Base.Bold(true).Render(textwidth.Truncate("📐 "+textwidth.Sanitize(d.Title), width, "…")),
		t.S.Dim.Render(fmt.Sprintf("노드 %d · 엣지 %d", d.Nodes, d.Edges)),
		t.S.Dim.Render(strings.Repeat("━", 400)),
	}
}

// registerDesign registers arch.design for this test (the registry is restored after).
func registerDesign(t *testing.T, r ui.CardRenderer) {
	t.Helper()
	t.Cleanup(ui.SnapshotRegistryForTest())
	if r.Lines == nil {
		r.Lines = designLines
	}
	ui.RegisterCard(ui.CardArchDesign, r)
}

const (
	designBody = "📐 결제 v3 · 노드 14"
	resultBody = "🧪 GET /health → 200"
)

func TestUnknownCardRendersBody(t *testing.T) {
	h := cardChannel(t, uitest.Size{W: 120, H: 40})
	h.Check()
	mustContain(t, h.Screen(), designBody, resultBody)
}

func TestRegisteredCardLines(t *testing.T) {
	registerDesign(t, ui.CardRenderer{})
	for _, sz := range append(slices.Clone(uitest.Required), uitest.Size{W: 40, H: 14}) {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := cardChannel(t, sz)
			h.Check() // no line wider than its pane
			scr := h.Screen()
			if strings.Contains(scr, designBody) {
				t.Fatalf("a registered card is drawn by its renderer, not its body:\n%s", scr)
			}
			if sz.W < 80 {
				// Minimal density: one line per message, the renderer's first.
				mustContain(t, scr, "📐 결제 v3", resultBody)
				return
			}
			mustContain(t, scr, "노드 14 · 엣지 20", resultBody)
		})
	}
	// Compact (line) density shows the renderer's first line.
	h := cardChannel(t, uitest.Size{W: 120, H: 40})
	ctx := h.Context()
	m := fixtures.Messages(fixtures.ChCards)[1]
	if got := compactBody(ctx, ctx.Theme, &m, 40); !strings.Contains(ansi.Strip(got), "📐 결제 v3") {
		t.Fatalf("compact card = %q", got)
	}
}

func TestCardEnterOpens(t *testing.T) {
	var opened []string
	registerDesign(t, ui.CardRenderer{Open: func(_ *ui.Context, m *api.Message) tea.Cmd {
		return func() tea.Msg { opened = append(opened, m.ID); return nil }
	}})
	h := cardChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, designBody)
	h.Press("enter")
	if !slices.Equal(opened, []string{fixtures.MsgCardDesign}) {
		t.Fatalf("opened = %v", opened)
	}
	if _, ok := threadRoute(h); ok {
		t.Fatal("enter on a card with Open must not open the thread")
	}
	// t still opens the thread.
	selectText(t, h, designBody)
	h.Press("t")
	if r, ok := threadRoute(h); !ok || r.ID != fixtures.MsgCardDesign {
		t.Fatalf("t on a card: thread = %+v %v", r, ok)
	}

	// Open nil (an unregistered type, or a renderer without Open) => enter = thread.
	h2 := cardChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h2, resultBody)
	h2.Press("enter")
	if r, ok := threadRoute(h2); !ok || r.ID != fixtures.MsgCardResult {
		t.Fatalf("enter without Open: thread = %+v %v", r, ok)
	}
	if len(opened) != 1 {
		t.Fatalf("opened = %v", opened)
	}
}

// Card messages cannot be edited (api.md 5.10): e does nothing, even on my own
// card, and the menu has no edit item.
func TestCardNotEditable(t *testing.T) {
	h := cardChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, resultBody) // mine, unregistered type
	h.Press("e")
	cc := channelOf(t, h).composer
	if cc.editing != nil || cc.focused() {
		t.Fatal("e edits a card")
	}
	if h.Model.Frame().Toast != nil {
		t.Fatalf("e on a card toasts:\n%s", h.Screen())
	}
	h.Press("m")
	if labels := menuLabels(t, h); slices.Contains(labels, itemEdit) {
		t.Fatalf("card menu has %q: %v", itemEdit, labels)
	}
}

func TestCardKeyHook(t *testing.T) {
	var keys []string
	registerDesign(t, ui.CardRenderer{Key: func(_ *ui.Context, m *api.Message, k tea.KeyPressMsg) (bool, tea.Cmd) {
		if k.String() != "b" {
			return false, nil
		}
		return true, func() tea.Msg { keys = append(keys, m.ID+":b"); return nil }
	}})
	h := cardChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h, designBody)
	before := h.Nav()
	h.Press("b")
	if !slices.Equal(keys, []string{fixtures.MsgCardDesign + ":b"}) {
		t.Fatalf("Key got %v", keys)
	}
	if fmt.Sprint(h.Nav()) != fmt.Sprint(before) {
		t.Fatalf("a handled key reached chat: nav %+v", h.Nav())
	}
	// Not handled => chat's own key (t opens the thread).
	h.Press("t")
	if r, ok := threadRoute(h); !ok || r.ID != fixtures.MsgCardDesign {
		t.Fatalf("unhandled t: thread = %+v %v", r, ok)
	}
	// A plain message never reaches Key.
	h2 := cardChannel(t, uitest.Size{W: 120, H: 40})
	selectText(t, h2, "결제 설계 올려 주세요")
	h2.Press("b")
	if len(keys) != 1 {
		t.Fatalf("Key called for a text message: %v", keys)
	}
}

// A card by a user I blocked is drawn folded; its renderer's Key and Open
// never see keys on it (chat handles it like any folded message).
func TestCardBlockedAuthorFolded(t *testing.T) {
	var got []string
	registerDesign(t, ui.CardRenderer{
		Key: func(_ *ui.Context, m *api.Message, k tea.KeyPressMsg) (bool, tea.Cmd) {
			got = append(got, "key:"+k.String())
			return true, nil
		},
		Open: func(_ *ui.Context, m *api.Message) tea.Cmd {
			return func() tea.Msg { got = append(got, "open:"+m.ID); return nil }
		},
	})
	h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChCards},
		uitest.WithSeed(func(s *store.Store) {
			cardSeed(s)
			s.Blocks[fixtures.UserJunho] = uitest.Start
		}))
	if scr := h.Screen(); !strings.Contains(scr, "차단한 사용자의 메시지") {
		t.Fatalf("the blocked author's card is not folded:\n%s", scr)
	}
	for _, k := range []string{"b", "enter", "e"} {
		m := selectText(t, h, designBody)
		if m.ID != fixtures.MsgCardDesign {
			t.Fatalf("selected %s", m.ID)
		}
		h.Press(k)
		if cc := channelOf(t, h).composer; cc.editing != nil {
			t.Fatalf("%s edits a blocked author's card", k)
		}
	}
	if len(got) != 0 {
		t.Fatalf("renderer hooks ran on a folded card: %v", got)
	}
}

// New goldens only: #카드 lives in its own seed (fixtures.Cards).
func TestCardGolden(t *testing.T) {
	registerDesign(t, ui.CardRenderer{})
	for _, sz := range append(slices.Clone(uitest.Required), uitest.Size{W: 40, H: 14}) {
		t.Run(fmt.Sprintf("%dx%d", sz.W, sz.H), func(t *testing.T) {
			h := cardChannel(t, sz)
			paneGolden(t, h, layout.SlotMain, "cards")
		})
	}
}
