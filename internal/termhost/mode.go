// Package termhost starts external programs for tools in one of three ways
// (spec A7): exec (hand the whole terminal over, tea.Exec), window (a tmux
// popup) or pane (an embedded PTY drawn by a VT emulator). Tools describe
// what they want in a Request; Choose picks the way.
//
// termhost does not import internal/ui or any tool package.
package termhost

import (
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/patrache/masil/internal/config"
)

// Mode is how a program is opened.
type Mode string

// Modes.
const (
	ModeAuto   Mode = "auto"   // let Choose decide (A7.2 rules)
	ModePane   Mode = "pane"   // embedded PTY pane (macOS·Linux)
	ModeExec   Mode = "exec"   // screen switch: the program owns the terminal
	ModeWindow Mode = "window" // tmux display-popup
)

// Request is what a tool asks for. Env is added on top of os.Environ() (pane
// mode also adds TERM/COLORTERM/MASIL_PANE); masil never adds its own secrets.
type Request struct {
	Program string
	Args    []string
	Dir     string
	Env     []string
	Title   string // shown in tab titles and the return toast
	ToolID  string // leet, workspace, arch, git, api
	Mode    Mode   // "" or ModeAuto = decide; anything else is an explicit choice
	// Exec, when set, is the argv actually started — the user's login shell
	// around Program (launch.ViaLoginShell, 0.1.3 patch). Program and Args
	// stay what masil checks, names and shows (titles, the running list,
	// ExitInfo).
	Exec []string
	// PaneExec, when set, replaces Exec in pane mode: the login-shell wrap
	// that also wipes the pane emulator's scrollback of rc-file output. Exec
	// and window mode run on a real terminal whose scrollback is the user's,
	// so they keep Exec.
	PaneExec []string
}

// command is the program and args to start in mode m: PaneExec in pane mode
// when set, else Exec when set, else Program/Args.
func (r Request) command(m Mode) (string, []string) {
	if m == ModePane && len(r.PaneExec) > 0 {
		return r.PaneExec[0], r.PaneExec[1:]
	}
	if len(r.Exec) > 0 {
		return r.Exec[0], r.Exec[1:]
	}
	return r.Program, r.Args
}

// Env is the environment Choose decides in. CurrentEnv fills it; tests build it.
type Env struct {
	GOOS, TERM, TMUX, TmuxVersion string
	Embedded                      bool // [terminal] embedded
	Tiny                          bool // the layout is in tiny mode
	ToolOpen                      Mode // [tools.<section>] open
}

// detectTmuxVersion reports the running tmux version ("" when unknown). The
// tmux popup code (popup.go) points it at TmuxVersion.
var detectTmuxVersion = func() string { return "" }

// CurrentEnv reads the process environment and config for toolID (a tool ID,
// mapped to its config section by config.ToolConfigSection). tmux is probed
// only when $TMUX is set. A nil cfg means defaults.
func CurrentEnv(cfg *config.Config, toolID string, tiny bool) Env {
	env := Env{
		GOOS:     runtime.GOOS,
		TERM:     os.Getenv("TERM"),
		TMUX:     os.Getenv("TMUX"),
		Embedded: cfg == nil || cfg.Terminal.Embedded,
		Tiny:     tiny,
		ToolOpen: Mode(cfg.ToolOpen(toolID)),
	}
	if env.TMUX != "" {
		env.TmuxVersion = detectTmuxVersion()
	}
	return env
}

// Choose picks the mode for req in env (spec A7.2, first match wins):
//
//  1. Windows -> exec.
//  2. [terminal] embedded = false or TERM=dumb -> exec. embedded = false
//     only turns panes off (CA 3.2: tools then use exec or the external
//     window), so with tool config open = "window" it falls to rule 4.
//  3. tiny layout -> exec.
//  4. tool config open = "window" and inside tmux >= 3.2 -> window
//     (otherwise exec, downgraded).
//  5. otherwise -> pane.
//
// Tool config open = "exec" always gives exec; "pane" behaves like auto.
// An explicit req.Mode (the hub's one-off menu) wins over the tool config:
// exec is always possible; pane falls back to exec on Windows, with embedded
// panes off, TERM=dumb or in tiny mode; window falls back to exec outside
// tmux >= 3.2 or on Windows. downgraded reports that a wanted window/pane
// could not be honoured (the caller shows a toast).
func Choose(req Request, env Env) (m Mode, downgraded bool) {
	paneOK := env.GOOS != "windows" && env.Embedded && env.TERM != "dumb" && !env.Tiny
	windowOK := env.GOOS != "windows" && env.TMUX != "" && tmuxAtLeast(env.TmuxVersion, 3, 2)
	switch req.Mode {
	case ModeExec:
		return ModeExec, false
	case ModePane:
		if paneOK {
			return ModePane, false
		}
		return ModeExec, true
	case ModeWindow:
		if windowOK {
			return ModeWindow, false
		}
		return ModeExec, true
	}
	switch {
	case env.ToolOpen == ModeExec:
		return ModeExec, false
	case env.GOOS == "windows":
		return ModeExec, false
	case env.TERM == "dumb", !env.Embedded && env.ToolOpen != ModeWindow:
		return ModeExec, false
	case env.Tiny:
		return ModeExec, false
	case env.ToolOpen == ModeWindow:
		if windowOK {
			return ModeWindow, false
		}
		return ModeExec, true
	}
	return ModePane, false
}

// parseTmuxVersion reads "3.2", "3.3a", "next-3.5" or "tmux 3.2a" (tmux -V).
func parseTmuxVersion(v string) (major, minor int, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "tmux ")
	v = strings.TrimPrefix(strings.TrimSpace(v), "next-")
	majorStr, rest, _ := strings.Cut(v, ".")
	major, err := strconv.Atoi(majorStr)
	if err != nil || major < 0 {
		return 0, 0, false
	}
	digits := rest
	for i, r := range rest {
		if r < '0' || r > '9' {
			digits = rest[:i]
			break
		}
	}
	if digits != "" {
		if minor, err = strconv.Atoi(digits); err != nil {
			return 0, 0, false
		}
	}
	return major, minor, true
}

// tmuxAtLeast reports whether version v is >= major.minor; unparsable => false.
func tmuxAtLeast(v string, major, minor int) bool {
	ma, mi, ok := parseTmuxVersion(v)
	if !ok {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}
