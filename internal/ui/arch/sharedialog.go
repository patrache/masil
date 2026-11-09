package arch

import (
	"context"
	"encoding/json"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// shareDialog is arch.share (P3 Task 22): pick a space, read the plaintext
// warning (share_plaintext_warning — wrapped, never shortened, at every size),
// then the design is created as a room document and opened. The personal
// design stays as it is.
type shareDialog struct {
	design  *archdoc.Design
	title   string
	spaces  []shareSpace
	cur     int
	step    int // 0 = space, 1 = the warning
	yes     bool
	warning string // the warning text (Bounds has no catalog)
	hint    string
	busy    bool
	size    ui.Size
}

type shareSpace struct{ id, name string }

var (
	_ ui.Overlay    = (*shareDialog)(nil)
	_ ui.KeyHandler = (*shareDialog)(nil)
	_ ui.Titled     = (*shareDialog)(nil)
)

// shareDoneMsg is CreateDoc's answer.
type shareDoneMsg struct {
	dlg   *shareDialog
	doc   *api.Doc
	space string
	err   error
}

// shareW is the dialog's widest outer width.
const shareW = 60

func (m *canvasModel) newShareDialog(ctx *ui.Context) *shareDialog {
	d := &shareDialog{design: m.design, title: m.title(ctx), yes: true, warning: ctx.T.Archcanvas.SharePlaintextWarning()}
	if ctx.Store != nil {
		for _, id := range ctx.Store.SpaceOrder {
			if sp := ctx.Store.Spaces[id]; sp != nil {
				d.spaces = append(d.spaces, shareSpace{id: id, name: cleanText(sp.Name)})
			}
		}
	}
	return d
}

func (d *shareDialog) Init(*ui.Context) tea.Cmd           { return nil }
func (d *shareDialog) Resize(_ *ui.Context, size ui.Size) { d.size = size }
func (d *shareDialog) Modal() bool                        { return true }
func (d *shareDialog) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Archcanvas.ShareTitle(), ui.Badge{}
}

// Bounds is tall enough for the whole wrapped warning (the app clamps it to
// the body; a tiny body shows the dialog without a border, full size).
func (d *shareDialog) Bounds(body layout.Rect) layout.Rect {
	w := min(shareW, body.W)
	rows := len(d.spaces) + 4
	if d.step == 1 {
		rows = len(textwidth.Wrap(d.warning, max(w-4, 1))) + 6
	}
	return centeredRect(body, w, rows+2)
}

func (d *shareDialog) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	m, ok := msg.(shareDoneMsg)
	if !ok || m.dlg != d {
		return nil
	}
	d.busy = false
	if m.err != nil || m.doc == nil {
		d.hint = ctx.T.Archcanvas.ShareFailed()
		return nil
	}
	id := m.doc.ID
	return tea.Batch(closeOverlay,
		ui.OpenTool(ui.ToolArch, keyDoc+id, map[string]string{queryDoc: id}),
		ui.Toast(ctx.T.Archcanvas.ShareDone(m.space), ui.ToastSuccess))
}

func (d *shareDialog) HandleKey(ctx *ui.Context, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if d.busy {
		return true, nil
	}
	switch k.String() {
	case "j", "down":
		if d.step == 0 {
			d.cur = min(d.cur+1, max(len(d.spaces)-1, 0))
		}
	case "k", "up":
		if d.step == 0 {
			d.cur = max(d.cur-1, 0)
		}
	case "left", "h", "right", "l", "tab", "shift+tab":
		if d.step == 1 {
			d.yes = !d.yes
		}
	case "y":
		if d.step == 1 {
			return true, d.create(ctx)
		}
	case "n":
		if d.step == 1 {
			return true, closeOverlay
		}
	case "enter":
		if d.step == 0 {
			if len(d.spaces) > 0 {
				d.step, d.yes, d.hint = 1, true, ""
			}
			return true, nil
		}
		if !d.yes {
			return true, closeOverlay
		}
		return true, d.create(ctx)
	default:
		return false, nil
	}
	return true, nil
}

// create makes the room document from the design.
func (d *shareDialog) create(ctx *ui.Context) tea.Cmd {
	cl := ctx.API
	if cl == nil || d.cur >= len(d.spaces) {
		d.hint = ctx.T.Archcanvas.ShareFailed()
		return nil
	}
	snap, err := json.Marshal(snapshotOf(d.design))
	if err != nil {
		d.hint = ctx.T.Archcanvas.ShareFailed()
		return nil
	}
	sp := d.spaces[d.cur]
	req := api.CreateDocRequest{Type: "arch", Title: d.docTitle(), Snapshot: snap}
	d.busy = true
	return func() tea.Msg {
		doc, err := cl.CreateDoc(context.Background(), sp.id, req)
		return shareDoneMsg{dlg: d, doc: doc, space: sp.name, err: err}
	}
}

// docTitle is the room document's title (the design's, at most 80 graphemes).
func (d *shareDialog) docTitle() string {
	t := cleanText(d.title)
	if t == "" {
		t = "arch"
	}
	return textwidth.Truncate(t, 160, "…")
}

// lines is the dialog's content at width w.
func (d *shareDialog) lines(ctx *ui.Context, w int) []formLine {
	a := ctx.T.Archcanvas
	var out []formLine
	if d.step == 0 {
		if len(d.spaces) == 0 {
			out = append(out, formLine{s: a.ShareNoSpaces(), st: sDim, row: -1})
		} else {
			out = append(out, formLine{s: a.SharePick(), st: sAccBold, row: -1})
			for i, sp := range d.spaces {
				st, mark := sBase, "  "
				if i == d.cur {
					st, mark = sSel, "▸ "
				}
				out = append(out, formLine{s: mark + sp.name, st: st, row: i})
			}
		}
	} else {
		for _, l := range textwidth.Wrap(d.warning, max(w, 1)) {
			out = append(out, formLine{s: l, st: sBase, row: -1})
		}
		out = append(out, formLine{row: -1})
		yes, no := "[ "+a.ShareYes()+" ]", "[ "+a.KeyCancel()+" ]"
		ys, ns := sBase, sBase
		if d.yes {
			ys = sSel
		} else {
			ns = sSel
		}
		out = append(out, formLine{s: yes, st: ys, row: 0}, formLine{s: no, st: ns, row: 1})
	}
	if d.hint != "" {
		out = append(out, formLine{s: d.hint, st: sWarn, row: -1})
	}
	return append(out, formLine{s: a.ShareHint(), st: sDim, row: -1})
}

func (d *shareDialog) View(ctx *ui.Context, _ bool) string {
	w, h := d.size.W, d.size.H
	if w <= 0 || h <= 0 {
		return ""
	}
	b := newBuf(w, h)
	b.fill(layout.Rect{W: w, H: h}, sBase)
	pad := 1
	if w < 12 {
		pad = 0
	}
	lines := d.lines(ctx, w-2*pad)
	// the buttons share a row when they fit
	var rows [][]formLine
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l.row == 0 && d.step == 1 && i+1 < len(lines) && textwidth.Width(l.s)+2+textwidth.Width(lines[i+1].s) <= w-2*pad {
			rows = append(rows, []formLine{l, lines[i+1]})
			i++
			continue
		}
		rows = append(rows, []formLine{l})
	}
	// the warning never goes: the key hint is dropped first when rows run out
	if len(rows) > h && len(rows) > 0 {
		rows = rows[:h]
	}
	for y, row := range rows {
		x := pad
		for _, l := range row {
			x += b.text(x, y, l.s, w-pad-x, l.st)
			x += 2
		}
	}
	return b.render(ctx.Theme)
}
