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

// The tiny layout shows a summary only (spec B6.7): alone, the folder and a
// hint; sharing, who is in, the keyboard holder, the proposals to answer
// right there, and how to end.

// tinyView draws the summary.
func (s *Surface) tinyView(ctx *ui.Context, w, h int) []string {
	t := ctx.T.Workspace
	th := ctx.Theme
	dim := lipgloss.NewStyle().Foreground(th.P.Dim)
	if s.own == nil {
		lines := []string{s.bandLine(ctx, w)}
		for _, l := range textwidth.Wrap(t.TinySoloSummary(textwidth.Sanitize(s.name)), max(w-1, 1)) {
			lines = append(lines, " "+dim.Render(l))
		}
		if n := len(s.docs.Dirty()); n > 0 {
			lines = append(lines, th.S.Warning.Render(textwidth.Truncate(" "+t.TermTitleUnsaved(n), w, "…")))
		}
		return lines
	}
	host := s.own.host
	ms := host.Members()
	ps := host.Proposals()
	parts := []string{t.TinyOwnerSummary(len(ms))}
	now := nowClock(ctx).Now()
	for _, m := range ms {
		if m.Level == wire.LevelKeyboard {
			parts = append(parts, t.TinyKeyboard(s.peer(ctx, m.Seat, m.UserID).handle, mmss(m.KeyboardUntil.Sub(now))))
		}
	}
	parts = append(parts, t.TinyProposals(len(ps)))
	red := lipgloss.NewStyle().Foreground(th.P.Acc2).Bold(true)
	var lines []string
	for _, l := range textwidth.Wrap(strings.Join(parts, " · "), max(w-1, 1)) {
		lines = append(lines, " "+red.Render(l))
	}
	sel := s.tinySel(len(ps))
	hint := t.TinyOwnerKeys()
	if sel < len(ps) && ps[sel].Kind != "" { // a file request: only a or r
		hint = t.TinyOwnerKeysFs()
	}
	keys := packItems(strings.Split(hint, " · "), " · ", max(w-1, 1))
	room := max(h-len(lines)-len(keys), 0)
	for i, p := range ps {
		if i >= room {
			break
		}
		name := s.peer(ctx, p.Seat, s.seatUser(p.Seat)).handle
		head := name
		if p.Kind != "" { // a rename or delete is named as one
			head = t.ProposalFrom(name, proposalKind(ctx, p))
		}
		text := textwidth.Truncate(" "+head+": "+proposalText(ctx, p), w, "…")
		if i == sel {
			text = th.S.Selected.Render(textwidth.PadRight(text, w))
		}
		lines = append(lines, text)
	}
	for len(lines) < h-len(keys) {
		lines = append(lines, "")
	}
	for _, l := range keys {
		lines = append(lines, " "+dim.Render(l))
	}
	return lines
}

// tinySel clamps the selected proposal.
func (s *Surface) tinySel(n int) int {
	if s.own == nil {
		return 0
	}
	s.own.tinySel = max(0, min(s.own.tinySel, n-1))
	return s.own.tinySel
}

// tinyKey answers proposals and ends the share from the summary.
func (s *Surface) tinyKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if s.own == nil {
		return false, nil
	}
	ps := s.own.host.Proposals()
	k := msg.String()
	switch k {
	case "x":
		s.dlg = newEndDialog(s)
		return true, nil
	case "j", "down":
		s.own.tinySel++
		s.tinySel(len(ps))
		return true, nil
	case "k", "up":
		s.own.tinySel = max(0, s.own.tinySel-1)
		return true, nil
	}
	if len(ps) == 0 {
		return false, nil
	}
	p := ps[s.tinySel(len(ps))]
	var act share.ProposalAction
	switch k {
	case "s":
		act = share.ActInsertSend
	case "i":
		act = share.ActInsert
	case "a":
		act = share.ActApprove
	case "r":
		act = share.ActReject
	default:
		return false, nil
	}
	// The queue dialog's rule: a file request is carried out only with a, a
	// proposal is typed only with i or s.
	if (p.Kind != "") != (act == share.ActApprove) && act != share.ActReject {
		return true, nil
	}
	return true, s.actOnProposal(ctx, p, act, "")
}

// packItems puts items on as few lines as fit w, never splitting one (an
// item wider than w wraps on its own).
func packItems(items []string, sep string, w int) []string {
	var out []string
	cur := ""
	for _, it := range items {
		switch {
		case cur == "":
			cur = it
		case textwidth.Width(cur+sep+it) <= w:
			cur += sep + it
		default:
			out = append(out, textwidth.Wrap(cur, w)...)
			cur = it
		}
	}
	if cur != "" {
		out = append(out, textwidth.Wrap(cur, w)...)
	}
	return out
}
