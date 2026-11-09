package archexport

import (
	"slices"
	"strconv"
	"strings"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/textwidth"
)

// SnapshotText is the catalog text a snapshot needs (the caller reads it from
// its catalog; archexport has none).
type SnapshotText struct {
	Columns  [6]string                     // table heads: name, kind, instances, utilisation, p99, cost
	Kind     func(archdoc.NodeKind) string // a kind's display name (nil = the kind id)
	CostNote string                        // after the total cost, e.g. "/월 (추정 · 2026-11 가격표)"
}

// Snapshot limits.
const (
	snapshotMaxRows = 60 // picture rows
	miniW, miniH    = 60, 8
)

// Badges turns an analysis into the marks drawn on nodes: "×n" for two or more
// instances, the utilisation bar and p99, ✕ for an overloaded or down node and
// ⚠ for one at 80% or more, the bottleneck or a design-check target.
func Badges(d *archdoc.Design, r *archsim.Result) map[string]archdraw.NodeBadge {
	if d == nil || r == nil {
		return nil
	}
	warn := map[string]bool{}
	for _, f := range r.Findings {
		for _, id := range f.Targets {
			warn[id] = true
		}
	}
	if r.Bottleneck != nil {
		warn[r.Bottleneck.Target] = true
	}
	out := make(map[string]archdraw.NodeBadge, len(d.Nodes))
	for _, n := range d.Nodes {
		nr, ok := r.Nodes[n.ID]
		if !ok {
			continue
		}
		count := n.Instances
		if nr.AvgInstancesMilli > 0 {
			count = (nr.AvgInstancesMilli + 500) / 1000
		}
		b := archdraw.NodeBadge{
			BarPermille: nr.UtilPermille,
			P99Ms:       nr.P99Ms,
			Danger:      nr.Overloaded || nr.Down,
			Warn:        nr.UtilPermille >= 800 || warn[n.ID],
		}
		if count >= 2 {
			b.Text = "×" + strconv.Itoa(count)
		}
		out[n.ID] = b
	}
	return out
}

// grouped writes n with thousands commas ("1,840").
func grouped(n int) string {
	s := strconv.Itoa(max(n, -n))
	var b strings.Builder
	if n < 0 {
		b.WriteByte('-')
	}
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// fitZoom picks the most detailed zoom whose whole picture is at most w×h cells
// with no overlapping nodes; ok is false when none fits (ZoomDot is returned).
func fitZoom(sc *archdraw.Scene, zooms []archdraw.Zoom, w, h int) (z archdraw.Zoom, sw, sh int, ok bool) {
	bx, by, bw, bh := sc.Bounds()
	for _, z := range zooms {
		v := archdraw.Viewport{X: bx, Y: by, Zoom: z}
		sw, sh := v.ToScreen(bx+bw, by+bh)
		if sw <= w && sh <= h && len(sc.Overlaps(z)) == 0 {
			return z, sw, sh, true
		}
	}
	v := archdraw.Viewport{X: bx, Y: by, Zoom: archdraw.ZoomDot}
	sw, sh = v.ToScreen(bx+bw, by+bh)
	return archdraw.ZoomDot, sw, sh, false
}

func trimLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, " ")
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// picture draws the whole design (cut to w×h, with a last "…" line when cut).
func picture(d *archdoc.Design, r *archsim.Result, zooms []archdraw.Zoom, w, h int) []string {
	sc := archdraw.Build(d)
	if len(sc.NodeIDs()) == 0 {
		return nil
	}
	z, sw, sh, _ := fitZoom(sc, zooms, w, h)
	bx, by, _, _ := sc.Bounds()
	cut := sw > w || sh > h
	vh := min(sh, h)
	if cut {
		vh = min(sh, h-1)
	}
	g := sc.Render(archdraw.Viewport{X: bx, Y: by, W: min(sw, w), H: vh, Zoom: z}, archdraw.Overlay{Badges: Badges(d, r)})
	lines := trimLines(g.Plain())
	if cut {
		lines = append(lines, "…")
	}
	return lines
}

// Mini is the chat card's picture: at most 8 lines of at most 60 cells, at the
// normal zoom when the design fits there, else the dot zoom, cut with a last
// "…" line when even that does not fit. Labels are sanitized; no control
// characters.
func Mini(d *archdoc.Design, r *archsim.Result) []string {
	if d == nil {
		return nil
	}
	return picture(d, r, []archdraw.Zoom{archdraw.ZoomNormal, archdraw.ZoomDot}, miniW, miniH)
}

// Snapshot is the text export (spec C2.11): the title, the picture at the most
// detailed zoom that fits width (80 or 120; other widths are clamped to
// 40..200), then a table of every node and the totals. Every line is at most
// width cells; the only control byte is '\n'. r may be nil (no numbers).
func Snapshot(d *archdoc.Design, r *archsim.Result, width int, txt SnapshotText) []byte {
	width = min(max(width, 40), 200)
	var lines []string
	if d == nil {
		return nil
	}
	if t := cleanText(d.Title); t != "" {
		lines = append(lines, textwidth.Truncate(t, width, "…"), "")
	}
	pic := picture(d, r, archdraw.Zooms(), width, snapshotMaxRows)
	if len(pic) > 0 {
		lines = append(lines, pic...)
		lines = append(lines, "")
	}
	lines = append(lines, table(d, r, width, txt)...)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(strings.TrimRight(textwidth.Truncate(l, width, "…"), " "))
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// table is the node table and the totals line.
func table(d *archdoc.Design, r *archsim.Result, width int, txt SnapshotText) []string {
	nodes := slices.Clone(d.Nodes)
	slices.SortFunc(nodes, func(a, b archdoc.Node) int { return strings.Compare(a.ID, b.ID) })
	kind := func(k archdoc.NodeKind) string {
		if txt.Kind != nil {
			return cleanText(txt.Kind(k))
		}
		return cleanText(string(k))
	}
	rows := [][6]string{}
	var head [6]string
	for i, c := range txt.Columns {
		head[i] = cleanText(c)
	}
	rows = append(rows, head)
	for i := range nodes {
		n := &nodes[i]
		row := [6]string{nodeLabel(n), kind(n.Kind), strconv.Itoa(n.Instances), "-", "-", "-"}
		if r != nil {
			if nr, ok := r.Nodes[n.ID]; ok {
				row[3] = strconv.Itoa(max(nr.UtilPermille, 0)/10) + "%"
				row[4] = archdraw.FormatMs(nr.P99Ms)
				row[5] = "$" + grouped(nr.CostUSDMonth)
				if nr.Overloaded || nr.Down {
					row[3] += " ✕"
				}
			}
		}
		rows = append(rows, row)
	}
	var cw [6]int
	for _, row := range rows {
		for i, c := range row {
			cw[i] = max(cw[i], textwidth.Width(c))
		}
	}
	const gap = 2
	other := 0
	for _, w := range cw[1:] {
		other += w + gap
	}
	cw[0] = max(min(cw[0], width-other), 4)
	var out []string
	for _, row := range rows {
		var b strings.Builder
		for i, c := range row {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", gap))
			}
			if i >= 2 { // numbers right-aligned
				b.WriteString(textwidth.PadLeft(c, cw[i]))
				continue
			}
			b.WriteString(textwidth.PadRight(textwidth.Truncate(c, cw[i], "…"), cw[i]))
		}
		out = append(out, b.String())
	}
	if r != nil {
		out = append(out, "", "p99 "+archdraw.FormatMs(r.P99Ms)+" · ~$"+grouped(r.Cost.TotalUSDMonth)+cleanText(txt.CostNote))
	}
	return out
}
