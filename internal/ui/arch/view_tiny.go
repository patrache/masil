package arch

import (
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// drawSummary draws the tiny view (spec C2.6 초소형): title · p99 · errors /
// the cost (never cut) / the bottleneck / the keys.
func (m *canvasModel) drawSummary(ctx *ui.Context, b *buf, r layout.Rect) {
	if r.W <= 0 || r.H <= 0 || m.result == nil {
		return
	}
	t := ctx.T.Archcanvas
	res := m.result
	lines := []struct {
		s  string
		st sty
	}{
		{t.TinySummary(m.tinyTitle(ctx, r.W), archdraw.FormatMs(res.P99Ms), pct1(res.ErrorPermille)), sBase},
		{m.costShort(ctx, res.Cost.TotalUSDMonth), sBase},
	}
	if bn := res.Bottleneck; bn != nil && res.RPS > 0 {
		lines = append(lines, struct {
			s  string
			st sty
		}{t.TinyBottleneck(m.label(bn.Target), limitName(ctx.T, bn.Limit), max(bn.UtilPermille, 0)/10), sWarn})
	}
	lines = append(lines, struct {
		s  string
		st sty
	}{t.TinyRun() + " · " + t.TinyEditHint(), sDim})
	for i, l := range lines {
		if i >= r.H {
			break
		}
		b.line(r.X, r.Y+i, r.W, l.s, l.st)
	}
}

// tinyTitle cuts the title so the summary's "· p99 … · 에러 …" stays whole.
func (m *canvasModel) tinyTitle(ctx *ui.Context, w int) string {
	res := m.result
	rest := textwidth.Width(ctx.T.Archcanvas.TinySummary("", archdraw.FormatMs(res.P99Ms), pct1(res.ErrorPermille)))
	return textwidth.Truncate(m.title(ctx), max(w-rest, 2), "…")
}
