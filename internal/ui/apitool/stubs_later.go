package apitool

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
)

// Entry points the later screens fill in.

func (t *Tool) openEnv(*ui.Context) tea.Cmd    { return nil }
func (t *Tool) openRunner(*ui.Context) tea.Cmd { return nil }
func (t *Tool) runQuick(*ui.Context) tea.Cmd   { return nil }
func (t *Tool) tinyStatus(ctx *ui.Context) string {
	if s := t.rv.current(); s != nil && s.resp != nil {
		return statusText(ctx, s, t.size.W, true)
	}
	return ""
}
func registerToolLogin() {}
