package arch

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// faultDesign is client → api, the api inside AZ "AZ a" (az).
func faultDesign() *archdoc.Design {
	d := archdoc.New("faults")
	d.Seed = 42 // goldens: the same samples every run
	cl := archdoc.Defaults(archdoc.KindClient)
	cl.ID, cl.Label, cl.Pos = "client", "client", [2]int{0, 2}
	api := archdoc.Defaults(archdoc.KindAPI)
	api.ID, api.Label, api.Pos, api.Parent = "api", "api", [2]int{16, 2}, "az"
	d.Nodes = []archdoc.Node{api, cl}
	d.Boxes = []archdoc.Box{{ID: "az", Kind: archdoc.BoxAZ, Label: "AZ a", Rect: [4]int{12, 0, 24, 8}}}
	e := archdoc.DefaultEdge()
	e.ID, e.From, e.To = "e1", "client", "api"
	d.Edges = []archdoc.Edge{e}
	return d
}

func faultHarness(t *testing.T, d *archdoc.Design) (*uitest.Harness, *canvasModel) {
	t.Helper()
	return keyHarness(t, toolRoute("arch:file:faults", map[string]string{"file": "faults"}), withDesign(t, "faults", d))
}

func TestFaultMenuSchedulesAt(t *testing.T) {
	h, c := faultHarness(t, faultDesign())
	selectKeys(t, h, c, "api")
	h.Press("x")
	if c.faults == nil {
		t.Fatal("x opened no fault menu")
	}
	for range len(c.faults.opts) {
		if c.faults.opts[c.faults.cur].kind == archdoc.FaultAZOff {
			break
		}
		h.Press("j")
	}
	if o := c.faults.opts[c.faults.cur]; o.kind != archdoc.FaultAZOff || o.target != "az" {
		t.Fatalf("no AZ off for the api's AZ: %+v", c.faults.opts)
	}
	if !strings.Contains(h.Screen(), "AZ 끄기 · AZ a") {
		t.Errorf("menu lacks the AZ entry:\n%s", h.Screen())
	}
	h.Press("enter")
	if c.faults.step != stepWhen {
		t.Fatalf("after the kind: step %d", c.faults.step)
	}
	h.Press("j")
	h.Press(repeat("backspace", 6)...)
	h.Type("00:30")
	h.Press("enter")
	if c.faults != nil {
		t.Error("the menu stayed open")
	}
	fs := designOf(t, c).Faults
	if len(fs) != 1 {
		t.Fatalf("faults %+v", fs)
	}
	if f := fs[0]; f.Kind != archdoc.FaultAZOff || f.AtS != 30 || f.Target != "az" {
		t.Errorf("fault %+v, want az_off at 30 on az", f)
	}
}

func TestFaultMenuNowAndCount(t *testing.T) {
	h, c := faultHarness(t, faultDesign())
	selectKeys(t, h, c, "api")
	h.Press("x")
	for range len(c.faults.opts) {
		if c.faults.opts[c.faults.cur].kind == archdoc.FaultInstancesOff {
			break
		}
		h.Press("j")
	}
	h.Press("enter")
	if c.faults.step != stepAmount {
		t.Fatalf("instances off asks no count: step %d", c.faults.step)
	}
	h.Press(repeat("backspace", 6)...)
	h.Type("0")
	h.Press("enter")
	if c.faults.step != stepAmount || c.faults.hint == "" {
		t.Fatalf("count 0 was taken: step %d", c.faults.step)
	}
	h.Press("backspace")
	h.Type("1")
	h.Press("enter")
	h.Press("enter") // 지금
	fs := designOf(t, c).Faults
	if len(fs) != 1 || fs[0].Kind != archdoc.FaultInstancesOff || fs[0].Count != 1 || fs[0].AtS != 0 || fs[0].Target != "api" {
		t.Fatalf("faults %+v", fs)
	}
}

func TestPlaybackFollowsFrames(t *testing.T) {
	d := faultDesign()
	d.Faults = []archdoc.Fault{{ID: "f1", AtS: 30, Kind: archdoc.FaultAZOff, Target: "az"}}
	h, c := faultHarness(t, d)
	h.Press("space")
	if c.pb.tl == nil || !c.pb.running {
		t.Fatalf("space: timeline %v running %v", c.pb.tl != nil, c.pb.running)
	}
	if b := c.shownBadges()["api"]; b.Danger {
		t.Error("api down before the fault")
	}
	h.Advance(30 * time.Second)
	s := h.Screen()
	want := "00:30 " + h.Context().T.Archcanvas.EvBoxOff("AZ a")
	if !strings.Contains(s, want) {
		t.Errorf("no event line %q:\n%s", want, s)
	}
	if b := c.shownBadges()["api"]; !b.Danger {
		t.Errorf("api not drawn down after the AZ went off: %+v", b)
	}
	h.Press(">", ">")
	if c.speedX != 4 || !strings.Contains(h.Screen(), "×4") {
		t.Errorf("speed %d after > >:\n%s", c.speedX, h.Screen())
	}
	// pause, then step back a second with ←
	h.Press("space")
	pos := c.pb.pos
	h.Press("left")
	if c.pb.running || c.pb.pos != pos-1 {
		t.Errorf("← while paused: pos %d → %d running %v", pos, c.pb.pos, c.pb.running)
	}
}

func TestEditCancelsRun(t *testing.T) {
	h, c := faultHarness(t, faultDesign())
	h.Press("space")
	old, gen := c.pb.tl, c.runGen
	if old == nil {
		t.Fatal("no timeline")
	}
	openPanelOn(t, h, c, "api")
	typeField(t, h, c, "instances", "4")
	if c.pb.tl != nil || c.runGen == gen {
		t.Fatalf("an edit kept the run: gen %d → %d", gen, c.runGen)
	}
	h.Send(timedDoneMsg{instance: c.route.ID, gen: gen, tl: old})
	if c.pb.tl != nil {
		t.Error("the stale run result was taken")
	}
	if s := h.Screen(); !strings.Contains(s, h.Context().T.Archcanvas.EngineInstant()) {
		t.Errorf("not back to the instant analysis:\n%s", s)
	}
	if findNode(designOf(t, c), "api").Instances != 4 {
		t.Error("the edit was lost")
	}
}

func TestTinyStartStopOnly(t *testing.T) {
	h, c := faultHarness(t, faultDesign())
	h.Resize(40, 14)
	if c.regs.View != viewSummary {
		t.Fatalf("40x14: view %d", c.regs.View)
	}
	h.Press("space")
	if c.pb.tl == nil || !c.pb.running {
		t.Fatal("space in tiny did not start the run")
	}
	h.Advance(5 * time.Second)
	h.Golden("40x14")
	h.Press("space")
	if c.pb.running {
		t.Error("space in tiny did not stop the run")
	}
	handled, cmd := c.HandleKey(h.Context(), tea.KeyPressMsg{Code: 'a', Text: "a"})
	if msg, ok := cmd().(ui.ToastMsg); !handled || !ok || msg.Text != "창을 키워 주세요" {
		t.Errorf("a in tiny: %v %#v", handled, cmd())
	}
}
