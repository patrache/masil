package autostart

import (
	"os"
	"testing"

	"github.com/patrache/masil/internal/config"
)

// TestLinuxWiring checks that the exported API on Linux is the systemd --user backend.
func TestLinuxWiring(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, _ := fakeEnv(t, map[string]string{"systemctl --user ": ""})
	if err := Install(writeExe(t), config.Paths{}); err != nil {
		t.Fatal(err)
	}
	if st, err := Query(); err != nil || !st.Installed || st.Detail != "systemd --user "+unitPath(home) {
		t.Fatalf("Query = %+v, %v", st, err)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unitPath(home)); !os.IsNotExist(err) {
		t.Fatal("unit left behind")
	}
}
