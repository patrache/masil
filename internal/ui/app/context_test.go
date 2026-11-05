package app_test

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/uitest"
)

// runInit runs m.Init()'s Cmd tree synchronously (expanding tea.BatchMsg) and
// returns the produced msgs.
func runInit(t *testing.T, m *app.Model) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	var run func(c tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			for _, sub := range msg {
				run(sub)
			}
		default:
			out = append(out, msg)
		}
	}
	run(m.Init())
	return out
}

// newModel builds an app.Model straight from a raw Config (no harness option exists
// for one), offline and on a fake clock.
func newModel(t *testing.T, cfg *config.Config) *app.Model {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MASIL_CONFIG_DIR", dir)
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	clk := uitest.NewFakeClock(time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC))
	return app.New(app.Options{Paths: paths, Config: cfg, Now: clk.Now, Timers: clk, DisableRealtime: true})
}

func hasToast(msgs []tea.Msg, want ui.ToastMsg) bool {
	for _, m := range msgs {
		if tm, ok := m.(ui.ToastMsg); ok && tm.Text == want.Text && tm.Kind == want.Kind {
			return true
		}
	}
	return false
}

func TestContextInitFromConfig(t *testing.T) {
	cfg := config.Defaults()
	cfg.Theme = "b"
	cfg.Keys = map[string][]string{"palette.open": {"ctrl+p"}, "nope": {"x"}}
	cfg.UI.Images = "off"
	m := newModel(t, cfg)
	ctx := m.Context()
	if ctx.Theme.Name != theme.Midnight {
		t.Fatalf("Theme = %v, want midnight (alias b)", ctx.Theme.Name)
	}
	ctrlP := tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	ctrlK := tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	if !ctx.Keys.Matches(ctrlP, keymap.PaletteOpen) || ctx.Keys.Matches(ctrlK, keymap.PaletteOpen) {
		t.Fatal("[keys] palette.open = [ctrl+p] must replace ctrl+k")
	}
	if ctx.Caps.Graphics != termcaps.GraphicsNone {
		t.Fatalf("Caps.Graphics = %v, want None (images=off)", ctx.Caps.Graphics)
	}
	if ctx.Config != cfg {
		t.Fatal("Context().Config must be Options.Config")
	}
	if ctx.Timers == nil || ctx.RT == nil || ctx.Store == nil || ctx.API == nil || ctx.Loc == nil {
		t.Fatalf("Context fields must be set: %+v", ctx)
	}
	if !hasToast(runInit(t, m), ui.ToastMsg{Text: "키 설정 일부를 무시했어요", Kind: ui.ToastError}) {
		t.Fatal("Init must toast the ignored key override")
	}

	bad := config.Defaults()
	bad.Theme = "neon" // bypassing config.Load
	m = newModel(t, bad)
	if m.Context().Theme.Name != theme.Amber {
		t.Fatalf("unknown theme => %v, want amber", m.Context().Theme.Name)
	}
	if msgs := runInit(t, m); hasToast(msgs, ui.ToastMsg{Text: "키 설정 일부를 무시했어요", Kind: ui.ToastError}) {
		t.Fatal("no key overrides => no toast")
	}
	def := keymap.Default()
	for _, a := range []keymap.Action{keymap.PaletteOpen, keymap.FeedToggle, keymap.NavBack, keymap.ComposeSend} {
		got, want := m.Context().Keys.Binding(a).Keys(), def.Binding(a).Keys()
		if len(got) != len(want) {
			t.Fatalf("%s keys = %v, want defaults %v", a, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s keys = %v, want defaults %v", a, got, want)
			}
		}
	}

	nilCfg := newModel(t, nil)
	if nilCfg.Context().Config == nil || nilCfg.Context().Theme.Name != theme.Amber {
		t.Fatal("nil Config => config.Defaults() with the amber theme")
	}
}
