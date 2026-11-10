package workspace

import (
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/workspace/launch"
	"github.com/patrache/masil/internal/workspace/wire"
)

// 0.1.3 patch (tools-nav fix 2): ctrl+t — also ctrl+ㅅ on a Korean layout —
// leaves a workspace terminal that holds the keyboard and goes back to the
// chat; the terminal keeps running and never sees the key.
func TestCtrlTLeavesWorkspaceTerminal(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 't', Mod: tea.ModCtrl}, {Code: 'ㅅ', Mod: tea.ModCtrl}} {
		t.Run(key.String(), func(t *testing.T) {
			fake := setup(t)
			h := uitest.New(t)
			s := openFolder(t, h, "workspace:test", copyFixture(t))
			h.Press("ctrl+\\", "3", "n", "enter") // a terminal (the login shell)
			if !s.GrabsKeys() {
				t.Fatal("the terminal does not hold the keyboard")
			}
			started := fake.Started()
			h.Press("a")
			h.Send(key)
			if h.Nav().Front == app.FrontTools {
				t.Fatalf("%s did not leave the workspace terminal", key.String())
			}
			if len(fake.Started()) != len(started) {
				t.Fatal("ctrl+t started something")
			}
			ids := fake.IDs()
			if len(ids) != 1 {
				t.Fatalf("panes = %v, want the one terminal", ids)
			}
			if keys := fake.Keys(ids[0]); !slices.Equal(keys, []string{"a"}) {
				t.Fatalf("the terminal got %q, want only a", keys)
			}
			if !s.GrabsKeys() {
				t.Fatal("the terminal stopped (or gave up the keyboard)")
			}
		})
	}
}

// 0.1.3 patch (tools-nav fix 5): claude in a workspace terminal starts through
// the login shell ($SHELL zsh/bash, not Windows); v0.1.3-1 security review:
// the shell starts in $HOME (ExecDir) and its script changes to the folder,
// passed as $1, so the rc files never run inside the repo; masil's own argv — what Check, the share dialog and the
// title see — is unchanged, --settings file included. Other shells, other
// programs: a direct exec, as before.
func TestClaudeStartsThroughLoginShell(t *testing.T) {
	cases := []struct {
		shell string
		argv  []string
		wrap  bool
	}{
		{"/bin/zsh", []string{"claude"}, runtime.GOOS != "windows"},
		{"/usr/local/bin/bash", []string{"claude", "-c"}, runtime.GOOS != "windows"},
		{"/usr/bin/fish", []string{"claude"}, false},
		{"", []string{"claude"}, false},
		{"/bin/zsh", []string{"nvim"}, false},
		{"/bin/zsh", []string{"/opt/bin/claude"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.shell+" "+strings.Join(tc.argv, " "), func(t *testing.T) {
			fake := setup(t)
			t.Setenv("SHELL", tc.shell)
			home := t.TempDir()
			t.Setenv("HOME", home)
			h := uitest.New(t)
			s := openFolder(t, h, "workspace:test", copyFixture(t))
			h.Send(launchMsgForTest(s, tc.argv))
			st := fake.Started()
			if len(st) != 1 {
				t.Fatalf("started = %+v", st)
			}
			req := st[0]
			if req.Dir != s.dir || req.Program != tc.argv[0] {
				t.Fatalf("request = %+v, want %s in %s", req, tc.argv[0], s.dir)
			}
			if !tc.wrap {
				if req.Exec != nil || req.PaneExec != nil || req.ExecDir != "" {
					t.Fatalf("exec = %q, pane exec %q, want a direct exec", req.Exec, req.PaneExec)
				}
				return
			}
			// Review fix: only the pane argv wipes scrollback (\033[3J); exec
			// mode (the macOS fallback) writes to the user's real terminal.
			if req.ExecDir != home {
				t.Fatalf("the login shell starts in %q, want $HOME %q", req.ExecDir, home)
			}
			want := append([]string{tc.shell, "-l", "-i", "-c", launch.PaneShellScript, "claude", s.dir}, req.Args...)
			if !slices.Equal(req.PaneExec, want) {
				t.Fatalf("pane exec = %q, want %q", req.PaneExec, want)
			}
			want = append([]string{tc.shell, "-l", "-i", "-c", launch.ExecShellScript, "claude", s.dir}, req.Args...)
			if !slices.Equal(req.Exec, want) || strings.Contains(strings.Join(req.Exec, " "), "3J") {
				t.Fatalf("exec = %q, want %q (no scrollback erase)", req.Exec, want)
			}
			if !slices.Contains(req.Args, "--settings") || !slices.Contains(req.Args, "--permission-mode") {
				t.Fatalf("masil's claude args lost: %q", req.Args)
			}
			term := s.term.current()
			if term == nil || term.Argv()[0] != "claude" || !launch.Check(term.Argv()).Allowed {
				t.Fatalf("the share check sees %q", term.Argv())
			}
		})
	}
}

// v0.1.3-1 security review: only the folder's owner starts programs (the
// login-shell claude among them). A share guest's surface has no launcher:
// its keys and a launch request reaching it start nothing.
func TestGuestNeverLaunches(t *testing.T) {
	fake := setup(t)
	t.Setenv("SHELL", "/bin/zsh")
	h := uitest.New(t)
	r := newGuestRig(t, h, nil)
	r.admit(wire.LevelEditor)
	before := len(fake.Started())
	for _, keys := range [][]string{{"n"}, {"ctrl+\\", "n"}, {"ctrl+\\", "e"}, {"ctrl+\\", "3"}, {"n"}} {
		h.Press(keys...)
		r.settle()
		h.Press("esc")
	}
	h.Send(launchMsgForTest(r.s, []string{"claude"}))
	r.settle()
	if got := fake.Started(); len(got) != before {
		t.Fatalf("a guest started %+v", got[before:])
	}
	if r.s.dlg != nil && strings.Contains(r.h.Screen(), h.Context().T.Workspace.NewtermTitle()) {
		t.Fatalf("a guest got the new-terminal list:\n%s", h.Screen())
	}
}
