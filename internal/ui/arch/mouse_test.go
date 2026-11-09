package arch

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
)

// mouseHarness is a harness (mouse on) at w x h on route r.
func mouseHarness(t *testing.T, r ui.Route, w, hh int, opts ...uitest.Option) (*uitest.Harness, *canvasModel) {
	t.Helper()
	h := newHarness(t, append([]uitest.Option{uitest.WithRoute(r)}, opts...)...)
	h.Resize(w, hh)
	return h, canvasOf(t, h, r)
}

// detail zooms the canvas to the detail level (screen cells = world cells).
func detail(t *testing.T, h *uitest.Harness, c *canvasModel) {
	t.Helper()
	for range 2 {
		if c.vp.Zoom != archdraw.ZoomDetail {
			h.Press("+")
		}
	}
	if c.vp.Zoom != archdraw.ZoomDetail {
		t.Fatalf("zoom %d after +", c.vp.Zoom)
	}
}

// scr maps a pane-local point to the terminal screen.
func scr(t *testing.T, h *uitest.Harness, lx, ly int) (int, int) {
	t.Helper()
	p, ok := h.Model.Frame().Layout.Pane(layout.SlotMain)
	if !ok {
		t.Fatal("no main pane")
	}
	in := p.Rect
	if p.Bordered {
		in = in.Inner()
	}
	return in.X + lx, in.Y + ly
}

// worldScr maps a world point to the terminal screen through the canvas.
func worldScr(t *testing.T, h *uitest.Harness, c *canvasModel, wx, wy int) (int, int) {
	t.Helper()
	sx, sy := c.vp.ToScreen(wx, wy)
	return scr(t, h, c.regs.Canvas.X+sx, c.regs.Canvas.Y+sy)
}

// canvasScr is the canvas cell (cx, cy) on the terminal screen.
func canvasScr(t *testing.T, h *uitest.Harness, c *canvasModel, cx, cy int) (int, int) {
	t.Helper()
	return scr(t, h, c.regs.Canvas.X+cx, c.regs.Canvas.Y+cy)
}

// paletteScr is palette item it's row on the terminal screen.
func paletteScr(t *testing.T, h *uitest.Harness, c *canvasModel, it paletteItem) (int, int) {
	t.Helper()
	all := paletteItems()
	idx := slices.Index(all, it)
	_, items := paletteRows(h.Context().T)
	row := slices.Index(items, idx)
	if idx < 0 || row < 0 || c.regs.Palette.W == 0 || row >= c.regs.Palette.H {
		t.Fatalf("palette item %+v not on screen (palette %+v)", it, c.regs.Palette)
	}
	return scr(t, h, c.regs.Palette.X+2, c.regs.Palette.Y+row)
}

func boxOfKind(d *archdoc.Design, k archdoc.BoxKind) *archdoc.Box {
	for i := range d.Boxes {
		if d.Boxes[i].Kind == k {
			return &d.Boxes[i]
		}
	}
	return nil
}

// drag is h.Drag with (x0, y0) → (x1, y1) given as pairs.
func drag(h *uitest.Harness, x0, y0, x1, y1 int) { h.Drag(x0, y0, x1, y1) }

