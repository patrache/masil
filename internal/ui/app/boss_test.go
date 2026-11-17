package app_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// TestBossToggle (v0.1.3-5): ctrl+g swaps the window for the decoy; while it
// shows no key, paste or click reaches masil and nothing of it is drawn;
// ctrl+g again brings back exactly the screen that was there.
func TestBossToggle(t *testing.T) {
	dev := ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}
	h := uitest.New(t, uitest.WithRoute(dev))
	before := h.Screen()
	var reached []tea.Msg
	h.Instance(dev.Key()).(*stub.Chat).OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.PasteMsg, tea.MouseMsg:
			reached = append(reached, msg)
		}
	}

	h.Press("ctrl+g")
	decoy := h.Screen()
	if !strings.Contains(decoy, "esc to interrupt") || !strings.Contains(decoy, "> refactor the session cache") {
		t.Fatalf("ctrl+g: no decoy on screen:\n%s", decoy)
	}
	if strings.Contains(decoy, "masil") || strings.Contains(decoy, "dev") {
		t.Fatalf("decoy leaks masil:\n%s", decoy)
	}

	h.Press("j", "enter", "ctrl+t", "ctrl+k", "ㅎ")
	h.Paste("hello")
	h.Click(5, 5)
	if len(reached) != 0 {
		t.Fatalf("decoy let %d inputs reach the chat: %v", len(reached), reached)
	}
	if h.Screen() == before {
		t.Fatal("a swallowed key closed the decoy")
	}

	h.Advance(time.Duration(30) * time.Second) // it keeps "working"
	if later := h.Screen(); later == decoy || !strings.Contains(later, "Read(internal/cache/session.go)") {
		t.Fatalf("decoy did not advance:\n%s", later)
	}

	h.Press("ctrl+g")
	if got := h.Screen(); got != before {
		t.Fatalf("ctrl+g again: not back to the same screen\n got:\n%s\nwant:\n%s", got, before)
	}
	h.Check()
}

// TestBossKoreanLayout: on a Korean layout ctrl+g may arrive as ctrl+ㅎ.
func TestBossKoreanLayout(t *testing.T) {
	h := uitest.New(t)
	before := h.Screen()
	h.Send(tea.KeyPressMsg{Code: 'ㅎ', Mod: tea.ModCtrl})
	if !strings.Contains(h.Screen(), "esc to interrupt") {
		t.Fatalf("ctrl+ㅎ did not open the decoy:\n%s", h.Screen())
	}
	h.Send(tea.KeyPressMsg{Code: 'ㅎ', Mod: tea.ModCtrl})
	if h.Screen() != before {
		t.Fatal("ctrl+ㅎ again did not return")
	}
}
