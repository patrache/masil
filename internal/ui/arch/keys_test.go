package arch

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// keyHarness is a ui.mouse=false harness at 120x40 on route r, with the
// canvas in front.
func keyHarness(t *testing.T, r ui.Route, opts ...uitest.Option) (*uitest.Harness, *canvasModel) {
	t.Helper()
	h := newHarness(t, append([]uitest.Option{uitest.WithRoute(r)}, opts...)...)
	h.Context().Config.UI.Mouse = false
	h.Resize(120, 40)
	c := canvasOf(t, h, r)
	if got := h.Model.Focus().RouteKey; got != r.Key() {
		t.Fatalf("focus on %q, want the canvas %q", got, r.Key())
	}
	return h, c
}

// designOf reads the design back from the canvas's sink (what is saved).
func designOf(t *testing.T, c *canvasModel) *archdoc.Design {
	t.Helper()
	d, _ := archdoc.FromObjects(c.sink.Objects(), nil)
	if d == nil {
		t.Fatal("no design in the sink")
	}
	return d
}

// pickKeys are the presses that move the palette cursor from its first row to it.
func pickKeys(t *testing.T, it paletteItem) []string {
	t.Helper()
	for i, p := range paletteItems() {
		if p == it {
			return repeat("j", i)
		}
	}
	t.Fatalf("no palette item %+v", it)
	return nil
}

// boxPickKeys are the presses that move the box-kind cursor (g) to k.
func boxPickKeys(t *testing.T, k archdoc.BoxKind) []string {
	t.Helper()
	for i, b := range archdoc.BoxKinds() {
		if b == k {
			return repeat("j", i)
		}
	}
	t.Fatalf("no box kind %s", k)
	return nil
}

func repeat(k string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = k
	}
	return out
}

func nodeOfKind(d *archdoc.Design, k archdoc.NodeKind) *archdoc.Node {
	for i := range d.Nodes {
		if d.Nodes[i].Kind == k {
			return &d.Nodes[i]
		}
	}
	return nil
}

func TestKeyboardBuildFlow(t *testing.T) {
	h, c := keyHarness(t, toolRoute("arch:new:k1", nil))
	h.Press("a")
	h.Press(pickKeys(t, paletteItem{node: archdoc.KindAPI})...)
	h.Press("enter")
	h.Press("a")
	h.Press(pickKeys(t, paletteItem{node: archdoc.KindDBPrimary})...)
	h.Press("enter")
	d := designOf(t, c)
	api, db := nodeOfKind(d, archdoc.KindAPI), nodeOfKind(d, archdoc.KindDBPrimary)
	if len(d.Nodes) != 2 || api == nil || db == nil {
		t.Fatalf("after two adds: %+v", d.Nodes)
	}
	if c.sel != db.ID {
		t.Fatalf("the new node is not selected: sel %q, db %q", c.sel, db.ID)
	}
	if over := c.scene.Overlaps(archdraw.ZoomDetail); len(over) != 0 {
		t.Fatalf("added nodes overlap: %v", over)
	}

	// select the API server, connect it to the DB
	for range 4 {
		if c.sel == api.ID {
			break
		}
		h.Press("h")
	}
	if c.sel != api.ID {
		t.Fatalf("h did not reach the API server (sel %q, api at %v, db at %v)", c.sel, api.Pos, db.Pos)
	}
	h.Press("c")
	if c.mode != modeConnect {
		t.Fatalf("c: mode %d", c.mode)
	}
	h.Press("l", "enter")
	d = designOf(t, c)
	if len(d.Edges) != 1 || d.Edges[0].From != api.ID || d.Edges[0].To != db.ID {
		t.Fatalf("edges after connect: %+v", d.Edges)
	}

	// wrap both in a new AZ box
	h.Press("v", "l")
	if c.mode != modeMulti || !c.multi[api.ID] || !c.multi[db.ID] {
		t.Fatalf("v l: mode %d multi %v", c.mode, c.multi)
	}
	h.Press("g")
	h.Press(boxPickKeys(t, archdoc.BoxAZ)...)
	h.Press("enter")
	d = designOf(t, c)
	if len(d.Boxes) != 1 || d.Boxes[0].Kind != archdoc.BoxAZ {
		t.Fatalf("boxes after g: %+v", d.Boxes)
	}
	box := d.Boxes[0]
	api, db = nodeOfKind(d, archdoc.KindAPI), nodeOfKind(d, archdoc.KindDBPrimary)
	if api.Parent != box.ID || db.Parent != box.ID {
		t.Fatalf("parents %q %q, want %q", api.Parent, db.Parent, box.ID)
	}
	if c.sel != box.ID || c.mode != modeNormal {
		t.Fatalf("after g: sel %q mode %d, want the new box in normal mode", c.sel, c.mode)
	}

	// move the box (and what is in it) two grid steps right
	apiX, dbX, boxX := api.Pos[0], db.Pos[0], box.Rect[0]
	h.Press("m", "l", "l")
	if got := designOf(t, c); nodeOfKind(got, archdoc.KindAPI).Pos[0] != apiX {
		t.Fatal("move mode committed before enter")
	}
	h.Press("enter")
	d = designOf(t, c)
	api, db = nodeOfKind(d, archdoc.KindAPI), nodeOfKind(d, archdoc.KindDBPrimary)
	if api.Pos[0] != apiX+4 || db.Pos[0] != dbX+4 || d.Boxes[0].Rect[0] != boxX+4 {
		t.Fatalf("after m l l enter: api x %d (was %d), db x %d (was %d), box x %d (was %d)",
			api.Pos[0], apiX, db.Pos[0], dbX, d.Boxes[0].Rect[0], boxX)
	}
	if api.Parent != box.ID || db.Parent != box.ID {
		t.Fatalf("moving the box dropped its nodes: %q %q", api.Parent, db.Parent)
	}
	if is := d.Validate(); len(is) != 0 {
		t.Fatalf("final design invalid: %+v", is)
	}

	// a single node: H J K L step one grid cell (2 across, 1 down)
	c.sel = api.ID
	x, y := api.Pos[0], api.Pos[1]
	h.Press("L", "J")
	api = nodeOfKind(designOf(t, c), archdoc.KindAPI)
	if api.Pos != [2]int{x + 2, y + 1} {
		t.Fatalf("L J: pos %v, want %v", api.Pos, [2]int{x + 2, y + 1})
	}
	h.Check()
}