// mouseBuild is acceptance #1 at 120x40 with the mouse only; it returns the
// final design.
func mouseBuild(t *testing.T) *archdoc.Design {
	h, c := mouseHarness(t, toolRoute("arch:new:m1", nil), 120, 40)
	detail(t, h, c)
	// 1–2: parts from the palette
	px, py := paletteScr(t, h, c, paletteItem{node: archdoc.KindAPI})
	tx, ty := canvasScr(t, h, c, 20, 4)
	drag(h, px, py, tx, ty)
	px, py = paletteScr(t, h, c, paletteItem{node: archdoc.KindDBPrimary})
	tx, ty = canvasScr(t, h, c, 20, 14)
	drag(h, px, py, tx, ty)
	d := designOf(t, c)
	api, db := nodeOfKind(d, archdoc.KindAPI), nodeOfKind(d, archdoc.KindDBPrimary)
	if len(d.Nodes) != 2 || api == nil || db == nil {
		t.Fatalf("palette drops: %+v", d.Nodes)
	}
	if wx, wy := c.vp.ToWorld(20, 4); api.Pos != [2]int{snapX(wx), wy} {
		t.Fatalf("api dropped at %v, want the pointer %v", api.Pos, [2]int{snapX(wx), wy})
	}
	// 3: the ◆ handle onto the DB
	w, hh := archdraw.NodeSize(c.vp.Zoom)
	hx, hy := worldScr(t, h, c, api.Pos[0], api.Pos[1])
	dx, dy := worldScr(t, h, c, db.Pos[0], db.Pos[1])
	drag(h, hx+w-1, hy+hh/2, dx+w/2, dy+hh/2)
	d = designOf(t, c)
	if len(d.Edges) != 1 || d.Edges[0].From != api.ID || d.Edges[0].To != db.ID {
		t.Fatalf("handle drag: edges %+v", d.Edges)
	}
	// 4: an AZ box on an empty spot
	px, py = paletteScr(t, h, c, paletteItem{box: archdoc.BoxAZ})
	tx, ty = canvasScr(t, h, c, 40, 2)
	drag(h, px, py, tx, ty)
	d = designOf(t, c)
	az := boxOfKind(d, archdoc.BoxAZ)
	if az == nil {
		t.Fatalf("no AZ box after the drop: %+v", d.Boxes)
	}
	// 5: the API server into the AZ
	ax, ay := worldScr(t, h, c, api.Pos[0], api.Pos[1])
	bx, by := worldScr(t, h, c, az.Rect[0]+2, az.Rect[1]+2)
	drag(h, ax+3, ay+1, bx+3, by+1)
	d = designOf(t, c)
	api = nodeOfKind(d, archdoc.KindAPI)
	if api.Parent != az.ID {
		t.Fatalf("api parent %q after the drop into the AZ %q (api at %v, box %v)", api.Parent, az.ID, api.Pos, az.Rect)
	}
	// 6: move it inside the box
	from := api.Pos
	ax, ay = worldScr(t, h, c, api.Pos[0], api.Pos[1])
	drag(h, ax+3, ay+1, ax+3+4, ay+1+1)
	d = designOf(t, c)
	api = nodeOfKind(d, archdoc.KindAPI)
	if api.Pos != [2]int{from[0] + 4, from[1] + 1} || api.Parent != az.ID {
		t.Fatalf("drag move: pos %v parent %q, want %v in %q", api.Pos, api.Parent, [2]int{from[0] + 4, from[1] + 1}, az.ID)
	}
	if is := d.Validate(); len(is) != 0 {
		t.Fatalf("invalid: %+v", is)
	}
	h.Check()
	return d
}

func TestMouseOnlyBuildFlow(t *testing.T) { mouseBuild(t) }

// pressAndMove presses at (x0, y0) and moves to (x1, y1) without releasing.
func pressAndMove(h *uitest.Harness, x0, y0, x1, y1 int) {
	h.Send(tea.MouseClickMsg{X: x0, Y: y0, Button: tea.MouseLeft})
	h.Send(tea.MouseMotionMsg{X: x1, Y: y0, Button: tea.MouseLeft})
	h.Send(tea.MouseMotionMsg{X: x1, Y: y1, Button: tea.MouseLeft})
}

// payHarness opens the fixture at w x h with the mouse on.
func payHarness(t *testing.T, w, hh int) (*uitest.Harness, *canvasModel) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	h, c := mouseHarness(t, r, w, hh, withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	if c.regs.View == viewCanvas {
		detail(t, h, c)
	}
	return h, c
}

// visibleNode is a node drawn whole on the canvas with free cells to its right.
func visibleNode(t *testing.T, c *canvasModel) *archdoc.Node {
	t.Helper()
	w, hh := archdraw.NodeSize(c.vp.Zoom)
	for _, id := range c.scene.NodeIDs() {
		n := c.node(id)
		sx, sy := c.vp.ToScreen(n.Pos[0], n.Pos[1])
		if sx >= 0 && sy >= 1 && sx+w+8 <= c.vp.W && sy+hh+3 <= c.vp.H {
			return n
		}
	}
	t.Fatal("no node on the canvas")
	return nil
}

