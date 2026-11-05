// Package config resolves the local config directory and reads/writes config.toml (CA 3.1–3.3).
package config

// Paths lists every local file under the config directory (CA 3.2).
type Paths struct {
	Dir, Cache                                        string
	Config, Credentials, DeviceID, State, DaemonState string
	AlertsShown, AlertsShownLock, TUILock, DaemonLock string
	LogDir                                            string
}

// ResolvePaths uses MASIL_CONFIG_DIR / ~/.config/masil, MASIL_CACHE_DIR / UserCacheDir.
func ResolvePaths() (Paths, error) {
	// TODO(core): Task 4
	return Paths{}, nil
}

// EnsureDir creates the config directory with 0700 (+ logs/) and fixes permissions.
func EnsureDir(p Paths) error {
	// TODO(core): Task 4
	return nil
}