func TestUndoRedoOwnOnly(t *testing.T) {
	h, c := keyHarness(t, toolRoute("arch:new:k1", nil))
	h.Press("a")
	h.Press(pickKeys(t, paletteItem{node: archdoc.KindAPI})...)
	h.Press("enter")
	if n := len(designOf(t, c).Nodes); n != 1 {
		t.Fatalf("%d nodes after add", n)
	}
	// someone else's edit (another object): undo must leave it alone
	c.sink.Apply([]archdoc.Change{{Kind: archdoc.ChangeSet, Obj: "meta", Field: "title", Value: []byte(`"남의 제목"`)}})
	c.rebuild(h.Context())
	h.Press("u")
	d := designOf(t, c)
	if len(d.Nodes) != 0 {
		t.Fatalf("u kept the node: %+v", d.Nodes)
	}
	if d.Title != "남의 제목" {
		t.Fatalf("u undid someone else's title: %q", d.Title)
	}
	h.Press("ctrl+r")
	if d := designOf(t, c); len(d.Nodes) != 1 || d.Title != "남의 제목" {
		t.Fatalf("ctrl+r: nodes %d title %q", len(d.Nodes), d.Title)
	}
	h.Press("u", "u", "u") // more than there is: nothing breaks
	if d := designOf(t, c); len(d.Nodes) != 0 {
		t.Fatalf("extra u: %d nodes", len(d.Nodes))
	}
}