func TestDragGhostDuringMotion(t *testing.T) {
	h, c := payHarness(t, 120, 40)
	n := visibleNode(t, c)
	before := string(mustEncode(t, designOf(t, c)))
	x, y := worldScr(t, h, c, n.Pos[0], n.Pos[1])
	pressAndMove(h, x+2, y+1, x+2+4, y+1+2)
	ov := c.overlay()
	if ov.Ghost == nil || ov.Ghost.ID != n.ID || ov.Ghost.X != n.Pos[0]+4 || ov.Ghost.Y != n.Pos[1]+2 {
		t.Fatalf("mid-drag ghost %+v, want %s at %d,%d", ov.Ghost, n.ID, n.Pos[0]+4, n.Pos[1]+2)
	}
	screen := strings.Split(h.Screen(), "\n")
	if !strings.Contains(screen[y+2+1], "╎") && !strings.Contains(screen[y+2], "╌") {
		t.Errorf("no dashed ghost on screen:\n%s", h.Screen())
	}
	if got := []rune(screen[y])[x]; got != '┌' {
		t.Errorf("the original node is not drawn at its place (%q):\n%s", got, h.Screen())
	}
	if string(mustEncode(t, designOf(t, c))) != before {
		t.Fatal("committed before the release")
	}
	h.Check()
	h.Send(tea.MouseReleaseMsg{X: x + 6, Y: y + 3, Button: tea.MouseLeft})
	if got := c.node(n.ID).Pos; got != [2]int{n.Pos[0] + 4, n.Pos[1] + 2} {
		t.Errorf("after the release: pos %v", got)
	}
	if c.overlay().Ghost != nil {
		t.Error("ghost left after the release")
	}
	h.Press("u")
	if got := c.node(n.ID).Pos; got != n.Pos {
		t.Errorf("one u did not undo the drop: %v", got)
	}
}

// boxDesign is an empty AZ box and an API server outside it.
func boxDesign() *archdoc.Design {
	d := archdoc.New("상자")
	d.Boxes = []archdoc.Box{{ID: "b_az", Kind: archdoc.BoxAZ, Label: "AZ", Rect: [4]int{30, 2, 36, 15}}}
	n := archdoc.Defaults(archdoc.KindAPI)
	n.ID, n.Label, n.Pos = "n_api", "API", [2]int{4, 4}
	d.Nodes = []archdoc.Node{n}
	return d
}

func TestDropIntoBoxHighlights(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": "box"})
	h, c := mouseHarness(t, r, 120, 40, withDesign(t, "box", boxDesign()))
	detail(t, h, c)
	c.vp.X, c.vp.Y = 0, 0
	x, y := worldScr(t, h, c, 4, 4)
	tx, ty := worldScr(t, h, c, 36, 6)
	pressAndMove(h, x+1, y+1, tx+1, ty+1)
	ov := c.overlay()
	if ov.Ghost == nil || ov.Ghost.IntoBox != "b_az" {
		t.Fatalf("over the AZ: ghost %+v, want IntoBox b_az", ov.Ghost)
	}
	g := c.scene.Render(c.vp, ov)
	bx, by := c.vp.ToScreen(30, 2)
	if st := g.At(bx, by).Style; st != archdraw.StyleSelected {
		t.Errorf("the target box border style %d, want selected", st)
	}
	// not over the box: no highlight
	h.Send(tea.MouseMotionMsg{X: x + 1, Y: ty + 1, Button: tea.MouseLeft})
	if ov := c.overlay(); ov.Ghost == nil || ov.Ghost.IntoBox != "" {
		t.Errorf("outside the box: ghost %+v", ov.Ghost)
	}
	h.Send(tea.MouseMotionMsg{X: tx + 1, Y: ty + 1, Button: tea.MouseLeft})
	h.Send(tea.MouseReleaseMsg{X: tx + 1, Y: ty + 1, Button: tea.MouseLeft})
	if n := c.node("n_api"); n.Parent != "b_az" || n.Pos != [2]int{36, 6} {
		t.Errorf("dropped node at %v in %q, want 36,6 in b_az", n.Pos, n.Parent)
	}
}

