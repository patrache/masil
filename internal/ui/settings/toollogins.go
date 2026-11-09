package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/secretsui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// The "도구 로그인" section (v0.1.3 spec A9.4): one row per tool that keeps a
// login on this device — "LeetCode · sam_lc · 키체인 · [지우기]". Values are
// never shown. The section is drawn only while at least one registered tool
// (feature flag on) reports a login, so the 0.1.2 settings screen is unchanged.

// secTools is the section id (Route.Anchor "tools"); its title is the catalog's.
const secTools = "tools"

// rowToolPrefix + tool id is a tool login row.
const rowToolPrefix = "tools."

// toolLogin is one RegisterToolLogin entry.
type toolLogin struct {
	tool     string
	describe func(ctx *ui.Context) (account string, ok bool)
	forget   func(ctx *ui.Context) tea.Cmd
}

var (
	toolLoginsMu sync.Mutex
	toolLogins   []toolLogin
)

// RegisterToolLogin adds tool's row (init()-time; P1 LeetCode, P4 API test).
// describe names the login ("sam_lc", "변수 3개"; ok=false: no row) and must
// be cheap (it runs on every draw, never touching the OS store). [지우기]
// calls forget when it is not nil; otherwise every value the tool keeps for
// this masil account is deleted from ctx.Secrets. An empty tool id, a nil
// describe or a duplicate panics.
func RegisterToolLogin(tool string, describe func(ctx *ui.Context) (account string, ok bool), forget func(ctx *ui.Context) tea.Cmd) {
	if tool == "" || describe == nil {
		panic("settings: RegisterToolLogin: empty tool id or nil describe")
	}
	toolLoginsMu.Lock()
	defer toolLoginsMu.Unlock()
	if slices.ContainsFunc(toolLogins, func(l toolLogin) bool { return l.tool == tool }) {
		panic(fmt.Sprintf("settings: RegisterToolLogin: duplicate tool %q", tool))
	}
	toolLogins = append(toolLogins, toolLogin{tool: tool, describe: describe, forget: forget})
}

// ResetToolLoginsForTest empties the registrations and returns the restore
// func (t.Cleanup(settings.ResetToolLoginsForTest())). Tests only.
func ResetToolLoginsForTest() (restore func()) {
	if !testing.Testing() {
		panic("settings: ResetToolLoginsForTest called outside a test")
	}
	toolLoginsMu.Lock()
	saved := slices.Clone(toolLogins)
	toolLogins = nil
	toolLoginsMu.Unlock()
	return func() {
		toolLoginsMu.Lock()
		toolLogins = saved
		toolLoginsMu.Unlock()
	}
}

// registeredToolLogins are the registrations in hub order (ToolSpec.Order) of
// the tools whose flag is on, with their specs.
func registeredToolLogins() ([]toolLogin, []ui.ToolSpec) {
	toolLoginsMu.Lock()
	all := slices.Clone(toolLogins)
	toolLoginsMu.Unlock()
	var ls []toolLogin
	var specs []ui.ToolSpec
	for _, l := range all {
		if spec, ok := ui.ToolByID(l.tool); ok {
			ls, specs = append(ls, l), append(specs, spec)
		}
	}
	idx := make([]int, len(ls))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int { return specs[a].Order - specs[b].Order })
	outL, outS := make([]toolLogin, len(ls)), make([]ui.ToolSpec, len(ls))
	for i, j := range idx {
		outL[i], outS[i] = ls[j], specs[j]
	}
	return outL, outS
}

