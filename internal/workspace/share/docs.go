package share

import (
	"errors"
	"io/fs"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/fsroot"
	"github.com/patrache/masil/internal/workspace/journal"
	"github.com/patrache/masil/internal/workspace/ot"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Document limits and timing (spec B7.3-B7.6).
const (
	MaxOpenDocs   = 20
	DiskPoll      = 500 * time.Millisecond
	AutosaveDelay = 2 * time.Second
	// journalDelay batches recovery-journal writes.
	journalDelay = time.Second
)

// Open failure codes (doc_state.error) and read-only reasons
// (doc_state.reason).
const (
	DocErrDenied   = "denied"
	DocErrEscape   = "escape"
	DocErrIgnored  = "ignored"
	DocErrInvalid  = "invalid"
	DocErrAbsolute = "absolute"
	DocErrNotFound = "not_found"
	DocErrTooLarge = "too_large"
	DocErrBinary   = "binary"
	DocErrTooMany  = "too_many"
	DocErrFailed   = "unreadable"

	DocReasonLarge    = "large"
	DocReasonMixedEOL = "mixed_eol"
)

// doc_reject codes (workspace.md 7).
const (
	RejectStaleBase  = "stale_base"
	RejectFutureBase = "future_base"
	RejectBaseLen    = "base_len"
	RejectLevel      = "level"
	RejectReadOnly   = "read_only"
	RejectTooLarge   = "too_large"
	RejectNotOpen    = "not_open"
)

var (
	ErrTooManyDocs = errors.New("share: too many open documents")
	ErrReadOnly    = errors.New("share: document is read-only")
	ErrDocTooLarge = errors.New("share: document would grow past the shared-editing limit")
	ErrNotOpen     = errors.New("share: document is not open")
)

// OpenError is a document that cannot be opened; Code is a DocErr* value.
type OpenError struct {
	Code string
	Err  error
}

func (e *OpenError) Error() string { return "share: cannot open document (" + e.Code + ")" }
func (e *OpenError) Unwrap() error { return e.Err }

// DocErrorCode maps an Open/Check error to its DocErr* code.
func DocErrorCode(err error) string {
	var oe *OpenError
	if errors.As(err, &oe) {
		return oe.Code
	}
	var pe *fsroot.PathError
	if errors.As(err, &pe) {
		switch pe.Reason {
		case fsroot.ReasonEscape:
			return DocErrEscape
		case fsroot.ReasonAbsolute:
			return DocErrAbsolute
		case fsroot.ReasonDenied:
			return DocErrDenied
		case fsroot.ReasonIgnored:
			return DocErrIgnored
		default:
			return DocErrInvalid
		}
	}
	if errors.Is(err, ErrTooManyDocs) {
		return DocErrTooMany
	}
	if errors.Is(err, fs.ErrNotExist) {
		return DocErrNotFound
	}
	return DocErrFailed
}

// DocView is a document as opened.
type DocView struct {
	Rel      string
	Seq      int64
	Text     string
	ReadOnly bool
	Reason   string
}

// DocEvent is something that happened to an open document on its own (disk
// changes, saves).
type DocEvent interface{ docEvent() }

// DocDiskMerged: an outside change was merged as edit Seq (author disk).
type DocDiskMerged struct {
	Rel string
	Seq int64
	Op  ot.Op
}

// DocDeletedOnDisk: the file went away; saving creates it again.
type DocDeletedOnDisk struct{ Rel string }

// DocSaved: the document was written (By = seat, 0 = owner).
type DocSaved struct {
	Rel string
	By  int
}

// DocRecreated: a deleted file exists again (saved or put back outside).
type DocRecreated struct{ Rel string }

func (DocDiskMerged) docEvent()    {}
func (DocDeletedOnDisk) docEvent() {}
func (DocSaved) docEvent()         {}
func (DocRecreated) docEvent()     {}

// doc is one document's real state on the owner's PC.
type doc struct {
	rel        string
	seq        *ot.Sequencer
	disk       *ot.DiskTracker
	info       fsroot.FileInfo
	bytes      int // UTF-8 size of the current text
	readOnly   bool
	reason     string
	refs       int
	deleted    bool
	dirtyBy    map[int]bool
	lastEdit   time.Time
	journalDue time.Time
}

func (d *doc) view() DocView {
	return DocView{Rel: d.rel, Seq: d.seq.Seq(), Text: string(d.seq.Text()), ReadOnly: d.readOnly, Reason: d.reason}
}

func (d *doc) dirty() bool { return d.disk.Dirty() || d.deleted }

// seqHook is told every sequenced edit (with the DocManager lock held).
type seqHook func(rel string, seq int64, author int, clientOpID string, op ot.Op)

// DocManager is the owner PC's documents: the single sequencer of each open
// file, disk merging, saving and the recovery journal. The solo workspace
// uses it directly; while sharing, the Host drives Tick and relays every
// sequenced edit. Safe for concurrent use.
type DocManager struct {
	mu       sync.Mutex
	root     *fsroot.Root
	j        *journal.Journal
	clock    Clock
	watcher  *fsroot.Watcher
	docs     map[string]*doc
	autosave bool
	lastPoll time.Time
	events   []DocEvent
	onSeq    seqHook
	onDisk   func(rel string) // told before an outside change of rel is merged
	hookBy   *Host            // the host onSeq and onDisk belong to
}

// NewDocManager manages the documents of root; j may be nil (no recovery
// journal).
func NewDocManager(root *fsroot.Root, j *journal.Journal, clock Clock) *DocManager {
	if clock == nil {
		clock = wallClock{}
	}
	return &DocManager{root: root, j: j, clock: clock, watcher: fsroot.NewWatcher(root), docs: map[string]*doc{}}
}

func (m *DocManager) setHook(by *Host, f seqHook, onDisk func(rel string)) {
	m.mu.Lock()
	m.onSeq, m.onDisk, m.hookBy = f, onDisk, by
	m.mu.Unlock()
}

// clearHook removes by's hook (a later host's hook stays).
func (m *DocManager) clearHook(by *Host) {
	m.mu.Lock()
	if m.hookBy == by {
		m.onSeq, m.onDisk, m.hookBy = nil, nil, nil
	}
	m.mu.Unlock()
}

// Open opens rel (forPeer applies the deny list and ignore rules) and
// returns its current state. Each Open is matched by one Close.
func (m *DocManager) Open(rel string, forPeer bool) (DocView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	clean, err := m.root.Check(rel, forPeer)
	if !forPeer {
		var pe *fsroot.PathError
		if errors.As(err, &pe) && pe.Reason == fsroot.ReasonDenied {
			// The owner may open their own secret files; only peers may not.
			clean, err = cleanOwnerPath(m.root, rel)
		}
	}
	if err != nil {
		return DocView{}, err
	}
	if d := m.docs[clean]; d != nil {
		d.refs++
		return d.view(), nil
	}
	if len(m.docs) >= MaxOpenDocs {
		return DocView{}, &OpenError{Code: DocErrTooMany, Err: ErrTooManyDocs}
	}
	text, info, err := m.root.ReadText(clean)
	if err != nil {
		return DocView{}, openError(info, err)
	}
	d := m.load(clean, text, info)
	d.refs = 1
	return d.view(), nil
}

// cleanOwnerPath cleans an owner path without the deny list (the owner's
// file functions refuse only escapes).
func cleanOwnerPath(r *fsroot.Root, rel string) (string, error) {
	_, info, err := r.ReadText(rel)
	if info.Rel != "" {
		return info.Rel, nil
	}
	return "", err
}

func openError(info fsroot.FileInfo, err error) error {
	switch {
	case errors.Is(err, fsroot.ErrNotText) && info.Kind == fsroot.KindTooLarge:
		return &OpenError{Code: DocErrTooLarge, Err: err}
	case errors.Is(err, fsroot.ErrNotText):
		return &OpenError{Code: DocErrBinary, Err: err}
	case errors.Is(err, fs.ErrNotExist):
		return &OpenError{Code: DocErrNotFound, Err: err}
	}
	var pe *fsroot.PathError
	if errors.As(err, &pe) {
		return err
	}
	return &OpenError{Code: DocErrFailed, Err: err}
}

func (m *DocManager) load(rel, text string, info fsroot.FileInfo) *doc {
	rs := []rune(text)
	d := &doc{rel: rel, seq: ot.NewSequencer(rs), disk: ot.NewDiskTracker(rs), info: info, bytes: len(text), dirtyBy: map[int]bool{}}
	switch info.Kind {
	case fsroot.KindReadOnlyLarge:
		d.readOnly, d.reason = true, DocReasonLarge
	case fsroot.KindReadOnlyMixedEOL:
		d.readOnly, d.reason = true, DocReasonMixedEOL
	}
	m.docs[rel] = d
	m.watcher.Track(rel, info.FP)
	return d
}

// View is the current state of an open document.
func (m *DocManager) View(rel string) (DocView, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.docs[rel]
	if d == nil {
		return DocView{}, false
	}
	return d.view(), true
}

// PendingSeq is the seq author's clientOpID was sequenced at in rel (0 when
// it was not, or the id is empty) — doc_state.pending_seq after a reconnect.
func (m *DocManager) PendingSeq(rel string, author int, clientOpID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.docs[rel]
	if d == nil || clientOpID == "" {
		return 0
	}
	seq, _ := d.seq.AppliedID(author, clientOpID)
	return seq
}

// Submit sequences author's edit op (made against baseSeq) of rel. author
// is the sender's seat (0 = owner). A clientOpID already sequenced from the
// same author returns its first seq with ot.ErrDuplicate and changes
// nothing.
func (m *DocManager) Submit(rel string, author int, clientOpID string, baseSeq int64, op ot.Op) (int64, ot.Op, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.docs[rel]
	if d == nil {
		return 0, nil, ErrNotOpen
	}
	if d.readOnly {
		return 0, nil, ErrReadOnly
	}
	if clientOpID != "" {
		if seq, ok := d.seq.AppliedID(author, clientOpID); ok {
			return seq, nil, ot.ErrDuplicate
		}
	}
	grow := 0
	for _, c := range op {
		grow += len(c.Insert)
	}
	if d.bytes+grow > fsroot.MaxCollab {
		return 0, nil, ErrDocTooLarge
	}
	seq, applied, err := d.seq.SubmitID(author, clientOpID, baseSeq, op)
	if err != nil {
		return seq, nil, err
	}
	_ = d.disk.Edited(applied)
	d.bytes = len(string(d.seq.Text()))
	now := m.clock.Now()
	d.dirtyBy[author] = true
	d.lastEdit = now
	if d.journalDue.IsZero() {
		d.journalDue = now.Add(journalDelay)
	}
	m.sequenced(d, seq, author, clientOpID, applied)
	return seq, applied, nil
}

func (m *DocManager) sequenced(d *doc, seq int64, author int, clientOpID string, op ot.Op) {
	if m.onSeq != nil {
		m.onSeq(d.rel, seq, author, clientOpID, op)
	}
}

// Save writes rel to disk atomically (by = the seat that asked, 0 = owner).
// When the disk changed since the last read, the outside change is merged
// first and the write retried. A deleted file is created again.
func (m *DocManager) Save(rel string, by int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked(rel, by)
}

func (m *DocManager) saveLocked(rel string, by int) error {
	d := m.docs[rel]
	if d == nil {
		return ErrNotOpen
	}
	if d.readOnly {
		return ErrReadOnly
	}
	for range 3 {
		text := d.seq.Text()
		prev := d.info
		if d.deleted {
			prev.FP = fsroot.Fingerprint{}
		}
		ni, err := m.root.WriteText(rel, string(text), prev)
		if errors.Is(err, fsroot.ErrChangedOnDisk) {
			if err := m.mergeDisk(d); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		wasDeleted := d.deleted
		d.info, d.deleted = ni, false
		d.disk.Saved(text)
		m.watcher.Track(rel, ni.FP)
		clear(d.dirtyBy)
		d.journalDue = time.Time{}
		if m.j != nil {
			_ = m.j.Drop(rel)
		}
		m.events = append(m.events, DocSaved{Rel: rel, By: by})
		if wasDeleted {
			m.events = append(m.events, DocRecreated{Rel: rel})
		}
		m.unloadIfIdle(d)
		return nil
	}
	return fsroot.ErrChangedOnDisk
}

// mergeDisk reads rel again and sequences the outside change (author disk)
// on top of the unsaved edits.
func (m *DocManager) mergeDisk(d *doc) error {
	text, info, err := m.root.ReadText(d.rel)
	if errors.Is(err, fs.ErrNotExist) {
		if !d.deleted {
			d.deleted = true
			m.events = append(m.events, DocDeletedOnDisk{Rel: d.rel})
		}
		m.watcher.Track(d.rel, fsroot.Fingerprint{})
		return nil
	}
	if err != nil {
		return err
	}
	// The file may now be something else (a retargeted link, a folder
	// swapped in): the host re-checks it before anything is relayed.
	if m.onDisk != nil {
		m.onDisk(d.rel)
	}
	x, err := d.disk.DiskChanged([]rune(text))
	if err != nil {
		return err
	}
	d.info = info
	m.watcher.Track(d.rel, info.FP)
	if d.deleted {
		d.deleted = false
		m.events = append(m.events, DocRecreated{Rel: d.rel})
	}
	if x.IsNoop() {
		return nil
	}
	seq, applied, err := d.seq.SubmitID(-1, "", d.seq.Seq(), x)
	if err != nil {
		return err
	}
	d.bytes = len(string(d.seq.Text()))
	m.events = append(m.events, DocDiskMerged{Rel: d.rel, Seq: seq, Op: applied})
	m.sequenced(d, seq, -1, "", applied)
	return nil
}

// Tick checks the disk every DiskPoll, autosaves (when on) AutosaveDelay
// after the last edit, writes the recovery journal and returns what
// happened since the last call.
func (m *DocManager) Tick(now time.Time) []DocEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastPoll.IsZero() || now.Sub(m.lastPoll) >= DiskPoll {
		m.lastPoll = now
		for _, ch := range m.watcher.Poll() {
			d := m.docs[ch.Rel]
			if d == nil {
				continue
			}
			if ch.Deleted {
				if !d.deleted {
					d.deleted = true
					m.events = append(m.events, DocDeletedOnDisk{Rel: d.rel})
				}
				continue
			}
			_ = m.mergeDisk(d)
		}
	}
	for _, rel := range m.relsLocked() {
		d := m.docs[rel]
		if m.autosave && !d.readOnly && d.dirty() && !d.lastEdit.IsZero() && now.Sub(d.lastEdit) >= AutosaveDelay {
			_ = m.saveLocked(rel, 0)
			if m.docs[rel] == nil {
				continue
			}
		}
		if !d.journalDue.IsZero() && !now.Before(d.journalDue) {
			d.journalDue = time.Time{}
			if m.j != nil {
				if d.dirty() {
					_ = m.j.Put(rel, string(d.seq.Text()), d.info.FP)
				} else {
					_ = m.j.Drop(rel)
				}
			}
		}
	}
	return m.takeEvents()
}

func (m *DocManager) takeEvents() []DocEvent {
	ev := m.events
	m.events = nil
	return ev
}

// takeEventsLocked takes the queued events (for the host right after a
// call that may queue some, such as Save).
func (m *DocManager) takeEventsLocked() []DocEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.takeEvents()
}

