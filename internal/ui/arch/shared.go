package arch

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
)

// More read-only reasons (a room document).
const (
	roKicked readOnlyReason = iota + roBroken + 1 // the server closed the document for me
	roLocked                                      // locked, and I am not its creator or a space admin
)

// sharedSink is a room document (P3 Task 22, CollabHub type arch): edits go
// to the collab session as ops (optimistic: the view has them at once), and
// remote changes arrive as collabEventMsg. objs is the session's view as
// design objects, kept in step with it.
type sharedSink struct {
	s     *collab.Session
	docID string
	objs  archdoc.Objects

	meta     *api.Doc // GetDoc (nil until it answers)
	online   bool     // the socket is live
	everLive bool     // it was live once (before that the header says "connecting")
	kicked   bool
	locked   bool // locked and I may not edit it
	peers    []collab.Presence

	lastOverwrite *collab.EventOverwritten // the last of my values someone else replaced
	submits       int                      // Submit calls (tests)
	presenceSent  int                      // drag presence frames handed to the session (tests)
	sentSel       []string                 // the selection last sent as presence
	sentSelOK     bool

	preview *versionPreview // a saved version shown instead (arch.versions; esc ends it)
}

// collabEventMsg carries a session event of canvas instance to Update
// (collab.Options.OnEvent runs on the session's goroutine).
type collabEventMsg struct {
	instance string
	E        collab.Event
}

// docMetaMsg is GetDoc's answer for instance.
type docMetaMsg struct {
	instance string
	doc      *api.Doc
	err      error
}

func (s *sharedSink) Objects() archdoc.Objects {
	if s.preview != nil {
		return s.preview.objs
	}
	return s.objs
}

func (s *sharedSink) ReadOnly() (bool, readOnlyReason) {
	switch {
	case s.preview != nil:
		return true, roPreview
	case s.kicked:
		return true, roKicked
	case s.locked:
		return true, roLocked
	}
	return false, roNone
}

func (s *sharedSink) Status() saveState {
	if s.s != nil && len(s.s.Pending()) > 0 {
		return savePending
	}
	return saveSaved
}

func (s *sharedSink) Apply(ch []archdoc.Change) tea.Cmd {
	if len(ch) == 0 || s.s == nil {
		return nil
	}
	ops := changeToOps(ch)
	if len(ops) == 0 {
		return nil
	}
	s.objs = s.objs.Apply(ch)
	// one frame carries at most collab.OpsMax ops
	for len(ops) > 0 {
		n := min(len(ops), collab.OpsMax)
		s.s.Submit(ops[:n])
		s.submits++
		ops = ops[n:]
	}
	return nil
}

// close ends the session (the canvas goes, or the server closed it for me).
func (s *sharedSink) close() {
	if s.s != nil {
		s.s.Close()
	}
}

// changeToOps turns design changes into collab ops (arch.md 2.2).
func changeToOps(ch []archdoc.Change) []collab.Op {
	var out []collab.Op
	for _, c := range ch {
		switch c.Kind {
		case archdoc.ChangePut:
			var fields map[string]json.RawMessage
			if json.Unmarshal(c.Value, &fields) != nil {
				continue
			}
			out = append(out, collab.Op{Op: collab.OpPut, ID: c.Obj, Type: archdoc.ObjType(c.Obj), Fields: fields})
		case archdoc.ChangeSet:
			v := c.Value
			if v == nil {
				v = json.RawMessage("null") // the field is cleared (read back as its default)
			}
			out = append(out, collab.Op{Op: collab.OpSet, ID: c.Obj, Field: c.Field, Value: v})
		case archdoc.ChangeDel:
			out = append(out, collab.Op{Op: collab.OpDel, ID: c.Obj})
		case archdoc.ChangeRestore:
			out = append(out, collab.Op{Op: collab.OpUndel, ID: c.Obj})
		}
	}
	return out
}

// objectsOf is the document's live objects as design objects (a null field
// is a cleared one and left out).
func objectsOf(d *collab.Doc) archdoc.Objects {
	out := archdoc.Objects{}
	if d == nil {
		return out
	}
	for _, o := range d.Snapshot().Objects {
		if o.Deleted {
			continue
		}
		f := make(map[string]json.RawMessage, len(o.Fields))
		for k, v := range o.Fields {
			if string(v) != "null" {
				f[k] = v
			}
		}
		out[o.ID] = f
	}
	return out
}