func TestCanvasLeavesGlobalsAlone(t *testing.T) {
	r := toolRoute("arch:new:k1", nil)
	h, c := keyHarness(t, r)
	h.Press("a")
	h.Press(pickKeys(t, paletteItem{node: archdoc.KindAPI})...)
	h.Press("enter")

	h.Press("ctrl+k")
	open := false
	for _, o := range h.Model.Frame().Overlays {
		open = open || o.ID == "palette.overlay"
	}
	if !open {
		t.Fatalf("ctrl+k did not open the palette: %+v", h.Model.Frame().Overlays)
	}
	h.Press("esc")

	// a second canvas: [ and ] reach the tool switcher
	h.Send(ui.OpenTool(ui.ToolArch, "arch:new:k2", nil)())
	before := h.Nav().ToolActive
	h.Press("[")
	if h.Nav().ToolActive == before {
		t.Error("[ did not switch tools")
	}
	h.Press("]")
	if h.Nav().ToolActive != before {
		t.Errorf("] did not switch back: %d, want %d", h.Nav().ToolActive, before)
	}

	// esc in move mode only cancels it; a second esc is not the canvas's
	ctx := h.Context()
	c.mode = modeNormal
	if handled, _ := c.HandleKey(ctx, tea.KeyPressMsg{Code: 'm', Text: "m"}); !handled || c.mode != modeMove {
		t.Fatalf("m: handled %v mode %d", handled, c.mode)
	}
	if handled, _ := c.HandleKey(ctx, tea.KeyPressMsg{Code: tea.KeyEscape}); !handled || c.mode != modeNormal {
		t.Fatalf("esc in move mode: handled %v mode %d", handled, c.mode)
	}
	if handled, _ := c.HandleKey(ctx, tea.KeyPressMsg{Code: tea.KeyEscape}); handled {
		t.Error("the second esc was taken by the canvas")
	}
	for _, k := range []tea.KeyPressMsg{
		{Code: 'k', Mod: tea.ModCtrl}, {Code: 't', Mod: tea.ModCtrl}, {Code: 'c', Mod: tea.ModCtrl},
		{Code: tea.KeyTab}, {Code: tea.KeyTab, Mod: tea.ModShift}, {Code: '[', Text: "["}, {Code: ']', Text: "]"},
	} {
		if handled, _ := c.HandleKey(ctx, k); handled {
			t.Errorf("canvas took %s", k.String())
		}
	}

	// tab moves the focus on (here: off the tool surface)
	h.Press("tab")
	if h.Model.Focus().RouteKey == r.Key() {
		t.Error("tab left the focus on the canvas")
	}

	// ctrl+t toggles the tool surface
	front := h.Nav().Front
	h.Press("ctrl+t")
	if h.Nav().Front == front {
		t.Error("ctrl+t did not toggle the surface")
	}
}

