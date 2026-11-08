package autostart

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/patrache/masil/internal/config"
)

// TestDarwinWiring checks that the exported API on macOS is the launchd backend.
func TestDarwinWiring(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, _ := fakeEnv(t, map[string]string{"launchctl bootstrap ": ""})
	plist := filepath.Join(home, "Library/LaunchAgents/com.patrache.masil.daemon.plist")
	if err := Install(writeExe(t), config.Paths{}); err != nil {
		t.Fatal(err)
	}
	if st, err := Query(); err != nil || !st.Installed || st.Detail != "launchd "+plist {
		t.Fatalf("Query = %+v, %v", st, err)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plist); !os.IsNotExist(err) {
		t.Fatal("plist left behind")
	}
}
