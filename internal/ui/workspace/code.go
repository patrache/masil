package workspace

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/codeassist"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/workspace/editor"
	"github.com/patrache/masil/internal/workspace/ot"
	"github.com/patrache/masil/internal/workspace/share"
)

// The code cell (spec B6.3): the open documents, one shown, edited through
// the DocManager (author 0 = the owner) so the disk, the journal and (when
// sharing) everyone see the same sequence of edits.

type openDoc struct {
	rel      string
	seq      int64
	base     []rune // the text as of seq
	ed       *editor.Editor
	readOnly bool
	reason   string
	deleted  bool
	undo     ot.Undo
	// merged: an outside change was merged as edit mergeSeq; mergeInv puts
	// it back (u) while nothing came after it.
	merged   bool
	mergeSeq int64
	mergeInv ot.Op
}

type codeCell struct {
	docs []*openDoc
	cur  int
	msg  string // why the last chosen file did not open
	top  int    // rows above the editor (header and banner) as last drawn
}

// setupAssist gives a new document's editor its language (by the file's
// extension) and the other open documents' words for completion (local
// only: the menu is never sent; an accepted item is an ordinary edit).
func (c *codeCell) setupAssist(ctx *ui.Context, d *openDoc) {
	d.ed.SetLanguage(codeassist.FromFile(d.rel))
	d.ed.SetWordSource(func() []map[string]int {
		var out []map[string]int
		for _, o := range c.docs {
			if o != d {
				out = append(out, o.ed.Words())
			}
		}
		return out
	})
	c.applyAssist(ctx, d)
}

// applyAssist follows the settings' switches (code_highlight, code_complete)
// and the theme (it can change at run time).
func (c *codeCell) applyAssist(ctx *ui.Context, d *openDoc) {
	if ctx != nil {
		d.ed.SetTheme(ctx.Theme)
	}
	hl, cp := true, true
	if ctx != nil && ctx.Config != nil {
		hl, cp = ctx.Config.UI.CodeHighlight, ctx.Config.UI.CodeComplete
	}
	d.ed.SetAssist(hl, cp)
}

// current is the shown document (nil when none is open).
func (c *codeCell) current() *openDoc {
	if c.cur < 0 || c.cur >= len(c.docs) {
		return nil
	}
	return c.docs[c.cur]
}

func (c *codeCell) find(rel string) *openDoc {
	for _, d := range c.docs {
		if d.rel == rel {
			return d
		}
	}
	return nil
}

// openDoc opens rel in the code cell (or shows it again) and focuses it.
func (s *Surface) openDoc(ctx *ui.Context, rel string) tea.Cmd {
	if s.docs == nil {
		return nil
	}
	if d := s.code.find(rel); d != nil {
		s.code.cur = slices.Index(s.code.docs, d)
		s.code.msg = ""
		s.setFocus(ctx, PaneCode)
		return nil
	}
	v, err := s.docs.Open(rel, false)
	if err != nil {
		t := ctx.T.Workspace
		switch share.DocErrorCode(err) {
		case share.DocErrTooLarge:
			s.code.msg = t.CodeCannotOpenLarge()
		case share.DocErrBinary:
			s.code.msg = t.CodeCannotOpenBinary()
		default:
			s.code.msg = t.CodeCannotOpen()
		}
		s.code.cur = -1
		return nil
	}
	return s.attachDoc(ctx, v)
}

// attachDoc shows a document the DocManager already opened (one Open each).
func (s *Surface) attachDoc(ctx *ui.Context, v share.DocView) tea.Cmd {
	if d := s.code.find(v.Rel); d != nil { // opened twice: keep one Open
		s.docs.Close(v.Rel)
		s.resync(d)
		s.code.cur = slices.Index(s.code.docs, d)
		s.setFocus(ctx, PaneCode)
		return s.armPump(ctx)
	}
	d := &openDoc{rel: v.Rel, seq: v.Seq, base: []rune(v.Text), readOnly: v.ReadOnly, reason: v.Reason}
	d.ed = editor.New(ctx.Theme, v.Text, v.ReadOnly)
	s.code.setupAssist(ctx, d)
	s.code.docs = append(s.code.docs, d)
	s.showPeers(d.rel) // cursors that arrived before the owner opened it
	s.code.cur = len(s.code.docs) - 1
	s.code.msg = ""
	s.setFocus(ctx, PaneCode)
	return s.armPump(ctx)
}

