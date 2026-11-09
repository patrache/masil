package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/keymap"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// localeProbe is an overlay that counts the LocaleChangedMsgs it receives.
type localeProbe struct {
	got  []ui.LocaleChangedMsg
	size ui.Size
}

func (p *localeProbe) Init(*ui.Context) tea.Cmd { return nil }
func (p *localeProbe) Update(_ *ui.Context, msg tea.Msg) tea.Cmd {
	if m, ok := msg.(ui.LocaleChangedMsg); ok {
		p.got = append(p.got, m)
	}
	return nil
}
func (p *localeProbe) Resize(_ *ui.Context, s ui.Size) { p.size = s }
func (p *localeProbe) View(*ui.Context, bool) string {
	rows := make([]string, p.size.H)
	for i := range rows {
		rows[i] = strings.Repeat(" ", p.size.W)
	}
	return strings.Join(rows, "\n")
}
func (p *localeProbe) Bounds(body layout.Rect) layout.Rect {
	return layout.Rect{X: body.X + 2, Y: body.Y + 2, W: 10, H: 4}
}
func (p *localeProbe) Modal() bool { return false }

// localeSeen records the LocaleChangedMsgs every placed stub instance gets.
func localeSeen(t *testing.T, h *uitest.Harness) map[string][]ui.LocaleChangedMsg {
	t.Helper()
	seen := map[string][]ui.LocaleChangedMsg{}
	for _, r := range h.Model.Frame().Routes {
		key := r.Key()
		rec := func(_ *ui.Context, msg tea.Msg) {
			if m, ok := msg.(ui.LocaleChangedMsg); ok {
				seen[key] = append(seen[key], m)
			}
		}
		switch c := h.Instance(key).(type) {
		case *stub.Chat:
			c.OnUpdate = rec
		case *stub.List:
			c.OnUpdate = rec
		default:
			t.Fatalf("Instance(%q) = %T, want a stub", key, c)
		}
		seen[key] = nil
	}
	return seen
}

// observed counts the broadcasts of LocaleChangedMsg.
func observed(h *uitest.Harness) *[]ui.LocaleChangedMsg {
	var got []ui.LocaleChangedMsg
	app.SetObserverForTest(h.Model, func(msg tea.Msg) {
		if m, ok := msg.(ui.LocaleChangedMsg); ok {
			got = append(got, m)
		}
	})
	return &got
}

func TestSetLocaleSwapsContext(t *testing.T) {
	t.Setenv("MASIL_LANG", "") // MASIL_LANG beats a switch
	i18n.SetOpenForTest(t, i18n.Ko, i18n.En)
	h := uitest.New(t)
	h.Resize(200, 60)
	probe := &localeProbe{}
	h.Send(ui.OpenOverlayMsg{Overlay: probe})
	seen := localeSeen(t, h)
	if len(seen) < 3 {
		t.Fatalf("only %d placed instances at 200x60", len(seen))
	}
	bc := observed(h)

	h.SetLocale("en")

	ctx := h.Context()
	if ctx.Lang != i18n.En || ctx.T != i18n.For(i18n.En) {
		t.Fatalf("ctx.Lang = %q, ctx.T locale = %q, want en", ctx.Lang, ctx.T.Locale())
	}
	want := ui.LocaleChangedMsg{From: i18n.Ko, To: i18n.En}
	if len(*bc) != 1 || (*bc)[0] != want {
		t.Fatalf("broadcasts = %v, want one %v", *bc, want)
	}
	for key, got := range seen {
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s got %v, want one %v", key, got, want)
		}
	}
	if len(probe.got) != 1 || probe.got[0] != want {
		t.Errorf("overlay got %v, want one %v", probe.got, want)
	}
	// TestKeymapLocalizeOnSwitch (P5 Task 3): the help descriptions follow.
	if got, wantDesc := ctx.Keys.Binding(keymap.ToolsToggle).Help().Desc, i18n.For(i18n.En).Tools.KeyToggle(); got != wantDesc {
		t.Fatalf("keymap desc after switch = %q, want %q", got, wantDesc)
	}
	// The same language again: nothing to redraw, no broadcast.
	h.SetLocale("en")
	if len(*bc) != 1 {
		t.Fatalf("switching to the shown language broadcast again: %v", *bc)
	}
}

func TestSetLocaleClosedLanguage(t *testing.T) {
	for _, tc := range []struct {
		name string
		open []i18n.Locale
		want i18n.Locale
	}{
		{"en open", []i18n.Locale{i18n.Ko, i18n.En}, i18n.En},
		{"ko only", []i18n.Locale{i18n.Ko}, i18n.Ko},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i18n.SetOpenForTest(t, tc.open...)
			t.Setenv("MASIL_LANG", "")
			h := uitest.New(t)
			h.SetLocale("zh-Hans")
			if ctx := h.Context(); ctx.Lang != tc.want || ctx.T.Locale() != tc.want {
				t.Fatalf("closed zh-Hans => Lang %q (T %q), want %q", ctx.Lang, ctx.T.Locale(), tc.want)
			}
			for _, odd := range []string{"", "auto", "ja", "EN", "zh-Hant", "nonsense!!"} {
				h.SetLocale(odd) // never panics; the shown language stays usable
				h.Check()
			}
		})
	}
}

