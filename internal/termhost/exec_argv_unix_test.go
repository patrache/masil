//go:build !windows

package termhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 0.1.3 patch (tools-nav fix 5): Request.Exec is the argv actually started —
// a login-shell wrapper — while Program/Args stay what masil names, checks
// and shows (the pane's Program, its exit info, the running list).

// fakeShell writes an executable "zsh" in a temp dir that prints startup
// noise (an rc file's output), then runs its -c script with sh and the
// remaining args ($0 = the first one), as a login shell would.
func fakeShell(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "zsh")
	script := "#!/bin/sh\n" +
		"printf 'RC NOISE p10k instant prompt\\n'\n" +
		"[ \"$1\" = -l ] && [ \"$2\" = -i ] && [ \"$3\" = -c ] || { echo bad-flags; exit 9; }\n" +
		"shift 3; s=$1; shift; exec /bin/sh -c \"claude() { printf CLAUDE; printf '<%s>' \\\"\\$@\\\"; sleep 30; }; $s\" \"$@\"\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPaneStartsExecArgv(t *testing.T) {
	sh := fakeShell(t)
	req := Request{
		Program: "claude", Args: []string{"--settings", "/tmp/a b.json"}, Title: "claude",
		Exec: []string{sh, "-l", "-i", "-c", `printf '\033[3J\033[H\033[2J'; claude "$@"`, "claude", "--settings", "/tmp/a b.json"},
	}
	p, err := StartPane(req, Size{W: 60, H: 6}, PaneOptions{})
	if err != nil {
		t.Fatalf("StartPane: %v", err)
	}
	t.Cleanup(func() {
		_ = p.Close()
		select {
		case <-p.exited:
		case <-time.After(5 * time.Second):
			t.Errorf("pane did not exit at cleanup")
		}
	})
	waitFor(t, 10*time.Second, "claude through the shell", screenHas(p, "CLAUDE<--settings><"))
	screen := plain(p.Render())
	if !strings.Contains(screen, "CLAUDE<--settings></tmp/a b.json>") {
		t.Fatalf("args not passed intact:\n%s", screen)
	}
	if strings.Contains(screen, "RC NOISE") {
		t.Fatalf("the shell's startup output stayed in the pane:\n%s", screen)
	}
	if p.Program() != "claude" || p.name() != "claude" {
		t.Fatalf("Program() = %q, name %q, want claude", p.Program(), p.name())
	}
}

func TestPopupArgsUseExec(t *testing.T) {
	req := Request{Program: "claude", Args: []string{"-c"}, Exec: []string{"/bin/zsh", "-l", "-i", "-c", "s", "claude", "-c"}}
	got := strings.Join(popupArgs(req, "3.2"), " ")
	if want := "display-popup -E -w 90% -h 90% -- /bin/zsh -l -i -c s claude -c"; got != want {
		t.Fatalf("popup args = %q, want %q", got, want)
	}
}
