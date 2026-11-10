package autostart

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/config"
)

// The launchd backend is OS-independent code (only install_darwin.go wires it to Install),
// so these tests run on every OS.

func TestInstallDarwin(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, map[string]string{"launchctl bootstrap ": ""})
	exe := writeExe(t) // temp executable file, absolute path
	if err := launchdInstall(exe, config.Paths{Dir: ignoredDir}); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(home, "Library/LaunchAgents/com.patrache.masil.daemon.plist")
	b, _ := os.ReadFile(plist)
	if !bytes.Equal(b, launchdPlist(exe, "")) {
		t.Fatal("plist content mismatch")
	}
	if fi, err := os.Stat(plist); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o644) {
		t.Fatalf("plist mode: %v, %v", fi, err)
	}
	want := []string{
		"launchctl bootout gui/501/com.patrache.masil.daemon",
		"launchctl bootstrap gui/501 " + plist,
	}
	if !slices.Equal(*calls, want) {
		t.Fatalf("calls %q", *calls)
	}
	*calls = nil
	if err := launchdInstall(exe, config.Paths{}); err != nil {
		t.Fatal("reinstall must be idempotent:", err)
	}
	if !slices.Equal(*calls, want) {
		t.Fatalf("reinstall must bootout + bootstrap again: %q", *calls)
	}
}

func TestInstallDarwinConfigDir(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", ignoredDir)
	home, _ := fakeEnv(t, map[string]string{"launchctl bootstrap ": ""})
	exe := writeExe(t)
	if err := launchdInstall(exe, config.Paths{Dir: ignoredDir}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, "Library/LaunchAgents/com.patrache.masil.daemon.plist"))
	if !bytes.Equal(b, launchdPlist(exe, ignoredDir)) {
		t.Fatalf("plist must carry MASIL_CONFIG_DIR:\n%s", b)
	}
}

func TestInstallDarwinBootstrapRetry(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, nil)
	prevRun := run
	failures := 1
	run = func(name string, args ...string) ([]byte, error) {
		out, err := prevRun(name, args...) // records the call
		if len(args) > 0 && args[0] == "bootstrap" {
			if failures > 0 {
				failures--
				return []byte("Bootstrap failed: 5: Input/output error"), err
			}
			return nil, nil
		}
		return out, err
	}
	if err := launchdInstall(writeExe(t), config.Paths{}); err != nil {
		t.Fatal("a bootstrap racing the previous bootout must be retried:", err)
	}
	plist := filepath.Join(home, "Library/LaunchAgents/com.patrache.masil.daemon.plist")
	want := []string{
		"launchctl bootout gui/501/com.patrache.masil.daemon",
		"launchctl bootstrap gui/501 " + plist,
		"launchctl bootstrap gui/501 " + plist,
	}
	if !slices.Equal(*calls, want) {
		t.Fatalf("calls %q", *calls)
	}
}

func TestInstallDarwinBootstrapFails(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	fakeEnv(t, nil)
	err := launchdInstall(writeExe(t), config.Paths{})
	if err == nil || !strings.Contains(err.Error(), "launchctl bootstrap") || !strings.Contains(err.Error(), "No such process") {
		t.Fatalf("want a launchctl bootstrap error with its output, got %v", err)
	}
}

func TestQueryDetectsMissingExe(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, _ := fakeEnv(t, map[string]string{})
	exe := writeExe(t)
	launchdInstall(exe, config.Paths{})
	os.Remove(exe)
	st, err := launchdQuery()
	if err != nil || !st.Installed || st.Running || st.ExeMissing != exe || strings.Contains(st.Detail, exe) {
		t.Fatalf("status %+v, %v (home %s)", st, err, home)
	}
}

func TestQueryDarwin(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, map[string]string{
		"launchctl bootstrap ":                              "",
		"launchctl print gui/501/com.patrache.masil.daemon": "gui/501/com.patrache.masil.daemon = {\n\tactive count = 1\n\tstate = running\n}\n",
	})
	plist := filepath.Join(home, "Library/LaunchAgents/com.patrache.masil.daemon.plist")

	st, err := launchdQuery()
	if err != nil || st.Installed || st.Detail != "launchd "+plist {
		t.Fatalf("not installed: %+v, %v", st, err)
	}
	if err := launchdInstall(writeExe(t), config.Paths{}); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	st, err = launchdQuery()
	if err != nil || !st.Installed || !st.Running || st.Detail != "launchd "+plist {
		t.Fatalf("running: %+v, %v", st, err)
	}
	if !slices.Equal(*calls, []string{"launchctl print gui/501/com.patrache.masil.daemon"}) {
		t.Fatalf("calls %q", *calls)
	}
}

func TestQueryDarwinLoadedNotRunning(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	fakeEnv(t, map[string]string{
		"launchctl bootstrap ": "",
		"launchctl print ":     "gui/501/com.patrache.masil.daemon = {\n\tstate = not running\n\tlast exit code = 1\n}\n",
	})
	if err := launchdInstall(writeExe(t), config.Paths{}); err != nil {
		t.Fatal(err)
	}
	st, err := launchdQuery()
	if err != nil || !st.Installed || st.Running {
		t.Fatalf("status %+v, %v", st, err)
	}
}

func TestUninstallDarwin(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, map[string]string{})
	launchdInstall(writeExe(t), config.Paths{})
	*calls = nil
	if err := launchdUninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "Library/LaunchAgents/com.patrache.masil.daemon.plist")); !os.IsNotExist(err) {
		t.Fatal("plist left behind")
	}
	if !slices.Equal(*calls, []string{"launchctl bootout gui/501/com.patrache.masil.daemon"}) {
		t.Fatalf("calls %q", *calls)
	}
	if err := launchdUninstall(); err != nil {
		t.Fatal("second uninstall must be a no-op:", err)
	}
}