func TestSetLocaleKeepsState(t *testing.T) {
	t.Setenv("MASIL_LANG", "") // MASIL_LANG beats a switch
	i18n.SetOpenForTest(t, i18n.Ko, i18n.En)
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(200, 60)
	ch := devChat(t, h)
	h.Press("i")
	h.Type("안녕 반가워")
	h.Press("esc")
	h.Press("k", "k", "k") // the thread root
	h.Press("enter")       // opens the thread beside the channel
	n := h.Nav()
	thr := n.Tabs[n.Active].Thread
	if thr == nil {
		t.Fatal("no thread open")
	}
	before := h.Screen() // draws, which settles the scroll anchor
	serial, sel, anchor, draft := ch.Serial, selectedID(ch), ch.List.Anchor(), ch.Composer.Value()
	focus, nav := h.Model.Focus(), h.Nav()

	h.SetLocale("en")
	h.Check() // draws in en
	now := devChat(t, h)
	if now != ch || now.Serial != serial {
		t.Fatalf("instance rebuilt: serial %d, want %d", now.Serial, serial)
	}
	if got := now.Composer.Value(); got != "안녕 반가워" || got != draft {
		t.Fatalf("draft = %q", got)
	}
	if got := selectedID(now); got != sel {
		t.Fatalf("selection = %q, want %q", got, sel)
	}
	if got := now.List.Anchor(); got != anchor {
		t.Fatalf("scroll anchor = %+v, want %+v", got, anchor)
	}
	if f := h.Model.Focus(); f != focus {
		t.Fatalf("focus = %+v, want %+v", f, focus)
	}
	if got := h.Nav(); got.Tabs[got.Active].Thread == nil || got.Tabs[got.Active].Thread.Key() != nav.Tabs[nav.Active].Thread.Key() {
		t.Fatalf("thread closed by the switch: %+v", got.Tabs[got.Active].Thread)
	}

	h.SetLocale("ko")
	if after := h.Screen(); after != before {
		t.Fatalf("back to ko, the screen differs:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestWithLocaleThenSetLocale(t *testing.T) {
	t.Setenv("MASIL_LANG", "") // MASIL_LANG beats a switch
	h := uitest.New(t, uitest.WithLocale(i18n.En))
	if h.Context().Lang != i18n.En {
		t.Fatalf("WithLocale(en): Lang = %q", h.Context().Lang)
	}
	bc := observed(h)
	h.SetLocale("ko")
	want := ui.LocaleChangedMsg{From: i18n.En, To: i18n.Ko}
	if len(*bc) != 1 || (*bc)[0] != want {
		t.Fatalf("broadcasts = %v, want one %v", *bc, want)
	}
	if h.Context().Lang != i18n.Ko || h.Context().T != i18n.KoT() {
		t.Fatalf("Lang = %q after SetLocale(ko)", h.Context().Lang)
	}
}

// TestSetLocaleEnvWins: MASIL_LANG stays a one-time override for the whole run
// (i18n spec 6.2: MASIL_LANG > config) — a switch in the app (config=choice)
// does not beat it (P5 review).
func TestSetLocaleEnvWins(t *testing.T) {
	i18n.SetOpenForTest(t, i18n.Ko, i18n.En)
	t.Setenv("MASIL_LANG", "en")
	h := uitest.New(t) // starts in ko (the harness's Options.Locale)
	for _, choice := range []string{"ko", "auto"} {
		h.SetLocale(choice)
		if got := h.Model.Locale(); got.Effective != i18n.En || got.Source != i18n.SourceEnv {
			t.Fatalf("MASIL_LANG=en, SetLocale(%q) => %+v, want en from MASIL_LANG", choice, got)
		}
		if h.Context().Lang != i18n.En {
			t.Fatalf("MASIL_LANG=en, SetLocale(%q) => Lang %q", choice, h.Context().Lang)
		}
	}
}

// TestSetLocaleReachesUnplaced: the pane-factory panes (rail, sidebar,
// members) and a cached route instance that is not placed now also get the
// LocaleChangedMsg exactly once — their row caches would keep the old
// language otherwise (P5 review).
func TestSetLocaleReachesUnplaced(t *testing.T) {
	t.Cleanup(ui.SnapshotRegistryForTest())
	i18n.SetOpenForTest(t, i18n.Ko, i18n.En)
	t.Setenv("MASIL_LANG", "")
	panes := map[layout.SlotID]*localeProbe{}
	for _, s := range []layout.SlotID{layout.SlotRail, layout.SlotSidebar, layout.SlotMembers} {
		p := &localeProbe{}
		panes[s] = p
		ui.RegisterPane(s, func(*ui.Context) ui.Component { return p })
	}
	h := uitest.New(t, uitest.WithRoute(devRoute))
	h.Resize(200, 60)
	ch := devChat(t, h)
	var cached []ui.LocaleChangedMsg
	ch.OnUpdate = func(_ *ui.Context, msg tea.Msg) {
		if m, ok := msg.(ui.LocaleChangedMsg); ok {
			cached = append(cached, m)
		}
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChGeneral}})
	for _, r := range h.Model.Frame().Routes {
		if r.Key() == devRoute.Key() {
			t.Fatalf("%s is still placed after navigating away", devRoute.Key())
		}
	}
	if h.Instance(devRoute.Key()) != ch {
		t.Fatalf("%s is not cached after navigating away", devRoute.Key())
	}
	var placed []layout.SlotID
	for s := range panes {
		if _, ok := h.Model.Frame().Layout.Pane(s); ok {
			placed = append(placed, s)
		}
	}
	if len(placed) == 0 {
		t.Fatal("no pane-factory slot placed at 200x60")
	}

	h.SetLocale("en")

	want := ui.LocaleChangedMsg{From: i18n.Ko, To: i18n.En}
	if len(cached) != 1 || cached[0] != want {
		t.Errorf("unplaced cached %s got %v, want one %v", devRoute.Key(), cached, want)
	}
	for _, s := range placed {
		if got := panes[s].got; len(got) != 1 || got[0] != want {
			t.Errorf("pane %s got %v, want one %v", s, got, want)
		}
	}
}