// closeDoc drops the shown document (its unsaved edits stay in the
// DocManager and the journal until saved or discarded).
func (s *Surface) closeDoc(ctx *ui.Context, d *openDoc) {
	i := slices.Index(s.code.docs, d)
	if i < 0 {
		return
	}
	s.docs.Close(d.rel)
	s.code.docs = slices.Delete(s.code.docs, i, i+1)
	if s.code.cur >= len(s.code.docs) {
		s.code.cur = len(s.code.docs) - 1
	}
	if len(s.code.docs) == 0 {
		s.setFocus(ctx, PaneTree)
	}
}

// resync puts the DocManager's text back into the editor (a gap in the
// sequence, a rejected edit, a discard).
func (s *Surface) resync(d *openDoc) {
	text, seq, ok := s.docs.Text(d.rel)
	if !ok {
		return
	}
	d.seq, d.base = seq, []rune(text)
	d.ed.TakeOps()
	d.ed.Reset(text)
	d.undo = ot.Undo{}
	d.merged = false
}

// flushOps submits the editor's new edits as the owner's.
func (s *Surface) flushOps(d *openDoc) {
	for _, op := range d.ed.TakeOps() {
		if op.IsNoop() {
			continue
		}
		before := d.base
		if !s.submit(d, op) {
			return
		}
		d.undo.Record(op, before)
		d.merged = false
	}
}

// submit sends one owner edit made against d.seq; false (after a resync)
// when the DocManager did not take it as the very next edit.
func (s *Surface) submit(d *openDoc, op ot.Op) bool {
	seq, applied, err := s.docs.Submit(d.rel, 0, "", d.seq, op)
	if err != nil || seq != d.seq+1 {
		s.resync(d)
		return false
	}
	after, err := applied.Apply(d.base)
	if err != nil {
		s.resync(d)
		return false
	}
	d.seq, d.base = seq, after
	s.shiftCursors(d.rel, applied)
	return true
}

// applyRemote applies an edit someone else (or the disk) made as seq.
func (s *Surface) applyRemote(d *openDoc, seq int64, op ot.Op) {
	switch {
	case seq <= d.seq:
		return
	case seq != d.seq+1:
		s.resync(d)
		return
	}
	after, err := op.Apply(d.base)
	if err != nil {
		s.resync(d)
		return
	}
	d.ed.ApplyRemote(op)
	d.undo.Remote(op)
	d.seq, d.base = seq, after
	s.shiftCursors(d.rel, op)
}

// docEvents applies what happened to the open documents on their own.
func (s *Surface) docEvents(ctx *ui.Context, evs []share.DocEvent) tea.Cmd {
	for _, ev := range evs {
		switch e := ev.(type) {
		case share.DocDiskMerged:
			if d := s.code.find(e.Rel); d != nil {
				before := d.base
				s.applyRemote(d, e.Seq, e.Op)
				if d.seq == e.Seq {
					d.merged, d.mergeSeq, d.mergeInv = true, e.Seq, e.Op.Invert(before)
				}
			}
		case share.DocDeletedOnDisk:
			if d := s.code.find(e.Rel); d != nil {
				d.deleted = true
			}
		case share.DocRecreated:
			if d := s.code.find(e.Rel); d != nil {
				d.deleted = false
			}
		case share.DocSaved:
			if d := s.code.find(e.Rel); d != nil {
				d.deleted = false
			}
		}
	}
	return nil
}

// codeKey handles a key in the code cell.
func (s *Surface) codeKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	d := s.code.current()
	if d == nil {
		if msg.String() == "n" {
			s.openNewTerm(ctx, s.termMode())
			return true, nil
		}
		return false, nil
	}
	if d.merged && !d.ed.CapturesInput() {
		switch msg.String() {
		case "u":
			if d.seq == d.mergeSeq && d.mergeInv != nil {
				inv := d.mergeInv
				d.merged = false
				if s.submit(d, inv) {
					d.ed.ApplyRemote(inv)
					d.undo.Remote(inv)
				}
				return true, nil
			}
		case "d":
			d.merged = false
			s.docs.Discard(d.rel)
			s.resync(d)
			return true, nil
		}
	}
	s.code.applyAssist(ctx, d)
	handled, cmd := d.ed.HandleKey(msg)
	s.flushOps(d)
	s.sendPresence(d)
	if !handled {
		return msg.String() == "esc", nil // esc stays in the cell (no accidental leave)
	}
	if cmd == nil {
		return true, nil
	}
	// The editor's own requests are answered here, at once (no broadcast).
	switch m := cmd().(type) {
	case editor.UndoMsg:
		s.undoRedo(d, true)
	case editor.RedoMsg:
		s.undoRedo(d, false)
	case editor.SaveMsg:
		c := s.save(ctx, d)
		if m.Close {
			s.closeDoc(ctx, d)
		}
		return true, c
	case editor.CloseMsg:
		s.closeDoc(ctx, d)
	}
	return true, nil
}

