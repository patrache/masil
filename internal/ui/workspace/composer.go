package workspace

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// The proposal line under the participant's terminal cell (spec B8.3): at
// the prompter and editor levels the participant writes here and enter
// sends it to the owner's queue; the owner types it in (or not).

// composer is the proposal being written.
type composer struct {
	r []rune
}

// composing: the proposal line is shown (prompter or editor, a terminal is
// shared, not panning).
func (g *guestShare) composing() bool {
	if g.phase != guestIn || g.level < wire.LevelPrompter || g.level >= wire.LevelKeyboard {
		return false
	}
	rt := g.current()
	return rt != nil && !rt.pan
}

// composeKey edits the proposal; enter sends it.
func (s *Surface) composeKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	g := s.guest
	c := &g.compose
	switch msg.String() {
	case "enter":
		text := strings.TrimSpace(string(c.r))
		if text == "" {
			return true, nil
		}
		if _, err := g.eng.Propose(text); err != nil {
			return true, ui.Toast(ctx.T.Workspace.PropFailed(), ui.ToastError)
		}
		c.r = nil
		return true, ui.Toast(ctx.T.Workspace.PropSent(), ui.ToastInfo)
	case "backspace":
		if len(c.r) > 0 {
			c.r = c.r[:len(c.r)-1]
		}
		return true, nil
	case "ctrl+u":
		c.r = nil
		return true, nil
	}
	if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
		c.add(msg.Text)
		return true, nil
	}
	return false, nil
}

// add appends typed or pasted text (control characters dropped, newlines
// kept: a proposal may be several lines), up to the proposal limit.
func (c *composer) add(text string) {
	rs := []rune(textwidth.SanitizeInput(text))
	c.r = append(c.r, rs[:min(len(rs), max(share.ProposalMax-len(c.r), 0))]...)
}

// shown is the proposal on one line (newlines as ⏎).
func (c *composer) shown() string {
	return textwidth.Sanitize(strings.ReplaceAll(string(c.r), "\n", "⏎"))
}

// view draws the proposal line w cells wide.
func (c *composer) view(ctx *ui.Context, w int, focused bool) string {
	th := ctx.Theme
	prompt := "› "
	if len(c.r) == 0 {
		return lipgloss.NewStyle().Foreground(th.P.Dim).Render(textwidth.Truncate(prompt+ctx.T.Workspace.ComposerPlaceholder(), w, "…"))
	}
	text := textwidth.TruncateLeft(c.shown(), max(w-textwidth.Width(prompt)-1, 1), "…")
	st := lipgloss.NewStyle().Foreground(th.P.FG)
	if focused {
		st = st.Bold(true)
	}
	return st.Render(prompt + text)
}

// cursorX is the cursor column on the proposal line.
func (c *composer) cursorX(w int) int {
	prompt := 2
	if len(c.r) == 0 {
		return prompt
	}
	return prompt + textwidth.Width(textwidth.TruncateLeft(c.shown(), max(w-prompt-1, 1), "…"))
}
