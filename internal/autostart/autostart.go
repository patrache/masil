// Package autostart registers `masil daemon run` with the OS login supervisor
// (launchd on macOS, systemd --user on Linux, the HKCU Run key on Windows).
//
// File/value rendering (render.go) is pure and tested by golden files on every OS;
// the OS-specific Install/Uninstall/Query run their side effects through the
// package-level seams below so tests can record them instead.
package autostart

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Status is what Query reports about the autostart registration.
type Status struct {
	Installed, Running bool
	Detail             string // where it is registered ("launchd <plist>", …)
	ExeMissing         string // the registered executable when it no longer exists ("" = present)
}

// Fixed registration names (contract 9.2).
const (
	Label        = "com.patrache.masil.daemon"
	UnitName     = "masil-daemon.service"
	RunValueName = "masil-daemon"
)

// ErrUnsupported is returned on operating systems without an autostart backend.
var ErrUnsupported = errors.New("autostart: not supported on this OS")

// Seams for the OS-specific Install/Uninstall/Query; tests swap them.
var (
	homeDir = os.UserHomeDir
	getuid  = os.Getuid
	run     = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
	lookPath = exec.LookPath // Linux Install checks for systemctl
)

// StableExecutable maps a Homebrew Cellar path (<prefix>/Cellar/masil/<ver>/bin/masil) to <prefix>/bin/masil
// when that exists, and rejects `go run` temp binaries (path containing "go-build") with an
// *ExeError{Kind: ExeTemporary}.
func StableExecutable(exe string) (string, error) {
	slash := filepath.ToSlash(exe)
	if strings.Contains(slash, "go-build") {
		return "", &ExeError{Kind: ExeTemporary, Exe: exe}
	}
	if i := strings.Index(slash, "/Cellar/masil/"); i >= 0 {
		link := filepath.FromSlash(slash[:i] + "/bin/masil")
		if _, err := os.Stat(link); err == nil {
			return link, nil
		}
	}
	return exe, nil
}
