package uitest

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// No stub is registered in this test binary: unregistered routes draw
// widgets.Placeholder("준비 중").
func TestPlaceholderShellGolden(t *testing.T) {
	ForEachSize(t, AllSizes(), func(t *testing.T, h *Harness) { h.Golden("shell") })
}

// probeKind is a test-only route kind; the probe stands in for any component so
// the Send/Advance rules can be observed from inside Update. (CA 10.2 rules; the
// plan's "core.testprobe" overlay is a route here because overlays arrive in Task 28.)
const probeKind ui.RouteKind = "core.testprobe"

type probe struct {
	got    []tea.Msg
	onMsg  func(ctx *ui.Context, msg tea.Msg) tea.Cmd
	size   ui.Size
	inited bool
}

func (p *probe) Init(*ui.Context) tea.Cmd { p.inited = true; return nil }
func (p *probe) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if _, ok := msg.(probeMsg); !ok {
		return nil
	}
	p.got = append(p.got, msg)
	if p.onMsg != nil {
		return p.onMsg(ctx, msg)
	}
	return nil
}
func (p *probe) Resize(_ *ui.Context, s ui.Size) { p.size = s }
func (p *probe) View(ctx *ui.Context, _ bool) string {
	line := ctx.Theme.S.Base.Render(strings.Repeat(" ", max(p.size.W, 0)))
	lines := make([]string, max(p.size.H, 0))
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// probeMsg is the only msg type the probe records.
type probeMsg string

func msgCmd(m tea.Msg) tea.Cmd { return func() tea.Msg { return m } }

// withProbe registers the probe route kind (restored on cleanup) and opens a fresh
// probe in a fresh harness. The factory hands out the probe of the latest call, so a
// test may build several harnesses.
func withProbe(t *testing.T, onMsg func(ctx *ui.Context, msg tea.Msg) tea.Cmd) (*Harness, *probe, *recTB) {
	t.Helper()
	p := &probe{onMsg: onMsg}
	if _, ok := ui.Registered().Routes[probeKind]; !ok {
		t.Cleanup(ui.SnapshotRegistryForTest())
		ui.RegisterRoute(probeKind, func(*ui.Context, ui.Route) ui.Component { return currentProbe })
	}
	currentProbe = p
	rec := &recTB{TB: t}
	h := New(rec, WithRoute(ui.Route{Kind: probeKind}))
	if len(rec.errs) != 0 {
		t.Fatalf("harness setup reported: %v", rec.errs)
	}
	if !p.inited {
		t.Fatal("probe was never instantiated (route not shown in Main?)")
	}
	return h, p, rec
}

// currentProbe is what the registered probe factory returns.
var currentProbe *probe

func gotNames(p *probe) []string {
	var out []string
	for _, m := range p.got {
		out = append(out, string(m.(probeMsg)))
	}
	return out
}

func TestSendRunsCmdsBreadthFirst(t *testing.T) {
	h, p, rec := withProbe(t, func(_ *ui.Context, msg tea.Msg) tea.Cmd {
		switch msg.(probeMsg) {
		case "start":
			return tea.Batch(msgCmd(probeMsg("a")), msgCmd(probeMsg("b")))
		case "a":
			return msgCmd(probeMsg("c"))
		}
		return nil
	})
	h.Send(probeMsg("start"))
	if want := []string{"start", "a", "b", "c"}; !reflect.DeepEqual(gotNames(p), want) {
		t.Fatalf("Update order = %v, want %v", gotNames(p), want)
	}
	if len(rec.errs) != 0 {
		t.Fatalf("unexpected reports: %v", rec.errs)
	}

	// An endless cascade is cut at depth 32 with a Fatal.
	h2, p2, rec2 := withProbe(t, func(_ *ui.Context, msg tea.Msg) tea.Cmd {
		return msgCmd(probeMsg("again"))
	})
	h2.Send(probeMsg("loop"))
	if rec2.fatals != 1 {
		t.Fatalf("fatals = %d (errs %v), want 1 for a cascade deeper than 32", rec2.fatals, rec2.errs)
	}
	if n := len(p2.got); n != 33 { // depth 0..32 delivered, 33 refused
		t.Fatalf("delivered %d msgs before the depth cut, want 33", n)
	}
}

func TestTeaInternalMsgsRecorded(t *testing.T) {
	h, p, rec := withProbe(t, func(_ *ui.Context, msg tea.Msg) tea.Cmd {
		if msg == probeMsg("quit") {
			return tea.Batch(tea.Quit, msgCmd(probeMsg("after")))
		}
		return nil
	})
	h.Send(probeMsg("quit"))
	var quits int
	for _, m := range h.TeaMsgs() {
		if _, ok := m.(tea.QuitMsg); ok {
			quits++
		}
	}
	if quits != 1 {
		t.Fatalf("TeaMsgs() = %#v, want one tea.QuitMsg", h.TeaMsgs())
	}
	if want := []string{"quit", "after"}; !reflect.DeepEqual(gotNames(p), want) {
		t.Fatalf("Update order = %v, want %v (QuitMsg must not be re-dispatched)", gotNames(p), want)
	}
	if len(rec.errs) != 0 {
		t.Fatalf("unexpected reports: %v", rec.errs)
	}
}

func TestSequenceForbidden(t *testing.T) {
	h, _, rec := withProbe(t, func(_ *ui.Context, msg tea.Msg) tea.Cmd {
		if msg == probeMsg("seq") {
			return tea.Sequence(msgCmd(probeMsg("x")), msgCmd(probeMsg("y")))
		}
		return nil
	})
	h.Send(probeMsg("seq"))
	if len(rec.errs) != 1 || !strings.Contains(rec.errs[0], "tea.Sequence is forbidden in UI code (5.3.2)") {
		t.Fatalf("reports = %v, want one tea.Sequence error", rec.errs)
	}
	if rec.fatals != 0 {
		t.Fatalf("tea.Sequence must be an Errorf, not a Fatal")
	}
}

func TestBlockingCmdFails(t *testing.T) {
	h, _, rec := withProbe(t, func(_ *ui.Context, msg tea.Msg) tea.Cmd {
		if msg == probeMsg("block") {
			return func() tea.Msg { time.Sleep(blockLimit + 500*time.Millisecond); return nil }
		}
		return nil
	})
	start := time.Now()
	h.Send(probeMsg("block"))
	want := "Cmd blocked > " + blockLimit.String()
	if rec.fatals != 1 || len(rec.errs) != 1 || !strings.Contains(rec.errs[0], want) {
		t.Fatalf("fatals=%d reports=%v, want one Fatal containing %q", rec.fatals, rec.errs, want)
	}
	if el := time.Since(start); el > blockLimit+400*time.Millisecond {
		t.Fatalf("the watchdog fired after %v, want ~1s", el)
	}
}

func TestAdvanceDeliversInOrder(t *testing.T) {
	h, p, rec := withProbe(t, func(ctx *ui.Context, msg tea.Msg) tea.Cmd {
		switch msg.(probeMsg) {
		case "arm":
			return tea.Batch(ui.After(ctx, 3*time.Second, probeMsg("a")), ui.After(ctx, time.Second, probeMsg("b")))
		case "b":
			return ui.After(ctx, time.Second, probeMsg("c"))
		}
		return nil
	})
	start := h.Clock().Now()
	h.Send(probeMsg("arm"))
	h.Advance(3 * time.Second)
	if want := []string{"arm", "b", "c", "a"}; !reflect.DeepEqual(gotNames(p), want) {
		t.Fatalf("delivery order = %v, want %v", gotNames(p), want)
	}
	if got := h.Clock().Now(); !got.Equal(start.Add(3 * time.Second)) {
		t.Fatalf("Clock().Now() = %v, want start+3s", got)
	}
	h.Advance(1500 * time.Millisecond)
	if got := h.Clock().Now(); !got.Equal(start.Add(4500 * time.Millisecond)) {
		t.Fatalf("Advance with nothing due must still move the clock; Now() = %v", got)
	}
	if len(rec.errs) != 0 {
		t.Fatalf("unexpected reports: %v", rec.errs)
	}
}

func TestHarnessDefaults(t *testing.T) {
	h := New(t)
	dir := os.Getenv("MASIL_CONFIG_DIR")
	if dir == "" || !strings.HasPrefix(dir, os.TempDir()) {
		t.Fatalf("MASIL_CONFIG_DIR = %q, want a t.TempDir()", dir)
	}
	ctx := h.Context()
	if ctx.Paths.Dir != dir {
		t.Fatalf("Context().Paths.Dir = %q, want %q", ctx.Paths.Dir, dir)
	}
	if os.Getenv("MASIL_CACHE_DIR") == "" || ctx.Paths.Cache != os.Getenv("MASIL_CACHE_DIR") {
		t.Fatalf("MASIL_CACHE_DIR = %q, Paths.Cache = %q", os.Getenv("MASIL_CACHE_DIR"), ctx.Paths.Cache)
	}
	if os.Getenv("MASIL_SERVER") != h.API.URL {
		t.Fatalf("MASIL_SERVER = %q, want the apitest URL %q", os.Getenv("MASIL_SERVER"), h.API.URL)
	}
	want := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	if got := ctx.Now(); !got.Equal(want) {
		t.Fatalf("Context().Now() = %v, want %v", got, want)
	}
	if ctx.Loc == nil || ctx.Loc.String() != "Asia/Seoul" {
		t.Fatalf("Loc = %v, want Asia/Seoul", ctx.Loc)
	}
	if ctx.Theme.Name != theme.Amber {
		t.Fatalf("Theme = %v, want amber", ctx.Theme.Name)
	}
	if ctx.Caps.Graphics != termcaps.GraphicsHalfBlock {
		t.Fatalf("Caps.Graphics = %v, want HalfBlock", ctx.Caps.Graphics)
	}
	if ctx.Config.UI.Animations {
		t.Fatal("Config.UI.Animations must be false")
	}
	if ctx.Timers != ui.Timers(h.Clock()) {
		t.Fatal("Context().Timers must be the harness FakeClock")
	}
	if ctx.RT == nil || ctx.Store == nil || ctx.API == nil {
		t.Fatalf("RT/Store/API must be set: %v %v %v", ctx.RT, ctx.Store, ctx.API)
	}
	if ctx.Me() == nil || ctx.Me().Handle != "sam" {
		t.Fatalf("default seed must log in the fixture user sam; Me = %+v", ctx.Me())
	}
	if l := ctx.Layout; l.Cols != 120 || l.Rows != 40 {
		t.Fatalf("default size = %dx%d, want 120x40", l.Cols, l.Rows)
	}
}
