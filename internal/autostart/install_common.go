package autostart

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/patrache/masil/internal/config"
)

// The three backends (launchd, systemd --user, HKCU Run) are plain OS-independent code so
// every backend is tested on every OS; install_<goos>.go only picks which one Install,
// Uninstall and Query use.

// checkExe requires exe to be an absolute path to an existing regular file.
func checkExe(exe string) error {
	if !filepath.IsAbs(exe) {
		return &ExeError{Kind: ExeNotAbsolute, Exe: exe}
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return &ExeError{Kind: ExeMissing, Exe: exe, Err: err}
	}
	if !fi.Mode().IsRegular() {
		return &ExeError{Kind: ExeNotRegular, Exe: exe}
	}
	return nil
}

// configDirFor returns the config dir to pass to the daemon: p.Dir (made absolute, since the
// supervisor starts the daemon from another working directory) only when MASIL_CONFIG_DIR is set.
func configDirFor(p config.Paths) (string, error) {
	if os.Getenv("MASIL_CONFIG_DIR") == "" || p.Dir == "" {
		return "", nil
	}
	dir, err := filepath.Abs(p.Dir)
	if err != nil {
		return "", fmt.Errorf("autostart: resolve config dir %s: %w", p.Dir, err)
	}
	return dir, nil
}

// withExeCheck sets st.ExeMissing to exe when the registered executable is
// gone (the CLI renders the note); a registration naming no executable reports "?".
func withExeCheck(st *Status, exe string) {
	if exe != "" {
		if fi, err := os.Stat(exe); err == nil && fi.Mode().IsRegular() {
			return
		}
	}
	if exe == "" {
		exe = "?"
	}
	st.ExeMissing = exe
}

// fileExists reports whether path exists; errors other than "not exist" are returned.
func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// runCmd runs name args through the run seam and wraps a failure with the command line and its output.
func runCmd(name string, args ...string) ([]byte, error) {
	out, err := run(name, args...)
	if err != nil {
		line := strings.Join(append([]string{name}, args...), " ")
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return out, fmt.Errorf("%s: %w: %s", line, err, msg)
		}
		return out, fmt.Errorf("%s: %w", line, err)
	}
	return out, nil
}
