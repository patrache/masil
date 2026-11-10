package arch

import (
	"encoding/json"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archdraw"
	"github.com/patrache/masil/internal/archsim/pricing"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// panelState is the settings panel's editing state (P3 Task 18): the row
// cursor, the number being typed and the open choice list. target is the
// selection the cursor belongs to (a new selection starts at the top).
type panelState struct {
	target  string
	cur     int
	editing bool
	ti      textinput.Model // the value being typed (editing)
	hint    string
	pick    *picker
	// where the typed number is drawn (the real cursor), set by the last frame
	curX, curY int
}

// panelInputMax is the widest value the panel's input takes (cells).
const panelInputMax = 60

// newPanelInput is a bubbles textinput for the panel holding v, the cursor
// at its end: no prompt, the real cursor (virtual cursor off: Focus starts no
// blink), ctrl+k (the palette) out of DeleteAfterCursor, clipboard paste and
// suggestions unbound. Feed it through typeKey only.
func newPanelInput(v string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.SetVirtualCursor(false)
	km := textinput.DefaultKeyMap()
	keys := slices.DeleteFunc(slices.Clone(km.DeleteAfterCursor.Keys()), func(k string) bool { return k == "ctrl+k" })
	km.DeleteAfterCursor = key.NewBinding(key.WithKeys(keys...))
	none := key.NewBinding(key.WithDisabled())
	km.Paste = none
	km.AcceptSuggestion, km.NextSuggestion, km.PrevSuggestion = none, none, none
	ti.KeyMap = km
	ti.Focus()
	ti.SetValue(v)
	ti.CursorEnd()
	return ti
}

// startInput opens the input on v.
func (p *panelState) startInput(v string) {
	p.editing, p.hint, p.ti = true, "", newPanelInput(v)
}

// stopInput closes the input.
func (p *panelState) stopInput() {
	p.editing, p.hint, p.ti = false, "", textinput.Model{}
}

// input is the value typed so far.
func (p *panelState) input() string {
	if !p.editing {
		return ""
	}
	return p.ti.Value()
}

// typeKey feeds k to the input: typed text is one sanitized line inserted at
// the cursor (refused past panelInputMax cells), other keys move or delete.
func (p *panelState) typeKey(k tea.KeyPressMsg) tea.Cmd {
	var msg tea.Msg = k
	if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta|tea.ModSuper|tea.ModHyper) == 0 {
		txt := strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(textwidth.Sanitize(k.Text))
		msg = tea.PasteMsg{Content: txt}
	}
	prev, pos := p.ti.Value(), p.ti.Position()
	var cmd tea.Cmd
	p.ti, cmd = p.ti.Update(msg)
	if textwidth.Width(p.ti.Value()) > panelInputMax {
		p.ti.SetValue(prev)
		p.ti.SetCursor(pos)
	}
	return cmd
}

// cursorCells is how many cells of the input come before its cursor.
func (p *panelState) cursorCells() int {
	rs := []rune(p.ti.Value())
	return textwidth.Width(string(rs[:min(p.ti.Position(), len(rs))]))
}

// formRow is one row of the settings form.
type formRow struct {
	def     fieldDef
	obj     string
	connect bool   // the list view's "연결 대상 고르기"
	info    string // a line under the row that cannot be selected (the hourly price)
}

// openPanel gives the settings panel the keys (e, the menu's 설정, enter in
// the list view). The list view shows the form in its place; elsewhere a
// panel drawer is pulled out.
func (m *canvasModel) openPanel() {
	m.mode = modePanel
	m.panel = panelState{target: m.sel}
	if m.regs.PanelDrawer && m.regs.View != viewList {
		m.drawer.panel = true
		m.layout()
	}
}

// closePanel takes the keys back from the panel (whatever was typed is dropped).
func (m *canvasModel) closePanel() {
	if m.mode == modePanel {
		m.mode = modeNormal
	}
	m.panel = panelState{}
}

