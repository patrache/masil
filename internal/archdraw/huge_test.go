package archdraw

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/patrache/masil/internal/archdoc"
)

// hugeDesign is an unvalidated design (archdraw takes those, see the package
// doc): boxes of size big and nodes and edges at coordinates of size big.
func hugeDesign(big int) *archdoc.Design {
	d := archdoc.New("huge")
	d.Boxes = []archdoc.Box{
		{ID: "b", Kind: archdoc.BoxRegion, Label: "r", Rect: [4]int{-big / 2, -big / 2, big, big}},
		{ID: "c", Kind: archdoc.BoxVPC, Parent: "b", Label: "v", Rect: [4]int{-big / 4, -big / 4, big, big}},
	}
	a := archdoc.Defaults(archdoc.KindAPI)
	a.ID, a.Pos = "a", [2]int{-big, 2}
	z := archdoc.Defaults(archdoc.KindAPI)
	z.ID, z.Pos = "z", [2]int{big, big}
	m := archdoc.Defaults(archdoc.KindAPI)
	m.ID, m.Pos = "m", [2]int{0, 0}
	d.Nodes = []archdoc.Node{a, z, m}
	d.Edges = []archdoc.Edge{
		{ID: "e1", From: "a", To: "z", Protocol: archdoc.ProtoHTTP1},
		{ID: "e2", From: "m", To: "z", Protocol: archdoc.ProtoHTTP1},
		{ID: "e3", From: "a", To: "m", Protocol: archdoc.ProtoHTTP1},
	}
	return d
}

// A design with huge boxes or far coordinates (and peers' drags anywhere) costs
// the screen size, never the world size, and never panics: every loop is
// clipped to the grid and the coordinate arithmetic cannot overflow
// (v0.1.3 security t).
func TestHugeDesignRendersFast(t *testing.T) {
	for _, big := range []int{1 << 30, 1 << 61, math.MaxInt} {
		t.Run(fmt.Sprint(big), func(t *testing.T) {
			sc := Build(hugeDesign(big))
			for _, z := range Zooms() {
				v := Viewport{X: -60, Y: -20, W: 120, H: 40, Zoom: z}
				ov := Overlay{
					Ghost: &Ghost{ID: "b", X: -big, Y: -big},
					Remote: []RemoteMark{
						{User: "p", Ghost: &Ghost{ID: "m", X: big, Y: math.MinInt}},
						{User: "q", Ghost: &Ghost{ID: "c", X: 5, Y: 5}},
					},
					ConnectFrom: "m",
					ConnectTo:   [2]int{-big, big},
				}
				t0 := time.Now()
				g := sc.Render(v, ov)
				sc.HitTest(v, 60, 20)
				sc.Bounds()
				if d := time.Since(t0); d > 50*time.Millisecond {
					t.Errorf("zoom %d: frame took %v", z, d)
				}
				checkGrid(t, g)
			}
		})
	}
}
