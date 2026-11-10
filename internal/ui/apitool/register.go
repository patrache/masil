package apitool

import (
	"path/filepath"
	"strconv"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
)

// instanceID is the API tool's one instance on the tool surface.
const instanceID = ui.ToolAPI + ":main"

// Route query keys (CA 5.6). queryCmd carries a palette command to the
// instance ("new", "import", "env", "runner", "send"); queryNonce makes a
// repeated command a new query (RouteUpdatedMsg). queryCol + queryReq open a
// personal collection's request; queryDoc + queryReq a shared collection's
// (instance "api:doc:<doc_id>", resolved by P4 Task 18 — until then the
// last personal collection opens).
const (
	queryCmd   = "cmd"
	queryNonce = "n"
	queryCol   = "col"
	queryReq   = "req"
	queryDoc   = "doc"
)

var nonce atomic.Int64

func init() {
	ui.RegisterTool(ui.ToolSpec{
		ID:          ui.ToolAPI,
		Order:       5,
		Icon:        "🧪",
		Name:        func(t *i18n.T) string { return t.Apitest.ToolName() },
		Available:   func(*ui.Context) bool { return features.On(ui.ToolAPI) },
		Status:      hubStatus,
		New:         New,
		Open:        func(*ui.Context, termhost.Mode) tea.Cmd { return openCmd("") },
		WideDefault: true,
	})
	registerCommands()
	registerOverlays()
	registerToolLogin()
}

// openCmd opens (or shows) the instance, running command c in it.
func openCmd(c string) tea.Cmd {
	var q map[string]string
	if c != "" {
		q = map[string]string{queryCmd: c, queryNonce: strconv.FormatInt(nonce.Add(1), 10)}
	}
	return ui.OpenTool(ui.ToolAPI, instanceID, q)
}

// hubStatus is the hub row's status: the last collection and environment.
func hubStatus(ctx *ui.Context) string {
	a := ctx.T.Apitest
	var st savedState
	ctx.ToolState.Section(ui.ToolAPI, &st)
	switch {
	case st.Collection == "":
		return a.HubStatusEmpty()
	case st.Env == "":
		return a.HubStatusNoEnv(textwidth.Sanitize(st.Collection))
	}
	return a.HubStatus(textwidth.Sanitize(st.Collection), textwidth.Sanitize(st.Env))
}

// on is every api.* command's first condition (JC21).
func on(*ui.Context) bool { return features.On(ui.ToolAPI) }

// instanceOpen reports whether the API tool is open on the tool surface.
func instanceOpen(ctx *ui.Context) bool {
	if !on(ctx) || ctx.OpenTools == nil {
		return false
	}
	for _, o := range ctx.OpenTools() {
		if o.Route.Arg == ui.ToolAPI {
			return true
		}
	}
	return false
}

func registerCommands() {
	cmd := func(id string, label func(a i18n.ApitestT) string, avail func(*ui.Context) bool, run func(*ui.Context) tea.Cmd, kw ...string) {
		palette.Register(palette.Command{
			ID:        id,
			Group:     palette.GroupCommand,
			Keywords:  append([]string{"api"}, kw...),
			Label:     func(t *i18n.T) string { return label(t.Apitest) },
			Available: avail,
			Run:       func(ctx *ui.Context, _ string) tea.Cmd { return run(ctx) },
		})
	}
	cmd("api.open", i18n.ApitestT.CmdOpen, on, func(*ui.Context) tea.Cmd { return openCmd("") }, "http", "rest")
	cmd("api.new", i18n.ApitestT.CmdNew, on, func(*ui.Context) tea.Cmd { return openCmd("new") }, "request", "curl")
	cmd("api.import", i18n.ApitestT.CmdImport, on, func(*ui.Context) tea.Cmd { return openCmd("import") }, "import", "postman", "openapi")
	cmd("api.env", i18n.ApitestT.CmdEnv, on, func(*ui.Context) tea.Cmd { return openCmd("env") }, "env", "environment")
	cmd("api.runner", i18n.ApitestT.CmdRunner, on, func(*ui.Context) tea.Cmd { return openCmd("runner") }, "run", "runner")
	cmd("api.send", i18n.ApitestT.CmdSend, instanceOpen, func(*ui.Context) tea.Cmd { return openCmd("send") }, "send")
	cmd("api.history.clear", i18n.ApitestT.CmdHistoryClear, on, clearHistory, "history", "clear")
}

// clearHistory deletes the local history (off the UI goroutine).
func clearHistory(ctx *ui.Context) tea.Cmd {
	dir := filepath.Join(apitool.OpenLocal(ctx.Paths).Dir, "history")
	done := ctx.T.Apitest.HistoryCleared()
	now := ctx.Now
	return func() tea.Msg {
		h := apitool.OpenHistory(dir, apitool.DefaultHistoryOptions(), now)
		if err := h.Clear(); err != nil {
			return ui.ErrorMsg{Err: err}
		}
		return tea.BatchMsg{ui.Toast(done, ui.ToastSuccess), func() tea.Msg { return historyClearedMsg{} }}
	}
}

// historyClearedMsg tells open instances the history is gone.
type historyClearedMsg struct{}
