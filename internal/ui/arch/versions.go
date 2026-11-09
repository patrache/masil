package arch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/widgets"
)

// roPreview: the canvas shows a saved version (read-only until esc).
const roPreview = roLocked + 1

// versionsStep is what the versions dialog shows.
type versionsStep int

const (
	versionsList versionsStep = iota
	versionsName              // typing a name for "save this state"
	versionsDiff              // the changes from the version before
)

// versionsDialog is arch.versions (P3 Task 23) on a room document: the
// versions newest first (a named one as "v3 — 리뷰용", an automatic one by
// its time), enter = read-only preview on the canvas, n = save the current
// state with a name, d = the changes from the version before, r = restore
// (a new version; the history stays).
type versionsDialog struct {
	instance string // the canvas the preview goes to
	docID    string
	labels   map[string]string // object key → its label now (the diff names)

	items   []api.DocVersion
	loaded  bool
	cur     int
	step    versionsStep
	input   string
	hint    string
	busy    bool
	diff    []formLine
	diffTop int
	size    ui.Size
	curX    int
	curY    int
}

var (
	_ ui.Overlay    = (*versionsDialog)(nil)
	_ ui.KeyHandler = (*versionsDialog)(nil)
	_ ui.Titled     = (*versionsDialog)(nil)
	_ ui.Cursorer   = (*versionsDialog)(nil)
)

// versionsListMsg is ListDocVersions' answer (sel = the version number to
// put the cursor on, 0 = the top).
type versionsListMsg struct {
	dlg   *versionsDialog
	items []api.DocVersion
	sel   int
	err   error
}

// versionSavedMsg is CreateDocVersion's or RestoreDocVersion's answer
// (from = the restored version, 0 for a named save).
type versionSavedMsg struct {
	dlg  *versionsDialog
	v    *api.DocVersion
	from int
	err  error
}

// versionSnapMsg is GetDocVersion's answer for the preview (prev = the
// version before it, for a diff).
type versionSnapMsg struct {
	dlg        *versionsDialog
	v          api.DocVersion
	snap, prev *api.DocVersionSnapshot
	diff       bool
	err        error
}

// versionPreviewMsg puts version label (objs) on canvas instance.
type versionPreviewMsg struct {
	instance string
	label    string
	objs     archdoc.Objects
}

// versionPreview is the version a room document's canvas shows instead of
// the live state.
type versionPreview struct {
	label string
	objs  archdoc.Objects
}

func (m *canvasModel) newVersionsDialog(sh *sharedSink) *versionsDialog {
	d := &versionsDialog{instance: m.route.ID, docID: sh.docID, labels: map[string]string{}}
	if m.design != nil {
		for _, n := range m.design.Nodes {
			d.labels["n:"+n.ID] = cleanText(n.Label)
		}
		for _, b := range m.design.Boxes {
			d.labels["b:"+b.ID] = cleanText(b.Label)
		}
	}
	return d
}

func (d *versionsDialog) Init(ctx *ui.Context) tea.Cmd    { return d.load(ctx, 0) }
func (d *versionsDialog) Resize(_ *ui.Context, s ui.Size) { d.size = s }
func (d *versionsDialog) Modal() bool                     { return true }
func (d *versionsDialog) Bounds(body layout.Rect) layout.Rect {
	h := min(max(len(d.items)+6, 12), 24)
	if d.step == versionsDiff {
		h = min(max(len(d.diff)+4, 12), 24)
	}
	return centeredRect(body, 72, h)
}
func (d *versionsDialog) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Archcanvas.VersionsTitle(), ui.Badge{}
}

// load asks for the list again.
func (d *versionsDialog) load(ctx *ui.Context, sel int) tea.Cmd {
	cl := ctx.API
	if cl == nil {
		return nil
	}
	id := d.docID
	return func() tea.Msg {
		items, err := cl.ListDocVersions(context.Background(), id)
		return versionsListMsg{dlg: d, items: items, sel: sel, err: err}
	}
}