func (m *DocManager) relsLocked() []string {
	out := make([]string, 0, len(m.docs))
	for rel := range m.docs {
		out = append(out, rel)
	}
	slices.Sort(out)
	return out
}

// Dirty lists the documents with unsaved edits (or deleted on disk).
func (m *DocManager) Dirty() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, rel := range m.relsLocked() {
		if m.docs[rel].dirty() {
			out = append(out, rel)
		}
	}
	return out
}

// DirtyBy lists the documents seat edited since their last save.
func (m *DocManager) DirtyBy(seat int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, rel := range m.relsLocked() {
		if d := m.docs[rel]; d.dirtyBy[seat] && d.dirty() {
			out = append(out, rel)
		}
	}
	return out
}

// Close drops one Open of rel; with no one left and nothing unsaved the
// document leaves memory.
func (m *DocManager) Close(rel string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.docs[rel]; d != nil {
		d.refs--
		m.unloadIfIdle(d)
	}
}

func (m *DocManager) unloadIfIdle(d *doc) {
	if d.refs > 0 || d.dirty() {
		return
	}
	m.watcher.Untrack(d.rel)
	delete(m.docs, d.rel)
}

// SetAutosave turns autosave (AutosaveDelay after the last edit) on or off.
func (m *DocManager) SetAutosave(on bool) {
	m.mu.Lock()
	m.autosave = on
	m.mu.Unlock()
}

