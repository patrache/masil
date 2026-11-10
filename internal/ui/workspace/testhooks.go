package workspace

import (
	"testing"

	"github.com/patrache/masil/internal/workspace/share"
)

// SetDialForTest makes shares connect through dial with the engine on
// ManualTick (the pump ticks it on the harness clock). Other packages' tests
// need it too (internal/ui/e2e shares a workspace through sharetest next to
// the other tools, P0 Task 31), so it is not in export_test.go; it panics
// outside a test binary, as termpane.SetLaunchForTest does.
func SetDialForTest(dial share.DialFunc) (restore func()) {
	if !testing.Testing() {
		panic("workspace: SetDialForTest outside a test binary")
	}
	oldDial, oldManual := dialFor, manualTick
	dialFor, manualTick = dial, true
	return func() { dialFor, manualTick = oldDial, oldManual }
}
