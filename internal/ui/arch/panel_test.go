package arch

import (
	"fmt"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/archsim/pricing"
	"github.com/patrache/masil/internal/ui/uitest"
)

// panelDesign is client → api → db in a row.
func panelDesign() *archdoc.Design {
	d := archdoc.New("panel")
	d.Seed = 42
	node := func(id string, k archdoc.NodeKind, x int) archdoc.Node {
		n := archdoc.Defaults(k)
		n.ID, n.Label, n.Pos = id, id, [2]int{x, 0}
		return n
	}
	d.Nodes = []archdoc.Node{node("api", archdoc.KindAPI, 16), node("client", archdoc.KindClient, 0), node("db", archdoc.KindDBPrimary, 32)}
	e1, e2 := archdoc.DefaultEdge(), archdoc.DefaultEdge()
	e1.ID, e1.From, e1.To = "e1", "client", "api"
	e2.ID, e2.From, e2.To = "e2", "api", "db"
	d.Edges = []archdoc.Edge{e1, e2}
	return d
}

// panelHarness opens d as personal design "panel" (keyboard only) at 120x40.
func panelHarness(t *testing.T, d *archdoc.Design) (*uitest.Harness, *canvasModel) {
	t.Helper()
	return keyHarness(t, toolRoute("arch:file:panel", map[string]string{"file": "panel"}), withDesign(t, "panel", d))
}

// openPanelOn selects id with keys and gives the settings panel the keys.
func openPanelOn(t *testing.T, h *uitest.Harness, c *canvasModel, id string) {
	t.Helper()
	selectKeys(t, h, c, id)
	h.Press("e")
	if c.mode != modePanel {
		t.Fatalf("e: mode %d, want the panel", c.mode)
	}
}

// toField moves the panel cursor to field (from wherever it is).
func toField(t *testing.T, h *uitest.Harness, c *canvasModel, field string) {
	t.Helper()
	h.Press(repeat("k", 60)...)
	for range 60 {
		if row, ok := c.panelRow(h.Context()); ok && !row.connect && row.def.Field == field {
			return
		}
		h.Press("j")
	}
	t.Fatalf("no panel row %q", field)
}

// pickValue picks value in field's choice list.
func pickValue(t *testing.T, h *uitest.Harness, c *canvasModel, field, value string) {
	t.Helper()
	toField(t, h, c, field)
	h.Press("enter")
	if c.panel.pick == nil {
		t.Fatalf("enter on %s opened no list", field)
	}
	h.Press(repeat("k", len(c.panel.pick.items))...)
	for range len(c.panel.pick.items) {
		if c.panel.pick.value() == value {
			break
		}
		h.Press("j")
	}
	if c.panel.pick.value() != value {
		t.Fatalf("%s has no choice %q", field, value)
	}
	h.Press("enter")
}

// typeField types text into field (enter opens it, the old value is erased)
// and presses enter once.
func typeField(t *testing.T, h *uitest.Harness, c *canvasModel, field, text string) {
	t.Helper()
	toField(t, h, c, field)
	h.Press("enter")
	if !c.panel.editing {
		t.Fatalf("enter on %s is not editing", field)
	}
	h.Press(repeat("backspace", 20)...)
	h.Type(text)
	h.Press("enter")
}

func TestPickInstanceFillsSpecs(t *testing.T) {
	h, c := panelHarness(t, panelDesign())
	openPanelOn(t, h, c, "api")
	pickValue(t, h, c, "provider", "aws")
	pickValue(t, h, c, "region", "ap-northeast-2")
	pickValue(t, h, c, "instance_type", "c7g.xlarge")
	n := findNode(designOf(t, c), "api")
	if n.Provider != "aws" || n.Region != "ap-northeast-2" || n.InstanceType != "c7g.xlarge" {
		t.Fatalf("picked %s %s %s", n.Provider, n.Region, n.InstanceType)
	}
	if n.CPUCores != 4 || n.MemoryMiB != 8192 || n.NICMbps != 12500 {
		t.Errorf("specs %d cores %d MiB %d Mbps, want 4 / 8192 / 12500", n.CPUCores, n.MemoryMiB, n.NICMbps)
	}
	it, _ := pricing.Lookup(pricing.AWS, "ap-northeast-2", "c7g.xlarge")
	want := "시간당 ~$" + usdMicro(it.HourlyMicroUSD)
	if s := h.Screen(); !strings.Contains(s, want) || !strings.Contains(s, "c7g.xlarge") {
		t.Errorf("panel lacks %q:\n%s", want, s)
	}
}

