package arch

import (
	"strconv"
	"strings"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/archsim/pricing"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// asOf is the price table date the numbers use.
func (m *canvasModel) asOf() string {
	if m.result != nil && m.result.Cost.AsOf != "" {
		return cleanText(m.result.Cost.AsOf)
	}
	return pricing.AsOf()
}

// costShort is "~$1,840/월 (추정 · 2026-11 가격표)" — never cut (no_truncate).
func (m *canvasModel) costShort(ctx *ui.Context, usd int) string {
	return ctx.T.Archcanvas.CostShort(humanize.Grouped(int64(usd)), m.asOf())
}

// pct1 is a permille as a percent with one decimal ("0.4").
func pct1(permille int) string {
	p := max(permille, 0)
	return strconv.Itoa(p/10) + "." + strconv.Itoa(p%10)
}

func countShort(ctx *ui.Context, n int) string { return humanize.For(ctx.Lang).Count(n) }

// limitName is a bottleneck resource's display name.
func limitName(t *i18n.T, l archsim.LimitKind) string {
	a := t.Archcanvas
	switch l {
	case archsim.LimitCPU:
		return a.LimitCpu()
	case archsim.LimitIOPS:
		return a.LimitIops()
	case archsim.LimitNIC:
		return a.LimitNic()
	case archsim.LimitPool:
		return a.LimitPool()
	case archsim.LimitExt:
		return a.LimitExtLimit()
	case archsim.LimitQueue:
		return a.LimitQueue()
	case archsim.LimitLink:
		return a.LimitLink()
	}
	return cleanText(string(l))
}

// label is a node, box or edge id's display label.
func (m *canvasModel) label(id string) string {
	if l, ok := m.labels[id]; ok {
		return l
	}
	return cleanText(id)
}

// checkText is one design-check finding.
func (m *canvasModel) checkText(t *i18n.T, f archdoc.Finding) string {
	var names []string
	for _, id := range f.Targets {
		names = append(names, m.label(id))
	}
	targets := strings.Join(names, ", ")
	a := t.Archcanvas
	switch f.Code {
	case archdoc.CheckSPOFSingleInstance:
		return a.CheckSpofSingleInstance(targets)
	case archdoc.CheckSPOFSingleLB:
		return a.CheckSpofSingleLb(targets)
	case archdoc.CheckPrimaryReplicaSameAZ:
		return a.CheckPrimaryReplicaSameAz(targets)
	case archdoc.CheckSingleAZService:
		return a.CheckSingleAzService(targets)
	case archdoc.CheckUnconnected:
		return a.CheckUnconnected(targets)
	case archdoc.CheckCycle:
		return a.CheckCycle(targets)
	case archdoc.CheckAZOutsideVPC:
		return a.CheckAzOutsideVpc(targets)
	case archdoc.CheckVPCOutsideRegion:
		return a.CheckVpcOutsideRegion(targets)
	}
	return targets
}

// checksLine is "점검  ⚠ … · ⚠ …" (or "점검  문제 없음").
func (m *canvasModel) checksLine(t *i18n.T) string {
	if m.result == nil || len(m.result.Findings) == 0 {
		return t.Archcanvas.ChecksLabel() + "  " + t.Archcanvas.CheckOk()
	}
	var parts []string
	for _, f := range m.result.Findings {
		parts = append(parts, "⚠ "+m.checkText(t, f))
	}
	return t.Archcanvas.ChecksLabel() + "  " + strings.Join(parts, " · ")
}

// bottleneckText is "병목 ⚠ <label> <limit> <util>%" ("" = none).
func (m *canvasModel) bottleneckText(t *i18n.T) string {
	r := m.result
	if r == nil || r.Bottleneck == nil || r.RPS == 0 {
		return ""
	}
	bn := r.Bottleneck
	return t.Archcanvas.MetricBottleneck(m.label(bn.Target), limitName(t, bn.Limit), max(bn.UtilPermille, 0)/10,
		humanize.Grouped(int64(max(bn.Demand, 0))), unitText(bn.Limit, bn.PerUnitMilli))
}

// milliDec is a thousandth as a decimal with no trailing zeros ("1250" → "1.25").
func milliDec(milli int) string {
	milli = max(milli, 0)
	s := humanize.Grouped(int64(milli / 1000))
	if f := milli % 1000; f != 0 {
		s += "." + strings.TrimRight(strconv.Itoa(1000 + f)[1:], "0")
	}
	return s
}

// unitText is what one request uses of the limited resource ("8ms",
// "1.25 IOPS", "18KB", "1"): archsim.Bottleneck.PerUnitMilli in its unit.
func unitText(l archsim.LimitKind, perUnitMilli int) string {
	switch l {
	case archsim.LimitCPU, archsim.LimitPool:
		return milliDec(perUnitMilli) + "ms" // CPU µs = milli-ms
	case archsim.LimitIOPS:
		return milliDec(perUnitMilli) + " IOPS"
	case archsim.LimitNIC, archsim.LimitLink:
		return milliDec(perUnitMilli) + "KB"
	}
	return milliDec(perUnitMilli)
}

// bottleneckStyle: danger from 100%, else warning.
func (m *canvasModel) bottleneckStyle() sty {
	if r := m.result; r != nil && r.Bottleneck != nil && r.Bottleneck.UtilPermille >= 1000 {
		return sErr
	}
	return sWarn
}

// topCosts is "비용 상위 결제 API 41% · 주 DB 28%" — each node's share of
// the total ("" = no costs). The share keeps "$" to the one estimated total.
func (m *canvasModel) topCosts(t *i18n.T) string {
	r := m.result
	if r == nil || r.Cost.TotalUSDMonth <= 0 || len(r.Cost.Top3) == 0 {
		return ""
	}
	var parts []string
	for _, id := range r.Cost.Top3 {
		share := r.Cost.ByNode[id] * 100 / r.Cost.TotalUSDMonth
		parts = append(parts, m.label(id)+" "+strconv.Itoa(share)+"%")
	}
	return t.Archcanvas.TopCosts(strings.Join(parts, " · "))
}

// traffic is the summed client traffic and the first client's model.
func (m *canvasModel) traffic() (rps int, first *archdoc.Traffic) {
	for _, n := range m.design.Nodes {
		if n.Kind != archdoc.KindClient || n.Traffic == nil {
			continue
		}
		rps += max(n.Traffic.RPS, 0)
		if first == nil {
			t := *n.Traffic
			first = &t
		}
	}
	return rps, first
}

// drawHeader draws the top line: title · save state · engine, traffic on the right.
func (m *canvasModel) drawHeader(ctx *ui.Context, b *buf, r layout.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	t := ctx.T.Archcanvas
	b.fill(r, sBase)
	var state string
	switch m.sink.Status() {
	case saveSaved:
		state = t.SaveSaved()
	case savePending:
		state = t.SavePending()
	case saveUnsaved:
		state = t.SaveUnsaved()
	case saveFailed:
		state = t.SaveFailed()
	case saveCopy:
		state = t.ReadOnlyCopy()
	}
	right := ""
	if rps, tr := m.traffic(); tr != nil {
		right = t.TrafficSummary(countShort(ctx, rps), max(tr.ReadPermille, 0)/10, max(tr.ReqKB, 0))
	}
	title := m.title(ctx)
	rest := " · " + state + " · ▶ " + m.engineText(ctx.T)
	rw := textwidth.Width(right)
	if rw+textwidth.Width(rest)+12 > r.W {
		right, rw = "", 0
	}
	tw := max(r.W-1-textwidth.Width(rest)-rw-1, 4)
	x := r.X + 1
	x += b.text(x, r.Y, textwidth.Truncate(title, tw, "…"), tw, sAccBold)
	x += b.text(x, r.Y, rest, max(r.X+r.W-x-rw, 0), sDim)
	if right != "" {
		b.text(r.X+r.W-rw-1, r.Y, right, rw, sDim)
	}
}

// drawMetrics draws the totals, the cost, the bottleneck and the checks.
func (m *canvasModel) drawMetrics(ctx *ui.Context, b *buf, r layout.Rect) {
	if r.W <= 0 || r.H <= 0 || m.result == nil {
		return
	}
	t := ctx.T.Archcanvas
	b.fill(r, sBase)
	res := m.result
	cost := m.costShort(ctx, res.Cost.TotalUSDMonth)
	bn := m.bottleneckText(ctx.T)
	rps, p50, p99, errP := m.totals()
	last := m.checksLine(ctx.T)
	if ev := m.eventLine(ctx.T); ev != "" {
		last = ev // a playing run: its latest event instead of the checks
	}
	if r.H >= 3 {
		var row0 string
		if rps == 0 && m.frame() == nil {
			row0 = t.MetricsTotal() + "  " + t.MetricNoTraffic()
		} else {
			row0 = t.MetricsTotal() + "  p50 " + archdraw.FormatMs(p50) + "  p99 " + archdraw.FormatMs(p99) +
				"  " + t.MetricErr(pct1(errP)) + "  " + t.MetricThroughput(countShort(ctx, rps))
			if res.Overloaded && m.frame() == nil {
				row0 += "  ✕ " + t.MetricOverloaded()
			}
		}
		if tc := m.topCosts(ctx.T); tc != "" {
			row0 += "  ·  " + tc
		}
		b.line(r.X+1, r.Y, r.W-2, row0, sBase)
		x := r.X + 1 + b.text(r.X+1, r.Y+1, cost, r.W-2, sBase)
		if bn != "" {
			b.text(x, r.Y+1, textwidth.Truncate("  ·  "+bn, max(r.X+r.W-1-x, 0), "…"), max(r.X+r.W-1-x, 0), m.bottleneckStyle())
		}
		b.line(r.X+1, r.Y+2, r.W-2, last, sDim)
		return
	}
	// one line: the cost first so it is never cut, then what fits
	x := r.X + 1 + b.text(r.X+1, r.Y, cost, r.W-2, sBase)
	tail := ""
	if rps > 0 || m.frame() != nil {
		tail = " · p99 " + archdraw.FormatMs(p99) + " · " + t.MetricErr(pct1(errP))
		if ev := m.eventLine(ctx.T); ev != "" {
			tail += " · " + ev
		} else if res.Bottleneck != nil {
			tail += " · ⚠ " + m.label(res.Bottleneck.Target)
		}
	}
	b.text(x, r.Y, textwidth.Truncate(tail, max(r.X+r.W-1-x, 0), "…"), max(r.X+r.W-1-x, 0), sDim)
	if r.H >= 2 {
		b.line(r.X+1, r.Y+1, r.W-2, last, sDim)
	}
}

func humanizeGrouped(n int) string { return humanize.Grouped(int64(n)) }

func strconvItoa(n int) string { return strconv.Itoa(n) }