func scrX(t *testing.T, h *uitest.Harness, c *canvasModel) int {
	x, _ := scr(t, h, c.regs.Canvas.X, c.regs.Canvas.Y)
	return x
}

func scrY(t *testing.T, h *uitest.Harness, c *canvasModel) int {
	_, y := scr(t, h, c.regs.Canvas.X, c.regs.Canvas.Y)
	return y
}

func TestBoxDragMovesChildren(t *testing.T) {
	h, c := payHarness(t, 120, 40)
	d := designOf(t, c)
	box := boxOfKind(d, archdoc.BoxAZ)
	in := descendants(d, box.ID)
	c.vp.X, c.vp.Y = box.Rect[0]-4, box.Rect[1]-2
	x, y := worldScr(t, h, c, box.Rect[0], box.Rect[1])
	// the box's top border, right of its label
	bx := x + box.Rect[2] - 3
	if got := c.scene.HitTest(c.vp, bx-scrX(t, h, c), y-scrY(t, h, c)); got.Kind != archdraw.HitBoxBorder || got.ID != box.ID {
		t.Fatalf("hit %+v at the box border", got)
	}
	drag(h, bx, y, bx+6, y+2)
	after := designOf(t, c)
	for _, b := range d.Boxes {
		if in[b.ID] {
			if got := findBox(after, b.ID).Rect; got[0] != b.Rect[0]+6 || got[1] != b.Rect[1]+2 {
				t.Errorf("box %s at %v, want moved by 6,2 from %v", b.ID, got, b.Rect)
			}
		}
	}
	moved := 0
	for _, n := range d.Nodes {
		got := findNode(after, n.ID)
		if in[n.Parent] {
			moved++
			if got.Pos != [2]int{n.Pos[0] + 6, n.Pos[1] + 2} || got.Parent != n.Parent {
				t.Errorf("node %s at %v in %q, want moved with its box", n.ID, got.Pos, got.Parent)
			}
		} else if got.Pos != n.Pos {
			t.Errorf("node %s outside the box moved", n.ID)
		}
	}
	if moved == 0 {
		t.Fatal("the box had no nodes")
	}
}

func TestRightClickMenu(t *testing.T) {
	h, c := payHarness(t, 120, 40)
	n := visibleNode(t, c)
	x, y := worldScr(t, h, c, n.Pos[0], n.Pos[1])
	h.Send(tea.MouseClickMsg{X: x + 2, Y: y + 1, Button: tea.MouseRight})
	h.Send(tea.MouseReleaseMsg{X: x + 2, Y: y + 1, Button: tea.MouseRight})
	if c.menu == nil || c.menu.target != n.ID {
		t.Fatalf("right click: menu %+v", c.menu)
	}
	screen := h.Screen()
	for _, want := range []string{"설정", "장애", "삭제", "댓글"} {
		if !strings.Contains(screen, want) {
			t.Errorf("menu lacks %q:\n%s", want, screen)
		}
	}
	h.Check()
	// click 삭제
	lines := strings.Split(screen, "\n")
	mx, my := -1, -1
	for row, l := range lines {
		if i := strings.Index(l, "삭제"); i >= 0 && row >= y {
			mx, my = len([]rune(l[:i])), row
			break
		}
	}
	if mx < 0 {
		t.Fatal("no 삭제 row")
	}
	h.Click(mx, my)
	if c.node(n.ID) != nil {
		t.Errorf("삭제 did not remove %s", n.ID)
	}
	if c.menu != nil {
		t.Error("the menu stayed open")
	}
	// esc closes a menu without doing anything
	m := visibleNode(t, c)
	x, y = worldScr(t, h, c, m.Pos[0], m.Pos[1])
	h.Send(tea.MouseClickMsg{X: x + 2, Y: y + 1, Button: tea.MouseRight})
	h.Press("esc")
	if c.menu != nil || c.node(m.ID) == nil {
		t.Error("esc: menu open or node gone")
	}
}