func TestManualEditMarksCustom(t *testing.T) {
	h, c := panelHarness(t, panelDesign())
	openPanelOn(t, h, c, "api")
	pickValue(t, h, c, "instance_type", "c7g.xlarge")
	typeField(t, h, c, "cpu_cores", "8")
	n := findNode(designOf(t, c), "api")
	if n.CPUCores != 8 || n.InstanceType != "custom" {
		t.Fatalf("after typing 8 cores: %d cores, type %q", n.CPUCores, n.InstanceType)
	}
	if n.CustomHourlyMicroUSD == 0 {
		t.Error("the custom type lost the hourly price")
	}
	if s := h.Screen(); !strings.Contains(s, "사용자 지정") {
		t.Errorf("no 사용자 지정:\n%s", s)
	}
}

func TestResultUpdatesSameUpdate(t *testing.T) {
	h, c := panelHarness(t, panelDesign())
	openPanelOn(t, h, c, "api")
	before := c.result.Nodes["api"].UtilPermille
	util := func(p int) string { return h.Context().T.Archcanvas.PanelUtil(p / 10) }
	if s := h.Screen(); !strings.Contains(s, util(before)) {
		t.Fatalf("no %q on screen:\n%s", util(before), s)
	}
	toField(t, h, c, "instances")
	h.Press("enter")
	h.Press(repeat("backspace", 5)...)
	h.Type("4")
	h.Press("enter") // one Update: commit → Analyze → the screen
	after := c.result.Nodes["api"].UtilPermille
	if after == before || findNode(designOf(t, c), "api").Instances != 4 {
		t.Fatalf("util %d → %d, instances %d", before, after, findNode(designOf(t, c), "api").Instances)
	}
	if s := h.Screen(); !strings.Contains(s, util(after)) {
		t.Errorf("screen still shows the old use (want %q):\n%s", util(after), s)
	}
}

func TestRangeRejected(t *testing.T) {
	h, c := panelHarness(t, panelDesign())
	openPanelOn(t, h, c, "api")
	before := string(mustEncode(t, designOf(t, c)))
	hint := h.Context().T.Archcanvas.RangeHint("1", "1,000")
	for _, bad := range []string{"0", "abc", "1001"} {
		typeField(t, h, c, "instances", bad)
		if got := string(mustEncode(t, designOf(t, c))); got != before {
			t.Errorf("%q changed the design", bad)
		}
		if !c.panel.editing || !strings.Contains(h.Screen(), hint) {
			t.Errorf("%q: editing %v, no hint %q:\n%s", bad, c.panel.editing, hint, h.Screen())
		}
		h.Press("esc")
	}
	if c.undo.CanUndo() {
		t.Error("a refused value was committed")
	}
}

// iopsDesign is client → api → db with all writes so the DB disk fills first.
func iopsDesign(rps int) *archdoc.Design {
	d := panelDesign()
	for i := range d.Nodes {
		n := &d.Nodes[i]
		switch n.ID {
		case "client":
			n.Traffic.RPS, n.Traffic.ReadPermille = rps, 0
		case "api":
			n.Instances = 20
		case "db":
			n.CPUCores = 32
		}
	}
	return d
}

func TestBottleneckLine(t *testing.T) {
	d := iopsDesign(2352)
	r := archsim.Analyze(d, archsim.Options{})
	if bn := r.Bottleneck; bn == nil || bn.Target != "db" || bn.Limit != archsim.LimitIOPS || bn.UtilPermille/10 != 98 {
		t.Fatalf("fixture bottleneck %+v", r.Bottleneck)
	}
	h, _ := panelHarness(t, d)
	var line string
	for _, l := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(l, "병목") {
			line = l
		}
	}
	for _, want := range []string{"⚠", "IOPS", "98%", "db", "2,352/s × 1.25 IOPS"} {
		if !strings.Contains(line, want) {
			t.Errorf("bottleneck line %q lacks %q", line, want)
		}
	}
}