// versionLabel is a version's name in the list ("v3 — 리뷰용").
func versionLabel(ctx *ui.Context, v api.DocVersion) string {
	a := ctx.T.Archcanvas
	switch {
	case v.Name != nil && *v.Name != "":
		return a.VersionsNamed(v.No, cleanText(*v.Name))
	case v.RestoredFrom != nil:
		return a.VersionsRestored(v.No, *v.RestoredFrom)
	}
	return a.VersionsAuto(v.No, versionTime(ctx, v))
}

// versionTime is when v was saved, on the user's clock.
func versionTime(ctx *ui.Context, v api.DocVersion) string {
	t := v.CreatedAt
	if ctx.Loc != nil {
		t = t.In(ctx.Loc)
	}
	return t.Format("01-02 15:04")
}

func (d *versionsDialog) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	a := ctx.T.Archcanvas
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if d.step == versionsName {
			d.typeText(msg.Content)
		}
	case versionsListMsg:
		if msg.dlg != d {
			return nil
		}
		d.busy, d.loaded = false, true
		if msg.err != nil {
			d.hint = a.VersionsFailed()
			return nil
		}
		d.items, d.cur = msg.items, 0
		for i, v := range d.items {
			if v.No == msg.sel {
				d.cur = i
			}
		}
	case versionSavedMsg:
		if msg.dlg != d {
			return nil
		}
		d.busy = false
		var ae *api.Error
		switch {
		case msg.err != nil && errors.As(msg.err, &ae) && ae.Code == "limit_exceeded":
			d.hint = a.VersionsLimit()
			return nil
		case msg.err != nil || msg.v == nil:
			d.hint = a.VersionsFailed()
			return nil
		}
		d.step, d.input = versionsList, ""
		toast := a.VersionsSaved(msg.v.No)
		if msg.from > 0 {
			toast = a.VersionsRestoredToast(msg.from, msg.v.No)
		}
		return tea.Batch(d.load(ctx, msg.v.No), ui.Toast(toast, ui.ToastSuccess))
	case versionSnapMsg:
		if msg.dlg != d {
			return nil
		}
		d.busy = false
		if msg.err != nil || msg.snap == nil {
			d.hint = a.VersionsFailed()
			return nil
		}
		cur, err := snapObjects(msg.snap)
		if err != nil {
			d.hint = a.VersionsFailed()
			return nil
		}
		if !msg.diff {
			inst, label := d.instance, versionLabel(ctx, msg.v)
			return tea.Batch(closeOverlay, func() tea.Msg { return versionPreviewMsg{instance: inst, label: label, objs: cur} })
		}
		prev, err := snapObjects(msg.prev)
		if err != nil {
			d.hint = a.VersionsFailed()
			return nil
		}
		d.diff, d.diffTop = d.diffLines(ctx, msg.prev.Version.No, msg.v.No, prev, cur), 0
		d.step = versionsDiff
	}
	return nil
}

// snapObjects is a version's snapshot as design objects (read back through
// FromObjects, as the canvas does).
func snapObjects(s *api.DocVersionSnapshot) (archdoc.Objects, error) {
	snap, err := collab.DecodeSnapshot(s.Snapshot)
	if err != nil {
		return nil, err
	}
	objs := objectsOf(collab.NewDoc(snap))
	d, _ := archdoc.FromObjects(objs, nil)
	if d == nil {
		return nil, errors.New("arch: unreadable version")
	}
	return d.ToObjects(), nil
}

