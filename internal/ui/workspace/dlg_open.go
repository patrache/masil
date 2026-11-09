package workspace

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// newPicker is the folder picker (an instance without a folder, spec B6.1):
// the recent folders (device only) and a path field.
func newPicker(ctx *ui.Context, s *Surface) *form {
	recent := recentFolders(ctx)
	path := &field{label: ctx.T.Workspace.PathPrompt()}
	return &form{
		title: func(ctx *ui.Context) string { return ctx.T.Workspace.PickFolderTitle() },
		build: func(ctx *ui.Context) []row {
			t := ctx.T.Workspace
			rows := []row{{kind: rowHead, text: t.RecentFolders()}}
			if len(recent) == 0 {
				rows = append(rows, row{kind: rowText, text: t.RecentNone(), dim: true})
			}
			for _, dir := range recent {
				rows = append(rows, row{kind: rowOption, text: textwidth.Sanitize(baseName(dir)) + "  " + textwidth.Sanitize(dir), act: func(ctx *ui.Context) tea.Cmd {
					return s.pickFolder(ctx, dir)
				}})
			}
			rows = append(rows, row{kind: rowText}, row{kind: rowInput, input: path, act: func(ctx *ui.Context) tea.Cmd {
				return s.pickFolder(ctx, path.value())
			}})
			return rows
		},
		cancel: func(*ui.Context) tea.Cmd { return nil },
	}
}

// pickFolder opens dir (the instance already showing it, if any) and closes
// the picker.
func (s *Surface) pickFolder(ctx *ui.Context, dir string) tea.Cmd {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[1:])
		}
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		var fi os.FileInfo
		if fi, err = os.Stat(abs); err == nil && !fi.IsDir() {
			err = os.ErrInvalid
		}
	}
	if err != nil {
		return ui.Toast(ctx.T.Workspace.OpenFailed(), ui.ToastError)
	}
	open := openFolderCmd(abs)
	liveMu.Lock()
	for _, o := range live {
		if o.root != nil && o.dir == abs && !o.closed {
			r := o.route
			open = func() tea.Msg { return ui.NavigateMsg{Route: r} }
			break
		}
	}
	liveMu.Unlock()
	return tea.Batch(open, ui.CloseTool(s.id))
}
