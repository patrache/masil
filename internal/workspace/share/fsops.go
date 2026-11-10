package share

import (
	"errors"
	"io/fs"
	"os"
	"strconv"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/wire"
)

// fs_result errors besides the path codes.
const (
	FsErrRejected = "rejected"
	FsErrExists   = "exists"
	FsErrFailed   = "failed"
	FsErrFull     = "full"
)

// Proposal kinds: a typed-text proposal has no kind.
const (
	KindRename = wire.FsRename
	KindDelete = wire.FsDelete
)

// onFsReq: create happens at once (editor level); rename and delete wait in
// the owner's approval queue next to the proposals.
func (h *Host) onFsReq(seat uint8, m wire.FsReq) {
	mb := h.members[seat]
	reply := func(errCode string) {
		h.sendToPeer(mb, wire.KindFsResult, mustJSON(wire.FsResult{Op: m.Op, From: m.From, To: m.To, Error: errCode}))
	}
	if h.docs == nil {
		reply(FsErrFailed)
		return
	}
	root := h.docs.root
	from, err := root.Check(m.From, true)
	if err != nil {
		reply(h.pathViolation(seat, m.From, err))
		return
	}
	switch m.Op {
	case wire.FsCreate:
		err := root.Create(from)
		switch {
		case errors.Is(err, fs.ErrExist):
			reply(FsErrExists)
		case err != nil:
			reply(FsErrFailed)
		default:
			h.record(audit.EvFsAction, int(seat), mb.user, map[string]any{"op": m.Op, "from": from, "action": "auto"})
			reply("")
		}
	case wire.FsRename, wire.FsDelete:
		to := ""
		if m.Op == wire.FsRename {
			if to, err = root.Check(m.To, true); err != nil {
				reply(h.pathViolation(seat, m.To, err))
				return
			}
		}
		if err := root.CheckSubtree(from); err != nil {
			reply(h.pathViolation(seat, m.From, err))
			return
		}
		if m.Op == wire.FsRename && h.renameTargetTaken(from, to) {
			reply(FsErrExists)
			return
		}
		if len(h.props) >= ProposalQueueMax {
			reply(FsErrFull)
			return
		}
		h.propSeq++
		p := &proposal{Proposal: Proposal{ID: "p" + strconv.Itoa(h.propSeq), Seat: seat, Kind: m.Op, From: from, To: to, At: h.now()}}
		h.props = append(h.props, p)
		h.record(audit.EvFsRequest, int(seat), mb.user, map[string]any{"id": p.ID, "op": m.Op, "from": from, "to": to})
		h.emit(EventProposal{Proposal: p.Proposal})
	}
}

// handleFsProposal approves (any action but reject) or rejects a rename or
// delete request.
func (h *Host) handleFsProposal(i int, p *proposal, action ProposalAction) {
	h.props = append(h.props[:i], h.props[i+1:]...)
	mb := h.members[p.Seat]
	user := ""
	if mb != nil {
		user = mb.user
	}
	res := wire.FsResult{Op: p.Kind, From: p.From, To: p.To}
	act := "approve"
	if action == ActReject {
		act, res.Error = "reject", FsErrRejected
	} else if code := h.recheckFs(p); code != "" {
		res.Error = code
	} else {
		var err error
		if p.Kind == wire.FsRename {
			err = h.docs.root.Rename(p.From, p.To)
		} else {
			err = h.docs.root.Remove(p.From)
		}
		if err != nil {
			res.Error = FsErrFailed
		}
	}
	h.record(audit.EvFsAction, int(p.Seat), user, map[string]any{"id": p.ID, "op": p.Kind, "from": p.From, "to": p.To, "action": act, "error": res.Error})
	h.sendToPeer(mb, wire.KindFsResult, mustJSON(res))
}

// renameTargetTaken reports an existing to: a participant's rename never
// replaces a file or folder (v0.1.3 security (x)). The same entry under
// another spelling (a case-only rename on a case-insensitive disk) is not
// taken.
func (h *Host) renameTargetTaken(from, to string) bool {
	root := h.docs.root
	tfi, err := root.Lstat(to)
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	ffi, err := root.Lstat(from)
	return err != nil || !os.SameFile(ffi, tfi)
}

// recheckFs repeats the request checks when the owner approves: the folder
// and the ignore rules may have changed while the request waited. It
// returns the fs_result error code ("" = go ahead).
func (h *Host) recheckFs(p *proposal) string {
	root := h.docs.root
	if _, err := root.Check(p.From, true); err != nil {
		return DocErrorCode(err)
	}
	if p.Kind == wire.FsRename {
		if _, err := root.Check(p.To, true); err != nil {
			return DocErrorCode(err)
		}
	}
	if err := root.CheckSubtree(p.From); err != nil {
		return DocErrorCode(err)
	}
	if p.Kind == wire.FsRename && h.renameTargetTaken(p.From, p.To) {
		return FsErrExists
	}
	return ""
}

// RequestFs asks the owner to create (at once), rename or delete (after the
// owner approves) a file; the answer arrives as EventFsResult.
func (g *Guest) RequestFs(op, from, to string) error {
	g.mu.Lock()
	err := g.sendLevel(wire.LevelEditor, wire.KindFsReq, mustJSON(wire.FsReq{Op: op, From: from, To: to}))
	g.mu.Unlock()
	g.flushManual()
	return err
}
