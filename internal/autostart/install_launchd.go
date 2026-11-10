package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/patrache/masil/internal/config"
)

// bootstrapAttempts / bootstrapRetryDelay bound the retry of `launchctl bootstrap` right after
// `bootout`: a bootout of a running job can return before launchd has finished tearing it down,
// and the immediate bootstrap then fails ("Bootstrap failed: 5: Input/output error").
const bootstrapAttempts = 4

var bootstrapRetryDelay = 500 * time.Millisecond

func launchdPlistPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("autostart: resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

func launchdDomain() string  { return "gui/" + strconv.Itoa(getuid()) }
func launchdService() string { return launchdDomain() + "/" + Label }

// launchdInstall writes the LaunchAgent plist (0644), boots out any loaded copy (failure ignored)
// and bootstraps the new one, so a reinstall always applies the new registration.
func launchdInstall(exe string, p config.Paths) error {
	if err := checkExe(exe); err != nil {
		return err
	}
	dir, err := configDirFor(p)
	if err != nil {
		return err
	}
	plist, err := launchdPlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return fmt.Errorf("autostart: create %s: %w", filepath.Dir(plist), err)
	}
	if err := config.WriteFileAtomic(plist, launchdPlist(exe, dir), 0o644); err != nil {
		return err
	}
	_, _ = run("launchctl", "bootout", launchdService()) // not loaded => fails; that is fine
	for attempt := 1; ; attempt++ {
		_, err = runCmd("launchctl", "bootstrap", launchdDomain(), plist)
		if err == nil {
			return nil
		}
		if attempt == bootstrapAttempts {
			return fmt.Errorf("autostart: %w", err)
		}
		time.Sleep(bootstrapRetryDelay)
	}
}

// launchdUninstall boots the job out (failure ignored) and removes the plist (absent is fine).
func launchdUninstall() error {
	plist, err := launchdPlistPath()
	if err != nil {
		return err
	}
	_, _ = run("launchctl", "bootout", launchdService())
	if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("autostart: remove %s: %w", plist, err)
	}
	return nil
}

// launchdQuery: Installed = plist exists, Running = `launchctl print` succeeds and reports
// "state = running", Detail = "launchd <plist>" (+ missing-executable note).
func launchdQuery() (Status, error) {
	plist, err := launchdPlistPath()
	if err != nil {
		return Status{}, err
	}
	st := Status{Detail: "launchd " + plist}
	installed, err := fileExists(plist)
	if err != nil {
		return st, fmt.Errorf("autostart: %w", err)
	}
	if out, err := run("launchctl", "print", launchdService()); err == nil {
		st.Running = strings.Contains(string(out), "state = running")
	}
	if !installed {
		return st, nil
	}
	st.Installed = true
	b, err := os.ReadFile(plist)
	if err != nil {
		return st, fmt.Errorf("autostart: read %s: %w", plist, err)
	}
	withExeCheck(&st, plistExe(b))
	return st, nil
}
