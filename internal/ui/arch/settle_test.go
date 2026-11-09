package arch

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// TestEditKeepsUntouchedRoutes: an edit rebuilds the scene from the previous
// one, so a route nothing touched is reused (the same points, not routed
// again) and a settle tick on a settled scene asks for no more.
func TestEditKeepsUntouchedRoutes(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	h, c := keyHarness(t, r, withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	if len(c.design.Edges) == 0 {
		t.Fatal("fixture has no edges")
	}
	z := c.vp.Zoom
	before := map[string][]archdraw.Point{}
	for _, e := range c.design.Edges {
		before[e.ID] = c.scene.Route(e, z)
	}
	if cmd := c.edit(h.Context(), func(d *archdoc.Design) { d.Title = "another title" }); cmd == nil {
		t.Fatal("the edit returned no command")
	}
	for _, e := range c.design.Edges {
		after := c.scene.Route(e, z)
		if len(after) == 0 || len(before[e.ID]) == 0 || &after[0] != &before[e.ID][0] {
			t.Errorf("edge %s routed again after an edit that moved nothing", e.ID)
		}
	}
	h.Screen()
	if cmd := c.Update(h.Context(), settleMsg{instance: r.ID}); cmd != nil {
		t.Errorf("a settled scene asked for another settle step")
	}
}

// TestWidget80x24ShowsCanvas: an 80×24 terminal (widget mode) shows the canvas
// at the normal zoom, 79 columns the list view (spec C2.6).
func TestWidget80x24ShowsCanvas(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	h := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	h.Resize(80, 24)
	c := canvasOf(t, h, r)
	if c.regs.View != viewCanvas || c.vp.Zoom != archdraw.ZoomNormal {
		t.Errorf("80x24 (inner %dx%d): view %d zoom %d, want the canvas at the normal zoom", c.size.W, c.size.H, c.regs.View, c.vp.Zoom)
	}
	h.Resize(79, 24)
	if c.regs.View != viewList {
		t.Errorf("79x24 (inner %dx%d): view %d, want the list", c.size.W, c.size.H, c.regs.View)
	}
}

// TestCloseWritesPendingEdits: closing a personal design within the save delay
// writes its last edit at once (the debounced save would find no instance).
func TestCloseWritesPendingEdits(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	h, c := keyHarness(t, r, withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	n := len(c.design.Nodes)
	h.Press("a", "enter")
	if len(c.design.Nodes) != n+1 || c.sink.Status() != savePending {
		t.Fatalf("after a: %d nodes, status %d", len(c.design.Nodes), c.sink.Status())
	}
	h.Send(ui.CloseToolMsg{InstanceID: r.ID})
	h.Advance(3 * time.Second)
	h.Flush()
	st, err := archdoc.OpenStore(filepath.Join(os.Getenv("MASIL_CONFIG_DIR"), "arch"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Load(fixtureName)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Nodes) != n+1 {
		t.Errorf("after closing: the file has %d nodes, want %d", len(d.Nodes), n+1)
	}
}

// TestEditsStopAtTheLimits: a design at its node, edge or box limit refuses
// one more (a toast) instead of letting the read-back cap drop an existing
// object — here z299 and its edge, which sort after the new n_<ulid> ids.
func TestEditsStopAtTheLimits(t *testing.T) {
	h := newHarness(t)
	ctx := h.Context()
	full := func() *archdoc.Design {
		d := archdoc.New("full")
		for i := range archdoc.MaxNodes {
			n := archdoc.Defaults(archdoc.KindAPI)
			n.ID, n.Pos = fmt.Sprintf("z%03d", i), [2]int{(i % 20) * 16, (i / 20) * 6}
			d.Nodes = append(d.Nodes, n)
		}
		e := archdoc.DefaultEdge()
		e.ID, e.From, e.To = "z_edge", "z000", "z299"
		d.Edges = append(d.Edges, e)
		return d
	}
	open := func(d *archdoc.Design) *canvasModel {
		c := newCanvas(ctx, toolRoute("arch:new:full", nil)).(*canvasModel)
		c.setSink(ctx, &localSink{instance: "arch:new:full", objs: d.ToObjects()})
		return c
	}
	limit := func(what string, cmd tea.Cmd) {
		t.Helper()
		if got := toastOf(cmd); got != ctx.T.Archcanvas.LimitReached() {
			t.Errorf("%s: toast %q, want the limit toast", what, got)
		}
	}
	stored := func(c *canvasModel) *archdoc.Design {
		d, _ := archdoc.FromObjects(c.sink.Objects(), nil)
		return d
	}

	c := open(full())
	limit("a 301st node", c.addPart(ctx, paletteItem{node: archdoc.KindDBPrimary}))
	c.sel = ""
	limit("a 301st node dropped", c.addPartAt(ctx, paletteItem{node: archdoc.KindDBPrimary}, 0, 200))
	if d := stored(c); len(d.Nodes) != archdoc.MaxNodes || findNode(d, "z299") == nil || len(d.Edges) != 1 {
		t.Errorf("after adds at the node limit: %d nodes, z299 kept %v, %d edges", len(d.Nodes), findNode(d, "z299") != nil, len(d.Edges))
	}

	d := full()
	for i := 1; len(d.Edges) < archdoc.MaxEdges; i++ {
		e := archdoc.DefaultEdge()
		e.ID, e.From, e.To = fmt.Sprintf("y%04d", i), fmt.Sprintf("z%03d", i%300), fmt.Sprintf("z%03d", (i/300+i+1)%300)
		if e.From != e.To {
			d.Edges = append(d.Edges, e)
		}
	}
	c = open(d)
	limit("a 1001st edge", c.connect(ctx, "z010", "z200"))
	if got := stored(c); len(got.Edges) != archdoc.MaxEdges || !slices.ContainsFunc(got.Edges, func(e archdoc.Edge) bool { return e.ID == "z_edge" }) {
		t.Errorf("after connect at the edge limit: %d edges", len(got.Edges))
	}

	d = archdoc.New("boxes")
	n := archdoc.Defaults(archdoc.KindAPI)
	n.ID, n.Pos = "z_node", [2]int{0, 0}
	d.Nodes = append(d.Nodes, n)
	for i := range archdoc.MaxBoxes {
		d.Boxes = append(d.Boxes, archdoc.Box{ID: fmt.Sprintf("zb%03d", i), Kind: archdoc.BoxRegion, Rect: [4]int{100 + i*20, 0, 18, 8}})
	}
	c = open(d)
	limit("a 101st box by g", c.wrap(ctx, []string{"z_node"}, archdoc.BoxRegion))
	c.sel = ""
	limit("a 101st box", c.addPart(ctx, paletteItem{box: archdoc.BoxRegion}))
	if got := stored(c); len(got.Boxes) != archdoc.MaxBoxes || findBox(got, "zb099") == nil || findNode(got, "z_node").Parent != "" {
		t.Errorf("after wraps at the box limit: %d boxes", len(got.Boxes))
	}
}

// toastOf runs cmd (and a batch's cmds) and returns the first toast's text.
func toastOf(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	switch msg := cmd().(type) {
	case ui.ToastMsg:
		return msg.Text
	case tea.BatchMsg:
		for _, c := range msg {
			if s := toastOf(c); s != "" {
				return s
			}
		}
	}
	return ""
}