// Discard throws the unsaved edits of rel away: the document goes back to
// what the disk holds (as an edit everyone receives).
func (m *DocManager) Discard(rel string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.docs[rel]
	if d == nil {
		return
	}
	disk := d.disk.Disk()
	if x := ot.Diff(d.seq.Text(), disk); !x.IsNoop() {
		if seq, applied, err := d.seq.SubmitID(-1, "", d.seq.Seq(), x); err == nil {
			d.bytes = len(string(d.seq.Text()))
			m.sequenced(d, seq, -1, "", applied)
		}
	}
	d.disk.Saved(disk)
	d.deleted = false
	clear(d.dirtyBy)
	d.journalDue = time.Time{}
	if m.j != nil {
		_ = m.j.Drop(rel)
	}
	m.unloadIfIdle(d)
}

// Recoverable lists the recovery journal (unsaved text from a masil that
// stopped).
func (m *DocManager) Recoverable() ([]journal.Entry, error) {
	if m.j == nil {
		return nil, nil
	}
	return m.j.Pending()
}

// Recover opens rel (one Open) and puts its journaled text back as an owner
// edit; the document is then unsaved until Save or Discard.
func (m *DocManager) Recover(rel string) (DocView, error) {
	entries, err := m.Recoverable()
	if err != nil {
		return DocView{}, err
	}
	var text *string
	for _, e := range entries {
		if e.Rel == rel {
			t := e.Text
			text = &t
		}
	}
	if text == nil {
		return DocView{}, fs.ErrNotExist
	}
	v, err := m.Open(rel, false)
	if err != nil {
		return DocView{}, err
	}
	if x := ot.Diff([]rune(v.Text), []rune(*text)); !x.IsNoop() {
		if _, _, err := m.Submit(v.Rel, 0, "", v.Seq, x); err != nil {
			return DocView{}, err
		}
	}
	v, _ = m.View(v.Rel)
	return v, nil
}