// formRows are the rows of the selection's form.
func (m *canvasModel) formRows(ctx *ui.Context) []formRow {
	sel := m.selectionOf(m.sel)
	var out []formRow
	for _, f := range fieldsFor(sel) {
		obj := m.objOf(sel, f)
		if obj == "" {
			continue
		}
		if n := sel.node; n != nil && obj == sel.obj {
			custom := pricing.Provider(n.Provider) == pricing.Custom
			if f.Field == "custom_hourly_micro_usd" && !custom {
				continue
			}
			if strings.HasPrefix(f.Field, "autoscale.") && f.Field != "autoscale.enabled" && !n.Autoscale.Enabled {
				continue
			}
		}
		row := formRow{def: f, obj: obj}
		if f.Field == "instance_type" && sel.node != nil {
			if h, ok := m.hourlyOf(sel.node); ok {
				row.info = ctx.T.Archcanvas.HourlyPrice(usdMicro(h), m.asOf())
			}
		}
		out = append(out, row)
	}
	if sel.node != nil {
		out = append(out, formRow{connect: true, obj: sel.obj})
	}
	return out
}

// panelField is the row under the cursor (tests and the cursor).
func (m *canvasModel) panelRow(ctx *ui.Context) (formRow, bool) {
	rows := m.formRows(ctx)
	if len(rows) == 0 {
		return formRow{}, false
	}
	m.panel.cur = min(max(m.panel.cur, 0), len(rows)-1)
	return rows[m.panel.cur], true
}

// panelKey is the keyboard while the panel has the keys; ok=false passes
// the key on to the canvas keys (hjkl still move the selection).
func (m *canvasModel) panelKey(ctx *ui.Context, k tea.KeyPressMsg, s string) (bool, tea.Cmd, bool) {
	if m.panel.target != m.sel {
		m.panel = panelState{target: m.sel}
	}
	if p := m.panel.pick; p != nil {
		switch s {
		case "j", "down":
			p.cur = min(p.cur+1, len(p.items)-1)
		case "k", "up":
			p.cur = max(p.cur-1, 0)
		case "esc":
			m.panel.pick = nil
		case "enter", "space":
			m.panel.pick = nil
			if len(p.items) == 0 {
				return true, nil, true
			}
			if p.action == pickConnect {
				return true, m.connect(ctx, m.sel, p.value()), true
			}
			return true, m.commitField(ctx, p.obj, p.field, jsonString(p.value())), true
		default:
			return !reservedGlobal(s), nil, true
		}
		return true, nil, true
	}
	if m.panel.editing {
		switch s {
		case "esc":
			m.panel.stopInput()
		case "enter":
			return true, m.commitInput(ctx), true
		default:
			if reservedGlobal(s) {
				return false, nil, true
			}
			return true, m.panel.typeKey(k), true
		}
		return true, nil, true
	}
	switch s {
	case "j", "down":
		m.panel.cur++
		m.panelRow(ctx)
		return true, nil, true
	case "k", "up":
		m.panel.cur = max(m.panel.cur-1, 0)
		return true, nil, true
	case "esc":
		m.closePanel()
		return true, nil, true
	case "enter", "space":
		return true, m.activate(ctx), true
	}
	return false, nil, false
}

// activate starts editing the row under the cursor.
func (m *canvasModel) activate(ctx *ui.Context) tea.Cmd {
	row, ok := m.panelRow(ctx)
	if !ok {
		return nil
	}
	if ro, cmd := m.readOnly(ctx); ro {
		return cmd
	}
	sel := m.selectionOf(m.sel)
	if row.connect {
		items := m.connectChoices(m.sel)
		m.panel.pick = &picker{action: pickConnect, obj: row.obj, items: items}
		return nil
	}
	f := row.def
	switch f.Input {
	case inputBool:
		return m.commitField(ctx, row.obj, f, jsonBool(!rawBool(m.raw(row.obj, f.Field))))
	case inputChoice, inputRef:
		items := m.choices(ctx.T, sel, f)
		cur := slices.IndexFunc(items, func(c choice) bool { return c.value == rawString(m.raw(row.obj, f.Field)) })
		m.panel.pick = &picker{field: f, obj: row.obj, items: items, cur: max(cur, 0)}
	case inputText:
		m.panel.startInput(cleanText(rawString(m.raw(row.obj, f.Field))))
	default:
		m.panel.startInput(strings.ReplaceAll(typed(f, rawInt(m.raw(row.obj, f.Field))), ",", ""))
	}
	return nil
}

