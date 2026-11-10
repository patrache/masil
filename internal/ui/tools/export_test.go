package tools

import (
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/termhost"
)

// SetEnvForTest makes the open-mode menu decide in f's environment instead of
// termhost.CurrentEnv; the returned func restores it.
func SetEnvForTest(f func(cfg *config.Config, toolID string, tiny bool) termhost.Env) (restore func()) {
	saved := currentEnv
	currentEnv = f
	return func() { currentEnv = saved }
}
