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
func TestViaLoginShell(t *testing.T) {
	args := []string{"claude", "--permission-mode", "manual", "--settings", "/tmp/masil-claude-1.json", "-c"}
	for _, shell := range []string{"/bin/zsh", "/usr/local/bin/bash", "/opt/homebrew/bin/zsh"} {
		for _, goos := range []string{"darwin", "linux"} {
			got := ViaLoginShell(args, shell, goos)
			want := append([]string{shell, "-l", "-i", "-c", LoginShellScript, "claude"}, args[1:]...)
			if !slices.Equal(got, want) {
				t.Errorf("%s %s: %q, want %q", shell, goos, got, want)
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
		if got := ViaLoginShell(tc.argv, tc.shell, tc.goos); got != nil {
			t.Errorf("%q shell %q %s: %q, want direct exec (nil)", tc.argv, tc.shell, tc.goos, got)
		}
	}
	// The script runs claude by name, after clearing what the rc files printed.
	if strings.Contains(LoginShellScript, "exec ") || strings.Contains(LoginShellScript, "command ") ||
		!strings.Contains(LoginShellScript, `claude "$@"`) {
		t.Errorf("script = %q", LoginShellScript)
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
	script := `claude() { for a in "$@"; do printf '%s\n' "$a"; done > ` + out + `; }; ` + LoginShellScript
	args := []string{"-p", "two words", `"quoted"`, "$HOME", "a;b", "--settings", "/tmp/x y.json"}
	cmd := exec.Command(sh, append([]string{"-c", script, "claude"}, args...)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sh: %v: %s", err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"); !slices.Equal(got, args) {
		t.Errorf("claude got %q, want %q", got, args)
	}
}