// keyID is the design id of object key "n:<id>" ("" for meta).
func keyID(obj string) string {
	if len(obj) > 2 && obj[1] == ':' {
		return obj[2:]
	}
	return ""
}

func jsonBool(b bool) json.RawMessage {
	if b {
		return json.RawMessage("true")
	}
	return json.RawMessage("false")
}

// commitInput checks the typed value and commits it; a number out of range
// (or not a number) stays in the box with the range under it.
func (m *canvasModel) commitInput(ctx *ui.Context) tea.Cmd {
	row, ok := m.panelRow(ctx)
	if !ok {
		m.panel.stopInput()
		return nil
	}
	f := row.def
	if f.Input == inputText {
		return m.commitField(ctx, row.obj, f, jsonString(strings.TrimSpace(m.panel.input())))
	}
	n, ok := parseTyped(m.panel.input())
	if !ok || n < f.Min || n > f.Max {
		m.panel.hint = ctx.T.Archcanvas.RangeHint(typed(fieldDef{}, f.Min), typed(fieldDef{}, f.Max))
		return nil
	}
	return m.commitField(ctx, row.obj, f, jsonInt(n*f.Unit.scale()))
}

// commitField sets field f of object obj to v (with the price-table rules
// below) as one commit, when the design takes it.
func (m *canvasModel) commitField(ctx *ui.Context, obj string, f fieldDef, v json.RawMessage) tea.Cmd {
	ch := m.fieldChanges(obj, f.Field, v)
	if _, issues := archdoc.FromObjects(m.objs.Apply(ch), nil); len(issues) > 0 {
		m.panel.hint = ctx.T.Archcanvas.InvalidValue()
		if !m.panel.editing {
			return ui.Toast(m.panel.hint, ui.ToastInfo)
		}
		return nil
	}
	m.panel.stopInput()
	return m.commit(ctx, ch)
}

// fieldChanges are the changes of setting field to v on obj. On a node,
// picking a provider, region or instance type fills CPU, memory and NIC
// from the price table; typing CPU, memory or NIC by hand turns a priced
// type into a custom one at the same hourly price (spec C2.8).
func (m *canvasModel) fieldChanges(obj, field string, v json.RawMessage) []archdoc.Change {
	set := func(f string, v json.RawMessage) archdoc.Change {
		return archdoc.Change{Kind: archdoc.ChangeSet, Obj: obj, Field: f, Value: v}
	}
	ch := []archdoc.Change{set(field, v)}
	sel := m.selectionOf(keyID(obj))
	n := sel.node
	if n == nil || sel.obj != obj {
		return ch
	}
	fill := func(p pricing.Provider, region, typ string) {
		if it, ok := pricing.Lookup(p, region, typ); ok {
			ch = append(ch, set("cpu_cores", jsonInt(int64(it.VCPU))), set("memory_mib", jsonInt(int64(it.MemoryMiB))))
			if it.NICMbps > 0 {
				ch = append(ch, set("nic_mbps", jsonInt(int64(it.NICMbps))))
			}
		}
	}
	switch field {
	case "provider":
		p := pricing.Provider(rawString(v))
		switch {
		case priced(string(p)):
			c := *n
			if _, ok := pricing.RegionOf(p, n.Region); n.Region != "" && !ok {
				c.Region = ""
				ch = append(ch, set("region", jsonString("")))
			}
			typ := pricing.DefaultType(p, n.Kind)
			ch = append(ch, set("instance_type", jsonString(typ)), set("custom_hourly_micro_usd", jsonInt(0)))
			fill(p, m.effRegion(&c, p), typ)
		case p == pricing.Custom:
			ch = append(ch, set("instance_type", jsonString(string(pricing.Custom))))
			if h, ok := m.hourlyOf(n); ok && n.CustomHourlyMicroUSD == 0 {
				ch = append(ch, set("custom_hourly_micro_usd", jsonInt(h)))
			}
		}
	case "region":
		if p := pricing.Provider(n.Provider); priced(n.Provider) {
			c := *n
			c.Region = rawString(v)
			fill(p, m.effRegion(&c, p), n.InstanceType)
		}
	case "instance_type":
		p := pricing.Provider(n.Provider)
		fill(p, m.effRegion(n, p), rawString(v))
	case "cpu_cores", "memory_mib", "nic_mbps":
		if priced(n.Provider) && n.InstanceType != string(pricing.Custom) {
			h, _ := m.hourlyOf(n)
			ch = append(ch,
				set("provider", jsonString(string(pricing.Custom))),
				set("instance_type", jsonString(string(pricing.Custom))),
				set("custom_hourly_micro_usd", jsonInt(h)))
		}
	}
	return ch
}

