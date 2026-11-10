package arch

import (
	"strconv"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

type panelLine struct {
	s    string
	st   sty
	wrap bool // wrap instead of cutting (costs and notices: no_truncate)
}

// gib is MiB as GiB with at most one decimal ("4", "13.1").
func gib(mib int) string {
	tenths := max(mib, 0) * 10 / 1024
	if tenths%10 == 0 {
		return strconv.Itoa(tenths / 10)
	}
	return strconv.Itoa(tenths/10) + "." + strconv.Itoa(tenths%10)
}

// gbps is Mbps as Gbps with at most one decimal.
func gbps(mbps int) string {
	tenths := max(mbps, 0) / 100
	if tenths%10 == 0 {
		return strconv.Itoa(tenths / 10)
	}
	return strconv.Itoa(tenths/10) + "." + strconv.Itoa(tenths%10)
}

// panelLines are the settings and results of the selection (the traffic
// model when nothing is selected).
func (m *canvasModel) panelLines(ctx *ui.Context) []panelLine {
	t := ctx.T.Archcanvas
	rule := func(s string) panelLine { return panelLine{s: "─ " + s + " ─", st: sDim} }
	if n := m.node(m.sel); n != nil {
		title := m.label(n.ID)
		if bd := m.badges[n.ID]; bd.Text != "" {
			title += " " + bd.Text
		}
		ls := []panelLine{{s: title, st: sAccBold}, {s: kindName(ctx.T, n.Kind), st: sDim}}
		if n.Provider != "" {
			typ := cleanText(n.InstanceType)
			switch {
			case n.Provider == "custom":
				ls = append(ls, panelLine{s: t.PanelCustomType()})
			case typ == "" || typ == "custom":
				ls = append(ls, panelLine{s: providerName(ctx.T, n.Provider) + " " + t.PanelCustomType()})
			default:
				ls = append(ls, panelLine{s: providerName(ctx.T, n.Provider) + " " + typ})
			}
		}
		inst := t.PanelInstances(n.Instances)
		if n.Autoscale.Enabled {
			inst += " " + t.PanelAutoscale(n.Autoscale.Min, n.Autoscale.Max)
		}
		ls = append(ls,
			panelLine{s: t.PanelCpuMem(n.CPUCores, gib(n.MemoryMiB))},
			panelLine{s: inst},
			panelLine{s: t.PanelNic(gbps(n.NICMbps))},
		)
		return append(ls, m.resultLines(ctx, selection{obj: archdoc.ObjKey('n', n.ID), node: n})...)
	}
	if bx := m.box(m.sel); bx != nil {
		count := 0
		for _, n := range m.design.Nodes {
			if n.Parent == bx.ID {
				count++
			}
		}
		ls := []panelLine{{s: m.label(bx.ID), st: sAccBold}, {s: boxKindName(ctx.T, bx.Kind), st: sDim}}
		if rc := cleanText(bx.RegionCode); rc != "" {
			ls = append(ls, panelLine{s: rc})
		}
		return append(ls, panelLine{s: t.PanelBox(count)})
	}
	if e := m.edge(m.sel); e != nil {
		return []panelLine{
			{s: t.PanelEdge(m.label(e.From), m.label(e.To)), st: sAccBold},
			{s: archdraw.EdgeLabel(*e), st: sDim},
		}
	}
	ls := []panelLine{{s: t.PanelTraffic(), st: sAccBold}}
	rps, tr := m.traffic()
	if tr == nil {
		ls = append(ls, panelLine{s: t.PanelNoClient(), st: sDim}, panelLine{s: t.TrafficHint(), st: sDim, wrap: true})
	} else {
		ls = append(ls,
			panelLine{s: t.PanelRps(humanizeGrouped(rps))},
			panelLine{s: t.PanelRead(max(tr.ReadPermille, 0) / 10)},
			panelLine{s: t.PanelSizes(tr.ReqKB, tr.RespKB)},
			panelLine{s: t.PanelCurve(curveName(ctx, tr.Curve))},
		)
	}
	if r := m.result; r != nil && r.RPS > 0 {
		ls = append(ls, rule(t.PanelResult()),
			panelLine{s: t.PanelLatency(archdraw.FormatMs(r.P50Ms), archdraw.FormatMs(r.P99Ms))},
			panelLine{s: t.MetricErr(pct1(r.ErrorPermille))},
		)
	}
	if r := m.result; r != nil {
		ls = append(ls, panelLine{s: m.costShort(ctx, r.Cost.TotalUSDMonth), wrap: true})
	}
	return ls
}

func curveName(ctx *ui.Context, c archdoc.Curve) string {
	a := ctx.T.Archcanvas
	switch c {
	case archdoc.CurveRamp:
		return a.CurveRamp()
	case archdoc.CurveSpike:
		return a.CurveSpike()
	case archdoc.CurveWave:
		return a.CurveWave()
	case archdoc.CurveConstant:
		return a.CurveConstant()
	}
	return cleanText(string(c))
}

// drawPanel draws the settings panel in r (its left column is the divider).
func (m *canvasModel) drawPanel(ctx *ui.Context, b *buf, r layout.Rect) {
	if r.W <= 2 || r.H <= 0 {
		return
	}
	b.fill(r, sBase)
	for y := r.Y; y < r.Y+r.H; y++ {
		b.set(r.X, y, "│", 1, sLine)
	}
	inner := r.W - 3
	if m.mode == modeComments {
		m.drawComments(ctx, b, r, r.X+2, inner)
		return
	}
	if m.mode == modePanel {
		m.drawForm(ctx, b, r, r.X+2, inner)
		return
	}
	y := r.Y
	for _, l := range m.panelLines(ctx) {
		parts := []string{l.s}
		if l.wrap {
			parts = textwidth.Wrap(l.s, inner)
		}
		for _, p := range parts {
			if y >= r.Y+r.H {
				return
			}
			b.line(r.X+2, y, inner, p, l.st)
			y++
		}
	}
}
