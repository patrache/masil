package autostart

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// runKeyPath is the per-user Run key under HKEY_CURRENT_USER.
const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

const runKeyDetail = `HKCU\` + runKeyPath + `\` + RunValueName

// runKeyStore is the HKCU Run key; a missing value reports an error matching fs.ErrNotExist
// (registry.ErrNotExist is ERROR_FILE_NOT_FOUND, which does).
type runKeyStore interface {
	Get(name string) (string, error)
	Set(name, value string) error
	Delete(name string) error
}

// runKeyInstall sets masil-daemon = `"<exe>" daemon run`. The Run key cannot carry environment
// variables, so a custom MASIL_CONFIG_DIR is refused instead of silently using the default dir.
func runKeyInstall(keys runKeyStore, exe string) error {
	if err := checkExe(exe); err != nil {
		return err
	}
	if os.Getenv("MASIL_CONFIG_DIR") != "" {
		return ErrRunKeyConfigDir
	}
	if err := keys.Set(RunValueName, runValue(exe)); err != nil {
		return fmt.Errorf("autostart: set %s: %w", runKeyDetail, err)
	}
	return nil
}

// runKeyUninstall deletes the value; an absent value is fine.
func runKeyUninstall(keys runKeyStore) error {
	if err := keys.Delete(RunValueName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("autostart: delete %s: %w", runKeyDetail, err)
	}
	return nil
}

// runKeyQuery: Installed = value exists, Running = false (no supervisor to ask; `masil daemon
// status` uses the daemon's own lock instead), Detail = the value's registry path.
func runKeyQuery(keys runKeyStore) (Status, error) {
	st := Status{Detail: runKeyDetail}
	v, err := keys.Get(RunValueName)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, fmt.Errorf("autostart: read %s: %w", runKeyDetail, err)
	}
	st.Installed = true
	withExeCheck(&st, runValueExe(v))
	return st, nil
}