// toolLoginRows are the section's rows (none: the section is not drawn).
func (s *screen) toolLoginRows(ctx *ui.Context) []row {
	logins, specs := registeredToolLogins()
	var out []row
	for i, l := range logins {
		account, ok := l.describe(ctx)
		if !ok {
			continue
		}
		name := l.tool
		if specs[i].Name != nil {
			name = specs[i].Name(ctx.T)
		}
		parts := []string{textwidth.Sanitize(account)}
		if where := secretsui.BackendLabel(ctx.T, toolWhere(ctx, l.tool)); where != "" {
			parts = append(parts, where)
		}
		parts = append(parts, ctx.T.Tools.LoginsForget())
		out = append(out, row{id: rowToolPrefix + l.tool, sec: secTools, kind: kindAction,
			label: textwidth.Sanitize(name), sub: strings.Join(parts, " · "), wrapSub: true})
	}
	return out
}

// toolWhere is where the tool's first value (by name) is kept ("" = none or
// unknown); it never calls the OS store.
func toolWhere(ctx *ui.Context, tool string) secrets.Backend {
	server, user, ok := ctx.SecretScope()
	if !ok || ctx.Secrets == nil {
		return ""
	}
	names, err := ctx.Secrets.List(context.Background(), tool, server, user)
	if err != nil || len(names) == 0 {
		return ""
	}
	return ctx.Secrets.Where(secrets.Key{Tool: tool, Server: server, UserID: user, Name: names[0]})
}

// toolLoginForgetMsg is the confirmed [지우기] of tool (handled on the UI
// goroutine: forget gets the live ctx).
type toolLoginForgetMsg struct{ tool string }

// toolLoginForgotMsg ends a [지우기] that deleted from ctx.Secrets.
type toolLoginForgotMsg struct {
	tool string
	err  error
}

// toolLoginOf is tool's registration.
func toolLoginOf(tool string) (toolLogin, bool) {
	toolLoginsMu.Lock()
	defer toolLoginsMu.Unlock()
	i := slices.IndexFunc(toolLogins, func(l toolLogin) bool { return l.tool == tool })
	if i < 0 {
		return toolLogin{}, false
	}
	return toolLogins[i], true
}

// confirmForget is enter on a tool login row: a Confirm, then forget.
func (s *screen) confirmForget(ctx *ui.Context, r row) tea.Cmd {
	tool := strings.TrimPrefix(r.id, rowToolPrefix)
	if _, ok := toolLoginOf(tool); !ok {
		return nil
	}
	delete(s.rowErr, r.id)
	tr := ctx.T.Tools
	confirmed := func() tea.Msg { return toolLoginForgetMsg{tool: tool} }
	return ui.OpenOverlay(widgets.Confirm(tr.LoginsForgetTitle(r.label), tr.LoginsForgetBody(), tr.LoginsForgetYes(), tr.LoginsForgetNo(), confirmed))
}

// forget runs the confirmed [지우기]: the tool's forget, else deleting every
// value it keeps for this masil account (off the UI goroutine: an OS store
// call can wait on a prompt).
func (s *screen) forget(ctx *ui.Context, tool string) tea.Cmd {
	l, ok := toolLoginOf(tool)
	if !ok {
		return nil
	}
	if l.forget != nil {
		return l.forget(ctx)
	}
	st := ctx.Secrets
	server, user, scoped := ctx.SecretScope()
	if !scoped || st == nil {
		return nil
	}
	return func() tea.Msg {
		c := context.Background()
		names, err := st.List(c, tool, server, user)
		for _, n := range names {
			if derr := st.Delete(c, secrets.Key{Tool: tool, Server: server, UserID: user, Name: n}); derr != nil {
				err = errors.Join(err, derr)
			}
		}
		return toolLoginForgotMsg{tool: tool, err: err}
	}
}

// onToolLoginForgot shows a failed [지우기] under its row.
func (s *screen) onToolLoginForgot(ctx *ui.Context, m toolLoginForgotMsg) {
	if m.err == nil {
		delete(s.rowErr, rowToolPrefix+m.tool)
		return
	}
	slog.Warn("forget tool login", "tool", m.tool, "err", m.err)
	s.rowErr[rowToolPrefix+m.tool] = ctx.T.Tools.LoginsForgetFailed()
}
