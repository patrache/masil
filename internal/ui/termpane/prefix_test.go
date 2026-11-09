package termpane_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/termpane"
)

func prefixCtx() *ui.Context {
	return &ui.Context{Keys: keymap.Default(), T: i18n.KoT(), Post: ui.DiscardPoster{}}
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+\\":
		return tea.KeyPressMsg{Code: '\\', Mod: tea.ModCtrl}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func TestPrefixExtraReservedPanics(t *testing.T) {
	for _, k := range []string{"x", "esc", "ctrl+\\", "z"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewPrefix([%q]) did not panic", k)
				}
			}()
			termpane.NewPrefix([]string{k})
		}()
	}
	termpane.NewPrefix([]string{"r", "s", "1"}) // a tool's own keys are fine
}

func TestPrefixActions(t *testing.T) {
	ctx := prefixCtx()
	cases := []struct {
		key  string
		want termpane.PrefixAction
		dir  string
	}{
		{"esc", termpane.PrefixLeave, ""},
		{"h", termpane.PrefixFocus, "h"},
		{"left", termpane.PrefixFocus, "h"},
		{"l", termpane.PrefixFocus, "l"},
		{"z", termpane.PrefixZoom, ""},
		{"p", termpane.PrefixPalette, ""},
		{"t", termpane.PrefixSurface, ""},
		{"[", termpane.PrefixScroll, ""},
		{"x", termpane.PrefixKill, ""},
		{"?", termpane.PrefixHelp, ""},
		{"ctrl+\\", termpane.PrefixLiteral, ""},
		{"r", termpane.PrefixExtra, ""},
		{"q", termpane.PrefixCancel, ""},
	}
	for _, c := range cases {
		p := termpane.NewPrefix([]string{"r"})
		if r := p.Key(ctx, press("a")); r.Action != termpane.PrefixNone || p.Active() {
			t.Fatalf("a before the prefix = %+v", r)
		}
		if r := p.Key(ctx, press("ctrl+\\")); r.Action != termpane.PrefixArm || r.Cmd == nil || !p.Active() {
			t.Fatalf("ctrl+\\ = %+v (active %v)", r, p.Active())
		}
		r := p.Key(ctx, press(c.key))
		if r.Action != c.want || r.Dir != c.dir || p.Active() {
			t.Errorf("ctrl+\\ %s = %+v (active %v), want action %v dir %q", c.key, r, p.Active(), c.want, c.dir)
		}
		if c.want == termpane.PrefixExtra && r.Key != c.key {
			t.Errorf("extra key = %q", r.Key)
		}
	}
}

func TestPrefixTimeoutMsg(t *testing.T) {
	ctx := prefixCtx()
	p, other := termpane.NewPrefix(nil), termpane.NewPrefix(nil)
	r := p.Key(ctx, press("ctrl+\\"))
	other.Key(ctx, press("ctrl+\\"))
	msg := r.Cmd()
	if !p.Update(msg) || p.Active() {
		t.Fatal("the timeout did not cancel the prefix")
	}
	if other.Update(msg) || !other.Active() {
		t.Fatal("another prefix took the timeout")
	}
	r = p.Key(ctx, press("ctrl+\\"))
	p.Key(ctx, press("esc"))
	p.Key(ctx, press("ctrl+\\")) // armed again: the stale timeout must not cancel it
	if p.Update(msg) || !p.Active() {
		t.Fatal("a stale timeout cancelled a new prefix")
	}
	_ = r
}
