package api

import "time"

// ---- workspace shares (v0.1.3 B10, api.md 2 / 5.26) ----

// WorkspaceShare is a shared workspace's server-side metadata: the content
// itself only ever crosses the share socket encrypted (docs/contract/workspace.md).
type WorkspaceShare struct {
	ID        string                 `json:"id"`
	Owner     UserSummary            `json:"owner"`
	Title     string                 `json:"title"`  // 1..40 code points, user data
	Status    string                 `json:"status"` // live|ended
	CreatedAt time.Time              `json:"created_at"`
	EndedAt   *time.Time             `json:"ended_at"`
	EndReason *string                `json:"end_reason"` // ended|timeout
	Members   []WorkspaceShareMember `json:"members"`    // seat order; removed members are not listed
}

// WorkspaceShareMember is one invited person and their seat.
type WorkspaceShareMember struct {
	User      UserSummary `json:"user"`
	Seat      int         `json:"seat"` // 1..8 (the owner is seat 0)
	InvitedAt time.Time   `json:"invited_at"`
	JoinedAt  *time.Time  `json:"joined_at"` // last socket connect
	LeftAt    *time.Time  `json:"left_at"`   // last socket close
	MaxLevel  *string     `json:"max_level"` // viewer|prompter|editor|keyboard: the highest level ever granted
	Verified  bool        `json:"verified"`  // the confirmation numbers were compared
}
