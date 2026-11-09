package arch

import (
	"context"
	"errors"
	"sort"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// sendDialog is arch.send_card (P3 Task 26): pick a conversation (text
// channels, DMs and group DMs, most recent first; s switches between the
// design card and the simulation card), see the card exactly as it will be
// posted, then send it.
type sendDialog struct {
	design *archdoc.Design
	result *archsim.Result
	ref    cardRef
	chans  []sendChan
	cur    int
	top    int // first conversation row shown
	sim    bool
	step   int // 0 = the conversation, 1 = the preview
	card   api.Card
	body   string
	hint   string
	busy   bool
	size   ui.Size
	rows   int // the preview's card lines at the widest dialog (Bounds)

	// a room document's state when the dialog opened: the card names the
	// version at seq (pin), never one with my unsaved edits missing
	seq     int64
	unsaved bool
}

// sendChan is a conversation: label names it in the preview and the toast,
// where (the space of a text channel) only in the list.
type sendChan struct{ id, label, where string }

var (
	_ ui.Overlay    = (*sendDialog)(nil)
	_ ui.KeyHandler = (*sendDialog)(nil)
	_ ui.Titled     = (*sendDialog)(nil)
)

// sendPinnedMsg is the version a room document's card names (pin).
type sendPinnedMsg struct {
	dlg *sendDialog
	no  int
	err error
}

// errDocMoved: the version made is not the state the dialog saw (someone
// edited in between).
var errDocMoved = errors.New("arch: the document moved on")

// sendDoneMsg is SendMessage's answer.
type sendDoneMsg struct {
	dlg   *sendDialog
	label string
	err   error
}

// sendW is the dialog's widest outer width: a whole card line (60 cells) fits.
const sendW = 72

// newSendDialog is nil for a room document whose space is not known yet
// (GetDoc has not answered): its card may not fall back to a copy.
func (m *canvasModel) newSendDialog(ctx *ui.Context) *sendDialog {
	d := &sendDialog{design: m.design, result: m.result}
	if sh := m.shared(); sh != nil {
		if sh.meta == nil || sh.meta.SpaceID == "" {
			return nil
		}
		d.ref = cardRef{spaceID: sh.meta.SpaceID, docID: sh.docID, room: true}
		if sh.preview != nil {
			d.ref.versionNo = sh.preview.no // the canvas shows that saved version
		} else {
			d.seq, d.unsaved = sh.s.View().Seq(), len(sh.s.Pending()) > 0
		}
	}
	if d.result == nil {
		d.result = archsim.Analyze(m.design, archsim.Options{})
	}
	d.chans = sendChannels(ctx)
	return d
}

// sendChannels are the conversations a card can go to, most recent first.
func sendChannels(ctx *ui.Context) []sendChan {
	if ctx.Store == nil {
		return nil
	}
	type row struct {
		sendChan
		at int64
	}
	var rows []row
	for id, ch := range ctx.Store.Channels {
		if ch == nil {
			continue
		}
		switch ch.Kind {
		case api.ChannelText, api.ChannelDM, api.ChannelGroupDM:
		default:
			continue
		}
		label, where := cleanText(ctx.Store.ChannelLabel(id)), ""
		if ch.Kind == api.ChannelText && ch.SpaceID != nil {
			if sp := ctx.Store.Spaces[*ch.SpaceID]; sp != nil {
				where = cleanText(sp.Name)
			}
		}
		var at int64
		if ch.LastMessageAt != nil {
			at = ch.LastMessageAt.UnixNano()
		}
		rows = append(rows, row{sendChan{id: id, label: label, where: where}, at})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].at != rows[j].at {
			return rows[i].at > rows[j].at
		}
		if rows[i].label != rows[j].label {
			return rows[i].label < rows[j].label
		}
		return rows[i].id < rows[j].id
	})
	out := make([]sendChan, len(rows))
	for i, r := range rows {
		out[i] = r.sendChan
	}
	return out
}