func (d *versionsDialog) HandleKey(ctx *ui.Context, k tea.KeyPressMsg) (bool, tea.Cmd) {
	s := k.String()
	if d.busy {
		return s != "esc", nil
	}
	switch d.step {
	case versionsName:
		return true, d.inputKey(ctx, k, s)
	case versionsDiff:
		switch s {
		case "esc", "backspace", "q", "left", "h":
			d.step, d.diff = versionsList, nil
		case "j", "down":
			d.diffTop = min(d.diffTop+1, max(len(d.diff)-1, 0))
		case "k", "up":
			d.diffTop = max(d.diffTop-1, 0)
		}
		return true, nil
	}
	a := ctx.T.Archcanvas
	d.hint = ""
	switch s {
	case "j", "down":
		d.cur = min(d.cur+1, max(len(d.items)-1, 0))
	case "k", "up":
		d.cur = max(d.cur-1, 0)
	case "home", "g":
		d.cur = 0
	case "end", "G":
		d.cur = max(len(d.items)-1, 0)
	case "n":
		d.step, d.input = versionsName, ""
	case "enter":
		if d.cur < len(d.items) {
			return true, d.fetch(ctx, false)
		}
	case "d":
		if d.cur < len(d.items) {
			if d.cur+1 >= len(d.items) {
				d.hint = a.VersionsDiffOldest()
				return true, nil
			}
			return true, d.fetch(ctx, true)
		}
	case "r":
		if d.cur < len(d.items) {
			v := d.items[d.cur]
			return true, ui.OpenOverlay(widgets.Confirm(a.VersionsRestoreTitle(v.No), a.VersionsRestoreBody(), a.VersionsRestoreYes(), a.KeyCancel(), d.restore(ctx, v.No)))
		}
	default:
		return false, nil
	}
	return true, nil
}

// fetch reads the version under the cursor (and, for a diff, the one before).
func (d *versionsDialog) fetch(ctx *ui.Context, diff bool) tea.Cmd {
	cl := ctx.API
	if cl == nil {
		return nil
	}
	id, v := d.docID, d.items[d.cur]
	var prevNo int
	if diff {
		prevNo = d.items[d.cur+1].No
	}
	d.busy = true
	return func() tea.Msg {
		bg := context.Background()
		snap, err := cl.GetDocVersion(bg, id, v.No)
		out := versionSnapMsg{dlg: d, v: v, snap: snap, diff: diff, err: err}
		if err == nil && diff {
			out.prev, out.err = cl.GetDocVersion(bg, id, prevNo)
		}
		return out
	}
}

// restore asks the server to restore version no (a new version).
func (d *versionsDialog) restore(ctx *ui.Context, no int) tea.Cmd {
	cl := ctx.API
	id := d.docID
	return func() tea.Msg {
		if cl == nil {
			return versionSavedMsg{dlg: d, err: errors.New("arch: no API")}
		}
		v, err := cl.RestoreDocVersion(context.Background(), id, no)
		return versionSavedMsg{dlg: d, v: v, from: no, err: err}
	}
}

// typeText adds typed or pasted text to the name (one line).
func (d *versionsDialog) typeText(s string) {
	s = strings.NewReplacer("\r\n", "", "\r", "", "\n", "", "\t", " ").Replace(s)
	d.input = textwidth.Truncate(d.input+textwidth.SanitizeInput(s), 160, "")
}

func (d *versionsDialog) inputKey(ctx *ui.Context, k tea.KeyPressMsg, s string) tea.Cmd {
	switch s {
	case "esc":
		d.step, d.input, d.hint = versionsList, "", ""
	case "backspace":
		if r := []rune(d.input); len(r) > 0 {
			d.input = string(r[:len(r)-1])
		}
	case "ctrl+u":
		d.input = ""
	case "enter":
		name := strings.TrimSpace(d.input)
		cl := ctx.API
		if name == "" || cl == nil {
			return nil
		}
		id := d.docID
		d.busy = true
		return func() tea.Msg {
			v, err := cl.CreateDocVersion(context.Background(), id, name)
			return versionSavedMsg{dlg: d, v: v, err: err}
		}
	default:
		if k.Text != "" {
			d.typeText(k.Text)
		}
	}
	return nil
}