// Text is the current text and seq of an open document.
func (m *DocManager) Text(rel string) (string, int64, bool) {
	v, ok := m.View(rel)
	return v.Text, v.Seq, ok
}

// size is the file size of rel as last read or written.
func (m *DocManager) size(rel string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.docs[rel]; d != nil {
		return d.info.Size
	}
	return 0
}

// ---- host side ----

// Notice codes for documents (notice.code; arg = path).
const (
	NoticeSaved      = "saved"
	NoticeSaveFailed = "save_failed"
	NoticeDiskMerged = "disk_merged"
	NoticeDeleted    = "deleted_on_disk"
	NoticeRecreated  = "recreated"
)

// Docs is the documents the host shares (nil without a folder).
func (h *Host) Docs() *DocManager { return h.docs }

// onDocSequenced relays one sequenced edit: doc_bcast to everyone and, for
// a participant's edit, its doc_ack right behind it, sealed under one outbox
// lock so no other edit can come between. Participant edits are only
// submitted by the host itself with h.mu held, so h.members may be read.
// Documents no participant has open (the owner's own files, deny-listed
// ones such as .env included) are never relayed (spec B7.3, B7.7).
func (h *Host) onDocSequenced(rel string, seq int64, author int, clientOpID string, op ot.Op) {
	if !h.peerVisible(rel) {
		return
	}
	var ackTo *member
	if author >= 1 && author <= int(wire.MaxSeat) {
		ackTo = h.members[uint8(author)]
	}
	bc := mustJSON(wire.DocBcast{Doc: rel, Seq: seq, Author: author, Ops: op})
	h.outMu.Lock()
	if h.bseal != nil && h.bsign != nil {
		_, _ = h.sealLocked(h.bseal, h.bsign, wire.KindDocBcast, 0, wire.SeatOwner, wire.SeatAll, bc)
	}
	if ackTo != nil && ackTo.o2p != nil && clientOpID != "" {
		ack := mustJSON(wire.DocAck{Doc: rel, ClientOpID: clientOpID, Seq: seq})
		_, _ = h.sealLocked(ackTo.o2p, nil, wire.KindDocAck, 0, wire.SeatOwner, ackTo.seat, ack)
	}
	h.outMu.Unlock()
	if author >= 1 {
		h.emit(EventDocApplied{Doc: rel, Seq: seq, Author: author, Op: op})
	}
}