// undoRedo applies the own-edits undo (or redo) as a local edit.
func (s *Surface) undoRedo(d *openDoc, undo bool) {
	if d.readOnly {
		return
	}
	var op ot.Op
	var ok bool
	if undo {
		op, ok = d.undo.Undo()
	} else {
		op, ok = d.undo.Redo()
	}
	if !ok || op == nil {
		return
	}
	if s.submit(d, op) {
		d.ed.ApplyRemote(op)
	}
}

// save writes d to disk (:w).
func (s *Surface) save(ctx *ui.Context, d *openDoc) tea.Cmd {
	t := ctx.T.Workspace
	if err := s.docs.Save(d.rel, 0); err != nil {
		return ui.Toast(t.SaveFailed(), ui.ToastError)
	}
	d.deleted = false
	return ui.Toast(t.Saved(textwidth.Sanitize(baseName(d.rel))), ui.ToastSuccess)
}

// paste types pasted text into the editor (insert mode only).
func (c *codeCell) paste(ctx *ui.Context, s *Surface, text string) {
	d := c.current()
	if d == nil || d.readOnly || d.ed.Mode() != "insert" {
		return
	}
	d.ed.Paste(textwidth.SanitizeInput(text))
	s.flushOps(d)
}

// dirty reports whether rel has unsaved edits.
func (s *Surface) dirty(rel string) bool {
	if s.docs == nil { // a participant: the owner keeps the unsaved state
		return false
	}
	return slices.Contains(s.docs.Dirty(), rel)
}

// view draws the code cell: a header (the path and its state), the disk
// banner when there is one, then the editor.
func (c *codeCell) view(ctx *ui.Context, s *Surface, w, h int, focused bool) []string {
	th := ctx.Theme
	t := ctx.T.Workspace
	dim := lipgloss.NewStyle().Foreground(th.P.Dim)
	d := c.current()
	if d == nil {
		lines := []string{dim.Render(strings.Repeat("─", w)), ""}
		msg := t.CodeEmptyHint()
		if c.msg != "" {
			msg = c.msg
		}
		for _, l := range textwidth.Wrap(msg, max(w-2, 1)) {
			lines = append(lines, " "+dim.Render(l))
		}
		return lines
	}
	var marks []string
	switch {
	case d.deleted:
		marks = append(marks, t.CodeDeletedOnDisk())
	case s.dirty(d.rel):
		marks = append(marks, t.CodeUnsaved())
	}
	switch d.reason {
	case share.DocReasonLarge:
		marks = append(marks, t.CodeReadonlyLarge())
	case share.DocReasonMixedEOL:
		marks = append(marks, t.CodeReadonlyMixedEol())
	}
	tail := strings.Join(marks, " · ")
	if n := len(c.docs); n > 1 {
		tail = strings.TrimPrefix(tail+" · "+strconv.Itoa(c.cur+1)+"/"+strconv.Itoa(n), " · ")
	}
	name := textwidth.Sanitize(d.rel)
	room := w - 2 - textwidth.Width(tail)
	if tail != "" {
		room -= 2
	}
	head := " " + textwidth.TruncateLeft(name, max(room, 1), "…")
	style := dim
	if focused {
		style = th.S.Title
	}
	line := style.Render(head)
	if tail != "" {
		line = textwidth.PadRight(line, max(w-textwidth.Width(tail)-1, 0)) + th.S.Warning.Render(tail)
	}
	lines := []string{line}
	if d.merged {
		for _, l := range textwidth.Wrap(t.DiskMerged(), max(w-2, 1)) {
			lines = append(lines, " "+th.S.Warning.Render(l))
		}
	}
	c.top = len(lines)
	c.applyAssist(ctx, d)
	eh := max(h-len(lines), 0)
	d.ed.Resize(w, eh)
	if eh > 0 {
		lines = append(lines, strings.Split(d.ed.View(focused), "\n")...)
	}
	return lines
}