func TestDeleteNoConfirm(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	h, c := keyHarness(t, r, withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	before := designOf(t, c)
	// the node with the most edges
	id, most := "", -1
	for _, n := range before.Nodes {
		k := 0
		for _, e := range before.Edges {
			if e.From == n.ID || e.To == n.ID {
				k++
			}
		}
		if k > most {
			id, most = n.ID, k
		}
	}
	c.sel = id
	h.Press("d")
	d := designOf(t, c)
	if len(d.Nodes) != len(before.Nodes)-1 || len(d.Edges) != len(before.Edges)-most {
		t.Fatalf("d: %d nodes %d edges, want %d and %d", len(d.Nodes), len(d.Edges), len(before.Nodes)-1, len(before.Edges)-most)
	}
	if is := d.Validate(); len(is) != 0 {
		t.Fatalf("after d: %+v", is)
	}
	if c.sel != "" {
		t.Errorf("selection %q survived its delete", c.sel)
	}
	h.Press("u")
	after := designOf(t, c)
	if len(after.Nodes) != len(before.Nodes) || len(after.Edges) != len(before.Edges) {
		t.Fatalf("one u: %d nodes %d edges, want %d and %d", len(after.Nodes), len(after.Edges), len(before.Nodes), len(before.Edges))
	}
	if string(mustEncode(t, after)) != string(mustEncode(t, before)) {
		t.Error("one u did not restore the design exactly")
	}
}

func mustEncode(t *testing.T, d *archdoc.Design) []byte {
	t.Helper()
	b, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// pileUp is the fixture with every node at the same place.
func pileUp(t *testing.T) *archdoc.Design {
	d := loadFixture(t, "pay_v3")
	for i := range d.Nodes {
		d.Nodes[i].Pos = [2]int{10, 10}
	}
	return d
}

func TestArrangeKey(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": "pile"})
	h, c := keyHarness(t, r, withDesign(t, "pile", pileUp(t)))
	if len(c.scene.Overlaps(archdraw.ZoomDetail)) == 0 {
		t.Fatal("the fixture does not overlap")
	}
	h.Press("=")
	if over := c.scene.Overlaps(archdraw.ZoomDetail); len(over) != 0 {
		t.Fatalf("after =: overlaps %v", over)
	}
	h.Press("u")
	if len(c.scene.Overlaps(archdraw.ZoomDetail)) == 0 {
		t.Error("u did not take the arrange back")
	}
}

func TestFindNode(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	h, c := keyHarness(t, r, withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	c.vp.X, c.vp.Y = 5000, 5000 // far away
	h.Press("/")
	if !c.CapturesInput() {
		t.Fatal("find does not capture typing")
	}
	h.Type("db")
	h.Press("enter")
	n := c.node(c.sel)
	if n == nil || (n.Kind != archdoc.KindDBPrimary && n.Kind != archdoc.KindDBReplica && !strings.Contains(strings.ToLower(n.Label), "db")) {
		t.Fatalf("find db: selected %q (%+v)", c.sel, n)
	}
	sx, sy := c.vp.ToScreen(n.Pos[0], n.Pos[1])
	w, hh := archdraw.NodeSize(c.vp.Zoom)
	if sx < 0 || sy < 0 || sx+w > c.vp.W || sy+hh > c.vp.H {
		t.Errorf("found node at screen %d,%d outside the %dx%d canvas", sx, sy, c.vp.W, c.vp.H)
	}
	if c.mode != modeNormal || c.CapturesInput() {
		t.Error("find did not end on enter")
	}
	// no match: a toast, the selection stays
	sel := c.sel
	h.Press("/")
	h.Type("없는노드")
	h.Press("enter")
	if c.sel != sel {
		t.Errorf("a failed find moved the selection to %q", c.sel)
	}
	if !strings.Contains(h.Screen(), "없는노드") {
		t.Errorf("no not-found toast:\n%s", h.Screen())
	}
}

// rowDesign is a row of API servers 14 cells apart: apart in detail, overlapping in dot.
func rowDesign() *archdoc.Design {
	d := archdoc.New("줄")
	for i := range 6 {
		n := archdoc.Defaults(archdoc.KindAPI)
		n.ID = "n_" + string(rune('a'+i))
		n.Pos = [2]int{i * 14, 2}
		d.Nodes = append(d.Nodes, n)
	}
	return d
}

func TestZoomOverlapNotice(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": "row"})
	h, c := keyHarness(t, r, withDesign(t, "row", rowDesign()))
	if len(c.scene.Overlaps(archdraw.ZoomDetail)) != 0 || len(c.scene.Overlaps(archdraw.ZoomDot)) == 0 {
		t.Fatal("fixture: want apart in detail and overlapping in dot")
	}
	h.Press("-", "-")
	if c.vp.Zoom != archdraw.ZoomDot {
		t.Fatalf("- -: zoom %d", c.vp.Zoom)
	}
	if !strings.Contains(h.Screen(), "확대를 높이세요") {
		t.Errorf("no zoom-in notice:\n%s", h.Screen())
	}
	sc, _ := c.visibleScene()
	if over := sc.Overlaps(archdraw.ZoomDot); len(over) != 0 {
		t.Errorf("overlapping nodes drawn: %v", over)
	}
	h.Check()
	h.Press("+", "+")
	if c.vp.Zoom != archdraw.ZoomDetail || strings.Contains(h.Screen(), "확대를 높이세요") {
		t.Errorf("+ +: zoom %d", c.vp.Zoom)
	}
}

func TestTinyEditKeys(t *testing.T) {
	r := toolRoute("arch:new:k1", nil)
	h, c := keyHarness(t, r)
	h.Resize(40, 14)
	if c.regs.View != viewSummary {
		t.Fatalf("40x14: view %d", c.regs.View)
	}
	handled, cmd := c.HandleKey(h.Context(), tea.KeyPressMsg{Code: 'a', Text: "a"})
	if !handled || cmd == nil {
		t.Fatal("a in tiny: not handled")
	}
	if msg, ok := cmd().(ui.ToastMsg); !ok || msg.Text != h.Context().T.Archcanvas.TooSmallEdit() {
		t.Errorf("a in tiny: %#v", cmd())
	}
	if n := len(designOf(t, c).Nodes); n != 0 {
		t.Errorf("tiny edit changed the design: %d nodes", n)
	}
}

func TestHelpAndHints(t *testing.T) {
	h, c := keyHarness(t, toolRoute("arch:new:k1", nil))
	if len(c.Help(h.Context())) == 0 {
		t.Error("no status hints")
	}
	h.Press("?")
	s := h.Screen()
	if !strings.Contains(s, "줄서기 공식") {
		t.Errorf("? does not show the model note:\n%s", s)
	}
	h.Check()
	h.Press("esc")
	if strings.Contains(h.Screen(), "줄서기 공식") {
		t.Error("esc did not close the help")
	}
	h.Press("m")
	if c.mode != modeNormal {
		t.Error("m with nothing selected entered move mode")
	}
}

func TestReadOnlyCopyRefusesEdits(t *testing.T) {
	r := toolRoute("arch:msg:x", map[string]string{"msg": "x"})
	h, c := keyHarness(t, r)
	c.setSink(h.Context(), &copySink{objs: loadFixture(t, "pay_v3").ToObjects()})
	before := mustEncode(t, designOf(t, c))
	c.sel = c.design.Nodes[0].ID
	h.Press("d")
	h.Press("a", "enter")
	if string(mustEncode(t, designOf(t, c))) != string(before) {
		t.Error("a read-only copy was edited")
	}
	if !strings.Contains(h.Screen(), "읽기 전용") {
		t.Errorf("no read-only notice:\n%s", h.Screen())
	}
}
