package tools

import (
	"log/slog"
	"slices"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The hub's open-mode menu (A7.2, hub "m"): this one open, the ways that work
// here. The choice is remembered (tools-state.json Common.LastOpen) as the
// menu's next starting row; enter and the digits keep opening with ModeAuto
// (the config decides).

// currentEnv is termhost.CurrentEnv (tests: SetEnvForTest).
var currentEnv = termhost.CurrentEnv

// concreteModes are the menu's ways, in menu order after auto.
var concreteModes = []termhost.Mode{termhost.ModePane, termhost.ModeExec, termhost.ModeWindow}

// possibleModes are the ways tool can open in env: each concrete mode that
// termhost.Choose honours as asked (no downgrade), with auto first when more
// than one works (Windows: exec alone).
func possibleModes(env termhost.Env, tool string) []termhost.Mode {
	var out []termhost.Mode
	for _, m := range concreteModes {
		if got, down := termhost.Choose(termhost.Request{ToolID: tool, Mode: m}, env); got == m && !down {
			out = append(out, m)
		}
	}
	if len(out) > 1 {
		out = append([]termhost.Mode{termhost.ModeAuto}, out...)
	}
	return out
}

// modeName is m in t's language.
func modeName(t *i18n.T, m termhost.Mode) string {
	switch m {
	case termhost.ModePane:
		return t.Tools.ModePane()
	case termhost.ModeExec:
		return t.Tools.ModeExec()
	case termhost.ModeWindow:
		return t.Tools.ModeWindow()
	}
	return t.Tools.ModeAuto()
}

// openMenu is the open-mode menu of one tool.
type openMenu struct {
	tool  string
	modes []termhost.Mode
	sel   int
}

// newOpenMenu builds tool's menu in ctx's environment, starting on the
// remembered last choice when it is still possible.
func newOpenMenu(ctx *ui.Context, tool string) *openMenu {
	env := currentEnv(ctx.Config, tool, ctx.Mode == layout.ModeTiny)
	m := &openMenu{tool: tool, modes: possibleModes(env, tool)}
	if i := slices.Index(m.modes, ctx.ToolState.Common(tool).LastOpen); i >= 0 {
		m.sel = i
	}
	return m
}

func (m *openMenu) move(d int) {
	if n := len(m.modes); n > 0 {
		m.sel = min(max(m.sel+d, 0), n-1)
	}
}

// choose remembers the selected way and returns it (ok=false: no way at all).
func (m *openMenu) choose(ctx *ui.Context) (termhost.Mode, bool) {
	if m.sel < 0 || m.sel >= len(m.modes) {
		return "", false
	}
	mode := m.modes[m.sel]
	c := ctx.ToolState.Common(m.tool)
	c.LastOpen = mode
	if err := ctx.ToolState.SetCommon(m.tool, c); err != nil {
		slog.Warn("tools-state.json: open mode not saved", "tool", m.tool, "err", err)
	}
	return mode, true
}