func TestDragCancelledByResizeAndRemoteDelete(t *testing.T) {
	h, c := payHarness(t, 120, 40)
	n := visibleNode(t, c)
	before := string(mustEncode(t, designOf(t, c)))
	x, y := worldScr(t, h, c, n.Pos[0], n.Pos[1])
	pressAndMove(h, x+2, y+1, x+8, y+3)
	if c.overlay().Ghost == nil {
		t.Fatal("no ghost mid-drag")
	}
	h.Resize(80, 24)
	if c.gest != nil || c.overlay().Ghost != nil {
		t.Error("resize left the gesture")
	}
	h.Send(tea.MouseReleaseMsg{X: x + 8, Y: y + 3, Button: tea.MouseLeft})
	if string(mustEncode(t, designOf(t, c))) != before {
		t.Error("a cancelled drag committed")
	}

	h.Resize(120, 40)
	n = visibleNode(t, c)
	x, y = worldScr(t, h, c, n.Pos[0], n.Pos[1])
	pressAndMove(h, x+2, y+1, x+8, y+3)
	// someone else deletes the node mid-drag
	c.sink.Apply([]archdoc.Change{{Kind: archdoc.ChangeDel, Obj: archdoc.ObjKey('n', n.ID)}})
	c.rebuild(h.Context())
	if c.gest != nil || c.overlay().Ghost != nil {
		t.Error("the remote delete left the ghost")
	}
	gone := string(mustEncode(t, designOf(t, c)))
	h.Send(tea.MouseMotionMsg{X: x + 9, Y: y + 3, Button: tea.MouseLeft})
	h.Send(tea.MouseReleaseMsg{X: x + 9, Y: y + 3, Button: tea.MouseLeft})
	if string(mustEncode(t, designOf(t, c))) != gone {
		t.Error("the release after a remote delete committed")
	}
}

func TestNarrowCanvasBlocksDrag(t *testing.T) {
	h, c := payHarness(t, 56, 24)
	if c.regs.View != viewList {
		t.Fatalf("56x24: view %d, want the list", c.regs.View)
	}
	h.Press("v")
	if c.regs.View != viewCanvas || c.regs.Canvas.W >= dragMinW {
		t.Fatalf("v: view %d canvas %+v", c.regs.View, c.regs.Canvas)
	}
	before := string(mustEncode(t, designOf(t, c)))
	n := c.node(c.scene.NodeIDs()[0])
	c.vp.X, c.vp.Y = n.Pos[0]-2, n.Pos[1]-1
	x, y := worldScr(t, h, c, n.Pos[0], n.Pos[1])
	drag(h, x+2, y+1, x+6, y+3)
	if string(mustEncode(t, designOf(t, c))) != before {
		t.Error("a narrow canvas drag changed the design")
	}
	if !strings.Contains(h.Screen(), "끌어다 놓을 수 없어요") {
		t.Errorf("no narrow_no_drag toast:\n%s", h.Screen())
	}
}

func TestWheelAndZoomButtons(t *testing.T) {
	h, c := payHarness(t, 120, 40)
	y0 := c.vp.Y
	x, y := canvasScr(t, h, c, 10, 10)
	h.Wheel(x, y, 2)
	if c.vp.Y <= y0 {
		t.Errorf("wheel down: vp.Y %d (was %d)", c.vp.Y, y0)
	}
	h.Wheel(x, y, -2)
	if c.vp.Y != y0 {
		t.Errorf("wheel back: vp.Y %d, want %d", c.vp.Y, y0)
	}
	z := c.vp.Zoom
	bx, by := canvasScr(t, h, c, c.regs.Canvas.W-2, 0) // －
	h.Click(bx, by)
	if c.vp.Zoom != z.Out() || z.Out() == z {
		t.Errorf("－ click: zoom %d (was %d)", c.vp.Zoom, z)
	}
	bx, by = canvasScr(t, h, c, c.regs.Canvas.W-4, 0) // ＋
	h.Click(bx, by)
	if c.vp.Zoom != z {
		t.Errorf("＋ click: zoom %d, want %d", c.vp.Zoom, z)
	}
}

