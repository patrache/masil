package gittool

import (
	"runtime"
	"time"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/lazygit"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
)

// SetEnvForTest makes the tool find lazygit, run git and read the OS in e
// instead of the real system; the returned func restores it. The hub's
// cached lookup is dropped both ways.
func SetEnvForTest(e lazygit.Env) (restore func()) {
	envMu.Lock()
	saved := envCur
	envCur, probe, probing = e, probeResult{}, false
	probeGen++
	envMu.Unlock()
	return func() {
		envMu.Lock()
		envCur, probe, probing = saved, probeResult{}, false
		probeGen++
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

// WaitProbeForTest waits until the hub's lazygit lookup has finished (Fatal
// after 2s); the next draw shows its answer.
func WaitProbeForTest(t interface {
	Helper()
	Fatal(...any)
}) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		envMu.Lock()
		busy := probing
		envMu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("gittool: the lazygit lookup did not finish")
		}
		runtime.Gosched()
	}
}

// LaunchedModeForTest is the mode termpane.Launch decided for instance c
// (termpane.Launched.Mode; "" before it launched or when c is not one).
func LaunchedModeForTest(c ui.Component) termhost.Mode {
	in, ok := c.(*instance)
	if !ok {
		return ""
	}
	return in.mode
}
