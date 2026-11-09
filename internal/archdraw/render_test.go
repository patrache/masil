package archdraw

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/textwidth"
)

func loadDesign(t testing.TB, name string) *archdoc.Design {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "designs", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := archdoc.Decode(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return d
}

// fixtureBadges are fixed analysis marks for the golden fixture.
func fixtureBadges() map[string]NodeBadge {
	return map[string]NodeBadge{
		"api-a":   {Text: "×3", BarPermille: 810, P99Ms: 140},
		"api-b":   {Text: "×3", BarPermille: 640, P99Ms: 95},
		"primary": {BarPermille: 980, P99Ms: 2500, Danger: true},
		"cache":   {Text: "×2", BarPermille: 120, P99Ms: 2},
		"lb":      {Text: "×2", BarPermille: 300, P99Ms: 1, Warn: true},
		"replica": {BarPermille: 350, P99Ms: 12},
	}
}

func checkGrid(t *testing.T, g *Grid) {
	t.Helper()
	lines := g.Plain()
	if len(lines) != g.H {
		t.Fatalf("Plain() has %d lines, want %d", len(lines), g.H)
	}
	for i, l := range lines {
		if w := textwidth.Width(l); w != g.W {
			t.Errorf("line %d width %d, want %d: %q", i, w, g.W, l)
		}
		for _, r := range l {
			if r < 0x20 || r == 0x7f || r == 0x1b || r == 0x202e {
				t.Errorf("line %d has control %U", i, r)
			}
		}
	}
}

func TestRenderGoldens(t *testing.T) {
	sc := Build(loadDesign(t, "three_tier"))
	for _, tc := range []struct {
		name string
		z    Zoom
	}{{"detail", ZoomDetail}, {"normal", ZoomNormal}, {"dot", ZoomDot}} {
		t.Run(tc.name, func(t *testing.T) {
			g := sc.Render(Viewport{X: 0, Y: 0, W: 100, H: 30, Zoom: tc.z}, Overlay{
				Badges:   fixtureBadges(),
				Selected: map[string]bool{"api-a": true},
				Comments: map[string]int{"api-b": 2},
				Buttons:  true,
			})
			checkGrid(t, g)
			golden.RequireEqual(t, []byte(strings.Join(g.Plain(), "\n")+"\n"))
		})
	}
}

func oneNode(label string) *archdoc.Design {
	d := archdoc.New("t")
	n := archdoc.Defaults(archdoc.KindAPI)
	n.ID, n.Label, n.Pos = "n1", label, [2]int{2, 1}
	d.Nodes = []archdoc.Node{n}
	return d
}

func TestLongKoreanLabelTruncated(t *testing.T) {
	sc := Build(oneNode("결제-API-서버-한국"))
	for _, z := range Zooms() {
		g := sc.Render(Viewport{W: 30, H: 8, Zoom: z}, Overlay{Badges: map[string]NodeBadge{"n1": {Text: "×4", BarPermille: 500}}})
		checkGrid(t, g)
		if !strings.Contains(strings.Join(g.Plain(), "\n"), "…") {
			t.Errorf("zoom %d: long label not cut with …:\n%s", z, strings.Join(g.Plain(), "\n"))
		}
	}
}

func TestHostileLabelSanitized(t *testing.T) {
	d := oneNode("\x1b[31mx‮y")
	d.Boxes = []archdoc.Box{{ID: "b1", Kind: archdoc.BoxAZ, Label: "\x07a‮b\x1b]0;t\x07", Rect: [4]int{0, 0, 28, 8}}}
	d.Nodes[0].Parent = "b1"
	sc := Build(d)
	for _, z := range Zooms() {
		g := sc.Render(Viewport{W: 30, H: 9, Zoom: z}, Overlay{Remote: []RemoteMark{{User: "\x1b[2Jhana‮", Selected: []string{"n1"}}}})
		checkGrid(t, g)
		s := strings.Join(g.Plain(), "")
		if strings.ContainsAny(s, "\x1b‮\x07") {
			t.Errorf("zoom %d: unsafe text survived: %q", z, s)
		}
	}
}

func TestViewportCulling(t *testing.T) {
	d := archdoc.New("big")
	for i := range 300 {
		n := archdoc.Defaults(archdoc.KindAPI)
		n.ID = fmt.Sprintf("n%03d", i)
		n.Label = fmt.Sprintf("L%03d", i)
		n.Pos = [2]int{(i % 20) * 16, (i / 20) * 6}
		d.Nodes = append(d.Nodes, n)
	}
	sc := Build(d)
	g := sc.Render(Viewport{W: 40, H: 10, Zoom: ZoomDetail}, Overlay{})
	checkGrid(t, g)
	s := strings.Join(g.Plain(), "\n")
	for i := range 300 {
		n := d.Nodes[i]
		label := n.Label
		visible := n.Pos[0] < 40 && n.Pos[1] < 10
		if got := strings.Contains(s, label); got != visible {
			t.Errorf("%s at %v: drawn = %v, want %v", label, n.Pos, got, visible)
		}
	}
}

func TestOverlapsAtDot(t *testing.T) {
	mk := func(gap int) *Scene {
		d := archdoc.New("o")
		for i, id := range []string{"a", "b"} {
			n := archdoc.Defaults(archdoc.KindAPI)
			n.ID, n.Pos = id, [2]int{i * gap, 0}
			d.Nodes = append(d.Nodes, n)
		}
		return Build(d)
	}
	far := mk(14)
	if got := far.Overlaps(ZoomDetail); len(got) != 0 {
		t.Errorf("14 apart at Detail: %v, want none", got)
	}
	if got := far.Overlaps(ZoomDot); strings.Join(got, ",") != "a,b" {
		t.Errorf("14 apart at Dot (2/3 scale, 10 wide): %v, want a,b", got)
	}
	if got := mk(16).Overlaps(ZoomDot); len(got) != 0 {
		t.Errorf("16 apart (the arrange pitch) at Dot: %v, want none", got)
	}
	near := mk(2)
	for _, z := range Zooms() {
		if got := near.Overlaps(z); strings.Join(got, ",") != "a,b" {
			t.Errorf("2 apart at zoom %d: %v, want a,b", z, got)
		}
	}
}

func TestNearest(t *testing.T) {
	d := archdoc.New("grid")
	ids := [3][3]string{{"a", "b", "c"}, {"d", "e", "f"}, {"g", "h", "i"}}
	for r := range 3 {
		for c := range 3 {
			n := archdoc.Defaults(archdoc.KindAPI)
			n.ID, n.Pos = ids[r][c], [2]int{c * 16, r * 6}
			d.Nodes = append(d.Nodes, n)
		}
	}
	// two equally near nodes to the right of "x": ties go to the smaller ID
	for _, tie := range []struct {
		id  string
		pos [2]int
	}{{"x", [2]int{0, 30}}, {"z2", [2]int{16, 24}}, {"z1", [2]int{16, 36}}} {
		n := archdoc.Defaults(archdoc.KindAPI)
		n.ID, n.Pos = tie.id, tie.pos
		d.Nodes = append(d.Nodes, n)
	}
	sc := Build(d)
	for _, tc := range []struct {
		from string
		dir  Dir
		want string
	}{
		{"e", DirRight, "f"}, {"e", DirLeft, "d"}, {"e", DirUp, "b"}, {"e", DirDown, "h"},
		{"a", DirLeft, ""}, {"c", DirRight, ""}, {"x", DirRight, "z1"}, {"nope", DirRight, ""},
	} {
		if got := sc.Nearest(tc.from, tc.dir); got != tc.want {
			t.Errorf("Nearest(%s, %d) = %q, want %q", tc.from, tc.dir, got, tc.want)
		}
	}
}

func TestViewportRoundTrip(t *testing.T) {
	for _, z := range Zooms() {
		v := Viewport{X: -37, Y: 11, W: 80, H: 24, Zoom: z}
		for sx := -5; sx < 90; sx++ {
			for sy := -3; sy < 30; sy++ {
				wx, wy := v.ToWorld(sx, sy)
				if gx, gy := v.ToScreen(wx, wy); gx != sx || gy != sy {
					t.Fatalf("zoom %d: ToScreen(ToWorld(%d,%d)) = %d,%d", z, sx, sy, gx, gy)
				}
			}
		}
	}
}

func TestBrokenDesignNoPanic(t *testing.T) {
	d := archdoc.New("broken")
	d.Boxes = []archdoc.Box{
		{ID: "x", Kind: archdoc.BoxVPC, Parent: "y", Rect: [4]int{0, 0, 40, 20}},
		{ID: "y", Kind: archdoc.BoxVPC, Parent: "x", Rect: [4]int{-4, -2, 48, 24}},
		{ID: "z", Kind: archdoc.BoxAZ, Parent: "gone", Rect: [4]int{2, 2, 1, -5}},
	}
	n := archdoc.Defaults(archdoc.KindAPI)
	n.ID, n.Parent = "n", "gone"
	d.Nodes = []archdoc.Node{n, n}
	d.Edges = []archdoc.Edge{{ID: "e1", From: "n", To: "missing"}, {ID: "e2", From: "n", To: "n"}}
	sc := Build(d)
	for _, z := range Zooms() {
		checkGrid(t, sc.Render(Viewport{X: -10, Y: -5, W: 60, H: 30, Zoom: z}, Overlay{Ghost: &Ghost{ID: "x", X: 3, Y: 3}}))
	}
	checkGrid(t, Build(nil).Render(Viewport{W: 5, H: 2}, Overlay{Buttons: true}))
	checkGrid(t, sc.Render(Viewport{W: 0, H: 0}, Overlay{}))
}
