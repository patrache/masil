package launch

import "path/filepath"

// PaneShellScript is the -c script ViaLoginShell runs for a pane. It first
// turns job control off (set +m): -i turns it on, and a claude FUNCTION's
// real claude would then run in its own process group, out of reach of the
// pane's SIGHUP/SIGKILL to the shell's group (a claude that hangs on close
// was left an orphan). The shell started in $HOME (v0.1.3-1 security
// review: the rc files must not run inside a possibly untrusted repo, whose
// files a prompt theme or a plugin might read or run), so it then changes
// to the workspace folder, its $1, stopping when it cannot, and drops it
// from the args. The rc files have already run, so functions and aliases
// still apply, and a function reading $PWD when called (the owner's
// account switch) sees the folder. It then clears the screen and its
// scrollback (whatever the rc files printed — a powerlevel10k instant
// prompt, a greeting — must not stay above claude in the pane's emulator),
// then runs claude by name with the remaining args as given ("$@"; $0 is
// "claude"), so a shell function or alias named claude applies. Never exec
// or command claude: both skip functions. The script is a constant: the
// folder and the args are data, never code.
const PaneShellScript = `set +m; cd -- "$1" || exit 1; shift; printf '\033[3J\033[H\033[2J'; claude "$@"`

// ExecShellScript is PaneShellScript for exec and window mode, where claude
// writes to the user's real terminal (or a tmux popup): it clears only the
// visible screen, never the scrollback — that is the user's own, from
// before masil started.
const ExecShellScript = `set +m; cd -- "$1" || exit 1; shift; printf '\033[H\033[2J'; claude "$@"`

// loginShells are the $SHELL base names claude is started through.
var loginShells = []string{"zsh", "bash"}

// ViaLoginShell is the argv that starts argv through the user's login shell
// (0.1.3 patch: the owner splits claude accounts with a zsh function, which a
// direct exec never runs): $SHELL -l -i -c <script> claude <dir> <args…>,
// the script PaneShellScript when pane (masil's own emulator) and
// ExecShellScript otherwise, so the user's functions, aliases and PATH
// apply. The caller starts it in the user's home folder (never in dir: the
// rc files run there) and the script changes to dir, the workspace folder,
// before claude runs. It is nil — exec argv directly in dir, as before —
// unless argv is the bare name "claude" (a path names that binary on
// purpose), dir is set, goos is not windows and shell's base name is zsh or
// bash. argv is masil's own, already through Check and Prepare: the wrapper
// adds nothing to claude's args.
func ViaLoginShell(argv []string, shell, goos, dir string, pane bool) []string {
	if goos == "windows" || len(argv) == 0 || argv[0] != "claude" || shell == "" || dir == "" {
		return nil
	}
	base := filepath.Base(shell)
	ok := false
	for _, s := range loginShells {
		ok = ok || base == s
	}
	if !ok {
		return nil
	}
	script := ExecShellScript
	if pane {
		script = PaneShellScript
	}
	out := make([]string, 0, len(argv)+6)
	out = append(out, shell, "-l", "-i", "-c", script, "claude", dir)
	return append(out, argv[1:]...)
}
