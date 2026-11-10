package share

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Proposal limits (spec B8.3).
const (
	ProposalMax       = 4000 // runes
	ProposalQueueMax  = 20
	ProposalPerMinute = 10
)

var (
	ErrTooLong      = errors.New("share: proposal too long")
	ErrNoProposal   = errors.New("share: no such proposal")
	ErrNothingToAsk = errors.New("share: nothing waits for a multi-line confirmation")
)

// ProposalAction is what the owner does with a proposal.
type ProposalAction int

const (
	ActInsert           ProposalAction = iota + 1 // paste only
	ActInsertSend                                 // paste and press enter
	ActEdit                                       // paste the owner's edited text
	ActReject                                     // drop it
	ActConfirmMultiline                           // yes, paste these lines without paste mode
	ActApprove                                    // carry out a rename/delete request
)

var actionNames = map[ProposalAction]string{
	ActInsert: "insert", ActInsertSend: "insert_send", ActEdit: "edit", ActReject: "reject",
}

// prop_result actions the owner sends besides actionNames.
const (
	PropQueued   = "queued"
	PropTooLong  = "too_long"
	PropFull     = "full"
	PropRateHit  = "rate_limited"
	PropRejected = "reject"
)

// Proposal is one waiting participant proposal: typed text (Kind "") or a
// file request (KindRename / KindDelete with From, To).
type Proposal struct {
	ID       string // the owner's id (not the participant's)
	Seat     uint8
	Kind     string
	Text     string
	From, To string
	At       time.Time
}

type proposal struct {
	Proposal
	peerID  string
	confirm *proposalStep // waiting for ActConfirmMultiline
}

type proposalStep struct {
	act  ProposalAction
	text string
}

// Proposals lists the waiting proposals, oldest first.
func (h *Host) Proposals() []Proposal {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Proposal, 0, len(h.props))
	for _, p := range h.props {
		out = append(out, p.Proposal)
	}
	return out
}

// onPropSubmit takes a proposal from seat (prompter level or above, already
// checked).
func (h *Host) onPropSubmit(seat uint8, m wire.PropSubmit) {
	mb := h.members[seat]
	if mb == nil || m.ID == "" || len(m.ID) > 64 {
		return
	}
	now := h.now()
	reply := func(action string) {
		h.sendToPeer(mb, wire.KindPropResult, mustJSON(wire.PropResult{ID: m.ID, Action: action}))
	}
	if utf8.RuneCountInString(m.Text) > ProposalMax {
		h.rejectFrame(seat, wire.KindPropSubmit, PropTooLong, false)
		reply(PropTooLong)
		return
	}
	recent := h.propTimes[seat][:0]
	for _, t := range h.propTimes[seat] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	h.propTimes[seat] = recent
	if len(recent) >= ProposalPerMinute {
		reply(PropRateHit)
		return
	}
	if len(h.props) >= ProposalQueueMax {
		reply(PropFull)
		return
	}
	h.propTimes[seat] = append(recent, now)
	h.propSeq++
	p := &proposal{Proposal: Proposal{ID: "p" + strconv.Itoa(h.propSeq), Seat: seat, Text: m.Text, At: now}, peerID: m.ID}
	h.props = append(h.props, p)
	h.record(audit.EvProposal, int(seat), mb.user, map[string]any{"id": p.ID, "text": m.Text})
	reply(PropQueued)
	h.emit(EventProposal{Proposal: p.Proposal})
}

func (h *Host) findProposal(id string) (int, *proposal) {
	for i, p := range h.props {
		if p.ID == id {
			return i, p
		}
	}
	return -1, nil
}

// HandleProposal applies the owner's choice. The text is sanitized right
// before it is typed; with paste mode on it goes in as one bracketed paste,
// otherwise text of more than one line returns needsConfirm=true (nothing is
// typed) and the UI asks again, then calls ActConfirmMultiline.
func (h *Host) HandleProposal(id string, action ProposalAction, edited string) (needsConfirm bool, err error) {
	h.mu.Lock()
	needsConfirm, err = h.handleProposalLocked(id, action, edited)
	h.mu.Unlock()
	h.flushManual()
	return needsConfirm, err
}

