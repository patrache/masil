package arch

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// faultStep is where the fault menu is.
type faultStep int

const (
	stepKind   faultStep = iota // which fault on what
	stepAmount                  // how many instances / how many ms
	stepWhen                    // now, or at mm:ss of the run
)

// faultOption is one fault the selection can take.
type faultOption struct {
	kind   archdoc.FaultKind
	target string
}

// faultMenu is the x menu (P3 Task 19), drawn over the canvas: pick a fault
// for the selection (node off · n instances off · +ms · its AZ off · its
// region off), then "now" or "at mm:ss of the timed run". The result is one
// put of a fault object.
type faultMenu struct {
	opts   []faultOption
	cur    int
	step   faultStep
	amount string // typed count or ms
	when   int    // 0 = now, 1 = at
	at     string // typed mm:ss
	hint   string
}

// enclosing is the nearest box of kind k around node or box id ("" = none).
func (m *canvasModel) enclosing(id string, k archdoc.BoxKind) string {
	parent := ""
	if n := m.node(id); n != nil {
		parent = n.Parent
	} else if b := m.box(id); b != nil {
		if b.Kind == k {
			return b.ID
		}
		parent = b.Parent
	}
	for depth := 0; parent != "" && depth <= len(m.design.Boxes); depth++ {
		b := m.box(parent)
		if b == nil {
			break
		}
		if b.Kind == k {
			return b.ID
		}
		parent = b.Parent
	}
	return ""
}

// faultOptions are the faults selection id can take (archdoc 1.3 targets).
func (m *canvasModel) faultOptions(id string) []faultOption {
	var out []faultOption
	switch {
	case m.node(id) != nil:
		out = append(out, faultOption{archdoc.FaultNodeOff, id}, faultOption{archdoc.FaultInstancesOff, id}, faultOption{archdoc.FaultLatency, id})
	case m.edge(id) != nil:
		out = append(out, faultOption{archdoc.FaultLatency, id})
	}
	if az := m.enclosing(id, archdoc.BoxAZ); az != "" {
		out = append(out, faultOption{archdoc.FaultAZOff, az})
	}
	if r := m.enclosing(id, archdoc.BoxRegion); r != "" {
		out = append(out, faultOption{archdoc.FaultRegionOff, r})
	}
	return out
}

func (o faultOption) label(t *i18n.T, target string) string {
	a := t.Archcanvas
	switch o.kind {
	case archdoc.FaultNodeOff:
		return a.FaultNodeOff(target)
	case archdoc.FaultInstancesOff:
		return a.FaultInstancesOff(target)
	case archdoc.FaultLatency:
		return a.FaultLatency(target)
	case archdoc.FaultAZOff:
		return a.FaultAzOff(target)
	}
	return a.FaultRegionOff(target)
}

// targetLabel is a fault target's label (a connection's "from → to").
func (m *canvasModel) targetLabel(t *i18n.T, id string) string {
	if e := m.edge(id); e != nil {
		return t.Archcanvas.PanelEdge(m.label(e.From), m.label(e.To))
	}
	return m.label(id)
}

// openFaults opens the fault menu of selection id.
func (m *canvasModel) openFaults(ctx *ui.Context, id string) tea.Cmd {
	if ro, cmd := m.readOnly(ctx); ro {
		return cmd
	}
	opts := m.faultOptions(id)
	if len(opts) == 0 {
		return ui.Toast(ctx.T.Archcanvas.FaultNone(), ui.ToastInfo)
	}
	m.cancelGesture()
	m.menu = nil
	m.faults = &faultMenu{opts: opts, at: "00:00"}
	return nil
}

// parseMMSS reads "mm:ss" (or plain seconds) in 0..600.
func parseMMSS(s string) (int, bool) {
	s = strings.TrimSpace(s)
	mm, ss, colon := strings.Cut(s, ":")
	if !colon {
		mm, ss = "0", s
	}
	a, err1 := strconv.Atoi(mm)
	b, err2 := strconv.Atoi(ss)
	if err1 != nil || err2 != nil || a < 0 || b < 0 || (colon && b >= 60) {
		return 0, false
	}
	sec := a*60 + b
	return sec, sec <= 600
}

// mmss is seconds as "mm:ss".
func mmss(sec int) string {
	sec = max(sec, 0)
	two := func(n int) string { return strconv.Itoa(100 + n)[1:] }
	return two(min(sec/60, 99)) + ":" + two(sec%60)
}