// normalized is d with ids, seed and timestamps replaced by stable names, so a
// mouse-built and a key-built design compare equal.
func normalized(t *testing.T, d *archdoc.Design) string {
	t.Helper()
	names := map[string]string{}
	for _, n := range d.Nodes {
		names[n.ID] = "n_" + string(n.Kind)
	}
	for _, b := range d.Boxes {
		names[b.ID] = "b_" + string(b.Kind)
	}
	for _, e := range d.Edges {
		names[e.ID] = "e_" + names[e.From] + "_" + names[e.To]
	}
	c := *d
	c.Seed = 0
	c.Nodes = slices.Clone(d.Nodes)
	c.Boxes = slices.Clone(d.Boxes)
	c.Edges = slices.Clone(d.Edges)
	for i := range c.Nodes {
		c.Nodes[i].ID, c.Nodes[i].Parent = names[c.Nodes[i].ID], names[c.Nodes[i].Parent]
	}
	for i := range c.Boxes {
		c.Boxes[i].ID, c.Boxes[i].Parent = names[c.Boxes[i].ID], names[c.Boxes[i].Parent]
	}
	for i := range c.Edges {
		c.Edges[i].ID, c.Edges[i].From, c.Edges[i].To = names[c.Edges[i].ID], names[c.Edges[i].From], names[c.Edges[i].To]
	}
	slices.SortFunc(c.Nodes, func(a, b archdoc.Node) int { return strings.Compare(a.ID, b.ID) })
	b, err := json.MarshalIndent(&c, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// moveKeys are the m-mode presses that move by (dx, dy) world cells.
func moveKeys(dx, dy int) []string {
	var out []string
	for ; dx > 0; dx -= gridX {
		out = append(out, "l")
	}
	for ; dx < 0; dx += gridX {
		out = append(out, "h")
	}
	for ; dy > 0; dy -= gridY {
		out = append(out, "j")
	}
	for ; dy < 0; dy += gridY {
		out = append(out, "k")
	}
	return out
}

func TestMouseKeyboardParity(t *testing.T) {
	want := mouseBuild(t)
	wantAPI, wantDB := nodeOfKind(want, archdoc.KindAPI), nodeOfKind(want, archdoc.KindDBPrimary)
	wantAZ := boxOfKind(want, archdoc.BoxAZ)

	h, c := keyHarness(t, toolRoute("arch:new:k1", nil))
	moveTo := func(id string, x, y int) {
		t.Helper()
		selectKeys(t, h, c, id)
		var at [2]int
		if n := c.node(id); n != nil {
			at = n.Pos
		} else {
			b := c.box(id)
			at = [2]int{b.Rect[0], b.Rect[1]}
		}
		h.Press("m")
		h.Press(moveKeys(x-at[0], y-at[1])...)
		h.Press("enter")
	}
	// the parts, each moved to where the mouse put it
	h.Press("a")
	h.Press(pickKeys(t, paletteItem{node: archdoc.KindAPI})...)
	h.Press("enter")
	api := c.sel
	h.Press("a")
	h.Press(pickKeys(t, paletteItem{node: archdoc.KindDBPrimary})...)
	h.Press("enter")
	db := c.sel
	moveTo(db, wantDB.Pos[0], wantDB.Pos[1])
	selectKeys(t, h, c, api)
	h.Press("c")
	for _, k := range []string{"j", "l", "k", "h"} {
		if c.cursor == db {
			break
		}
		h.Press(k)
	}
	if c.cursor != db {
		t.Fatalf("connect cursor %q never reached the DB %q", c.cursor, db)
	}
	h.Press("enter")
	h.Press("a")
	h.Press(pickKeys(t, paletteItem{box: archdoc.BoxAZ})...)
	h.Press("enter")
	moveTo(c.sel, wantAZ.Rect[0], wantAZ.Rect[1])
	moveTo(api, wantAPI.Pos[0], wantAPI.Pos[1])

	got := designOf(t, c)
	if g, w := normalized(t, got), normalized(t, want); g != w {
		t.Errorf("keyboard design differs from the mouse design:\nkeys:  %s\nmouse: %s", g, w)
	}
}
