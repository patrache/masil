package archdraw

import (
	"testing"

	"github.com/patrache/masil/internal/archdoc"
)

func TestHitTest(t *testing.T) {
	d := archdoc.New("hit")
	d.Boxes = []archdoc.Box{
		{ID: "az", Kind: archdoc.BoxAZ, Label: "AZ", Rect: [4]int{0, 0, 40, 14}},
		{ID: "vpc", Kind: archdoc.BoxVPC, Label: "VPC", Rect: [4]int{60, 0, 30, 14}},
	}
	n := archdoc.Defaults(archdoc.KindAPI)
	n.ID, n.Pos, n.Parent = "api", [2]int{4, 4}, "az"
	d.Nodes = []archdoc.Node{n}
	sc := Build(d)
	v := Viewport{W: 100, H: 30, Zoom: ZoomDetail}
	for _, tc := range []struct {
		name   string
		x, y   int
		want   HitKind
		wantID string
	}{
		{"node centre", 4 + 6, 4 + 1, HitNode, "api"},
		{"node top border", 4, 4, HitNode, "api"},
		{"handle", 4 + archdoc.NodeW - 1, 4 + archdoc.NodeH/2, HitHandle, "api"},
		{"box border left", 0, 7, HitBoxBorder, "az"},
		{"box label row", 3, 0, HitBoxBorder, "az"},
		{"inside box", 30, 8, HitBox, "az"},
		{"other box bottom", 70, 13, HitBoxBorder, "vpc"},
		{"empty", 50, 20, HitNone, ""},
		{"zoom in", 96, 0, HitZoomIn, ""},
		{"zoom in right half", 97, 0, HitZoomIn, ""},
		{"zoom out", 98, 0, HitZoomOut, ""},
		{"zoom out right half", 99, 0, HitZoomOut, ""},
		{"outside", 100, 0, HitNone, ""},
	} {
		got := sc.HitTest(v, tc.x, tc.y)
		if got.Kind != tc.want || got.ID != tc.wantID {
			t.Errorf("%s (%d,%d): %+v, want {%d %q}", tc.name, tc.x, tc.y, got, tc.want, tc.wantID)
		}
	}
	// scrolled and zoomed: the handle moves with the drawing
	v2 := Viewport{X: -8, Y: -3, W: 100, H: 30, Zoom: ZoomNormal}
	hx, hy := v2.ToScreen(4, 4)
	w, h := NodeSize(ZoomNormal)
	if got := sc.HitTest(v2, hx+w-1, hy+h/2); got.Kind != HitHandle || got.ID != "api" {
		t.Errorf("normal zoom handle: %+v", got)
	}
	g := sc.Render(v2, Overlay{})
	if c := g.At(hx+w-1, hy+h/2); c.Ch != "◆" {
		t.Errorf("handle cell drawn as %q", c.Ch)
	}
	if got := sc.BoxAtRect(10, 6); got != "az" {
		t.Errorf("BoxAtRect inside az = %q", got)
	}
	if got := sc.BoxAtRect(50, 6); got != "" {
		t.Errorf("BoxAtRect between boxes = %q", got)
	}
}