// pathViolation records a refused participant path and returns its code.
func (h *Host) pathViolation(seat uint8, rel string, err error) string {
	code := DocErrorCode(err)
	var pe *fsroot.PathError
	if errors.As(err, &pe) {
		h.record(audit.EvPathViolation, int(seat), h.users[seat], map[string]any{"rel": rel, "reason": pe.Reason.String()})
	}
	return code
}

// peerVisible reports whether some participant has rel open and it was not
// found hidden since. peerMu is a leaf lock: it is taken under h.mu or under
// DocManager.mu (the seq hook, which also runs for the owner's own edits
// without h.mu).
func (h *Host) peerVisible(rel string) bool {
	h.peerMu.Lock()
	defer h.peerMu.Unlock()
	return h.peerDocs[rel] > 0 && h.hidden[rel] == ""
}

// hide stops relaying rel at once; closeHidden then closes it for the
// participants (code = the doc_state error they get).
func (h *Host) hide(rel, code string) {
	h.peerMu.Lock()
	defer h.peerMu.Unlock()
	if h.peerDocs[rel] > 0 && h.hidden[rel] == "" {
		h.hidden[rel] = code
	}
}

// recheckPeerDoc runs the participant path check on rel again and hides it
// when it no longer passes. Opening checked rel once; what it names can
// change afterwards: an ignore rule added, a link retargeted, a folder
// swapped (v0.1.3 security (v)).
func (h *Host) recheckPeerDoc(rel string) {
	if !h.peerVisible(rel) {
		return
	}
	for _, f := range h.docs.root.RuleFiles(rel) {
		h.rules.TrackNow(f)
	}
	if _, err := h.docs.root.Check(rel, true); err != nil {
		h.hide(rel, DocErrorCode(err))
	}
}

