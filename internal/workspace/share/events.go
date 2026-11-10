package share

import (
	"time"

	"github.com/patrache/masil/internal/workspace/ot"
	"github.com/patrache/masil/internal/workspace/wire"
)

// Event is something the UI should show. Host.Drain and Guest.Drain hand
// them out in order; neither blocks.
type Event interface{ shareEvent() }

// End reasons (EventEnded.Reason, EventDeclined.Reason).
const (
	ReasonEnded        = "ended"        // the share ended (owner, other device, timeout)
	ReasonKill         = "kill"         // the owner's kill switch
	ReasonKicked       = "kicked"       // the owner removed me
	ReasonReplaced     = "replaced"     // my newer socket took over
	ReasonProtocol     = "protocol"     // the server closed 4000
	ReasonNotFound     = "not_found"    // the share is gone (404 on dial)
	ReasonUnauthorized = "unauthorized" // my session was revoked (4001, 401 on dial)
	ReasonSuspended    = "suspended"    // my account was suspended (4003, 403 on dial)
	ReasonLeft         = "left"         // I left
	ReasonDeclined     = "declined"     // the owner said no
	ReasonMismatch     = "mismatch"     // the check numbers differed
	ReasonCommit       = "commit"       // the revealed key did not match its commitment
	ReasonExpired      = "expired"      // the owner did not answer in 5 minutes
	reasonResumeBad    = "resume"       // hs_resume could not be opened: start over
)

// EventJoinRequest (Host): a participant finished the key exchange and waits
// for the owner.
type EventJoinRequest struct{ JoinRequest }

// EventJoinExpired (Host): a join request timed out.
type EventJoinExpired struct {
	Seat   uint8
	UserID string
}

// EventSASReady (Guest): the check number to compare is known (Guest.SAS).
type EventSASReady struct{ SAS string }

// EventPeerJoined (Host): a participant was let in.
type EventPeerJoined struct {
	Seat   uint8
	UserID string
}

// EventPeerLeft (Host): a participant's connection went away.
type EventPeerLeft struct {
	Seat   uint8
	UserID string
}

// EventGranted (Guest): the owner let me in.
type EventGranted struct {
	Level         wire.Level
	Verified      bool
	KeyboardUntil *time.Time
}

// EventDeclined (Guest): the join attempt ended without a grant.
type EventDeclined struct{ Reason string }

// EventMITMWarning (Host): the check numbers differed or the commitment did
// not match — someone may be in the middle.
type EventMITMWarning struct{ Seat uint8 }

// EventTamperWarning: frames keep failing authentication (spec B9).
type EventTamperWarning struct{}

// EventReconnecting: the socket dropped; the engine redials.
type EventReconnecting struct{}

// EventResumed: back after a short drop, without a new key exchange.
type EventResumed struct{}

// EventEnded: the share is over for this side.
type EventEnded struct{ Reason string }

// EventKeyboardPaused (Host): the owner was idle for OwnerIdlePause;
// keyboard input is held back until OwnerActivity.
type EventKeyboardPaused struct{}

// EventKeyboardResumed (Host): the owner is back; keyboard input flows.
type EventKeyboardResumed struct{}

// EventKeyboardEnded (Host): a keyboard holder dropped to editor (Reason is
// one of the Demote* values).
type EventKeyboardEnded struct {
	Seat   uint8
	Reason string
}

// EventRoster (Guest): the owner's people list changed.
type EventRoster struct{ Members []Member }

// Notice codes the engine itself uses.
const (
	// NoticeResumed: the owner's socket is back after a drop.
	NoticeResumed = "resumed"
)

// EventTermOut (Guest): output bytes of shared terminal Term.
type EventTermOut struct {
	Term  uint8
	Bytes []byte
}

// EventTermKeyframe (Guest): a full redraw of terminal Term; reset the
// replay screen and write Bytes.
type EventTermKeyframe struct {
	Term  uint8
	Bytes []byte
}

// EventTermStatus (Guest): terminal title, size, attention or unsaved count
// changed.
type EventTermStatus struct{ wire.TermStatus }

// EventProposal (Host): a participant proposal joined the queue.
type EventProposal struct{ Proposal }

// EventPropResult (Guest): what the owner did with my proposal (Action is
// queued, insert, insert_send, edit, reject, too_long, full, rate_limited).
type EventPropResult struct{ wire.PropResult }

// EventDoc (Host): a document event of the shared folder (disk merge,
// deletion, save, recreation).
type EventDoc struct{ DocEvent }

// EventDocApplied (Host): a participant's edit was sequenced; apply Op to
// the owner's editor of Doc.
type EventDocApplied struct {
	Doc    string
	Seq    int64
	Author int
	Op     ot.Op
}

// EventPresence: someone's cursor or selection moved.
type EventPresence struct{ wire.DocPresence }

// EventDocState (Guest): a document I asked for (Error set when refused).
type EventDocState struct{ wire.DocState }

// EventDocRemote (Guest): someone else's edit, transformed: apply Op to my
// view of Doc.
type EventDocRemote struct {
	Doc string
	Op  ot.Op
}

// EventDocReset (Guest): my copy of Doc was replaced (reconnect or a
// refused edit); redraw it from Text.
type EventDocReset struct {
	Doc  string
	Text string
}

// EventDocRejected (Guest): the owner refused my edit (Code); a fresh copy
// follows as EventDocReset.
type EventDocRejected struct{ wire.DocReject }

// EventTree (Guest): one folder of the shared tree.
type EventTree struct{ wire.TreeList }

// EventNotice (Guest): a notice from the owner (saved, disk_merged, …).
type EventNotice struct{ wire.Notice }

// EventFsResult (Guest): the answer to my file request.
type EventFsResult struct{ wire.FsResult }

// EventInput is what a keyboard-level participant typed into terminal Term,
// as delivered (the owner's input band, spec B8.4). Memory only: never log
// it.
type EventInput struct {
	Seat, Term uint8
	Data       []byte
}

func (EventInput) shareEvent() {}

func (EventDoc) shareEvent()         {}
func (EventDocApplied) shareEvent()  {}
func (EventPresence) shareEvent()    {}
func (EventDocState) shareEvent()    {}
func (EventDocRemote) shareEvent()   {}
func (EventDocReset) shareEvent()    {}
func (EventDocRejected) shareEvent() {}
func (EventTree) shareEvent()        {}
func (EventNotice) shareEvent()      {}
func (EventFsResult) shareEvent()    {}

func (EventTermOut) shareEvent()      {}
func (EventTermKeyframe) shareEvent() {}
func (EventTermStatus) shareEvent()   {}
func (EventProposal) shareEvent()     {}
func (EventPropResult) shareEvent()   {}

func (EventKeyboardPaused) shareEvent()  {}
func (EventKeyboardResumed) shareEvent() {}
func (EventKeyboardEnded) shareEvent()   {}
func (EventRoster) shareEvent()          {}
func (EventJoinRequest) shareEvent()     {}
func (EventJoinExpired) shareEvent()     {}
func (EventSASReady) shareEvent()        {}
func (EventPeerJoined) shareEvent()      {}
func (EventPeerLeft) shareEvent()        {}
func (EventGranted) shareEvent()         {}
func (EventDeclined) shareEvent()        {}
func (EventMITMWarning) shareEvent()     {}
func (EventTamperWarning) shareEvent()   {}
func (EventReconnecting) shareEvent()    {}
func (EventResumed) shareEvent()         {}
func (EventEnded) shareEvent()           {}
