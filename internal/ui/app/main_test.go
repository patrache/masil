package app_test

import (
	"os"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// TestMain is the only TestMain of the app test binary (internal tests included).
// Besides the stubs it registers the RouteTrends watch probe (sync_test.go).
func TestMain(m *testing.M) {
	stub.Install()
	if _, ok := ui.Registered().Routes[ui.RouteTrends]; !ok {
		ui.RegisterRoute(ui.RouteTrends, newWatchProbe)
	}
	os.Exit(m.Run())
}
