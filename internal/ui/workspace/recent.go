package workspace

import (
	"log/slog"
	"slices"

	"github.com/patrache/masil/internal/ui"
)

// maxRecent is how many folders the picker remembers (A8.5).
const maxRecent = 10

// wsState is this tool's section of tools-state.json (device only, never sent
// to the server): the recently opened folders, newest first.
type wsState struct {
	Recent []string `json:"recent"`
}

// recentFolders reads the remembered folders.
func recentFolders(ctx *ui.Context) []string {
	var st wsState
	ctx.ToolState.Section(ui.ToolWorkspace, &st)
	return st.Recent
}

// rememberFolder puts abs first in the recent list (no duplicates, at most
// maxRecent).
func rememberFolder(ctx *ui.Context, abs string) {
	var st wsState
	ctx.ToolState.Section(ui.ToolWorkspace, &st)
	st.Recent = slices.DeleteFunc(st.Recent, func(s string) bool { return s == abs || s == "" })
	st.Recent = append([]string{abs}, st.Recent...)
	if len(st.Recent) > maxRecent {
		st.Recent = st.Recent[:maxRecent]
	}
	if err := ctx.ToolState.SetSection(ui.ToolWorkspace, st); err != nil {
		slog.Warn("workspace: remember folder", "err", err)
	}
}
