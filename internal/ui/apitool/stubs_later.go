package apitool

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// Entry points the later screens fill in.

func (t *Tool) onPaste(*ui.Context, tea.PasteMsg) tea.Cmd { return nil }
func (t *Tool) saveRequest(*ui.Context) tea.Cmd           { return nil }
func (t *Tool) openEnv(*ui.Context) tea.Cmd               { return nil }
func (t *Tool) openRunner(*ui.Context) tea.Cmd            { return nil }
func (t *Tool) runQuick(*ui.Context) tea.Cmd              { return nil }
func (t *Tool) openImport(*ui.Context) tea.Cmd            { return nil }
func (t *Tool) openExport(*ui.Context) tea.Cmd            { return nil }
func (t *Tool) send(*ui.Context) tea.Cmd                  { return nil }
func (t *Tool) tinyStatus(ctx *ui.Context) string {
	if s := t.rv.current(); s != nil && s.resp != nil {
		return statusText(ctx, s, t.size.W, true)
	}
	return ""
}
func registerOverlays()  {}
func registerToolLogin() {}

func (e *editor) handleKey(ctx *ui.Context, t *Tool, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	return false, nil
}

func (e *editor) settingRows(*ui.Context) []string { return nil }
