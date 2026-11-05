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

// String names the decision (log attribute).
func (d Decision) String() string {
	switch d {
	case Skip:
		return "skip"
	case Silent:
		return "silent"
	case Popup:
		return "popup"
	}
	return "unknown"
}

// staleReplay is the age above which a replayed alert never pops (events.md 4.7, 7).
const staleReplay = 10 * time.Minute

// Decide applies the CA 4.6 rules in order.
func Decide(a api.Alert, in Input) Decision {
	// quiet is the "do not pop" outcome: the TUI still lists the alert in-app,
	// the daemon has no in-app surface.
	quiet := Silent
	if in.Role == RoleDaemon {
		quiet = Skip
	}
	switch {
	case in.Replayed && in.Now.Sub(a.CreatedAt) > staleReplay: // 1
		return quiet
	case a.Suppressed: // 2
		return quiet
	case !in.DesktopEnabled: // 3
		return quiet
	case in.Role == RoleDaemon && in.TUIPresent: // 4
		return Skip
	case in.Role == RoleTUI && in.TerminalFocusKnown && in.TerminalFocused && in.ViewingTarget: // 5
		return Silent
	}
	return Popup // 6
}
