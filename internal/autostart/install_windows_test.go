package autostart

import (
	"testing"

	"github.com/patrache/masil/internal/config"
)

var _ runKeyStore = registryRunKey{}

// TestWindowsWiring checks that the exported API on Windows goes through the runKey seam
// (swapped for a map so the real HKCU Run key is never touched).
func TestWindowsWiring(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	prev := runKey
	t.Cleanup(func() { runKey = prev })
	keys := mapRunKey{}
	runKey = keys
	exe := writeExe(t)
	if err := Install(exe, config.Paths{}); err != nil {
		t.Fatal(err)
	}
	if got, want := keys[RunValueName], `"`+exe+`" daemon run`; got != want {
		t.Fatalf("Run value = %q, want %q", got, want)
	}
	if st, err := Query(); err != nil || !st.Installed || st.Running {
		t.Fatalf("Query = %+v, %v", st, err)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("value left behind: %v", keys)
	}
}