func (h *Host) handleProposalLocked(id string, action ProposalAction, edited string) (bool, error) {
	if h.over || h.killed.Load() {
		return false, ErrEnded
	}
	i, p := h.findProposal(id)
	if p == nil {
		return false, ErrNoProposal
	}
	h.lastOwner = h.now()
	if p.Kind != "" {
		if action == ActConfirmMultiline {
			return false, ErrNothingToAsk
		}
		h.handleFsProposal(i, p, action)
		return false, nil
	}
	confirmed := false
	text := p.Text
	switch action {
	case ActReject:
		h.finishProposal(i, p, PropRejected, "")
		return false, nil
	case ActConfirmMultiline:
		if p.confirm == nil {
			return false, ErrNothingToAsk
		}
		action, text, confirmed = p.confirm.act, p.confirm.text, true
	case ActEdit:
		text = edited
	case ActInsert, ActInsertSend:
	case ActApprove:
		action = ActInsert
	default:
		return false, ErrNoProposal
	}
	return h.typeProposal(i, p, action, text, confirmed)
}

// typeProposal pastes text into the proposal terminal.
func (h *Host) typeProposal(i int, p *proposal, action ProposalAction, text string, confirmed bool) (bool, error) {
	st := h.proposalTerm()
	if st == nil {
		return false, ErrNoTerminal
	}
	clean := Sanitize(text)
	var out string
	switch {
	case st.t.BracketedPaste():
		out = "\x1b[200~" + clean + "\x1b[201~"
	case strings.Contains(clean, "\n") && !confirmed:
		p.confirm = &proposalStep{act: action, text: text}
		return true, nil
	default:
		out = clean
	}
	if action == ActInsertSend {
		out += "\r"
	}
	if h.killed.Load() {
		return false, ErrEnded
	}
	if err := st.t.Inject([]byte(out)); err != nil {
		return false, err
	}
	h.finishProposal(i, p, actionNames[action], clean)
	return false, nil
}

// proposalTerm is where proposals are typed: the lowest shared terminal.
func (h *Host) proposalTerm() *sharedTerm {
	ids := h.termIDs()
	if len(ids) == 0 {
		return nil
	}
	return h.terms[ids[0]]
}

func (h *Host) finishProposal(i int, p *proposal, action, typed string) {
	h.props = append(h.props[:i], h.props[i+1:]...)
	data := map[string]any{"id": p.ID, "action": action}
	if typed != "" {
		data["text"] = typed
	}
	user := ""
	if mb := h.members[p.Seat]; mb != nil {
		user = mb.user
		h.sendToPeer(mb, wire.KindPropResult, mustJSON(wire.PropResult{ID: p.peerID, Action: action}))
	}
	h.record(audit.EvProposalAction, int(p.Seat), user, data)
}

// dropProposals forgets the waiting proposals of seat (it was removed).
func (h *Host) dropProposals(seat uint8) {
	kept := h.props[:0]
	for _, p := range h.props {
		if p.Seat != seat {
			kept = append(kept, p)
		}
	}
	h.props = kept
}

// ---- guest side ----

// Propose sends text to the owner's proposal queue (prompter level or
// above). It returns my id for the proposal (EventPropResult carries it).
func (g *Guest) Propose(text string) (string, error) {
	g.mu.Lock()
	id, err := g.proposeLocked(text)
	g.mu.Unlock()
	g.flushManual()
	return id, err
}

func (g *Guest) proposeLocked(text string) (string, error) {
	if g.over {
		return "", ErrEnded
	}
	if g.phase != phaseGranted || g.level < wire.LevelPrompter {
		return "", ErrLevel
	}
	if utf8.RuneCountInString(text) > ProposalMax {
		return "", ErrTooLong
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	if err := g.sendOwner(wire.KindPropSubmit, mustJSON(wire.PropSubmit{ID: id, Text: text})); err != nil {
		return "", err
	}
	return id, nil
}
