package leet

import (
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
)

// SetLeetcodeForTest points every LeetCode client this package makes at
// baseURL (a leetcodetest fake) instead of leetcode.com; the returned func
// restores it. Production code has no way to change the address.
func SetLeetcodeForTest(baseURL string) (restore func()) {
	clientMu.Lock()
	saved := leetcodeURL
	leetcodeURL = baseURL
	clientMu.Unlock()
	return func() {
		clientMu.Lock()
		leetcodeURL = saved
		clientMu.Unlock()
	}
}

// SetOpenURLForTest replaces the browser opener; the returned func restores it.
func SetOpenURLForTest(f func(url string) error) (restore func()) {
	openMu.Lock()
	saved := openURL
	openURL = f
	openMu.Unlock()
	return func() {
		openMu.Lock()
		openURL = saved
		openMu.Unlock()
	}
}

// MismatchForTest is masil's slug for date when it differs from this device's.
func MismatchForTest(ctx *ui.Context, date string) string { return mismatchOn(ctx, date) }

// SetLookPathForTest replaces the PATH lookup of nvim; the returned func restores it.
func SetLookPathForTest(f func(string) (string, error)) (restore func()) {
	seamMu.Lock()
	saved := lookPath
	lookPath = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		lookPath = saved
		seamMu.Unlock()
	}
}

// SetTermEnvForTest makes the instance decide pane-or-not in e (Tiny from the
// layout, ToolOpen from the config) — the same as termpanetest's Fake env.
func SetTermEnvForTest(e termhost.Env) (restore func()) {
	seamMu.Lock()
	saved := termEnv
	termEnv = func(cfg *config.Config, toolID string, tiny bool) termhost.Env {
		out := e
		out.Tiny = tiny
		out.ToolOpen = termhost.Mode(cfg.ToolOpen(toolID))
		return out
	}
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		termEnv = saved
		seamMu.Unlock()
	}
}

// RequestsForTest is how many run / submit requests instance c got.
func RequestsForTest(c ui.Component) (run, submit int) {
	in := c.(*instance)
	return in.runReqs, in.submitReqs
}
