package gittool

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/lazygit"
)

// Test seams other packages' tests need too (internal/ui/leet and
// internal/ui/e2e open lazygit next to their own tool). Each panics outside
// a test binary, as termpane.SetLaunchForTest does.

func mustTest(name string) {
	if !testing.Testing() {
		panic("gittool: " + name + " outside a test binary")
	}
}

// SetEnvForTest makes the tool find lazygit, run git and read the OS in e
// instead of the real system; the returned func restores it. The hub's
// cached lookup is dropped both ways.
func SetEnvForTest(e lazygit.Env) (restore func()) {
	mustTest("SetEnvForTest")
	envMu.Lock()
	saved := envCur
	envCur, probe, probing = e, probeResult{}, false
	probeGen++
	envMu.Unlock()
	return func() {
		envMu.Lock()
		envCur, probe, probing = saved, probeResult{}, false
		probeGen++
		envMu.Unlock()
	}
}

// StandInVersion is what StandInEnvForTest's lazygit answers to --version.
const StandInVersion = "commit=abc, build date=2026-11-07T00:00:00Z, build source=homebrew, version=0.66.3, os=darwin, arch=arm64, git version=2.47.1"

// StandInEnvForTest is the system's environment — PATH lookups (a stand-in
// lazygit first on PATH is found and a real pane can start it), the
// environment variables, this OS — with a runner that starts no process:
// a lazygit binary answers --version (StandInVersion) and -cd (cfgDir), and
// anything else fails like git outside a repository. Starting processes
// inside a Cmd made the harness's 1 s budget a race under load (v0.1.3-1:
// TestModeSwitchKeepsBothTools, TestFiveToolsOpenTogether,
// TestPaneCapAcrossTools — "Cmd blocked > 1s" in the start Cmd's lazygit -cd).
func StandInEnvForTest(cfgDir string) lazygit.Env {
	mustTest("StandInEnvForTest")
	e := lazygit.SystemEnv()
	e.Run = standInRunner{cfgDir: cfgDir}
	return e
}

type standInRunner struct{ cfgDir string }

func (r standInRunner) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	base := strings.TrimSuffix(filepath.Base(name), ".exe")
	if base == "lazygit" && len(args) == 1 {
		switch args[0] {
		case "--version":
			return []byte(StandInVersion + "\n"), nil
		case "-cd":
			return []byte(r.cfgDir + "\n"), nil
		}
	}
	return nil, errors.New("exit status 128")
}
