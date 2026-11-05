package app_test

import (
	"os"
	"testing"

	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// TestMain is the only TestMain of the app test binary (internal tests included).
func TestMain(m *testing.M) {
	stub.Install()
	os.Exit(m.Run())
}
