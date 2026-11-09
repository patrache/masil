package workspace

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// newJoinSASDialog is the participant's check-number dialog (spec B6.3 #3–4):
// the number, how to compare it, and "match" / "different". "Different" ends
// the attempt (the owner is warned that someone may be in the middle). If
// the owner lets me in unchecked first, the dialog goes and the band shows
// 미확인.
func newJoinSASDialog(s *Surface) *form {
	g := s.guest
	f := &form{shareOnly: true, title: func(ctx *ui.Context) string { return ctx.T.Workspace.JoinSasTitle(g.ownerDisplay()) }}
	f.build = func(ctx *ui.Context) []row {
		t := ctx.T.Workspace
		return []row{{kind: rowText}, {kind: rowBig, text: formatSAS(g.sas)}, {kind: rowText},
			{kind: rowText, text: t.SasCompareHint()}, {kind: rowText},
			{kind: rowOption, text: t.JoinMatch(), act: func(ctx *ui.Context) tea.Cmd {
				s.dlg = nil
				return s.guestAnswer(ctx, true)
			}},
			{kind: rowOption, text: t.JoinMismatch(), warn: true, act: func(ctx *ui.Context) tea.Cmd {
				s.dlg = nil
				return s.guestAnswer(ctx, false)
			}},
		}
	}
	// esc is not "different": the number stays to be answered (the band
	// keeps waiting; ctrl+\ enter opens it again)
	f.cancel = func(*ui.Context) tea.Cmd { return nil }
	return f
}

// guestAnswer sends my answer to the check-number comparison.
func (s *Surface) guestAnswer(ctx *ui.Context, match bool) tea.Cmd {
	g := s.guest
	if g.eng == nil || g.answered {
		return nil
	}
	g.answered = true
	if err := g.eng.Confirm(match); err != nil {
		g.answered = false
		return ui.Toast(ctx.T.Workspace.OpenFailed(), ui.ToastError)
	}
	if match && g.phase < guestIn {
		g.phase = guestWaiting
	}
	return s.armPump(ctx)
}