// snapshotOf is a design as the first state of a room document.
func snapshotOf(d *archdoc.Design) collab.Snapshot {
	objs := d.ToObjects()
	keys := make([]string, 0, len(objs))
	for k := range objs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	s := collab.Snapshot{Objects: []collab.Object{}}
	for _, k := range keys {
		s.Objects = append(s.Objects, collab.Object{ID: k, Type: archdoc.ObjType(k), Fields: objs[k]})
	}
	return s
}

// myID is the signed-in user's ID ("" = unknown).
func myID(ctx *ui.Context) string {
	if me := ctx.Me(); me != nil {
		return me.ID
	}
	return ""
}

// openShared starts the session of room document docID and asks for its
// metadata (title, lock, creator).
func (m *canvasModel) openShared(ctx *ui.Context, docID string) tea.Cmd {
	inst := m.route.ID
	post := ctx.Post
	s := collab.Open(context.Background(), collab.Options{
		API:    ctx.API,
		DocID:  docID,
		UserID: myID(ctx),
		Dial:   ctx.CollabDial,
		OnEvent: func(e collab.Event) {
			if post != nil {
				post.Post(collabEventMsg{instance: inst, E: e})
			}
		},
	})
	m.setSink(ctx, &sharedSink{s: s, docID: docID, objs: objectsOf(s.View())})
	return m.fetchMeta(ctx, docID)
}

// fetchMeta asks for room document docID's metadata (docMetaMsg; a failure
// leaves the old metadata, and arch.send_card asks again).
func (m *canvasModel) fetchMeta(ctx *ui.Context, docID string) tea.Cmd {
	cl, inst := ctx.API, m.route.ID
	if cl == nil {
		return nil
	}
	return func() tea.Msg {
		d, err := cl.GetDoc(context.Background(), docID)
		return docMetaMsg{instance: inst, doc: d, err: err}
	}
}

// canEditLocked reports whether I may edit locked document d: its creator, or
// an owner or admin of its space.
func canEditLocked(ctx *ui.Context, d *api.Doc) bool {
	if d.CreatedBy.ID != "" && d.CreatedBy.ID == myID(ctx) {
		return true
	}
	if ctx.Store != nil {
		if sp := ctx.Store.Spaces[d.SpaceID]; sp != nil {
			return sp.MyRole == api.RoleOwner || sp.MyRole == api.RoleAdmin
		}
	}
	return false
}

// setMeta takes the document's metadata (GetDoc, doc.updated).
func (m *canvasModel) setMeta(ctx *ui.Context, sh *sharedSink, d *api.Doc) {
	sh.meta = d
	sh.locked = d.Locked && !canEditLocked(ctx, d)
	if sh.locked {
		m.cancelGesture()
		m.mode, m.menu, m.faults = modeNormal, nil, nil
	}
}

// onCollab handles one session event of a room document.
func (m *canvasModel) onCollab(ctx *ui.Context, e collab.Event) tea.Cmd {
	sh, ok := m.sink.(*sharedSink)
	if !ok {
		return nil
	}
	a := ctx.T.Archcanvas
	switch e := e.(type) {
	case collab.EventState:
		sh.online = e.Connected
		if e.Connected {
			sh.everLive = true
		}
	case collab.EventChanged:
		if !e.Remote {
			return nil // my own edit: the view already has it
		}
		if len(sh.objs) == 0 {
			m.placed = false // the first state arrived: fit the view to it
		}
		sh.objs = objectsOf(sh.s.View())
		return m.refresh(ctx)
	case collab.EventRejected:
		sh.objs = objectsOf(sh.s.View())
		cmd := m.refresh(ctx)
		if e.Code == "locked" || e.Code == "forbidden" {
			sh.locked = true
			return tea.Batch(cmd, ui.Toast(a.LockedNotice(), ui.ToastInfo))
		}
		return tea.Batch(cmd, ui.Toast(rejectText(ctx.T, e.Code, e.Detail), ui.ToastError))
	case collab.EventOverwritten:
		sh.lastOverwrite = &e
		if e.Field == "pos" || e.Field == "rect" {
			return ui.Toast(a.MovedBy(personName(ctx, e.By)), ui.ToastInfo)
		}
	case collab.EventPresence:
		sh.peers = e.Peers
	case collab.EventKicked:
		sh.kicked, sh.online = true, false
		sh.close()
		m.cancelGesture()
		m.mode, m.menu, m.faults = modeNormal, nil, nil
		cmds := []tea.Cmd{ui.Toast(a.KickedNotice(), ui.ToastAlert)}
		if len(e.Pending) > 0 {
			cmds = append(cmds, ui.Toast(a.KickedPending(len(e.Pending)), ui.ToastInfo))
		}
		return tea.Batch(cmds...)
	}
	return nil
}

