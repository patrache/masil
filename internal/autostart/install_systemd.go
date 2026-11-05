package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/patrache/masil/internal/config"
)

var errNoSystemd = errors.New("systemd 사용자 세션을 찾지 못했습니다 — 'masil daemon run' 을 셸 시작 파일에 넣어 실행하세요")

func systemdUnitPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("autostart: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user", UnitName), nil
}

func haveSystemctl() bool {
	_, err := lookPath("systemctl")
	return err == nil
}

// probeUserManager reports whether a systemd user manager answers. A systemctl
// binary alone is not enough: WSL without systemd, containers and SSH sessions
// with no user bus have systemctl but `systemctl --user` cannot reach anything.
func probeUserManager() error {
	if !haveSystemctl() {
		return errNoSystemd
	}
	if _, err := runCmd("systemctl", "--user", "show-environment"); err != nil {
		return fmt.Errorf("%w (%v)", errNoSystemd, err)
	}
	return nil
}

// systemdInstall checks that a user manager answers, writes the user unit (0644), then
// daemon-reload, enable and restart. restart (not `enable --now`) also replaces an already
// running old daemon with the new ExecStart on reinstall. If a systemctl step fails the
// registration is rolled back (best-effort disable, unit removed, daemon-reload) so status
// never reports an autostart that will not run.
func systemdInstall(exe string, p config.Paths) error {
	if err := checkExe(exe); err != nil {
		return err
	}
	if err := probeUserManager(); err != nil {
		return err
	}
	dir, err := configDirFor(p)
	if err != nil {
		return err
	}
	unit, err := systemdUnitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		return fmt.Errorf("autostart: create %s: %w", filepath.Dir(unit), err)
	}
	if err := config.WriteFileAtomic(unit, systemdUnit(exe, dir), 0o644); err != nil {
		return err
	}
	for i, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", UnitName},
		{"--user", "restart", UnitName},
	} {
		if _, err := runCmd("systemctl", args...); err != nil {
			if i >= 1 { // enable may have linked the unit into default.target.wants
				_, _ = run("systemctl", "--user", "disable", UnitName)
			}
			if rerr := os.Remove(unit); rerr != nil && !os.IsNotExist(rerr) {
				return fmt.Errorf("autostart: %w (and remove %s: %v)", err, unit, rerr)
			}
			_, _ = run("systemctl", "--user", "daemon-reload")
			return fmt.Errorf("autostart: %w", err)
		}
	}
	return nil
}

// systemdUninstall: disable --now (failure ignored), remove the unit (absent is fine), and
// daemon-reload when a unit file was actually removed. Without systemctl or without a reachable
// user manager only the file is removed (there is nothing loaded to reload).
func systemdUninstall() error {
	unit, err := systemdUnitPath()
	if err != nil {
		return err
	}
	manager := probeUserManager() == nil
	if manager {
		_, _ = run("systemctl", "--user", "disable", "--now", UnitName)
	}
	removed := true
	if err := os.Remove(unit); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("autostart: remove %s: %w", unit, err)
		}
		removed = false
	}
	if manager && removed {
		if _, err := runCmd("systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("autostart: %w", err)
		}
	}
	return nil
}

// systemdQuery: Installed = unit file exists, Running = `systemctl --user is-active` prints
// "active", Detail = "systemd --user <unit>" (+ missing-executable note).
func systemdQuery() (Status, error) {
	unit, err := systemdUnitPath()
	if err != nil {
		return Status{}, err
	}
	st := Status{Detail: "systemd --user " + unit}
	installed, err := fileExists(unit)
	if err != nil {
		return st, fmt.Errorf("autostart: %w", err)
	}
	if haveSystemctl() {
		// is-active exits non-zero for inactive units; the printed state is what counts.
		out, _ := run("systemctl", "--user", "is-active", UnitName)
		st.Running = strings.TrimSpace(string(out)) == "active"
	}
	if !installed {
		return st, nil
	}
	st.Installed = true
	b, err := os.ReadFile(unit)
	if err != nil {
		return st, fmt.Errorf("autostart: read %s: %w", unit, err)
	}
	st.Detail = withExeCheck(st.Detail, unitExe(b))
	return st, nil
}
