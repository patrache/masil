package termpane

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/termhost"
)

// SetLaunchEnvForTest replaces the environment Launch decides in.
func SetLaunchEnvForTest(f func(cfg *config.Config, toolID string, tiny bool) termhost.Env) (restore func()) {
	old := currentEnv
	currentEnv = f
	return func() { currentEnv = old }
}

// SetStartPaneForTest replaces termhost.StartPane for Launch.
func SetStartPaneForTest(f func(req termhost.Request, size termhost.Size, opts termhost.PaneOptions) (*termhost.Pane, error)) (restore func()) {
	old := startPane
	startPane = f
	return func() { startPane = old }
}

// SetExecForTest replaces termhost.Exec for Launch.
func SetExecForTest(f func(req termhost.Request, onExit func(termhost.ExitInfo) tea.Msg) tea.Cmd) (restore func()) {
	old := execProgram
	execProgram = f
	return func() { execProgram = old }
}

// SetPopupForTest replaces termhost.Popup for Launch.
func SetPopupForTest(f func(req termhost.Request) (<-chan termhost.ExitInfo, error)) (restore func()) {
	old := popupProgram
	popupProgram = f
	return func() { popupProgram = old }
}
