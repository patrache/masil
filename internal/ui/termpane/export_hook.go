package termpane

import "testing"

// SetLaunchForTest replaces Launch (termpanetest.Install) and returns the
// restore func. It panics outside a test binary.
func SetLaunchForTest(f LaunchFunc) (restore func()) {
	if !testing.Testing() {
		panic("termpane: SetLaunchForTest outside a test binary")
	}
	if f == nil {
		f = launch
	}
	old := launchFn.Swap(&f)
	return func() { launchFn.Store(old) }
}
