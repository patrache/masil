package autostart

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/config"
)

// The systemd backend is OS-independent code (only install_linux.go wires it to Install),
// so these tests run on every OS.

var systemdInstallCalls = []string{
	"systemctl --user daemon-reload",
	"systemctl --user enable masil-daemon.service",
	"systemctl --user restart masil-daemon.service",
}

func unitPath(home string) string {
	return filepath.Join(home, ".config/systemd/user/masil-daemon.service")
}

func TestInstallLinux(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, map[string]string{"systemctl --user ": ""})
	exe := writeExe(t)
	if err := systemdInstall(exe, config.Paths{Dir: ignoredDir}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(unitPath(home))
	if !bytes.Equal(b, systemdUnit(exe, "")) {
		t.Fatalf("unit content mismatch:\n%s", b)
	}
	if fi, err := os.Stat(unitPath(home)); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o644) {
		t.Fatalf("unit mode: %v, %v", fi, err)
	}
	if !slices.Equal(*calls, systemdInstallCalls) {
		t.Fatalf("calls %q", *calls)
	}
	// Reinstall / upgrade: the same three commands again, so a running old daemon is restarted
	// onto the new ExecStart (`enable --now` would leave it running).
	*calls = nil
	if err := systemdInstall(exe, config.Paths{}); err != nil {
		t.Fatal("reinstall must be idempotent:", err)
	}
	if !slices.Equal(*calls, systemdInstallCalls) {
		t.Fatalf("reinstall calls %q", *calls)
	}
	for _, c := range *calls {
		if strings.Contains(c, "enable --now") {
			t.Fatalf("must not use enable --now: %q", c)
		}
	}
}

func TestInstallLinuxConfigDir(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", ignoredDir)
	home, _ := fakeEnv(t, map[string]string{"systemctl --user ": ""})
	exe := writeExe(t)
	if err := systemdInstall(exe, config.Paths{Dir: ignoredDir}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(unitPath(home))
	if !bytes.Equal(b, systemdUnit(exe, ignoredDir)) {
		t.Fatalf("unit must carry MASIL_CONFIG_DIR:\n%s", b)
	}
}

func TestInstallLinuxNoSystemctl(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, map[string]string{"systemctl --user ": ""})
	lookPath = func(string) (string, error) { return "", errors.New("executable file not found in $PATH") }
	err := systemdInstall(writeExe(t), config.Paths{})
	if err == nil || !strings.Contains(err.Error(), "systemd 사용자 세션을 찾지 못했습니다 — 'masil daemon run' 을 셸 시작 파일에 넣어 실행하세요") {
		t.Fatalf("err = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls %q", *calls)
	}
	if _, err := os.Stat(unitPath(home)); !os.IsNotExist(err) {
		t.Fatal("unit written without systemctl")
	}
}

func TestInstallLinuxCommandFails(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	_, calls := fakeEnv(t, map[string]string{
		"systemctl --user daemon-reload": "",
		"systemctl --user enable ":       "",
	})
	err := systemdInstall(writeExe(t), config.Paths{})
	if err == nil || !strings.Contains(err.Error(), "systemctl --user restart masil-daemon.service") {
		t.Fatalf("err = %v", err)
	}
	if !slices.Equal(*calls, systemdInstallCalls) {
		t.Fatalf("calls %q", *calls)
	}
}

func TestQueryLinux(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, map[string]string{
		"systemctl --user daemon-reload":                  "",
		"systemctl --user enable ":                        "",
		"systemctl --user restart ":                       "",
		"systemctl --user is-active masil-daemon.service": "active\n",
	})
	st, err := systemdQuery()
	if err != nil || st.Installed || st.Detail != "systemd --user "+unitPath(home) {
		t.Fatalf("not installed: %+v, %v", st, err)
	}
	if err := systemdInstall(writeExe(t), config.Paths{}); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	st, err = systemdQuery()
	if err != nil || !st.Installed || !st.Running || st.Detail != "systemd --user "+unitPath(home) {
		t.Fatalf("running: %+v, %v", st, err)
	}
	if !slices.Equal(*calls, []string{"systemctl --user is-active masil-daemon.service"}) {
		t.Fatalf("calls %q", *calls)
	}
}

func TestQueryLinuxInactiveAndMissingExe(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	fakeEnv(t, map[string]string{
		"systemctl --user daemon-reload": "",
		"systemctl --user enable ":       "",
		"systemctl --user restart ":      "",
	})
	prevRun := run
	run = func(name string, args ...string) ([]byte, error) {
		out, err := prevRun(name, args...)
		if slices.Contains(args, "is-active") {
			return []byte("inactive\n"), errors.New("exit status 3")
		}
		return out, err
	}
	exe := writeExe(t)
	if err := systemdInstall(exe, config.Paths{}); err != nil {
		t.Fatal(err)
	}
	os.Remove(exe)
	st, err := systemdQuery()
	if err != nil || !st.Installed || st.Running || !strings.HasSuffix(st.Detail, " · 실행 파일 없음: "+exe) {
		t.Fatalf("status %+v, %v", st, err)
	}
}

func TestUninstallLinux(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, map[string]string{
		"systemctl --user daemon-reload": "",
		"systemctl --user enable ":       "",
		"systemctl --user restart ":      "",
	})
	if err := systemdInstall(writeExe(t), config.Paths{}); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	// disable --now fails here (not matched) and must be ignored.
	if err := systemdUninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unitPath(home)); !os.IsNotExist(err) {
		t.Fatal("unit left behind")
	}
	want := []string{
		"systemctl --user disable --now masil-daemon.service",
		"systemctl --user daemon-reload",
	}
	if !slices.Equal(*calls, want) {
		t.Fatalf("calls %q", *calls)
	}
	if err := systemdUninstall(); err != nil {
		t.Fatal("second uninstall must be a no-op:", err)
	}
}

func TestUninstallLinuxNoSystemctl(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	home, calls := fakeEnv(t, map[string]string{"systemctl --user ": ""})
	if err := systemdInstall(writeExe(t), config.Paths{}); err != nil {
		t.Fatal(err)
	}
	*calls = nil
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if err := systemdUninstall(); err != nil {
		t.Fatal("uninstall without systemctl must still remove the unit:", err)
	}
	if _, err := os.Stat(unitPath(home)); !os.IsNotExist(err) {
		t.Fatal("unit left behind")
	}
	if len(*calls) != 0 {
		t.Fatalf("calls %q", *calls)
	}
}
