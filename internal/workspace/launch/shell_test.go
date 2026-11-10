package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// 0.1.3 patch (tools-nav fix 5): claude starts through the user's login
// shell on darwin/linux when $SHELL is zsh or bash, so a shell FUNCTION named
// claude (account split by folder), aliases and PATH from the rc files apply:
// $SHELL -l -i -c '<script>' claude <args…>, the script calling claude by
// name (never exec/command, which skip functions).
//
// v0.1.3-1 security review: the shell itself starts in $HOME (the caller's
// ExecDir), so the rc files never run inside a possibly untrusted repo; the
// folder goes as $1 and the script cds there before claude runs:
// $SHELL -l -i -c '<script>' claude <folder> <args…>.
func TestViaLoginShell(t *testing.T) {
	args := []string{"claude", "--permission-mode", "manual", "--settings", "/tmp/masil-claude-1.json", "-c"}
	const dir = "/Users/me/my repo"
	for _, shell := range []string{"/bin/zsh", "/usr/local/bin/bash", "/opt/homebrew/bin/zsh"} {
		for _, goos := range []string{"darwin", "linux"} {
			for pane, script := range map[bool]string{true: PaneShellScript, false: ExecShellScript} {
				got := ViaLoginShell(args, shell, goos, dir, pane)
				want := append([]string{shell, "-l", "-i", "-c", script, "claude", dir}, args[1:]...)
				if !slices.Equal(got, want) {
					t.Errorf("%s %s pane=%v: %q, want %q", shell, goos, pane, got, want)
				}
			}
		}
	}
	for _, tc := range []struct {
		argv        []string
		shell, goos string
	}{
		{args, "/bin/zsh", "windows"},
		{args, "", "darwin"},
		{args, "/usr/bin/fish", "darwin"},
		{args, "/bin/sh", "linux"},
		{args, "/bin/dash", "linux"},
		{args, "zsh-not-really", "darwin"},
		{[]string{"/usr/local/bin/claude", "-c"}, "/bin/zsh", "darwin"}, // a path: that binary, as asked
		{[]string{"nvim"}, "/bin/zsh", "darwin"},
		{[]string{"zsh"}, "/bin/zsh", "darwin"},
		{nil, "/bin/zsh", "darwin"},
	} {
		for _, pane := range []bool{true, false} {
			if got := ViaLoginShell(tc.argv, tc.shell, tc.goos, dir, pane); got != nil {
				t.Errorf("%q shell %q %s: %q, want direct exec (nil)", tc.argv, tc.shell, tc.goos, got)
			}
		}
	}
	// No folder: no wrap (the shell would have nowhere to cd to).
	if got := ViaLoginShell(args, "/bin/zsh", "darwin", "", true); got != nil {
		t.Errorf("no folder: %q, want direct exec (nil)", got)
	}
	// The script turns job control off (claude stays in the shell's process
	// group, which Close signals), changes to the folder ($1, refusing to go
	// on when it cannot) and runs claude by name, after clearing what the rc
	// files printed.
	for _, s := range []string{PaneShellScript, ExecShellScript} {
		if strings.Contains(s, "exec ") || strings.Contains(s, "command ") ||
			!strings.HasPrefix(s, `set +m; cd -- "$1" || exit 1; shift; `) || !strings.HasSuffix(s, `; claude "$@"`) {
			t.Errorf("script = %q", s)
		}
	}
	// Review fix: only the pane script erases scrollback (\033[3J) — in exec
	// and window mode that is the user's real terminal's scrollback.
	if !strings.Contains(PaneShellScript, `\033[3J`) {
		t.Errorf("pane script %q does not clear the emulator's scrollback", PaneShellScript)
	}
	if strings.Contains(ExecShellScript, "3J") {
		t.Errorf("exec script %q erases the terminal's scrollback", ExecShellScript)
	}
}

// The script, run by a real sh-compatible shell with the args as given, calls
// whatever "claude" means there (here a function) with every arg intact —
// spaces, quotes, $ and ; are data, never code.
func TestLoginShellScriptPassesArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sh on Windows")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "args")
	folder := filepath.Join(dir, "a repo; $(touch pwned)")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	args := []string{"-p", "two words", `"quoted"`, "$HOME", "a;b", "--settings", "/tmp/x y.json"}
	for _, ls := range []string{PaneShellScript, ExecShellScript} {
		_ = os.Remove(out)
		// claude records where it runs ($PWD, as the owner's zsh function
		// reads it), then its args.
		script := `claude() { { printf '%s\n' "$PWD"; for a in "$@"; do printf '%s\n' "$a"; done; } > '` + out + `'; }; ` + ls
		cmd := exec.Command(sh, append([]string{"-c", script, "claude", folder}, args...)...)
		cmd.Dir = home
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("sh: %v: %s", err, b)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if pwd, _ := filepath.EvalSymlinks(got[0]); pwd != evalOr(folder) {
			t.Errorf("%q: claude ran in %q, want %q", ls, got[0], folder)
		}
		if !slices.Equal(got[1:], args) {
			t.Errorf("%q: claude got %q, want %q", ls, got[1:], args)
		}
		for _, d := range []string{home, folder, dir} {
			if _, err := os.Stat(filepath.Join(d, "pwned")); err == nil {
				t.Fatalf("the folder name ran as code")
			}
		}
	}
	// A folder that is gone: the script stops, claude never runs.
	_ = os.Remove(out)
	script := `claude() { echo ran > '` + out + `'; }; ` + PaneShellScript
	cmd := exec.Command(sh, "-c", script, "claude", filepath.Join(dir, "missing"))
	cmd.Dir = home
	if err := cmd.Run(); err == nil {
		t.Error("cd to a missing folder did not fail the script")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("claude ran although the cd failed")
	}
}

func evalOr(p string) string {
	if e, err := filepath.EvalSymlinks(p); err == nil {
		return e
	}
	return p
}
