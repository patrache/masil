package gittool

import "github.com/patrache/masil/internal/lazygit"

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
