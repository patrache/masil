package arch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim/pricing"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	_ "github.com/patrache/masil/internal/ui/tools" // the RouteTool factory and the hub
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

func TestMain(m *testing.M) {
	stub.Install() // home/channel stand-ins under the tool surface
	os.Exit(m.Run())
}

const fixtureName = "결제 시스템 v3"

// loadFixture reads testdata/designs/<name>.json.
func loadFixture(t testing.TB, name string) *archdoc.Design {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "designs", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := archdoc.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// withDesign seeds the store fixtures and saves d as the personal design name
// in the harness's MASIL_CONFIG_DIR/arch (the seed runs after the harness set it).
func withDesign(t *testing.T, name string, d *archdoc.Design) uitest.Option {
	return uitest.WithSeed(func(s *store.Store) {
		fixtures.Seed(s)
		st, err := archdoc.OpenStore(filepath.Join(os.Getenv("MASIL_CONFIG_DIR"), "arch"))
		if err != nil {
			t.Fatal(err)
		}
		if err := st.Save(name, d, true); err != nil {
			t.Fatal(err)
		}
	})
}

func toolRoute(id string, q map[string]string) ui.Route {
	return ui.Route{Kind: ui.RouteTool, ID: id, Arg: ui.ToolArch, Query: q}
}

func newHarness(t *testing.T, opts ...uitest.Option) *uitest.Harness {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "arch")
	return uitest.New(t, opts...)
}

// canvasOf returns the canvas instance of route r.
func canvasOf(t *testing.T, h *uitest.Harness, r ui.Route) *canvasModel {
	t.Helper()
	c, ok := h.Instance(r.Key()).(*canvasModel)
	if !ok {
		t.Fatalf("no canvas instance for %s (got %T)", r.Key(), h.Instance(r.Key()))
	}
	return c
}

func checkEstimate(t *testing.T, screen string) {
	t.Helper()
	if n := strings.Count(screen, "$"); n > 0 {
		if !strings.Contains(screen, "추정") || !strings.Contains(screen, pricing.AsOf()) {
			t.Errorf("a cost without 추정 and %s:\n%s", pricing.AsOf(), screen)
		}
		if strings.Count(screen, "추정") < n {
			t.Errorf("%d costs but %d 추정 notes:\n%s", n, strings.Count(screen, "추정"), screen)
		}
	}
}

func TestArchScreens(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	h := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	for _, s := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 120, H: 30}, {W: 80, H: 24}, {W: 60, H: 24}, {W: 50, H: 20}, {W: 40, H: 14}} {
		h.Resize(s.W, s.H)
		h.Golden(fmt.Sprintf("%dx%d", s.W, s.H))
		h.Check()
		screen := h.Screen()
		checkEstimate(t, screen)
		if !strings.Contains(screen, "결제") {
			t.Errorf("%dx%d: design not shown:\n%s", s.W, s.H, screen)
		}
	}
	c := canvasOf(t, h, r)
	title, _ := c.Title(h.Context())
	if title != fixtureName {
		t.Errorf("title %q", title)
	}

	// a hostile title (a card copy is never validated): sanitized everywhere
	d := loadFixture(t, "pay_v3")
	d.Title = "\x1b]0;x\x07제목\u202E"
	hostile := newCanvas(h.Context(), toolRoute("arch:msg:x", map[string]string{"msg": "x"}))
	hostile.(*canvasModel).setSink(h.Context(), &copySink{objs: d.ToObjects()})
	ht, _ := hostile.(ui.Titled).Title(h.Context())
	uitest.AssertTitle(t, "tool title", ht)
	if !strings.Contains(ht, "제목") {
		t.Errorf("hostile title lost its text: %q", ht)
	}
	if st := hostile.(ui.ToolStatuser).ToolStatus(h.Context()); strings.ContainsAny(st, "\x1b\x07\u202e") {
		t.Errorf("status carries controls: %q", st)
	}
}

func TestArchEmptyNewDesign(t *testing.T) {
	h := newHarness(t, uitest.WithRoute(toolRoute("arch:new:t1", nil)))
	h.Resize(120, 40)
	h.Golden("120x40")
	s := h.Screen()
	for _, want := range []string{"부품", "API 서버", "client 가 트래픽의 출발점이에요", "a 로 부품을 추가하세요"} {
		if !strings.Contains(s, want) {
			t.Errorf("empty design screen lacks %q:\n%s", want, s)
		}
	}
}

func TestArchFlagOff(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "")
	h := uitest.New(t)
	for _, s := range ui.Tools() {
		if s.ID == ui.ToolArch {
			t.Errorf("arch listed with the flag off")
		}
	}
	if _, ok := ui.ToolByID(ui.ToolArch); ok {
		t.Error("ToolByID(arch) with the flag off")
	}
	for _, id := range commandIDs {
		c, ok := palette.Lookup(id)
		if !ok {
			t.Errorf("command %s not registered", id)
			continue
		}
		if c.Available != nil && c.Available(h.Context()) {
			t.Errorf("command %s available with the flag off", id)
		}
	}
	t.Setenv("MASIL_FEATURES", "arch")
	if _, ok := ui.ToolByID(ui.ToolArch); !ok {
		t.Error("ToolByID(arch) missing with the flag on")
	}
	for _, id := range []string{"arch.new", "arch.open"} {
		if c, _ := palette.Lookup(id); c.Available == nil || !c.Available(h.Context()) {
			t.Errorf("%s not available with the flag on", id)
		}
	}
	if c, _ := palette.Lookup("arch.export"); c.Available(h.Context()) {
		t.Error("arch.export available with no canvas in front")
	}
}
