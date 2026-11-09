package gittool

import (
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/lazygit"
	"github.com/patrache/masil/internal/termhost"
)

// SetEnvForTest makes the tool find lazygit, run git and read the OS in e
// instead of the real system; the returned func restores it. The hub's
// cached lookup is dropped both ways.
func SetEnvForTest(e lazygit.Env) (restore func()) {
	envMu.Lock()
	saved := envCur
	envCur, probe = e, probeResult{}
	envMu.Unlock()
	return func() {
		envMu.Lock()
		envCur, probe = saved, probeResult{}
		envMu.Unlock()
	}
}

// SetTermEnvForTest makes the instance predict its open mode in e (Tiny from
// the layout, ToolOpen from the config) instead of termhost.CurrentEnv —
// use the same environment as termpanetest's Fake; the returned func
// restores it.
func SetTermEnvForTest(e termhost.Env) (restore func()) {
	saved := termEnv
	termEnv = func(cfg *config.Config, toolID string, tiny bool) termhost.Env {
		out := e
		out.Tiny = tiny
		out.ToolOpen = termhost.Mode(cfg.ToolOpen(toolID))
		return out
	}
	return func() { termEnv = saved }
}
