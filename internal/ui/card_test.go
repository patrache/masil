package ui_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
)

func cardLines(*ui.Context, *api.Message, int) []string { return []string{"card"} }

func TestRegisterCardDuplicatePanics(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.ResetRegistryForTest()
	if _, ok := ui.CardFor(ui.CardArchDesign); ok {
		t.Fatal("empty registry has a card")
	}
	ui.RegisterCard(ui.CardArchDesign, ui.CardRenderer{Lines: cardLines})
	mustPanic(t, "duplicate card", func() { ui.RegisterCard(ui.CardArchDesign, ui.CardRenderer{Lines: cardLines}) })
	mustPanic(t, "empty card type", func() { ui.RegisterCard("", ui.CardRenderer{Lines: cardLines}) })
	mustPanic(t, "nil Lines", func() { ui.RegisterCard(ui.CardAPIResult, ui.CardRenderer{}) })
	r, ok := ui.CardFor(ui.CardArchDesign)
	if !ok || r.Lines == nil || r.Open != nil || r.Key != nil {
		t.Fatalf("CardFor = %+v, %v", r, ok)
	}
	if _, ok := ui.CardFor(ui.CardAPIResult); ok {
		t.Fatal("unregistered type found")
	}
	// The snapshot / reset cycle carries cards like the other registrations.
	restore := ui.SnapshotRegistryForTest()
	ui.ResetRegistryForTest()
	if _, ok := ui.CardFor(ui.CardArchDesign); ok {
		t.Fatal("reset kept the card")
	}
	restore()
	if _, ok := ui.CardFor(ui.CardArchDesign); !ok {
		t.Fatal("restore lost the card")
	}
	if len(ui.Registered().Cards) != 1 {
		t.Fatalf("Registered().Cards = %d", len(ui.Registered().Cards))
	}
}

func TestCardTypeConstants(t *testing.T) {
	want := map[string]string{
		ui.CardLeetDaily: "leet.daily", ui.CardLeetSolve: "leet.solve",
		ui.CardArchDesign: "arch.design", ui.CardArchSim: "arch.sim",
		ui.CardAPIResult: "api.result", ui.CardAPIRun: "api.run",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("constant %q, want %q", got, w)
		}
	}
	_ = ui.CardRenderer{
		Open: func(*ui.Context, *api.Message) tea.Cmd { return nil },
		Key:  func(*ui.Context, *api.Message, tea.KeyPressMsg) (bool, tea.Cmd) { return false, nil },
	}
}