// panelLine kinds of the form.
type formLine struct {
	s   string
	st  sty
	row int // the selectable row index (-1 = none)
	in  bool
	cur int // in the settings panel: the real cursor's cell in s (other dialogs: its end)
}

// formLines are the form's lines: the title, the rows (or the open list),
// then the selection's results.
func (m *canvasModel) formLines(ctx *ui.Context, w int) []formLine {
	t := ctx.T.Archcanvas
	sel := m.selectionOf(m.sel)
	var out []formLine
	add := func(s string, st sty) { out = append(out, formLine{s: s, st: st, row: -1}) }
	switch {
	case sel.node != nil:
		add(m.label(sel.node.ID), sAccBold)
		add(kindName(ctx.T, sel.node.Kind), sDim)
	case sel.box != nil:
		add(m.label(sel.box.ID), sAccBold)
		add(boxKindName(ctx.T, sel.box.Kind), sDim)
	case sel.edge != nil:
		add(t.PanelEdge(m.label(sel.edge.From), m.label(sel.edge.To)), sAccBold)
	default:
		add(t.PanelTraffic(), sAccBold)
	}
	rows := m.formRows(ctx)
	if p := m.panel.pick; p != nil {
		head := t.ConnectPick()
		if p.action == pickField {
			head = p.field.Label(ctx.T)
		}
		add("─ "+head+" ─", sDim)
		for i, c := range p.items {
			st := sBase
			mark := "  "
			if i == p.cur {
				st, mark = sSel, "▸ "
			}
			out = append(out, formLine{s: mark + c.label, st: st, row: i})
		}
		return out
	}
	for i, r := range rows {
		label, val := "", ""
		if r.connect {
			label = "→ " + t.ConnectPick()
		} else {
			label, val = r.def.Label(ctx.T), m.shown(ctx.T, sel, r.obj, r.def)
		}
		st := sBase
		if i == m.panel.cur {
			st = sSel
		}
		if i == m.panel.cur && m.panel.editing {
			out = append(out, formLine{s: label + " " + m.panel.input(), st: sAcc, row: i, in: true,
				cur: textwidth.Width(label) + 1 + m.panel.cursorCells()})
			if m.panel.hint != "" {
				for _, l := range textwidth.Wrap(m.panel.hint, w) {
					add(l, sWarn)
				}
			}
		} else {
			lw := max(w-textwidth.Width(val)-1, 1)
			s := textwidth.PadRight(textwidth.Truncate(label, lw, "…"), lw) + " " + val
			out = append(out, formLine{s: s, st: st, row: i})
			if i == m.panel.cur && m.panel.hint != "" {
				for _, l := range textwidth.Wrap(m.panel.hint, w) {
					add(l, sWarn)
				}
			}
		}
		if r.info != "" {
			for _, l := range textwidth.Wrap(r.info, w) {
				add(l, sDim)
			}
		}
	}
	for _, l := range m.resultLines(ctx, sel) {
		parts := []string{l.s}
		if l.wrap {
			parts = textwidth.Wrap(l.s, w)
		}
		for _, p := range parts {
			add(p, l.st)
		}
	}
	return out
}

