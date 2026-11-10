//go:build !windows

package termhost

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/launch"
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
		Exec:     []string{"/nonexistent/exec-mode-only"},
		PaneExec: []string{sh, "-l", "-i", "-c", launch.PaneShellScript, "claude", t.TempDir(), "--settings", "/tmp/a b.json"},
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
	req := Request{Program: "claude", Args: []string{"-c"}, Exec: []string{"/bin/zsh", "-l", "-i", "-c", "s", "claude", "/w", "-c"},
		PaneExec: []string{"/bin/zsh", "-l", "-i", "-c", "pane-only", "claude", "/w", "-c"}, Dir: "/w", ExecDir: "/home/me"}
	got := strings.Join(popupArgs(req, "3.2"), " ")
	if want := "display-popup -E -d /home/me -w 90% -h 90% -- /bin/zsh -l -i -c s claude /w -c"; got != want {
		t.Fatalf("popup args = %q, want %q", got, want)
	}
	// Without the wrap the program starts in the folder itself.
	req = Request{Program: "nvim", Dir: "/w", ExecDir: "/home/me"}
	if got := strings.Join(popupArgs(req, "3.2"), " "); !strings.Contains(got, "-d /w ") {
		t.Fatalf("popup args = %q, want -d /w", got)
	}
}

// v0.1.3-1 security review: the login shell's rc files run in $HOME (the
// request's ExecDir), never in the workspace folder (a repo that may not be
// trusted); the script then changes to the folder, so a claude() FUNCTION
// that reads $PWD when called — the owner's account switch — still sees the
// folder. Real zsh and bash, with rc files that record $PWD.
func TestLoginShellRcRunsInHome(t *testing.T) {
	for _, name := range []string{"zsh", "bash"} {
		t.Run(name, func(t *testing.T) {
			shell, err := exec.LookPath(name)
			if err != nil {
				t.Skip("no " + name)
			}
			home, _ := filepath.EvalSymlinks(t.TempDir())
			folder, _ := filepath.EvalSymlinks(t.TempDir())
			rc := "printf 'RC %s\\n' \"$PWD\" >> '" + filepath.Join(home, "log") + "'\n" +
				"claude() { printf 'FN %s %s\\n' \"$PWD\" \"$*\" >> '" + filepath.Join(home, "log") + "'; }\n"
			for _, f := range []string{".zshrc", ".bash_profile", ".bashrc"} {
				if err := os.WriteFile(filepath.Join(home, f), []byte(rc), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, pane := range []bool{true, false} {
				_ = os.Remove(filepath.Join(home, "log"))
				argv := launch.ViaLoginShell([]string{"claude", "-c"}, shell, runtime.GOOS, folder, pane)
				req := Request{Program: "claude", Args: []string{"-c"}, Title: "claude", Dir: folder, ExecDir: home,
					Env: []string{"HOME=" + home, "ZDOTDIR=" + home}}
				if pane {
					req.PaneExec = argv
				} else {
					req.Exec = argv
				}
				if got := req.dir(ModePane); pane && got != home {
					t.Fatalf("pane dir = %q, want %q", got, home)
				}
				if got := req.dir(ModeExec); !pane && got != home {
					t.Fatalf("exec dir = %q, want %q", got, home)
				}
				if pane {
					p, err := StartPane(req, Size{W: 80, H: 6}, PaneOptions{})
					if err != nil {
						t.Fatalf("StartPane: %v", err)
					}
					waitExited(t, p, 10*time.Second)
				} else {
					c := &processCommand{req: req}
					var out strings.Builder
					c.SetStdout(&out)
					c.SetStderr(&out)
					_ = c.Run()
				}
				b, _ := os.ReadFile(filepath.Join(home, "log"))
				log := string(b)
				if strings.Contains(log, "RC "+folder) || !strings.Contains(log, "RC "+home+"\n") {
					t.Fatalf("pane=%v: rc files ran in the wrong folder:\n%s", pane, log)
				}
				if !strings.Contains(log, "FN "+folder+" -c\n") {
					t.Fatalf("pane=%v: claude() did not run in the folder with its args:\n%s", pane, log)
				}
			}
		})
	}
}

// Review fix (tools-nav): with -i the shell turns job control on, so a
// claude() FUNCTION's real claude runs in its own process group. Close's
// SIGHUP/SIGKILL go to the shell's group only, so a claude that ignores
// SIGHUP survived the SIGKILL as an orphan. The login-shell script turns job
// control off first: the child stays in the shell's group and dies with it.
func TestLoginShellCloseKillsFunctionChild(t *testing.T) {
	restore := closeGrace
	closeGrace = 100 * time.Millisecond
	t.Cleanup(func() { closeGrace = restore })
	fn := "claude() { trap '' HUP; /bin/sh -c 'echo CHILD $$; exec sleep 1000'; }\n"
	for _, name := range []string{"zsh", "bash"} {
		t.Run(name, func(t *testing.T) {
			shell, err := exec.LookPath(name)
			if err != nil {
				t.Skip("no " + name)
			}
			home := t.TempDir()
			for _, rc := range []string{".zshrc", ".bash_profile", ".bashrc"} {
				if err := os.WriteFile(filepath.Join(home, rc), []byte(fn), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			argv := launch.ViaLoginShell([]string{"claude", "-c"}, shell, runtime.GOOS, home, true)
			if argv == nil {
				t.Fatalf("ViaLoginShell(%s) = nil", shell)
			}
			req := Request{Program: "claude", Args: []string{"-c"}, Title: "claude", PaneExec: argv,
				Dir: home, ExecDir: home, Env: []string{"HOME=" + home, "ZDOTDIR=" + home}}
			p, err := StartPane(req, Size{W: 60, H: 6}, PaneOptions{})
			if err != nil {
				t.Fatalf("StartPane: %v", err)
			}
			var pid int
			waitFor(t, 10*time.Second, "child pid", func() bool {
				m := regexp.MustCompile(`CHILD (\d+)`).FindStringSubmatch(plain(p.Render()))
				if m != nil {
					pid, _ = strconv.Atoi(m[1])
				}
				return pid > 0
			})
			t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
			if err := p.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			waitExited(t, p, 5*time.Second)
			deadline := time.Now().Add(3 * time.Second)
			for {
				if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("claude's child %d outlived Close (orphaned in its own process group)", pid)
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
	}
}

// Review fix (tools-nav): exec mode runs Exec, never PaneExec — the pane
// script erases scrollback (\033[3J), and in exec mode that would be the
// user's real terminal's own, from before masil started.
func TestExecModeRunsExecNotPaneExec(t *testing.T) {
	sh := fakeShell(t)
	req := Request{
		Program: "claude", Args: []string{"-c"}, Title: "claude",
		Exec:     launch.ViaLoginShell([]string{"claude", "-c"}, sh, "darwin", "/w", false),
		PaneExec: launch.ViaLoginShell([]string{"claude", "-c"}, sh, "darwin", "/w", true),
	}
	if req.Exec == nil || req.PaneExec == nil {
		t.Fatalf("fake shell %q not wrapped", sh)
	}
	if prog, args := req.command(ModeExec); prog != sh || strings.Contains(strings.Join(args, " "), "3J") {
		t.Fatalf("exec mode starts %q %q", prog, args)
	}
	if _, args := req.command(ModeWindow); strings.Contains(strings.Join(args, " "), "3J") {
		t.Fatalf("window mode starts %q", args)
	}
	if _, args := req.command(ModePane); !strings.Contains(strings.Join(args, " "), "3J") {
		t.Fatalf("pane mode starts %q, want the pane script", args)
	}
}
