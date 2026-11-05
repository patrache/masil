// Package slash parses composer slash commands (CA 4.9).
package slash

import (
	"time"

	"github.com/patrache/masil/internal/api"
)

// Name is a slash command name.
type Name string

// Slash command names (CA 4.9, verbatim).
const (
	Me     Name = "me"
	Shrug       = "shrug"
	Poll        = "poll"
	Status      = "status"
	Focus       = "focus"
	Roll        = "roll"
	Invite      = "invite"
)

// Command is a parsed slash command.
type Command struct {
	Name          Name
	Text          string // /me, /shrug, /status text
	Emoji         string // /status
	Poll          *api.PollInput
	Dice          string // /roll "2d6"
	FocusOff      bool
	FocusDuration time.Duration // /focus 25m | /focus off | /focus (default)
	InviteUser    string
	InviteLink    bool
	LinkExpires   time.Duration
	LinkMaxUses   int // /invite @h | /invite link [7d] [10]
}

// Spec describes one slash command for completion and help.
type Spec struct {
	Name        Name
	Usage, Desc string
}

// Parse: ok=false => not a slash command (send as text); err => Korean usage message.
func Parse(input string) (cmd *Command, ok bool, err error) {
	// TODO(core): Task 7
	return nil, false, nil
}

// Catalog lists every command for composer completion & help.
func Catalog() []Spec {
	// TODO(core): Task 7
	return nil
}