// onDocEvent follows doc.updated for this document (lock, delete).
func (m *canvasModel) onDocEvent(ctx *ui.Context, ev realtime.Event) tea.Cmd {
	sh, ok := m.sink.(*sharedSink)
	if !ok || ev.Type != "doc.updated" {
		return nil
	}
	v, err := ev.Decode()
	if err != nil {
		return nil
	}
	du, ok := v.(realtime.DocUpdatedEvent)
	if !ok || du.Doc.ID != sh.docID {
		return nil
	}
	if du.Deleted {
		return m.onCollab(ctx, collab.EventKicked{Reason: "doc_deleted", Pending: sh.s.Pending()})
	}
	d := du.Doc
	m.setMeta(ctx, sh, &d)
	return nil
}

// rejectText is a refused edit in words (reject.detail from the arch
// validator, arch.md 2.4).
func rejectText(t *i18n.T, code, detail string) string {
	a := t.Archcanvas
	switch detail {
	case "arch_cycle":
		return a.RejectArchCycle()
	case "arch_depth":
		return a.RejectArchDepth()
	case "arch_limit":
		return a.RejectArchLimit()
	case "arch_bad_value", "arch_bad_field", "arch_bad_object":
		return a.RejectArchValue()
	}
	switch code {
	case "too_large":
		return a.RejectTooLarge()
	case "limit_exceeded":
		return a.RejectArchLimit()
	}
	return a.RejectOther(cleanText(code))
}

// readOnlyText is why an edit key does nothing.
func readOnlyText(t *i18n.T, why readOnlyReason) string {
	switch why {
	case roKicked:
		return t.Archcanvas.KickedNotice()
	case roLocked:
		return t.Archcanvas.LockedNotice()
	case roPreview:
		return t.Archcanvas.PreviewReadonly()
	}
	return t.Archcanvas.ReadOnlyCopy()
}

// roomHeader is a room document's state on the header line.
func (m *canvasModel) roomHeader(ctx *ui.Context, sh *sharedSink) string {
	a := ctx.T.Archcanvas
	switch {
	case sh.preview != nil:
		return a.PreviewHeader(sh.preview.label)
	case sh.kicked:
		return a.RoomKicked()
	case sh.locked:
		return a.RoomLocked()
	case !sh.online && sh.everLive:
		return a.RoomOffline()
	case !sh.online:
		return a.RoomConnecting()
	}
	return a.RoomLive(m.peopleLine(ctx, sh))
}

// saveCopy is arch.save_copy: the design as a new personal design.
func (m *canvasModel) saveCopy(ctx *ui.Context) tea.Cmd {
	a := ctx.T.Archcanvas
	if _, ok := m.sink.(*localSink); ok {
		return ui.Toast(a.CopyNotNeeded(), ui.ToastInfo)
	}
	st, err := storeOf(ctx)
	if err != nil {
		return ui.Toast(a.FilesFailed(), ui.ToastError)
	}
	d, _ := archdoc.FromObjects(m.sink.Objects(), nil)
	if d == nil {
		return ui.Toast(a.FilesFailed(), ui.ToastError)
	}
	name, err := saveNew(st, d, m.title(ctx))
	if err != nil {
		return ui.Toast(a.FilesFailed(), ui.ToastError)
	}
	return ui.Toast(a.CopySaved(cleanText(name)), ui.ToastSuccess)
}

// errNoName: every candidate name was taken.
var errNoName = errors.New("arch: no free design name")

// saveNew saves d as a new personal design named after the first usable
// base (made file-safe), adding " (2)" … to stay clear of existing ones.
func saveNew(st *archdoc.Store, d *archdoc.Design, bases ...string) (string, error) {
	for _, cand := range append(bases, "arch") {
		cand = fileSafe(cand)
		if cand == "" {
			continue
		}
		for i := 1; i <= 99; i++ {
			n := cand
			if i > 1 {
				n = cand + " (" + strconvItoa(i) + ")"
			}
			if _, err := archdoc.SafeName(n); err != nil {
				break
			}
			switch err := st.Save(n, d, false); {
			case err == nil:
				return n, nil
			case errors.Is(err, archdoc.ErrExists):
				continue
			default:
				return "", err
			}
		}
	}
	return "", errNoName
}

// showVersion puts a saved version on the canvas (nil = back to now).
func (m *canvasModel) showVersion(ctx *ui.Context, sh *sharedSink, p *versionPreview) tea.Cmd {
	sh.preview = p
	m.cancelGesture()
	m.mode, m.menu, m.faults = modeNormal, nil, nil
	return m.refresh(ctx)
}
