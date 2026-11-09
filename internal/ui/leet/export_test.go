package leet

import (
	"time"

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

// ReloadConfigForTest reads config.toml again into ctx.Config.
func ReloadConfigForTest(ctx *ui.Context) error {
	c, err := config.Load(ctx.Paths)
	if err != nil {
		return err
	}
	*ctx.Config = *c
	return nil
}

// DescribeLoginForTest is the settings row's describe for ctx's account.
func DescribeLoginForTest(ctx *ui.Context) (string, bool) { return describeLogin(ctx) }

// ForgetRunForTest drops every account's run memory (as a restart of masil does).
func ForgetRunForTest() {
	memoMu.Lock()
	defer memoMu.Unlock()
	memos = map[account]*memo{}
}

// PacerIntervalForTest is the run / submit gap of ctx's account.
func PacerIntervalForTest(ctx *ui.Context) time.Duration { return pacerOf(ctx).Interval }

// ForgetLoginForTest runs the settings row's [지우기] for ctx's account (its Cmd too).
func ForgetLoginForTest(ctx *ui.Context) {
	if cmd := forgetLogin(ctx); cmd != nil {
		cmd()
	}
}

// SetLoginAccountForTest remembers user (and premium) as ctx's account's
// LeetCode sign-in in tools-state, as a past login did.
func SetLoginAccountForTest(ctx *ui.Context, user string, premium bool) {
	setLCAccount(ctx, &lcAccount{Username: user, Premium: premium})
	memoMu.Lock()
	defer memoMu.Unlock()
	if m := memos[accountOf(ctx)]; m != nil {
		m.lc = nil // as read back on a later start
	}
}
