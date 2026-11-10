package leet_test

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest"
)

// 0.1.3 patch (tools-nav fix 2): ctrl+t — also ctrl+ㅅ on a Korean layout —
// leaves the nvim panel (nvim keeps running and never sees the key) and goes
// back to the chat.
func TestCtrlTLeavesNvimPanel(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 't', Mod: tea.ModCtrl}, {Code: 'ㅅ', Mod: tea.ModCtrl}} {
		t.Run(key.String(), func(t *testing.T) {
			e := setup(t)
			e.lc.SetDailyDate("2026-11-05")
			withNvim(t, true)
			h := uitest.New(t)
			openToday(t, h)
			h.Press("e", "i")
			h.Send(key)
			if h.Nav().Front == app.FrontTools {
				t.Fatalf("%s did not leave the nvim panel", key.String())
			}
			if keys := e.pane.Keys("leet:today"); !slices.Equal(keys, []string{"i"}) {
				t.Fatalf("nvim got %q, want only i", keys)
			}
		})
	}
}

// ... and from the built-in Go editor in insert mode (it captures printable
// keys): ctrl+t in any spelling still reaches the shell, never the file.
func TestCtrlTLeavesGoeditor(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 't', Mod: tea.ModCtrl}, {Code: 'ㅅ', Mod: tea.ModCtrl}, {Code: 'ㅅ', BaseCode: 't', Mod: tea.ModCtrl}} {
		t.Run(key.String(), func(t *testing.T) {
			e := setup(t)
			e.lc.SetDailyDate("2026-11-05")
			withNvim(t, false)
			h := uitest.New(t)
			openToday(t, h)
			h.Press("e", "i")
			h.Send(key)
			if h.Nav().Front == app.FrontTools {
				t.Fatalf("%s did not leave the Go editor", key.String())
			}
			if s := h.Screen(); strings.Contains(s, "ㅅ") {
				t.Fatalf("the key was typed:\n%s", s)
			}
		})
	}
}

// ... and from the leet terminal (v0.1.3-1): the shell has the keys, but
// ctrl+t in any spelling still leaves for the chat and never reaches it.
func TestCtrlTLeavesLeetTerminal(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 't', Mod: tea.ModCtrl}, {Code: 'ㅅ', Mod: tea.ModCtrl}, {Code: 'ㅅ', BaseCode: 't', Mod: tea.ModCtrl}} {
		t.Run(key.String(), func(t *testing.T) {
			e := setup(t)
			e.lc.SetDailyDate("2026-11-05")
			t.Setenv("SHELL", "/bin/zsh")
			h := uitest.New(t)
			openToday(t, h)
			h.Press("t")
			h.Type("ls")
			h.Send(key)
			if h.Nav().Front == app.FrontTools {
				t.Fatalf("%s did not leave the leet terminal", key.String())
			}
			if keys := e.pane.Keys("leet:today#term"); !slices.Equal(keys, []string{"l", "s"}) {
				t.Fatalf("the shell got %q, want only l s", keys)
			}
		})
	}
}
