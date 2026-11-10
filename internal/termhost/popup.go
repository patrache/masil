package termhost

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/patrache/masil/internal/textwidth"
)

// ErrNoTmux is returned by Popup outside tmux ($TMUX unset).
var ErrNoTmux = errors.New("termhost: not inside tmux")

// tmuxPath is the tmux binary (a seam for tests).
var tmuxPath = "tmux"

// tmuxVersionTimeout bounds `tmux -V`. A variable only so the fake tmux in
// tests can outlast a loaded machine's process start.
var tmuxVersionTimeout = 300 * time.Millisecond

func init() { detectTmuxVersion = TmuxVersion }

// TmuxVersion returns the tmux version ("3.3a", "next-3.5") from `tmux -V`,
// or "" when tmux is missing, fails or takes longer than 300ms.
func TmuxVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tmuxPath, "-V")
	cmd.WaitDelay = tmuxVersionTimeout
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	v = strings.TrimSpace(strings.TrimPrefix(v, "tmux"))
	if _, _, ok := parseTmuxVersion(v); !ok {
		return ""
	}
	return v
}

// popupArgs builds the tmux command line for req. -T (popup title) exists
// from tmux 3.3; on 3.2 it is left out. tmux expands -d and -T as formats
// ("#(cmd)" would run a shell command, "#S" is replaced), so every '#' in
// them is doubled. The program always goes as two or more arguments: tmux
// hands a lone argument to sh -c, which would re-parse a path with spaces or
// shell characters. env(1) carries req.Env (the popup runs in the tmux
// server's environment, not masil's) and is the second argument when there
// are no Args.
func popupArgs(req Request, version string) []string {
	args := []string{"display-popup", "-E"}
	if dir := req.dir(ModeWindow); dir != "" {
		args = append(args, "-d", tmuxEscape(dir))
	}
	args = append(args, "-w", "90%", "-h", "90%")
	if title := textwidth.Sanitize(req.Title); title != "" && tmuxAtLeast(version, 3, 3) {
		args = append(args, "-T", tmuxEscape(title))
	}
	args = append(args, "--")
	prog, pargs := req.command(ModeWindow)
	if len(req.Env) > 0 || len(pargs) == 0 {
		args = append(args, "env")
		args = append(args, req.Env...)
	}
	args = append(args, prog)
	return append(args, pargs...)
}

// tmuxEscape makes s literal in a tmux format.
func tmuxEscape(s string) string { return strings.ReplaceAll(s, "#", "##") }

// Popup opens req in a tmux popup covering 90% of the window and returns at
// once; masil keeps drawing and receiving meanwhile. The channel receives one
// ExitInfo (Mode ModeWindow, Code = tmux's exit code) when the popup closes,
// then is closed. Outside tmux it returns ErrNoTmux.
func Popup(req Request) (<-chan ExitInfo, error) {
	if os.Getenv("TMUX") == "" {
		return nil, ErrNoTmux
	}
	cmd := exec.Command(tmuxPath, popupArgs(req, TmuxVersion())...)
	ch := make(chan ExitInfo, 1)
	go func() {
		defer close(ch)
		info := ExitInfo{Program: req.Program, Title: req.Title, Mode: ModeWindow}
		info.Code, info.Err = exitResult(cmd.Run())
		ch <- info
	}()
	return ch, nil
}