// faultKey: the menu has every key until it closes.
func (m *canvasModel) faultKey(ctx *ui.Context, k tea.KeyPressMsg, s string) (bool, tea.Cmd) {
	f := m.faults
	typing := func(dst *string, ok func(r rune) bool) bool {
		switch s {
		case "backspace":
			if r := []rune(*dst); len(r) > 0 {
				*dst = string(r[:len(r)-1])
			}
			return true
		}
		if k.Text != "" && len(*dst) < 8 && strings.IndexFunc(k.Text, func(r rune) bool { return !ok(r) }) < 0 {
			*dst += k.Text
			return true
		}
		return false
	}
	digit := func(r rune) bool { return r >= '0' && r <= '9' }
	if s == "esc" {
		m.faults = nil
		return true, nil
	}
	switch f.step {
	case stepKind:
		switch s {
		case "j", "down":
			f.cur = min(f.cur+1, len(f.opts)-1)
		case "k", "up":
			f.cur = max(f.cur-1, 0)
		case "enter":
			switch f.opts[f.cur].kind {
			case archdoc.FaultInstancesOff:
				f.step, f.amount = stepAmount, "1"
			case archdoc.FaultLatency:
				f.step, f.amount = stepAmount, "100"
			default:
				f.step = stepWhen
			}
		}
	case stepAmount:
		if s == "enter" {
			n, err := strconv.Atoi(f.amount)
			hi := 1000
			if f.opts[f.cur].kind == archdoc.FaultLatency {
				hi = 600_000
			}
			if err != nil || n < 1 || n > hi {
				f.hint = ctx.T.Archcanvas.RangeHint("1", typed(fieldDef{}, int64(hi)))
				return true, nil
			}
			f.step, f.hint = stepWhen, ""
			return true, nil
		}
		typing(&f.amount, digit)
	case stepWhen:
		switch s {
		case "j", "down":
			f.when = 1
		case "k", "up":
			f.when = 0
		case "enter":
			at := 0
			if f.when == 1 {
				var ok bool
				if at, ok = parseMMSS(f.at); !ok {
					f.hint = ctx.T.Archcanvas.FaultAtHint()
					return true, nil
				}
			}
			return true, m.addFault(ctx, at)
		default:
			if f.when == 1 {
				typing(&f.at, func(r rune) bool { return digit(r) || r == ':' })
			}
		}
	}
	return !reservedGlobal(s), nil
}

// addFault puts the menu's fault at second at and closes the menu.
func (m *canvasModel) addFault(ctx *ui.Context, at int) tea.Cmd {
	f := m.faults
	m.faults = nil
	o := f.opts[f.cur]
	if len(m.design.Faults) >= archdoc.MaxFaults {
		return ui.Toast(ctx.T.Archcanvas.LimitReached(), ui.ToastInfo)
	}
	fault := archdoc.Fault{ID: archdoc.NewID('f'), AtS: at, Kind: o.kind, Target: o.target}
	n, _ := strconv.Atoi(f.amount)
	switch o.kind {
	case archdoc.FaultInstancesOff:
		fault.Count = n
	case archdoc.FaultLatency:
		fault.AddMs = n
	}
	return m.edit(ctx, func(d *archdoc.Design) { d.Faults = append(d.Faults, fault) })
}

// faultLines are the menu's lines (title first).
func (m *canvasModel) faultLines(t *i18n.T) []formLine {
	f := m.faults
	a := t.Archcanvas
	out := []formLine{{s: a.FaultMenuTitle(), st: sAccBold, row: -1}}
	opt := func(i int, s string, on bool) {
		st, mark := sBase, "  "
		if on {
			st, mark = sSel, "▸ "
		}
		out = append(out, formLine{s: mark + s, st: st, row: i})
	}
	switch f.step {
	case stepKind:
		for i, o := range f.opts {
			opt(i, o.label(t, m.targetLabel(t, o.target)), i == f.cur)
		}
	case stepAmount:
		o := f.opts[f.cur]
		out = append(out, formLine{s: o.label(t, m.targetLabel(t, o.target)), st: sDim, row: -1})
		head := a.FaultCount()
		if o.kind == archdoc.FaultLatency {
			head = a.FaultAddMs()
		}
		out = append(out, formLine{s: head + " " + f.amount, st: sAcc, row: 0, in: true})
	case stepWhen:
		o := f.opts[f.cur]
		out = append(out, formLine{s: o.label(t, m.targetLabel(t, o.target)), st: sDim, row: -1},
			formLine{s: a.FaultWhen(), st: sDim, row: -1})
		opt(0, a.FaultNow(), f.when == 0)
		opt(1, a.FaultAt(f.at), f.when == 1)
	}
	if f.hint != "" {
		out = append(out, formLine{s: f.hint, st: sWarn, row: -1})
	}
	return out
}

// faultRect is where the menu is drawn: centred on the canvas.
func (m *canvasModel) faultRect(t *i18n.T) layout.Rect {
	lines := m.faultLines(t)
	w := 24
	for _, l := range lines {
		w = max(w, textwidth.Width(l.s)+4)
	}
	c := m.regs.Canvas
	w, h := min(w, c.W), min(len(lines)+2, c.H)
	return layout.Rect{X: c.X + max((c.W-w)/2, 0), Y: c.Y + max((c.H-h)/2, 0), W: w, H: h}
}

// drawFaults draws the open fault menu.
func (m *canvasModel) drawFaults(ctx *ui.Context, b *buf) {
	if m.faults == nil {
		return
	}
	r := m.faultRect(ctx.T)
	if r.W < 4 || r.H < 3 {
		return
	}
	drawFrame(b, r)
	for i, l := range m.faultLines(ctx.T) {
		if 1+i >= r.H-1 {
			break
		}
		b.line(r.X+1, r.Y+1+i, r.W-2, " "+l.s, l.st)
	}
}

// drawFrame fills r and draws a single-line border around it.
func drawFrame(b *buf, r layout.Rect) {
	b.fill(r, sBase)
	for x := r.X; x < r.X+r.W; x++ {
		b.set(x, r.Y, "─", 1, sAcc)
		b.set(x, r.Y+r.H-1, "─", 1, sAcc)
	}
	for y := r.Y; y < r.Y+r.H; y++ {
		b.set(r.X, y, "│", 1, sAcc)
		b.set(r.X+r.W-1, y, "│", 1, sAcc)
	}
	b.set(r.X, r.Y, "┌", 1, sAcc)
	b.set(r.X+r.W-1, r.Y, "┐", 1, sAcc)
	b.set(r.X, r.Y+r.H-1, "└", 1, sAcc)
	b.set(r.X+r.W-1, r.Y+r.H-1, "┘", 1, sAcc)
}
