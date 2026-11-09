package workspace

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

// The proposal queue (spec B8.3): what participants asked to type, and
// their file rename/delete requests, waiting for the owner.

// onProposal: a new proposal joined the queue (the band counts it).
func (s *Surface) onProposal(*ui.Context, share.Proposal) tea.Cmd { return nil }

// proposalKind is a proposal's kind as shown.
func proposalKind(ctx *ui.Context, p share.Proposal) string {
	t := ctx.T.Workspace
	switch p.Kind {
	case wire.FsRename:
		return t.ProposalKindRename()
	case wire.FsDelete:
		return t.ProposalKindDelete()
	}
	return t.ProposalKindText()
}

// proposalText is the proposal folded to one line (sanitized).
func proposalText(ctx *ui.Context, p share.Proposal) string {
	switch p.Kind {
	case wire.FsRename:
		return textwidth.Sanitize(p.From) + " → " + textwidth.Sanitize(p.To)
	case wire.FsDelete:
		return textwidth.Sanitize(p.From)
	}
	lines := strings.Split(strings.TrimRight(p.Text, "\n"), "\n")
	first := textwidth.Sanitize(lines[0])
	if len(lines) > 1 {
		first += " " + ctx.T.Workspace.ProposalMoreLines(len(lines)-1)
	}
	return first
}

// actOnProposal runs one owner action; a multi-line paste into a program
// without paste mode asks once more.
func (s *Surface) actOnProposal(ctx *ui.Context, p share.Proposal, act share.ProposalAction, edited string) tea.Cmd {
	if s.own == nil {
		return nil
	}
	host := s.own.host
	need, err := host.HandleProposal(p.ID, act, edited)
	switch {
	case err == share.ErrNoTerminal:
		return ui.Toast(ctx.T.Workspace.ProposalNoTerminal(), ui.ToastError)
	case err != nil:
		return nil
	case need:
		prev := s.dlg
		s.dlg = &form{shareOnly: true, title: func(ctx *ui.Context) string { return ctx.T.Workspace.ProposalsTitle(len(host.Proposals())) },
			build: func(ctx *ui.Context) []row {
				t := ctx.T.Workspace
				rows := []row{{kind: rowText, text: t.MultilineConfirm(), warn: true}, {kind: rowText}}
				for _, l := range strings.Split(p.Text, "\n") {
					rows = append(rows, row{kind: rowText, text: textwidth.Sanitize(l), dim: true})
				}
				return append(rows, row{kind: rowText},
					row{kind: rowOption, text: t.ProposalInsert(), act: func(*ui.Context) tea.Cmd {
						_, _ = host.HandleProposal(p.ID, share.ActConfirmMultiline, "")
						s.dlg = prev
						s.closeEmptyQueue()
						return nil
					}},
					row{kind: rowOption, text: t.Cancel(), act: func(*ui.Context) tea.Cmd { s.dlg = prev; return nil }})
			}}
	}
	return nil
}

// closeEmptyQueue closes the queue dialog when nothing waits any more.
func (s *Surface) closeEmptyQueue() {
	if s.own != nil && s.dlg != nil && s.dlg.queue && len(s.own.host.Proposals()) == 0 {
		s.dlg = nil
	}
}

// openProposals shows the queue (ctrl+\ enter, ws.proposals, the band).
func (s *Surface) openProposals(ctx *ui.Context) tea.Cmd {
	if s.own == nil {
		return nil
	}
	s.dlg = newProposalsDialog(s)
	return nil
}

// newProposalsDialog lists the queue; i/s/e/r (a for file requests) act
// on the selected one.
func newProposalsDialog(s *Surface) *form {
	host := s.own.host
	var editing *field
	var editID string
	f := &form{shareOnly: true, queue: true}
	f.title = func(ctx *ui.Context) string { return ctx.T.Workspace.ProposalsTitle(len(host.Proposals())) }
	selected := func(ctx *ui.Context) (share.Proposal, bool) {
		ps := host.Proposals()
		rows := f.build(ctx)
		cur := f.current(rows)
		if cur < 0 || rows[cur].kind != rowOption {
			return share.Proposal{}, false
		}
		i := rows[cur].index
		if i < 0 || i >= len(ps) {
			return share.Proposal{}, false
		}
		return ps[i], true
	}
	f.hint = func(ctx *ui.Context) string {
		if p, ok := selected(ctx); ok && p.Kind != "" {
			return ctx.T.Workspace.ProposalsHintFs()
		}
		return ctx.T.Workspace.ProposalsHint()
	}
	f.keys = func(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
		if editing != nil {
			switch msg.String() {
			case "enter":
				text := editing.value()
				id := editID
				editing = nil
				for _, p := range host.Proposals() {
					if p.ID == id {
						cmd := s.actOnProposal(ctx, p, share.ActEdit, text)
						s.closeEmptyQueue()
						return true, cmd
					}
				}
				return true, nil
			case "esc":
				editing = nil
				return true, nil
			}
			editing.key(msg)
			return true, nil
		}
		p, ok := selected(ctx)
		if !ok {
			return false, nil
		}
		var act share.ProposalAction
		switch msg.String() {
		case "i":
			act = share.ActInsert
		case "s": // never enter: running a command is always an explicit s
			act = share.ActInsertSend
		case "r":
			act = share.ActReject
		case "a":
			act = share.ActApprove
		case "e":
			if p.Kind == "" {
				editing = &field{label: ctx.T.Workspace.ProposalEditLabel(), r: []rune(strings.ReplaceAll(textwidth.SanitizeInput(p.Text), "\n", " "))}
				editID = p.ID
			}
			return true, nil
		default:
			return false, nil
		}
		if p.Kind != "" && act != share.ActApprove && act != share.ActReject {
			return true, nil
		}
		if p.Kind == "" && act == share.ActApprove {
			return true, nil
		}
		cmd := s.actOnProposal(ctx, p, act, "")
		s.closeEmptyQueue()
		return true, cmd
	}
	f.build = func(ctx *ui.Context) []row {
		t := ctx.T.Workspace
		ps := host.Proposals()
		if len(ps) == 0 {
			return []row{{kind: rowText, text: t.ProposalsEmpty(), dim: true}}
		}
		var rows []row
		for i, p := range ps {
			name := s.peer(ctx, p.Seat, s.seatUser(p.Seat)).name
			rows = append(rows, row{kind: rowOption, index: i, text: t.ProposalFrom(name, proposalKind(ctx, p)) + ": " + proposalText(ctx, p)})
			if editing != nil && p.ID == editID {
				rows = append(rows, row{kind: rowInput, input: editing})
			}
		}
		return rows
	}
	return f
}

// seatUser is the user id at seat (members, then join requests).
func (s *Surface) seatUser(seat uint8) string {
	if s.own == nil {
		return ""
	}
	for _, m := range s.own.host.Members() {
		if m.Seat == seat {
			return m.UserID
		}
	}
	for _, m := range s.own.members {
		if uint8(m.Seat) == seat {
			return m.User.ID
		}
	}
	return ""
}
