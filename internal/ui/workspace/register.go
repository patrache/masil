// Package workspace is the workspace tool's screens (v0.1.3 spec B6, P2 Tasks
// 20–24): the folder picker, the solo surface (tree, code, terminals), the
// owner's share screens and dialogs. The engine lives in internal/workspace/…;
// this package only draws it and turns keys into engine calls.
package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/store/wsstore"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// pickerID is the folder picker's instance (a route without a folder).
const pickerID = ui.ToolWorkspace + ":pick"

// prefixKeys are the keys this tool adds after ctrl+\ (spec B6.7, JX6).
var prefixKeys = []string{"1", "2", "3", "4", "n", "S", "e", "f"}

func init() {
	ui.RegisterTool(ui.ToolSpec{
		ID:    ui.ToolWorkspace,
		Order: 2,
		Icon:  "🤖",
		Name:  func(t *i18n.T) string { return t.Workspace.HubName() },
		Available: func(*ui.Context) bool {
			return features.On(ui.ToolWorkspace)
		},
		Status: hubStatus,
		New:    func(ctx *ui.Context, r ui.Route) ui.Component { return newSurface(ctx, r) },
		Open: func(*ui.Context, termhost.Mode) tea.Cmd {
			return ui.OpenTool(ui.ToolWorkspace, pickerID, nil)
		},
		PrefixKeys: []ui.PrefixKey{
			{Key: "1", Help: func(t *i18n.T) string { return t.Workspace.PrefixHelpTree() }},
			{Key: "2", Help: func(t *i18n.T) string { return t.Workspace.PrefixHelpCode() }},
			{Key: "3", Help: func(t *i18n.T) string { return t.Workspace.PrefixHelpTerm() }},
			{Key: "4", Help: func(t *i18n.T) string { return t.Workspace.PrefixHelpPeople() }},
			{Key: "n", Help: func(t *i18n.T) string { return t.Workspace.PrefixHelpNewTerm() }},
			{Key: "S", Help: func(t *i18n.T) string { return t.Workspace.PrefixHelpShare() }},
			{Key: "e", Help: func(t *i18n.T) string { return t.Workspace.PrefixHelpExec() }},
			{Key: "f", Help: func(t *i18n.T) string { return t.Workspace.PrefixHelpFollow() }},
		},
		WideDefault: true,
	})
}

// instanceID is the tool instance of a folder: "workspace:" + 8 hex digits of
// the path's SHA-256 (the path itself never goes into routes or state.json).
func instanceID(abs string) string {
	sum := sha256.Sum256([]byte(abs))
	return ui.ToolWorkspace + ":" + hex.EncodeToString(sum[:])[:8]
}

// openFolderCmd opens (or shows again) the instance of folder abs.
func openFolderCmd(abs string) tea.Cmd {
	return ui.OpenTool(ui.ToolWorkspace, instanceID(abs), map[string]string{"open": abs})
}

// live lists the open surfaces (UI goroutine only) for the hub status.
var (
	liveMu sync.Mutex
	live   []*Surface
)

func addLive(s *Surface) {
	liveMu.Lock()
	defer liveMu.Unlock()
	live = append(live, s)
}

func dropLive(s *Surface) {
	liveMu.Lock()
	defer liveMu.Unlock()
	for i, x := range live {
		if x == s {
			live = append(live[:i], live[i+1:]...)
			return
		}
	}
}

// hubStatus is the hub row's status (P2 Task 20): the latest open folder, or
// the invite count.
func hubStatus(ctx *ui.Context) string {
	t := ctx.T.Workspace
	liveMu.Lock()
	var s *Surface
	for i := len(live) - 1; i >= 0; i-- {
		if live[i].root != nil {
			s = live[i]
			break
		}
	}
	liveMu.Unlock()
	if s != nil {
		name := textwidth.Sanitize(s.name)
		if s.sharing() {
			return t.HubStatusSharing(name, s.peopleCount())
		}
		return t.HubStatusPrivate(name, s.term.count())
	}
	n := 0
	if st := wsstore.Get(ctx.Store); st != nil {
		n = len(st.Invited())
	}
	return t.HubStatusIdle(n)
}
