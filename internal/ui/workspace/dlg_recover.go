package workspace

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/journal"
)

// newRecoverDialog offers the recovery journal's text when the folder opens
// (spec B7.6): recover puts it back as edits (unsaved until :w), discard
// drops it.
func newRecoverDialog(s *Surface, entries []journal.Entry) *form {
	rels := make([]string, len(entries))
	for i, e := range entries {
		rels[i] = e.Rel
	}
	return &form{
		title: func(ctx *ui.Context) string { return ctx.T.Workspace.RecoveryAvailable() },
		build: func(ctx *ui.Context) []row {
			t := ctx.T.Workspace
			rows := []row{{kind: rowText, text: t.RecoveryBody(len(rels))}, {kind: rowText}}
			for _, rel := range rels {
				rows = append(rows, row{kind: rowText, text: "· " + textwidth.Sanitize(rel), dim: true})
			}
			rows = append(rows, row{kind: rowText},
				row{kind: rowOption, text: t.RecoveryRestore(), act: func(ctx *ui.Context) tea.Cmd { return s.recoverAll(ctx, rels) }},
				row{kind: rowOption, text: t.RecoveryDiscard(), act: func(ctx *ui.Context) tea.Cmd { return s.discardRecovery(rels) }},
			)
			return rows
		},
	}
}

// recoverAll reopens each journaled document with its text as edits.
func (s *Surface) recoverAll(ctx *ui.Context, rels []string) tea.Cmd {
	s.dlg = nil
	var cmds []tea.Cmd
	failed := false
	for _, rel := range rels {
		v, err := s.docs.Recover(rel)
		if err != nil {
			failed = true
			continue
		}
		cmds = append(cmds, s.attachDoc(ctx, v))
	}
	if failed {
		cmds = append(cmds, ui.Toast(ctx.T.Workspace.CodeCannotOpen(), ui.ToastError))
	}
	return tea.Batch(cmds...)
}

// discardRecovery drops the journaled text.
func (s *Surface) discardRecovery(rels []string) tea.Cmd {
	s.dlg = nil
	if s.jr != nil {
		for _, rel := range rels {
			_ = s.jr.Drop(rel)
		}
	}
	return nil
}