// diffLines is the change list from version a (prev) to b (cur): a summary
// line, then added, removed and changed objects by their labels and catalog
// field names.
func (d *versionsDialog) diffLines(ctx *ui.Context, a, b int, prev, cur archdoc.Objects) []formLine {
	t := ctx.T.Archcanvas
	pd, _ := archdoc.FromObjects(prev, nil)
	cd, _ := archdoc.FromObjects(cur, nil)
	if pd == nil || cd == nil {
		return []formLine{{s: t.VersionsFailed(), st: sWarn, row: -1}}
	}
	sum := archdoc.Summarize(pd, cd)
	out := []formLine{
		{s: t.VersionsDiffTitle(a, b), st: sAccBold, row: -1},
		{s: t.VersionsDiffAdded(len(sum.Added)) + " · " + t.VersionsDiffRemoved(len(sum.Removed)) + " · " + t.VersionsDiffChanged(len(sum.Changed)), st: sDim, row: -1},
	}
	if len(sum.Added)+len(sum.Removed)+len(sum.Changed) == 0 {
		return append(out, formLine{s: t.VersionsDiffNone(), st: sBase, row: -1})
	}
	var name func(objs archdoc.Objects, key string) string
	name = func(objs archdoc.Objects, key string) string {
		if key == "meta" {
			return t.VersionsDiffMeta()
		}
		if strings.HasPrefix(key, "e:") { // a connection by its ends
			from, to := objString(objs, key, "from"), objString(objs, key, "to")
			if from != "" && to != "" {
				return t.PanelEdge(name(objs, "n:"+from), name(objs, "n:"+to))
			}
		}
		if l := objLabel(objs, key); l != "" {
			return l
		}
		if l := d.labels[key]; l != "" {
			return l
		}
		return cleanText(key)
	}
	for _, k := range sum.Added {
		out = append(out, formLine{s: "+ " + name(cur, k), st: sOK, row: -1})
	}
	for _, k := range sum.Removed {
		out = append(out, formLine{s: "- " + name(prev, k), st: sWarn, row: -1})
	}
	for _, c := range sum.Changed {
		out = append(out, formLine{s: "~ " + name(cur, c.Obj) + " · " + fieldLabel(ctx.T, c.Field) + " " + diffValue(c.From) + " → " + diffValue(c.To), st: sBase, row: -1})
	}
	return out
}

// objLabel is an object's label field ("" when it has none).
func objLabel(objs archdoc.Objects, key string) string {
	return cleanText(objString(objs, key, "label"))
}

// objString is an object's string field ("" when absent or not a string).
func objString(objs archdoc.Objects, key, field string) string {
	var s string
	if raw, ok := objs[key][field]; ok && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// fieldLabel is a field's catalog name (the settings panel's), else its key.
func fieldLabel(t *i18n.T, field string) string {
	switch field {
	case "pos":
		return t.Archcanvas.VerFieldPos()
	case "rect":
		return t.Archcanvas.VerFieldRect()
	case "parent":
		return t.Archcanvas.VerFieldParent()
	case labelField.Field:
		return labelField.Label(t)
	case "kind":
		return t.Archcanvas.VerFieldKind()
	case "title":
		return t.Archcanvas.VerFieldTitle()
	case "from":
		return t.Archcanvas.VerFieldFrom()
	case "to":
		return t.Archcanvas.VerFieldTo()
	case "at_s":
		return t.Archcanvas.VerFieldAtS()
	case "target":
		return t.Archcanvas.VerFieldTarget()
	case "count":
		return t.Archcanvas.VerFieldCount()
	case "add_ms":
		return t.Archcanvas.VerFieldAddMs()
	case "dur_s":
		return t.Archcanvas.VerFieldDurS()
	}
	for _, defs := range [][]fieldDef{nodeFields, boxFields, edgeFields, trafficFields} {
		for _, f := range defs {
			if f.Field == field && f.Label != nil {
				return f.Label(t)
			}
		}
	}
	return cleanText(field)
}

// diffValue is a stored value on one line ("—" when absent; a string
// without quotes).
func diffValue(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "—"
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return cleanText(s)
	}
	return textwidth.Truncate(cleanText(string(raw)), 24, "…")
}