func TestCostAlwaysEstimated(t *testing.T) {
	h, c := panelHarness(t, panelDesign())
	openPanelOn(t, h, c, "api")
	pickValue(t, h, c, "instance_type", "c7g.xlarge")
	for _, s := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 120, H: 30}, {W: 80, H: 24}, {W: 60, H: 24}, {W: 50, H: 20}, {W: 40, H: 14}} {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			h.Resize(s.W, s.H)
			h.Check()
			checkEstimate(t, h.Screen())
		})
	}
}

func TestRemoteDeleteClosesPanel(t *testing.T) {
	h, c := panelHarness(t, panelDesign())
	openPanelOn(t, h, c, "api")
	toField(t, h, c, "instances")
	h.Press("enter")
	h.Type("9") // typed, not committed
	// someone else deletes the node
	c.sink.Apply([]archdoc.Change{{Kind: archdoc.ChangeDel, Obj: archdoc.ObjKey('n', "api")}})
	cmd := c.refresh(h.Context())
	if c.mode == modePanel || c.panel.editing {
		t.Errorf("the panel stayed open: mode %d editing %v", c.mode, c.panel.editing)
	}
	if got, want := toastOf(cmd), h.Context().T.Archcanvas.NodeDeleted(); got != want {
		t.Errorf("toast %q, want %q", got, want)
	}
	if c.undo.CanUndo() {
		t.Error("the pending input was committed")
	}
	h.Press("enter")
	if findNode(designOf(t, c), "api") != nil {
		t.Error("the node came back")
	}
}

// connectByKeys is the canvas c-mode: hjkl until the cursor is on to, enter.
func connectByKeys(t *testing.T, h *uitest.Harness, c *canvasModel, to string) {
	t.Helper()
	h.Press("c")
	for range 8 {
		if c.cursor == to {
			break
		}
		before := c.cursor
		for _, k := range []string{"l", "j", "h", "k"} {
			h.Press(k)
			if c.cursor == to || c.cursor != before {
				break
			}
		}
	}
	if c.cursor != to {
		t.Fatalf("c-mode cannot reach %s (cursor %s)", to, c.cursor)
	}
	h.Press("enter")
}

func TestListViewFormEditing(t *testing.T) {
	addCache := func(h *uitest.Harness) {
		h.Press("a")
		h.Press(pickKeys(t, paletteItem{node: archdoc.KindCache})...)
		h.Press("enter")
	}
	// the canvas keys at 120x40
	h1, c1 := panelHarness(t, panelDesign())
	selectKeys(t, h1, c1, "api")
	addCache(h1)
	cache1 := c1.sel
	selectKeys(t, h1, c1, "db")
	h1.Press("d")
	selectKeys(t, h1, c1, "api")
	connectByKeys(t, h1, c1, cache1)
	want := normalized(t, designOf(t, c1))

	// the same in the list view's stacked form at 60x24
	h, c := panelHarness(t, panelDesign())
	h.Resize(60, 24)
	if c.regs.View != viewList {
		t.Fatalf("60x24: view %d, want the list", c.regs.View)
	}
	selectKeys(t, h, c, "api")
	h.Press("enter")
	if c.mode != modePanel || c.regs.View != viewList || !strings.Contains(h.Screen(), "인스턴스 수") {
		t.Fatalf("enter on a node: mode %d view %d\n%s", c.mode, c.regs.View, h.Screen())
	}
	h.Press("esc")
	addCache(h)
	cache := c.sel
	selectKeys(t, h, c, "db")
	h.Press("d")
	selectKeys(t, h, c, "api")
	h.Press("enter")
	h.Press(repeat("j", 60)...) // the last row: 연결 대상 고르기
	if row, _ := c.panelRow(h.Context()); !row.connect || !strings.Contains(h.Screen(), "연결 대상 고르기") {
		t.Fatalf("last form row is not the connect row:\n%s", h.Screen())
	}
	h.Press("enter")
	for range 10 {
		if c.panel.pick.value() == cache {
			break
		}
		h.Press("j")
	}
	h.Press("enter")
	if got := normalized(t, designOf(t, c)); got != want {
		t.Errorf("list form design differs from the canvas keys:\n%s\nwant\n%s", got, want)
	}
}