// onDocDisk is the DocManager's hook before an outside change of rel is
// merged (DocManager.mu held): a document that is hidden now is never
// relayed again.
func (h *Host) onDocDisk(rel string) { h.recheckPeerDoc(rel) }

// pollRules re-checks every document participants have open when an
// ignore-rule file that bears on one of them changed (every DiskPoll; the
// rule files are stat'ed, git runs only after a change).
func (h *Host) pollRules(now time.Time) {
	if h.rules == nil || now.Sub(h.lastRulePoll) < DiskPoll {
		return
	}
	h.lastRulePoll = now
	if len(h.rules.Poll()) == 0 {
		return
	}
	h.peerMu.Lock()
	rels := make([]string, 0, len(h.peerDocs))
	for rel := range h.peerDocs {
		rels = append(rels, rel)
	}
	h.peerMu.Unlock()
	slices.Sort(rels)
	for _, rel := range rels {
		h.recheckPeerDoc(rel)
	}
}

// closeHidden closes the hidden documents for every seat that has them
// open: each gets a doc_state with the reason and its edits are refused
// from then on (h.mu held).
func (h *Host) closeHidden() {
	h.peerMu.Lock()
	hidden := maps.Clone(h.hidden)
	h.peerMu.Unlock()
	if len(hidden) == 0 {
		return
	}
	rels := slices.Collect(maps.Keys(hidden))
	slices.Sort(rels)
	seats := make([]uint8, 0, len(h.opens))
	for seat := range h.opens {
		seats = append(seats, seat)
	}
	slices.Sort(seats)
	for _, rel := range rels {
		for _, seat := range seats {
			if !h.opens[seat][rel] {
				continue
			}
			delete(h.opens[seat], rel)
			h.peerOpen(rel, -1)
			h.docs.Close(rel)
			h.sendToPeer(h.members[seat], wire.KindDocState, mustJSON(wire.DocState{Doc: rel, Error: hidden[rel]}))
		}
		// Only now, with no seat left on it, may rel leave the hidden set:
		// the owner's edits keep arriving without h.mu.
		h.peerMu.Lock()
		delete(h.hidden, rel)
		h.peerMu.Unlock()
	}
}

