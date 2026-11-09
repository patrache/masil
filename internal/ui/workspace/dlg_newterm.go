package workspace

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/workspace/launch"
)

// lookPath finds programs for the new-terminal list (tests may swap it).
var lookPath = exec.LookPath

// openNewTerm shows the new-terminal list (spec B6.4): the login shell,
// claude and nvim when installed, then a typed command.
func (s *Surface) openNewTerm(ctx *ui.Context, mode termhost.Mode) {
	progs := launch.Programs(os.Getenv("SHELL"), lookPath)
	custom := &field{label: ctx.T.Workspace.NewtermCustom()}
	s.dlg = &form{
		title: func(ctx *ui.Context) string { return ctx.T.Workspace.NewtermTitle() },
		build: func(ctx *ui.Context) []row {
			var rows []row
			for _, p := range progs {
				if p.Custom() {
					rows = append(rows, row{kind: rowInput, input: custom, act: func(ctx *ui.Context) tea.Cmd {
						argv := strings.Fields(custom.value())
						if len(argv) == 0 {
							return nil
						}
						s.dlg = nil
						return s.launchTerm(ctx, argv, mode)
					}})
					continue
				}
				argv := p.Argv
				text := textwidth.Sanitize(p.Name)
				if full := strings.Join(argv, " "); full != p.Name {
					text += "  " + textwidth.Sanitize(full)
				}
				rows = append(rows, row{kind: rowOption, text: text, act: func(ctx *ui.Context) tea.Cmd {
					s.dlg = nil
					return s.launchTerm(ctx, argv, mode)
				}})
			}
			return rows
		},
	}
}