// lines is the dialog's content at width w.
func (d *versionsDialog) lines(ctx *ui.Context, w int) []formLine {
	a := ctx.T.Archcanvas
	var out []formLine
	switch d.step {
	case versionsDiff:
		out = append(out, d.diff[min(d.diffTop, len(d.diff)):]...)
		out = append(out, formLine{row: -1}, formLine{s: a.VersionsDiffHint(), st: sDim, row: -2})
		return out
	case versionsName:
		out = append(out, formLine{s: a.VersionsNamePrompt(), st: sAccBold, row: -1}, formLine{s: "› " + d.input, st: sBase, row: -1, in: true})
		if d.hint != "" {
			out = append(out, formLine{s: d.hint, st: sWarn, row: -1})
		}
		return append(out, formLine{row: -1}, formLine{s: a.VersionsNameHint(), st: sDim, row: -2})
	}
	switch {
	case !d.loaded:
		out = append(out, formLine{s: a.VersionsLoading(), st: sDim, row: -1})
	case len(d.items) == 0:
		out = append(out, formLine{s: a.VersionsEmpty(), st: sDim, row: -1})
	}
	for i, v := range d.items {
		st, mark := sBase, "  "
		if i == d.cur {
			st, mark = sSel, "▸ "
		}
		label := mark + versionLabel(ctx, v)
		who := ""
		if v.CreatedBy != nil {
			who = personName(ctx, v.CreatedBy.ID)
		}
		meta := "" // an automatic version shows its time in the label already
		if who != "" {
			meta = a.VersionsMeta(who, versionTime(ctx, v))
		}
		if gap := w - textwidth.Width(label) - textwidth.Width(meta); meta != "" && gap >= 2 {
			label += strings.Repeat(" ", gap) + meta
		}
		out = append(out, formLine{s: label, st: st, row: i})
	}
	out = append(out, formLine{row: -1})
	if d.hint != "" {
		out = append(out, formLine{s: d.hint, st: sWarn, row: -1})
	}
	return append(out, formLine{s: a.VersionsHint(), st: sDim, row: -2})
}

func (d *versionsDialog) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	b := newBuf(w, h)
	b.fill(layout.Rect{W: w, H: h}, sBase)
	// the key hint (row -2) wraps onto the bottom rows; the body scrolls to
	// keep the cursor in the rows above it
	var body []formLine
	var hint []string
	for _, l := range d.lines(ctx, w-2) {
		if l.row == -2 {
			hint = append(hint, textwidth.Wrap(l.s, max(w-2, 1))...)
			continue
		}
		body = append(body, l)
	}
	if len(hint) > h {
		hint = hint[len(hint)-h:]
	}
	rows := h - len(hint)
	if len(body) > rows {
		top := 0
		for i, l := range body {
			if d.step == versionsList && l.row == d.cur {
				top = max(0, i-(rows-1))
			}
		}
		body = body[top:min(len(body), top+rows)]
	}
	d.curX = -1
	for y, l := range body {
		b.line(1, y, w-2, l.s, l.st)
		if l.in {
			if x := 1 + textwidth.Width(l.s); x < w-1 {
				d.curX, d.curY = x, y
			}
		}
	}
	for i, l := range hint {
		b.line(1, h-len(hint)+i, w-2, l, sDim)
	}
	return b.render(ctx.Theme)
}

// Cursor is the real cursor at the end of the typed name.
func (d *versionsDialog) Cursor() *tea.Cursor {
	if d.step != versionsName || d.curX < 0 {
		return nil
	}
	return tea.NewCursor(d.curX, d.curY)
}
