package termpane

import (
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// The ctrl+\ prefix (v0.1.3 A7.5): inside a pane every key belongs to the
// program, so the way out is one prefix key (keymap termhost.escape) followed,
// within PrefixTimeout, by one action key. Reusable: P2's workspace cells read
// keys the same way.

// PrefixTimeout is how long the prefix waits for its next key.
const PrefixTimeout = 2 * time.Second

// PrefixAction is what a key meant to the prefix.
type PrefixAction int

// Prefix actions (Prefix.Key).
const (
	PrefixNone    PrefixAction = iota // not a prefix key: it belongs to the program
	PrefixLeave                       // esc: give the keyboard back to masil
	PrefixFocus                       // h/j/k/l or arrows: move the focus (Dir)
	PrefixZoom                        // z
	PrefixPalette                     // p
	PrefixSurface                     // t: tool surface <-> chat
	PrefixScroll                      // [: the scroll-back view
	PrefixKill                        // x: end the program (a kill switch first)
	PrefixHelp                        // ?
	PrefixLiteral                     // the prefix key again: send it to the program
	PrefixExtra                       // a tool's own key (Key)
	PrefixCancel                      // any other key: dropped
	PrefixArm                         // the prefix key: now waiting (Cmd = the timeout)
)

// PrefixResult is Prefix.Key's answer.
type PrefixResult struct {
	Action PrefixAction
	Dir    string // PrefixFocus: h, j, k or l
	Key    string // PrefixExtra: the key
	Cmd    tea.Cmd
}

// Prefix tracks one prefix key sequence.
type Prefix struct {
	extra  []string
	active bool
	gen    int
}

// prefixTimeoutMsg ends the wait of prefix p armed as generation gen.
type prefixTimeoutMsg struct {
	p   *Prefix
	gen int
}

// NewPrefix is a prefix that also accepts the tool's extra keys (ToolSpec
// PrefixKeys). An extra key in ui.ReservedPrefixKeys panics.
func NewPrefix(extra []string) *Prefix {
	for _, k := range extra {
		if slices.Contains(ui.ReservedPrefixKeys, k) {
			panic(fmt.Sprintf("termpane: NewPrefix: prefix key %q is reserved", k))
		}
	}
	return &Prefix{extra: slices.Clone(extra)}
}

// Active reports whether the prefix waits for its next key.
func (p *Prefix) Active() bool { return p.active }

// Cancel stops waiting.
func (p *Prefix) Cancel() { p.active = false }

// Key reads msg: the prefix key arms it (PrefixTimeout through ui.After);
// while armed, the next key is its action and the prefix ends.
func (p *Prefix) Key(ctx *ui.Context, msg tea.KeyPressMsg) PrefixResult {
	escape := ctx.Keys.MatchesEscape(msg)
	if !p.active {
		if !escape {
			return PrefixResult{Action: PrefixNone}
		}
		p.active = true
		p.gen++
		return PrefixResult{Action: PrefixArm, Cmd: ui.After(ctx, PrefixTimeout, prefixTimeoutMsg{p: p, gen: p.gen})}
	}
	p.active = false
	if escape {
		return PrefixResult{Action: PrefixLiteral}
	}
	s := msg.String()
	switch s {
	case "esc":
		return PrefixResult{Action: PrefixLeave}
	case "h", "left":
		return PrefixResult{Action: PrefixFocus, Dir: "h"}
	case "j", "down":
		return PrefixResult{Action: PrefixFocus, Dir: "j"}
	case "k", "up":
		return PrefixResult{Action: PrefixFocus, Dir: "k"}
	case "l", "right":
		return PrefixResult{Action: PrefixFocus, Dir: "l"}
	case "z":
		return PrefixResult{Action: PrefixZoom}
	case "p":
		return PrefixResult{Action: PrefixPalette}
	case "t":
		return PrefixResult{Action: PrefixSurface}
	case "[":
		return PrefixResult{Action: PrefixScroll}
	case "x":
		return PrefixResult{Action: PrefixKill}
	case "?":
		return PrefixResult{Action: PrefixHelp}
	}
	if slices.Contains(p.extra, s) {
		return PrefixResult{Action: PrefixExtra, Key: s}
	}
	return PrefixResult{Action: PrefixCancel}
}

// Update takes this prefix's timeout msg (true when msg was it): a wait still
// armed from that generation is cancelled.
func (p *Prefix) Update(msg tea.Msg) bool {
	m, ok := msg.(prefixTimeoutMsg)
	if !ok || m.p != p {
		return false
	}
	if m.gen == p.gen {
		p.active = false
		return true
	}
	return false
}
