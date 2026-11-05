// Package notify decides, formats, de-duplicates and shows OS notifications (CA 4.6).
package notify

import (
	"time"

	"github.com/patrache/masil/internal/api"
)

// Role is the deciding process.
type Role int

// Roles.
const (
	RoleTUI Role = iota
	RoleDaemon
)

// Input is everything Decide needs besides the alert.
type Input struct {
	Role           Role
	Now            time.Time
	Replayed       bool // Event.Replayed
	DesktopEnabled bool // config.notify.desktop
	// TUI only:
	TerminalFocusKnown bool
	TerminalFocused    bool
	ViewingTarget      bool // the alert's channel/thread/post is visible right now
	// Daemon only:
	TUIPresent bool // final decision from daemon (local lock / server signal / 3s grace)
}

// Decision is what to do with an alert.
type Decision int

// Decisions.
const (
	Skip   Decision = iota // nothing (stale replay, disabled, daemon with TUI present)
	Silent                 // in-app only (suppressed alert, or user is looking at it)
	Popup                  // OS notification (after ledger check)
)

// Decide applies the CA 4.6 rules in order.
func Decide(a api.Alert, in Input) Decision {
	// TODO(core): Task 15
	return Skip
}