func (d *sendDialog) Init(*ui.Context) tea.Cmd           { return nil }
func (d *sendDialog) Resize(_ *ui.Context, size ui.Size) { d.size = size }
func (d *sendDialog) Modal() bool                        { return true }
func (d *sendDialog) Title(ctx *ui.Context) (string, ui.Badge) {
	return ctx.T.Archcanvas.SendTitle(), ui.Badge{}
}

// Bounds fits the conversation list or the whole preview (the app clamps it
// to the body).
func (d *sendDialog) Bounds(body layout.Rect) layout.Rect {
	w := min(sendW, body.W)
	rows := len(d.chans) + 4
	if d.step == 1 {
		rows = d.rows + 6 // a narrower dialog wraps the cost line once or twice more
	}
	return centeredRect(body, w, rows+2)
}

func (d *sendDialog) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	if p, ok := msg.(sendPinnedMsg); ok && p.dlg == d {
		d.busy = false
		if p.err != nil || p.no < 1 {
			d.hint = ctx.T.Archcanvas.CardVersionFailed()
			return nil
		}
		d.ref.versionNo = p.no
		return d.preview(ctx)
	}
	m, ok := msg.(sendDoneMsg)
	if !ok || m.dlg != d {
		return nil
	}
	d.busy = false
	if m.err != nil {
		d.hint = ctx.T.Archcanvas.SendFailed()
		return nil
	}
	return tea.Batch(closeOverlay, ui.Toast(ctx.T.Archcanvas.SendDone(m.label), ui.ToastSuccess))
}

func (d *sendDialog) HandleKey(ctx *ui.Context, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if d.busy {
		return true, nil
	}
	switch k.String() {
	case "j", "down":
		if d.step == 0 {
			d.cur = min(d.cur+1, max(len(d.chans)-1, 0))
		}
	case "k", "up":
		if d.step == 0 {
			d.cur = max(d.cur-1, 0)
		}
	case "s":
		if d.step == 0 {
			d.sim = !d.sim
		}
	case "esc":
		if d.step == 1 {
			d.step, d.hint = 0, ""
			return true, nil
		}
		return false, nil
	case "enter":
		if d.step == 0 {
			return true, d.preview(ctx)
		}
		return true, d.send(ctx)
	default:
		return false, nil
	}
	return true, nil
}

// build makes the card of the chosen kind.
func (d *sendDialog) build(ctx *ui.Context) error {
	var err error
	if d.sim {
		d.card, d.body, err = simCard(ctx.T, d.design, d.result, d.ref, nil)
	} else {
		d.card, d.body, err = designCard(ctx.T, d.design, d.result, d.ref)
	}
	return err
}

// preview goes to the preview; a card over 16KB is never shown nor sent.
func (d *sendDialog) preview(ctx *ui.Context) tea.Cmd {
	if len(d.chans) == 0 || d.design == nil {
		return nil
	}
	if d.ref.room && d.ref.versionNo == 0 {
		return d.pin(ctx)
	}
	if err := d.build(ctx); err != nil {
		if errors.Is(err, errCardTooBig) {
			return tea.Batch(closeOverlay, ui.Toast(ctx.T.Archcanvas.CardTooBig(), ui.ToastInfo))
		}
		d.hint = ctx.T.Archcanvas.SendFailed()
		return nil
	}
	d.step, d.hint = 1, ""
	d.rows = len(d.previewLines(ctx, sendW-2))
	return nil
}

// pin finds the version of the room document at the state the dialog saw:
// the newest one when it is that state, else a named version made now. With
// my edits still unsaved there is no such version yet.
func (d *sendDialog) pin(ctx *ui.Context) tea.Cmd {
	cl := ctx.API
	if d.unsaved {
		d.hint = ctx.T.Archcanvas.CardVersionUnsaved()
		return nil
	}
	if cl == nil {
		d.hint = ctx.T.Archcanvas.CardVersionFailed()
		return nil
	}
	docID, seq := d.ref.docID, d.seq
	name := ctx.T.Archcanvas.CardVersionName(cutCodePoints(cardTitle(ctx.T, d.design), 50))
	d.busy, d.hint = true, ""
	return func() tea.Msg {
		bg := context.Background()
		vs, err := cl.ListDocVersions(bg, docID)
		if err != nil {
			return sendPinnedMsg{dlg: d, err: err}
		}
		var newest *api.DocVersion
		for i := range vs {
			if newest == nil || vs[i].No > newest.No {
				newest = &vs[i]
			}
		}
		if newest != nil && newest.Seq == seq {
			return sendPinnedMsg{dlg: d, no: newest.No}
		}
		v, err := cl.CreateDocVersion(bg, docID, name)
		if err != nil {
			return sendPinnedMsg{dlg: d, err: err}
		}
		if v.Seq != seq {
			return sendPinnedMsg{dlg: d, err: errDocMoved}
		}
		return sendPinnedMsg{dlg: d, no: v.No}
	}
}

