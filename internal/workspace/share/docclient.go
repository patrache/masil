package share

import (
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/patrache/masil/internal/workspace/ot"
	"github.com/patrache/masil/internal/workspace/wire"
)

// RemoteDoc is a participant's copy of one owner document: the OT client
// (one outstanding edit, the rest buffered) and own-edits-only undo.
type RemoteDoc struct {
	rel      string
	client   *ot.Client
	undo     ot.Undo
	readOnly bool
	reason   string
	resync   bool // reconnect: waiting for doc_state with pending_seq
	reload   bool // after doc_reject: waiting for a fresh doc_state
	// sentAt is when the outstanding edit (or the resync/reload request)
	// went out; zero when nothing waits.
	sentAt time.Time
}

// docAnswerWait: an edit or document request unanswered this long is asked
// again through a resync (the hub may have dropped it).
const docAnswerWait = 5 * time.Second

func (g *Guest) idPrefix() string { return "s" + strconv.Itoa(int(g.seat)) }

// OpenDoc asks the owner for rel; EventDocState answers (Error set when it
// cannot be opened). Opening an open document again reloads it from the
// owner (EventDocReset), dropping my unconfirmed edits.
func (g *Guest) OpenDoc(rel string) error {
	g.mu.Lock()
	err := g.sendLevel(wire.LevelViewer, wire.KindDocOpen, mustJSON(wire.DocOpen{Doc: rel}))
	if rd := g.docs[rel]; err == nil && rd != nil {
		rd.client.Resync()
		rd.resync, rd.reload, rd.sentAt = false, true, g.now()
	}
	g.mu.Unlock()
	g.flushManual()
	return err
}

// CloseDoc tells the owner I closed rel.
func (g *Guest) CloseDoc(rel string) error {
	g.mu.Lock()
	delete(g.docs, rel)
	err := g.sendLevel(wire.LevelViewer, wire.KindDocClose, mustJSON(wire.DocClose{Doc: rel}))
	g.mu.Unlock()
	g.flushManual()
	return err
}

// DocText is my current copy of rel.
func (g *Guest) DocText(rel string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	rd := g.docs[rel]
	if rd == nil {
		return "", false
	}
	return string(rd.client.Text()), true
}

// Edit applies my edit op to rel at once and sends it (editor level).
func (g *Guest) Edit(rel string, op ot.Op) error {
	g.mu.Lock()
	err := g.editLocked(rel, op, true)
	g.mu.Unlock()
	g.flushManual()
	return err
}

func (g *Guest) editLocked(rel string, op ot.Op, record bool) error {
	if g.over {
		return ErrEnded
	}
	if g.phase != phaseGranted || g.level < wire.LevelEditor {
		return ErrLevel
	}
	rd := g.docs[rel]
	if rd == nil {
		return ErrNotOpen
	}
	if rd.readOnly {
		return ErrReadOnly
	}
	before := rd.client.Text()
	p, err := rd.client.Local(op)
	if err != nil {
		return err
	}
	if record {
		rd.undo.Record(op, before)
	}
	g.sendPending(rd, p)
	return nil
}

func (g *Guest) sendPending(rd *RemoteDoc, p *ot.Pending) {
	if p == nil {
		return
	}
	rd.sentAt = g.now()
	_ = g.sendOwner(wire.KindDocOp, mustJSON(wire.DocOp{Doc: rd.rel, ClientOpID: p.ClientOpID, BaseSeq: p.BaseSeq, Ops: p.Op}))
}

// Undo reverts my latest edit still visible in rel (never someone else's)
// and returns the operation to apply to my view.
func (g *Guest) Undo(rel string) (ot.Op, bool) { return g.undoRedo(rel, false) }

// Redo re-applies my latest undone edit.
func (g *Guest) Redo(rel string) (ot.Op, bool) { return g.undoRedo(rel, true) }

func (g *Guest) undoRedo(rel string, redo bool) (ot.Op, bool) {
	g.mu.Lock()
	defer g.flushManual()
	defer g.mu.Unlock()
	rd := g.docs[rel]
	if rd == nil {
		return nil, false
	}
	var op ot.Op
	var ok bool
	if redo {
		op, ok = rd.undo.Redo()
	} else {
		op, ok = rd.undo.Undo()
	}
	if !ok {
		return nil, false
	}
	if err := g.editLocked(rel, op, false); err != nil {
		return nil, false
	}
	return op, true
}

// Save asks the owner to write rel to disk (editor level).
func (g *Guest) Save(rel string) error {
	g.mu.Lock()
	err := g.sendLevel(wire.LevelEditor, wire.KindDocSave, mustJSON(wire.DocSave{Doc: rel}))
	g.mu.Unlock()
	g.flushManual()
	return err
}

