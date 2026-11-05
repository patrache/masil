// Package config resolves the local config directory and reads/writes config.toml (CA 3.1–3.3).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Paths lists every local file under the config directory (CA 3.2).
type Paths struct {
	Dir, Cache                                        string
	Config, Credentials, DeviceID, State, DaemonState string
	AlertsShown, AlertsShownLock, TUILock, DaemonLock string
	LogDir                                            string
}

// ResolvePaths uses MASIL_CONFIG_DIR / ~/.config/masil, MASIL_CACHE_DIR / UserCacheDir.
// XDG_CONFIG_HOME is deliberately ignored on every OS (CA 3.1).
func ResolvePaths() (Paths, error) {
	dir := os.Getenv("MASIL_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("config: resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".config", "masil")
	}
	cache := os.Getenv("MASIL_CACHE_DIR")
	if cache == "" {
		uc, err := os.UserCacheDir()
		if err != nil {
			return Paths{}, fmt.Errorf("config: resolve cache dir: %w", err)
		}
		cache = filepath.Join(uc, "masil")
	}
	return Paths{
		Dir:             dir,
		Cache:           cache,
		Config:          filepath.Join(dir, "config.toml"),
		Credentials:     filepath.Join(dir, "credentials.json"),
		DeviceID:        filepath.Join(dir, "device_id"),
		State:           filepath.Join(dir, "state.json"),
		DaemonState:     filepath.Join(dir, "daemon-state.json"),
		AlertsShown:     filepath.Join(dir, "alerts-shown.json"),
		AlertsShownLock: filepath.Join(dir, "alerts-shown.lock"),
		TUILock:         filepath.Join(dir, "tui.lock"),
		DaemonLock:      filepath.Join(dir, "daemon.lock"),
		LogDir:          filepath.Join(dir, "logs"),
	}, nil
}

// EnsureDir creates the config directory with 0700 (+ logs/) and fixes permissions.
func EnsureDir(p Paths) error {
	for _, d := range []string{p.Dir, p.LogDir} {
		if d == "" {
			return fmt.Errorf("config: empty directory path")
		}
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("config: create %s: %w", d, err)
		}
		if err := fixPerm(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// fixPerm narrows path's permission bits to perm when they are wider
// (no-op on Windows, which ignores permission bits).
func fixPerm(path string, perm os.FileMode) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("config: stat %s: %w", path, err)
	}
	if st.Mode().Perm()&^perm != 0 {
		if err := os.Chmod(path, perm); err != nil {
			return fmt.Errorf("config: chmod %s: %w", path, err)
		}
	}
	return nil
}