// peerOpen counts one participant's open of rel (delta +1 / -1).
func (h *Host) peerOpen(rel string, delta int) {
	h.peerMu.Lock()
	defer h.peerMu.Unlock()
	if n := h.peerDocs[rel] + delta; n > 0 {
		h.peerDocs[rel] = n
	} else {
		delete(h.peerDocs, rel)
	}
}

func (h *Host) seatOpens(seat uint8) map[string]bool {
	m := h.opens[seat]
	if m == nil {
		m = map[string]bool{}
		h.opens[seat] = m
	}
	return m
}

func (h *Host) onDocOpen(seat uint8, m wire.DocOpen) {
	mb := h.members[seat]
	reply := func(st wire.DocState) { h.sendToPeer(mb, wire.KindDocState, mustJSON(st)) }
	if h.docs == nil {
		reply(wire.DocState{Doc: m.Doc, Error: DocErrNotFound})
		return
	}
	clean, err := h.docs.root.Check(m.Doc, true)
	if err != nil {
		reply(wire.DocState{Doc: m.Doc, Error: h.pathViolation(seat, m.Doc, err)})
		return
	}
	opens := h.seatOpens(seat)
	if !opens[clean] { // a seat holds one Open per document
		v, err := h.docs.Open(clean, true)
		if err != nil {
			reply(wire.DocState{Doc: m.Doc, Error: h.pathViolation(seat, m.Doc, err)})
			return
		}
		clean = v.Rel
		opens[clean] = true
		h.peerOpen(clean, 1)
		for _, f := range h.docs.root.RuleFiles(clean) {
			h.rules.TrackNow(f)
		}
	}
	// The state goes out under the manager's lock: no edit can be broadcast
	// between reading seq and queueing doc_state.
	h.docs.mu.Lock()
	if d := h.docs.docs[clean]; d != nil {
		st := wire.DocState{Doc: clean, Seq: d.seq.Seq(), Text: string(d.seq.Text()), ReadOnly: d.readOnly, Reason: d.reason}
		if m.Pending != "" {
			st.PendingSeq, _ = d.seq.AppliedID(int(seat), m.Pending)
		}
		reply(st)
	}
	h.docs.mu.Unlock()
}

func (h *Host) onDocClose(seat uint8, m wire.DocClose) {
	if h.docs != nil && h.opens[seat][m.Doc] {
		delete(h.opens[seat], m.Doc)
		h.peerOpen(m.Doc, -1)
		h.docs.Close(m.Doc)
	}
}

// closeSeatDocs drops every document seat had open.
func (h *Host) closeSeatDocs(seat uint8) {
	for rel := range h.opens[seat] {
		h.peerOpen(rel, -1)
		h.docs.Close(rel)
	}
	delete(h.opens, seat)
}

func rejectCode(err error) string {
	switch {
	case errors.Is(err, ErrReadOnly):
		return RejectReadOnly
	case errors.Is(err, ErrDocTooLarge):
		return RejectTooLarge
	case errors.Is(err, ErrNotOpen):
		return RejectNotOpen
	case errors.Is(err, ot.ErrStaleBase):
		return RejectStaleBase
	case errors.Is(err, ot.ErrFutureBase):
		return RejectFutureBase
	default:
		return RejectBaseLen
	}
}

func (h *Host) onDocOp(seat uint8, m wire.DocOp) {
	mb := h.members[seat]
	reject := func(code string) {
		h.rejectFrame(seat, wire.KindDocOp, code, false)
		h.sendToPeer(mb, wire.KindDocReject, mustJSON(wire.DocReject{Doc: m.Doc, ClientOpID: m.ClientOpID, Code: code}))
	}
	if h.docs == nil || !h.opens[seat][m.Doc] {
		reject(RejectNotOpen)
		return
	}
	if h.killed.Load() {
		return
	}
	seq, _, err := h.docs.Submit(m.Doc, int(seat), m.ClientOpID, m.BaseSeq, m.Ops)
	switch {
	case err == nil: // doc_bcast and doc_ack went out from onDocSequenced
	case errors.Is(err, ot.ErrDuplicate):
		h.sendToPeer(mb, wire.KindDocAck, mustJSON(wire.DocAck{Doc: m.Doc, ClientOpID: m.ClientOpID, Seq: seq}))
	default:
		reject(rejectCode(err))
	}
}