// send posts the previewed card.
func (d *sendDialog) send(ctx *ui.Context) tea.Cmd {
	cl := ctx.API
	if cl == nil || d.cur >= len(d.chans) {
		d.hint = ctx.T.Archcanvas.SendFailed()
		return nil
	}
	ch := d.chans[d.cur]
	c := d.card
	req := api.SendMessageRequest{Kind: api.MessageKindCard, Body: d.body, Card: &c}
	d.busy = true
	return func() tea.Msg {
		_, _, err := cl.SendMessage(context.Background(), ch.id, req)
		return sendDoneMsg{dlg: d, label: ch.label, err: err}
	}
}

// previewMessage is the card as the message chat will draw.
func (d *sendDialog) previewMessage() *api.Message {
	c := d.card
	return &api.Message{Kind: api.MessageKindCard, Body: d.body, Card: &c}
}

// previewLines is the card drawn as chat draws it at width w.
func (d *sendDialog) previewLines(ctx *ui.Context, w int) []cardLine {
	return cardLinesOf(ctx, d.previewMessage(), w)
}

// lines is the dialog's content at width w.
func (d *sendDialog) lines(ctx *ui.Context, w, h int) []formLine {
	a := ctx.T.Archcanvas
	var out []formLine
	if d.step == 1 {
		label := ""
		if d.cur < len(d.chans) {
			label = d.chans[d.cur].label
		}
		out = append(out, formLine{s: a.SendPreview(label), st: sAccBold, row: -1}, formLine{row: -1})
		for _, l := range d.previewLines(ctx, w) {
			out = append(out, formLine{s: l.s, st: l.st, row: -1})
		}
		out = append(out, formLine{row: -1})
		if d.hint != "" {
			out = append(out, formLine{s: d.hint, st: sWarn, row: -1})
		}
		return append(out, formLine{s: a.SendConfirmHint(), st: sDim, row: -1})
	}
	kind := a.SendKindDesign()
	if d.sim {
		kind = a.SendKindSim()
	}
	out = append(out, formLine{s: a.SendPick() + " · " + kind, st: sAccBold, row: -1})
	if len(d.chans) == 0 {
		out = append(out, formLine{s: a.SendNoChannels(), st: sDim, row: -1})
	}
	// the list scrolls to keep the cursor in view
	room := max(h-3, 1)
	if d.hint != "" {
		room = max(room-1, 1)
	}
	if d.cur < d.top {
		d.top = d.cur
	}
	if d.cur >= d.top+room {
		d.top = d.cur - room + 1
	}
	for i := d.top; i < len(d.chans) && i < d.top+room; i++ {
		st, mark := sBase, "  "
		if i == d.cur {
			st, mark = sSel, "▸ "
		}
		s := mark + d.chans[i].label
		if d.chans[i].where != "" {
			s += " · " + d.chans[i].where
		}
		out = append(out, formLine{s: s, st: st, row: i})
	}
	if d.hint != "" {
		out = append(out, formLine{s: d.hint, st: sWarn, row: -1})
	}
	return append(out, formLine{s: a.SendHint(), st: sDim, row: -1})
}

func (d *sendDialog) View(ctx *ui.Context, _ bool) string {
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
	lines := d.lines(ctx, w-2*pad, h)
	if len(lines) > h {
		lines = lines[:h]
	}
	for y, l := range lines {
		b.text(pad, y, textwidth.Truncate(l.s, w-2*pad, "…"), w-2*pad, l.st)
	}
	return b.render(ctx.Theme)
}