// Presence shows my cursor and selection in rel to the others.
func (g *Guest) Presence(rel string, cursor, selFrom, selTo int) {
	g.mu.Lock()
	_ = g.sendLevel(wire.LevelViewer, wire.KindDocPresence, mustJSON(wire.DocPresence{Doc: rel, Seat: g.seat, Cursor: cursor, SelFrom: selFrom, SelTo: selTo}))
	g.mu.Unlock()
	g.flushManual()
}

// ---- incoming (g.mu held) ----

func (g *Guest) onDocState(m wire.DocState) {
	if m.Error != "" {
		g.emit(EventDocState{DocState: m})
		return
	}
	text := []rune(m.Text)
	rd := g.docs[m.Doc]
	switch {
	case rd != nil && rd.resync:
		var p *ot.Pending
		var err error
		if m.PendingSeq == 0 {
			p, err = rd.client.Reset(m.Seq, text)
		} else {
			p, err = rd.client.ResetAcked(m.Seq, text)
		}
		if err != nil {
			rd.client.Drop(m.Seq, text)
			p = nil
		}
		rd.resync, rd.undo, rd.sentAt = false, ot.Undo{}, time.Time{}
		rd.readOnly, rd.reason = m.ReadOnly, m.Reason
		g.emit(EventDocReset{Doc: m.Doc, Text: string(rd.client.Text())})
		g.sendPending(rd, p)
	case rd != nil && rd.reload:
		rd.client.Drop(m.Seq, text)
		rd.reload, rd.undo, rd.sentAt = false, ot.Undo{}, time.Time{}
		rd.readOnly, rd.reason = m.ReadOnly, m.Reason
		g.emit(EventDocReset{Doc: m.Doc, Text: m.Text})
	case rd != nil:
		// A second answer to a request already answered (asked again on
		// resume or after a timeout): my copy is newer, keep it.
	default:
		g.docs[m.Doc] = &RemoteDoc{rel: m.Doc, client: ot.NewClient(g.idPrefix(), m.Seq, text), readOnly: m.ReadOnly, reason: m.Reason}
		g.emit(EventDocState{DocState: m})
	}
}

func (g *Guest) onDocAck(m wire.DocAck) {
	rd := g.docs[m.Doc]
	if rd == nil || rd.resync || rd.reload {
		return
	}
	p, err := rd.client.Ack(m.ClientOpID, m.Seq)
	if err != nil {
		return // not mine (a stale or forged ack); the client is unchanged
	}
	rd.sentAt = time.Time{}
	g.sendPending(rd, p)
}

func (g *Guest) onDocBcast(m wire.DocBcast) {
	rd := g.docs[m.Doc]
	if rd == nil || rd.resync || rd.reload || m.Author == int(g.seat) {
		return // my own edits are confirmed by doc_ack
	}
	switch seq := rd.client.Seq(); {
	case m.Seq <= seq:
		return
	case m.Seq != seq+1:
		g.resyncDoc(rd) // missed an edit
		return
	}
	op, err := rd.client.Remote(m.Seq, m.Ops)
	if err != nil {
		g.resyncDoc(rd)
		return
	}
	rd.undo.Remote(op)
	g.emit(EventDocRemote{Doc: m.Doc, Op: op})
}

func (g *Guest) onDocReject(m wire.DocReject) {
	rd := g.docs[m.Doc]
	if rd == nil {
		return
	}
	g.emit(EventDocRejected{DocReject: m})
	rd.client.Resync() // ignore acks and edits until the fresh state
	rd.reload, rd.sentAt = true, g.now()
	_ = g.sendOwner(wire.KindDocOpen, mustJSON(wire.DocOpen{Doc: m.Doc}))
}

// resyncDoc asks for rel again, telling the owner which edit still waits
// for its ack (contract 7, reconnect).
func (g *Guest) resyncDoc(rd *RemoteDoc) {
	if rd.resync || rd.reload {
		return
	}
	id := rd.client.Resync()
	rd.resync, rd.sentAt = true, g.now()
	_ = g.sendOwner(wire.KindDocOpen, mustJSON(wire.DocOpen{Doc: rd.rel, Pending: id}))
}

// tickDocs asks again for documents whose edit or request went unanswered.
func (g *Guest) tickDocs(now time.Time) {
	if g.phase != phaseGranted {
		return
	}
	for _, rel := range slices.Sorted(maps.Keys(g.docs)) {
		rd := g.docs[rel]
		if rd.sentAt.IsZero() || now.Sub(rd.sentAt) < docAnswerWait {
			continue
		}
		if rd.reload {
			rd.sentAt = now
			_ = g.sendOwner(wire.KindDocOpen, mustJSON(wire.DocOpen{Doc: rd.rel}))
			continue
		}
		rd.resync = false
		g.resyncDoc(rd)
	}
}

// afterResume re-syncs every open document (frames sent while away may be
// lost).
func (g *Guest) afterResume() {
	for _, rel := range slices.Sorted(maps.Keys(g.docs)) {
		rd := g.docs[rel]
		rd.resync, rd.reload = false, false
		g.resyncDoc(rd)
	}
}
