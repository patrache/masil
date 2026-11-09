package arch

import (
	"testing"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
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