func (h *Host) onDocSave(seat uint8, m wire.DocSave) {
	mb := h.members[seat]
	if h.docs == nil || !h.opens[seat][m.Doc] {
		return
	}
	if err := h.docs.Save(m.Doc, int(seat)); err != nil {
		_, _ = h.broadcast(wire.KindNotice, 0, mustJSON(wire.Notice{Code: NoticeSaveFailed, Seat: seat, Arg: m.Doc}))
		return
	}
	h.record(audit.EvPeerSave, int(seat), mb.user, map[string]any{"rel": m.Doc, "size": h.docs.size(m.Doc), "seat": int(seat)})
	h.pumpDocEvents(h.docs.takeEventsLocked())
}

func (h *Host) onDocPresence(seat uint8, m wire.DocPresence) {
	if !h.opens[seat][m.Doc] || !h.peerVisible(m.Doc) {
		return
	}
	m.Seat = seat
	_, _ = h.broadcast(wire.KindDocPresence, 0, mustJSON(m))
	h.emit(EventPresence{DocPresence: m})
}

// Presence shows the owner's cursor in rel to the participants.
func (h *Host) Presence(rel string, cursor, selFrom, selTo int) {
	h.mu.Lock()
	if !h.over && h.peerVisible(rel) {
		_, _ = h.broadcast(wire.KindDocPresence, 0, mustJSON(wire.DocPresence{Doc: rel, Seat: wire.SeatOwner, Cursor: cursor, SelFrom: selFrom, SelTo: selTo}))
	}
	h.mu.Unlock()
	h.flushManual()
}

// pumpDocEvents hands document events to the UI and tells the participants
// about the documents they have open.
func (h *Host) pumpDocEvents(evs []DocEvent) {
	for _, ev := range evs {
		h.emit(EventDoc{DocEvent: ev})
		if !h.peerVisible(docEventRel(ev)) {
			continue
		}
		var n wire.Notice
		switch e := ev.(type) {
		case DocDiskMerged:
			n = wire.Notice{Code: NoticeDiskMerged, Arg: e.Rel}
		case DocDeletedOnDisk:
			n = wire.Notice{Code: NoticeDeleted, Arg: e.Rel}
		case DocSaved:
			n = wire.Notice{Code: NoticeSaved, Seat: uint8(max(e.By, 0)), Arg: e.Rel}
		case DocRecreated:
			n = wire.Notice{Code: NoticeRecreated, Arg: e.Rel}
		default:
			continue
		}
		_, _ = h.broadcast(wire.KindNotice, 0, mustJSON(n))
	}
}

func docEventRel(ev DocEvent) string {
	switch e := ev.(type) {
	case DocDiskMerged:
		return e.Rel
	case DocDeletedOnDisk:
		return e.Rel
	case DocSaved:
		return e.Rel
	case DocRecreated:
		return e.Rel
	}
	return ""
}

// tickDocs runs the document timers and keeps the unsaved count current.
func (h *Host) tickDocs(now time.Time) {
	if h.docs == nil {
		return
	}
	h.pollRules(now)
	h.pumpDocEvents(h.docs.Tick(now))
	h.closeHidden()
	h.unsaved = len(h.docs.Dirty())
}

// EndCheck lists the documents participants changed and nobody saved: the
// UI asks save / discard / keep open before ending the share.
func (h *Host) EndCheck() []string {
	if h.docs == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for s := 1; s <= int(wire.MaxSeat); s++ {
		for _, rel := range h.docs.DirtyBy(s) {
			if !seen[rel] {
				seen[rel] = true
				out = append(out, rel)
			}
		}
	}
	slices.Sort(out)
	return out
}
