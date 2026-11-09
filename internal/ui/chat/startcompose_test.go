package chat

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// Typing Korean (an IME) into an unfocused conversation starts composing: the
// key focuses the composer and lands in it, without pressing compose.focus (i)
// in an English layout first.

// hangulKey is one IME-committed key press of text s (Code = its rune when s
// is a single rune, else tea.KeyExtended), like the harness's Type.
func hangulKey(s string) tea.KeyPressMsg {
	k := tea.KeyPressMsg{Code: tea.KeyExtended, Text: s}
	if rs := []rune(s); len(rs) == 1 {
		k.Code = rs[0]
	}
	return k
}

// composeSurface is one conversation view under test (channel or thread).
type composeSurface struct {
	name string
	// open opens the view focused with its composer unfocused.
	open func(t *testing.T, opts ...uitest.Option) *uitest.Harness
	// composer is the view's composer.
	composer func(t testing.TB, h *uitest.Harness) *chatComposer
	// selected is the id of the view's selected row.
	selected func(t testing.TB, h *uitest.Harness) string
	// readOnly marks the conversation read-only in a seed.
	readOnly func(s *store.Store)
	// oneRow: the conversation has a single message (no selection to move).
	oneRow bool
}

func composeSurfaces() []composeSurface {
	return []composeSurface{
		{
			name: "channel",
			open: func(t *testing.T, opts ...uitest.Option) *uitest.Harness {
				return open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}, append([]uitest.Option{uitest.WithSeed(bootstrappedSeed)}, opts...)...)
			},
			composer: func(t testing.TB, h *uitest.Harness) *chatComposer { return channelOf(t, h).composer },
			selected: selectedID,
			readOnly: func(s *store.Store) {
				bootstrappedSeed(s)
				s.Channels[fixtures.ChDev].ReadOnly = true
			},
		},
		{
			name: "dm", // on the DM surface (CA 5.3.3)
			open: func(t *testing.T, opts ...uitest.Option) *uitest.Harness {
				h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteHome}, append([]uitest.Option{uitest.WithSeed(bootstrappedSeed)}, opts...)...)
				h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}})
				if !h.Nav().InDM {
					t.Fatalf("precondition: the DM is on the DM surface: %+v", h.Nav())
				}
				return h
			},
			composer: func(t testing.TB, h *uitest.Harness) *chatComposer { return channelOf(t, h).composer },
			selected: selectedID,
			readOnly: func(s *store.Store) {
				bootstrappedSeed(s)
				s.Channels[fixtures.DMMinji].ReadOnly = true
			},
			oneRow: true,
		},
		{
			name: "thread",
			open: func(t *testing.T, opts ...uitest.Option) *uitest.Harness {
				h := open(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}, append([]uitest.Option{uitest.WithSeed(bootstrappedSeed)}, opts...)...)
				selectText(t, h, "오늘 배포")
				h.Press("t")
				return h
			},
			composer: func(t testing.TB, h *uitest.Harness) *chatComposer { return threadOfH(t, h).composer },
			selected: threadSelected,
			readOnly: func(s *store.Store) {
				bootstrappedSeed(s)
				s.Channels[fixtures.ChDev].ReadOnly = true
			},
		},
	}
}

func TestHangulStartsComposing(t *testing.T) {
	for _, sf := range composeSurfaces() {
		for _, tc := range []struct{ name, key string }{
			{"syllable", "안"},
			{"multi-rune commit", "안녕"},
			{"compatibility jamo", "ㅑ"},
			{"conjoining jamo", "ᄀ"},
		} {
			t.Run(sf.name+"/"+tc.name, func(t *testing.T) {
				h := sf.open(t)
				cc := sf.composer(t, h)
				if cc.focused() {
					t.Fatal("precondition: composer unfocused")
				}
				h.Send(hangulKey(tc.key))
				if !cc.focused() {
					t.Fatalf("%q focuses the composer", tc.key)
				}
				if v := cc.c.Value(); v != tc.key {
					t.Fatalf("composer value = %q, want %q (exactly once)", v, tc.key)
				}
				h.Type("하세요")
				// (the composer's own normalization may fold a lone conjoining jamo
				// once more text follows; the count proves nothing was doubled)
				if v := cc.c.Value(); !strings.HasSuffix(v, "하세요") || utf8.RuneCountInString(v) != utf8.RuneCountInString(tc.key)+3 {
					t.Fatalf("typing goes on: %q", v)
				}
				h.Check()
			})
		}
	}
}

func TestHangulStartsComposingAnnouncesTyping(t *testing.T) {
	for _, sf := range composeSurfaces() {
		t.Run(sf.name, func(t *testing.T) {
			h := sf.open(t)
			rt := &recordRT{}
			h.Context().RT = rt
			h.Send(hangulKey("안"))
			if len(rt.Typed) != 1 {
				t.Fatalf("the first key announces typing once: %v", rt.Typed)
			}
		})
	}
}

func TestASCIIKeepsShortcutsWhileUnfocused(t *testing.T) {
	for _, sf := range composeSurfaces() {
		t.Run(sf.name, func(t *testing.T) {
			h := sf.open(t)
			cc := sf.composer(t, h)
			h.Press("k")
			up := sf.selected(t, h)
			h.Press("j")
			if cc.focused() || cc.c.Value() != "" {
				t.Fatalf("j never composes (focused %v, %q)", cc.focused(), cc.c.Value())
			}
			if !sf.oneRow && sf.selected(t, h) == up {
				t.Fatal("j still moves the selection")
			}
			h.Send(tea.KeyPressMsg{Code: 'ㅁ', Text: "", Mod: tea.ModCtrl})
			h.Send(tea.KeyPressMsg{Code: 'ㅁ', Text: "ㅁ", Mod: tea.ModAlt})
			if cc.focused() || cc.c.Value() != "" {
				t.Fatal("ctrl/alt-modified keys never compose")
			}
		})
	}
}

func TestHangulReadOnlyDoesNotCompose(t *testing.T) {
	for _, sf := range composeSurfaces() {
		t.Run(sf.name, func(t *testing.T) {
			h := sf.open(t, uitest.WithSeed(sf.readOnly))
			cc := sf.composer(t, h)
			h.Send(hangulKey("안"))
			if cc.focused() || cc.c.Value() != "" {
				t.Fatalf("a read-only conversation takes no input (focused %v, %q)", cc.focused(), cc.c.Value())
			}
			h.Check()
		})
	}
}

func TestHangulReboundKeyKeepsAction(t *testing.T) {
	for _, sf := range composeSurfaces() {
		t.Run(sf.name, func(t *testing.T) {
			h := sf.open(t)
			km, errs := keymap.Load(map[string][]string{"list.down": {"ㅓ"}, "list.up": {"ㅏ"}, "help.toggle": {"ㅗ"}})
			if len(errs) != 0 {
				t.Fatal(errs)
			}
			h.Context().Keys = km
			cc := sf.composer(t, h)
			h.Send(hangulKey("ㅏ"))
			up := sf.selected(t, h)
			h.Send(hangulKey("ㅓ"))
			if cc.focused() || cc.c.Value() != "" {
				t.Fatalf("a bound key never composes (focused %v, %q)", cc.focused(), cc.c.Value())
			}
			if !sf.oneRow && sf.selected(t, h) == up {
				t.Fatal("the rebound list.down still moves the selection")
			}
			h.Send(hangulKey("ㅗ"))
			if cc.focused() || cc.c.Value() != "" {
				t.Fatal("a key bound to a soft global never composes")
			}
			mustContain(t, h.Screen(), "모호 폭 문자") // the help overlay opened
		})
	}
}
