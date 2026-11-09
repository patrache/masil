package share

import (
	"time"

	"github.com/patrache/masil/internal/workspace/wire"
)

// Event is something the UI should show. Host.Drain and Guest.Drain hand
// them out in order; neither blocks.
type Event interface{ shareEvent() }

// End reasons (EventEnded.Reason, EventDeclined.Reason).
const (
	ReasonEnded     = "ended"     // the share ended (owner, other device, timeout)
	ReasonKill      = "kill"      // the owner's kill switch
	ReasonKicked    = "kicked"    // the owner removed me
	ReasonReplaced  = "replaced"  // my newer socket took over
	ReasonProtocol  = "protocol"  // the server closed 4000
	ReasonNotFound  = "not_found" // the share is gone (404 on dial)
	ReasonLeft      = "left"      // I left
	ReasonDeclined  = "declined"  // the owner said no
	ReasonMismatch  = "mismatch"  // the check numbers differed
	ReasonCommit    = "commit"    // the revealed key did not match its commitment
	ReasonExpired   = "expired"   // the owner did not answer in 5 minutes
	reasonResumeBad = "resume"    // hs_resume could not be opened: start over
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
