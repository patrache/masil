package workspace

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/share"
)

// endHow is what happens to the participants' unsaved edits.
type endHow int

const (
	endKeep endHow = iota
	endSave
	endDiscard
)

// shareEndMsg: the engine finished ending the share.
type shareEndMsg struct{ surface string }

// newEndDialog ends the share (ws.share.end, x in tiny): the participants'
// unsaved files first (save / discard / keep open), an optional ctrl+c to
// the shared programs, then Host.End. The programs keep running.
func newEndDialog(s *Surface) *form {
	host := s.own.host
	unsaved := host.EndCheck()
	stop := false
	f := &form{shareOnly: true, title: func(ctx *ui.Context) string { return ctx.T.Workspace.EndTitle() }}
	f.build = func(ctx *ui.Context) []row {
		t := ctx.T.Workspace
		var rows []row
		if len(unsaved) > 0 {
			rows = append(rows, row{kind: rowHead, text: t.EndUnsavedHead()})
			for _, rel := range unsaved {
				rows = append(rows, row{kind: rowText, text: "· " + textwidth.Sanitize(rel)})
			}
			rows = append(rows, row{kind: rowText}, row{kind: rowText, text: t.EndUnsavedPrompt()}, row{kind: rowText})
		}
		rows = append(rows, row{kind: rowText, text: t.EndProgramsKeep(), dim: true})
		if s.own != nil && len(s.own.terms) > 0 {
			rows = append(rows, row{kind: rowOption, text: t.StopProgramOption(), checked: &stop, act: func(*ui.Context) tea.Cmd {
				stop = !stop
				return nil
			}})
		}
		rows = append(rows, row{kind: rowText})
		end := func(how endHow) func(*ui.Context) tea.Cmd {
			return func(ctx *ui.Context) tea.Cmd {
				s.dlg = nil
				return s.endShare(ctx, unsaved, how, stop)
			}
		}
		if len(unsaved) > 0 {
			rows = append(rows,
				row{kind: rowOption, text: t.EndSave(), act: end(endSave)},
				row{kind: rowOption, text: t.EndDiscard(), act: end(endDiscard)},
				row{kind: rowOption, text: t.EndKeep(), act: end(endKeep)})
		} else {
			rows = append(rows, row{kind: rowOption, text: t.EndConfirm(), act: end(endKeep)})
		}
		return append(rows, row{kind: rowOption, text: t.Cancel(), act: func(*ui.Context) tea.Cmd { s.dlg = nil; return nil }})
	}
	return f
}

// endShare settles the unsaved files, interrupts the programs when asked and
// ends the share in the background.
func (s *Surface) endShare(ctx *ui.Context, unsaved []string, how endHow, stop bool) tea.Cmd {
	o := s.own
	if o == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, rel := range unsaved {
		switch how {
		case endSave:
			if err := s.docs.Save(rel, 0); err != nil {
				cmds = append(cmds, ui.Toast(ctx.T.Workspace.SaveFailed(), ui.ToastError))
			}
		case endDiscard:
			s.docs.Discard(rel)
			if d := s.code.find(rel); d != nil {
				s.resync(d)
			}
		}
	}
	if stop {
		for _, t := range s.term.terms {
			if _, ok := o.terms[t.id]; ok {
				if st := shareTerminal(t); st != nil {
					_ = st.Inject([]byte{0x03})
				}
			}
		}
	}
	host, id := o.host, s.id
	cmds = append(cmds, func() tea.Msg {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := host.End(c); err != nil && err != share.ErrEnded {
			host.Kill()
		}
		return shareEndMsg{surface: id}
	})
	return tea.Batch(cmds...)
}
