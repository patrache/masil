// Package lazygit finds the user's lazygit, picks the repository and builds
// its launch arguments (v0.1.3 spec A11). It never bundles or links lazygit
// and never runs a shell: every command is an argument vector.
package lazygit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// TestedVersion is the lazygit version masil was tested with (A11.1, JA29:
// no minimum version).
const TestedVersion = "0.66.3"

// Errors.
var (
	ErrNotInstalled = errors.New("lazygit: not installed")
	ErrNotRepo      = errors.New("lazygit: not a git repository")
)

// Runner runs a program and returns its standard output.
type Runner interface {
	Output(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// Env is the outside world the package reads (tests fake it).
type Env struct {
	LookPath func(string) (string, error)
	Getenv   func(string) string
	Run      Runner
	GOOS     string
}

type execRunner struct{}

// waitDelay bounds how long Output waits for the output pipes after ctx ends
// (a child the program left behind can hold them open).
const waitDelay = 500 * time.Millisecond

func (execRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.WaitDelay = waitDelay
	return cmd.Output()
}

// SystemEnv is the real environment.
func SystemEnv() Env {
	return Env{LookPath: exec.LookPath, Getenv: os.Getenv, Run: execRunner{}, GOOS: runtime.GOOS}
}

// Locate returns the lazygit executable: cfgPath ([tools.lazygit] path) when
// set — ErrNotInstalled when it is not an executable — else lazygit on PATH.
func Locate(env Env, cfgPath string) (string, error) {
	name := "lazygit"
	if p := strings.TrimSpace(cfgPath); p != "" {
		name = expandHome(env, p)
	}
	p, err := env.LookPath(name)
	if err != nil || p == "" {
		return "", fmt.Errorf("%w: %s", ErrNotInstalled, name)
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return p, nil
}

// versionRe takes lazygit's own "version=" field (not "git version=").
var versionRe = regexp.MustCompile(`(?:^|,\s*)version=([0-9][0-9A-Za-z.+-]{0,31})(?:,|\s|$)`)

// Version runs "<bin> --version" and returns its version (e.g. "0.66.3").
func Version(ctx context.Context, env Env, bin string) (string, error) {
	out, err := env.Run.Output(ctx, "", bin, "--version")
	if err != nil {
		return "", fmt.Errorf("lazygit: --version: %w", err)
	}
	m := versionRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return "", errors.New("lazygit: --version: no version field")
	}
	return m[1], nil
}

// InstallHint returns the install commands (or page) for an OS (A11.1).
func InstallHint(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"brew install lazygit"}
	case "windows":
		return []string{"winget install -e --id=JesseDuffield.lazygit", "scoop install lazygit"}
	}
	return []string{"https://github.com/jesseduffield/lazygit#installation"}
}

// expandHome turns a leading "~" or "~/" into the home folder.
func expandHome(env Env, p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home := ""
	if env.Getenv != nil {
		home = env.Getenv("HOME")
		if home == "" {
			home = env.Getenv("USERPROFILE")
		}
	}
	if home == "" {
		return p
	}
	return filepath.Join(home, p[1:])
}