// drawForm draws the settings form in r (x0 = where the text starts).
func (m *canvasModel) drawForm(ctx *ui.Context, b *buf, r layout.Rect, x0, w int) {
	m.panel.curX, m.panel.curY = -1, -1
	if w <= 0 || r.H <= 0 {
		return
	}
	lines := m.formLines(ctx, w)
	focus := 0
	for i, l := range lines {
		if (m.panel.pick != nil && l.row == m.panel.pick.cur) || (m.panel.pick == nil && l.row == m.panel.cur) {
			focus = i
			break
		}
	}
	off := 0
	if focus >= r.H-1 {
		off = focus - r.H + 2
	}
	for i := 0; i < r.H && off+i < len(lines); i++ {
		l := lines[off+i]
		b.line(x0, r.Y+i, w, l.s, l.st)
		if l.in {
			m.panel.curX = x0 + min(l.cur, w-1)
			m.panel.curY = r.Y + i
		}
	}
}

// resultLines are the selection's results: a node's use bar, latency,
// errors and cost; nothing selected = the design's.
func (m *canvasModel) resultLines(ctx *ui.Context, sel selection) []panelLine {
	t := ctx.T.Archcanvas
	rule := panelLine{s: "─ " + t.PanelResult() + " ─", st: sDim}
	if n := sel.node; n != nil {
		nr, ok := m.result.Nodes[n.ID]
		if !ok {
			return nil
		}
		return []panelLine{rule,
			utilLine(ctx, nr.UtilPermille, nr.Down, nr.Overloaded),
			{s: t.PanelLatency(archdraw.FormatMs(nr.P50Ms), archdraw.FormatMs(nr.P99Ms))},
			{s: t.MetricErr(pct1(nr.ErrPermille))},
			{s: m.costShort(ctx, nr.CostUSDMonth), wrap: true},
		}
	}
	if sel.obj != "" {
		return nil
	}
	var ls []panelLine
	if r := m.result; r != nil && r.RPS > 0 {
		ls = append(ls, rule,
			panelLine{s: t.PanelLatency(archdraw.FormatMs(r.P50Ms), archdraw.FormatMs(r.P99Ms))},
			panelLine{s: t.MetricErr(pct1(r.ErrorPermille))},
		)
	}
	if r := m.result; r != nil {
		ls = append(ls, panelLine{s: m.costShort(ctx, r.Cost.TotalUSDMonth), wrap: true})
	}
	return ls
}

// useBar is a 10-cell use bar ("██████░░░░").
func useBar(permille int) string {
	k := min(max(permille, 0), 1000) / 100
	return strings.Repeat("█", k) + strings.Repeat("░", 10-k)
}

// utilLine is a node's use: warning colour from 85%, danger colour and ⚠
// from 100% (never colour alone, spec C2.9).
func utilLine(ctx *ui.Context, permille int, down, over bool) panelLine {
	t := ctx.T.Archcanvas
	s := useBar(permille) + " " + t.PanelUtil(max(permille, 0)/10)
	switch {
	case down:
		return panelLine{s: "✕ " + t.PanelDown(), st: sErr}
	case over || permille >= 1000:
		return panelLine{s: s + " ⚠ " + t.MetricOverloaded(), st: sErr}
	case permille >= 850:
		return panelLine{s: s, st: sWarn}
	}
	return panelLine{s: s}
}

// refresh redraws from the sink after someone else's change: a panel whose
// node is gone closes with a note (the typed value is dropped, not sent).
func (m *canvasModel) refresh(ctx *ui.Context) tea.Cmd {
	m.designChanged()
	m.rebuild(ctx)
	if m.panelGone {
		m.panelGone = false
		return ui.Toast(ctx.T.Archcanvas.NodeDeleted(), ui.ToastInfo)
	}
	return nil
}
