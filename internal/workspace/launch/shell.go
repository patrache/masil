package launch

import "path/filepath"

// LoginShellScript is the -c script ViaLoginShell runs. It first turns job
// control off (set +m): -i turns it on, and a claude FUNCTION's real claude
// would then run in its own process group, out of reach of the pane's
// SIGHUP/SIGKILL to the shell's group (a claude that hangs on close was left
// an orphan). The rc files have already run, so functions and aliases still
// apply. It then clears the screen and its scrollback (whatever the rc files
// printed — a powerlevel10k instant prompt, a greeting — must not stay above
// claude in the pane), then runs claude by name with the args as given ("$@";
// $0 is "claude"), so a shell function or alias named claude applies. Never
// exec or command claude: both skip functions. The script is a constant: the
// args are data, never code.
const LoginShellScript = `set +m; printf '\033[3J\033[H\033[2J'; claude "$@"`

// loginShells are the $SHELL base names claude is started through.
var loginShells = []string{"zsh", "bash"}

// ViaLoginShell is the argv that starts argv through the user's login shell
// (0.1.3 patch: the owner splits claude accounts with a zsh function, which a
// direct exec never runs): $SHELL -l -i -c LoginShellScript claude <args…>,
// with the workspace folder as the working directory (the caller's Dir), so
// the user's functions, aliases and PATH apply. It is nil — exec argv
// directly, as before — unless argv is the bare name "claude" (a path names
// that binary on purpose), goos is not windows and shell's base name is zsh
// or bash. argv is masil's own, already through Check and Prepare: the
// wrapper adds nothing to claude's args.
func ViaLoginShell(argv []string, shell, goos string) []string {
	if goos == "windows" || len(argv) == 0 || argv[0] != "claude" || shell == "" {
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
	out := make([]string, 0, len(argv)+5)
	out = append(out, shell, "-l", "-i", "-c", LoginShellScript, "claude")
	return append(out, argv[1:]...)
}
