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
// the login shell ($SHELL zsh/bash, not Windows) with the folder as its
// working directory; masil's own argv — what Check, the share dialog and the
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
				if req.Exec != nil {
					t.Fatalf("exec = %q, want a direct exec", req.Exec)
				}
				return
			}
			want := append([]string{tc.shell, "-l", "-i", "-c", launch.LoginShellScript, "claude"}, req.Args...)
			if !slices.Equal(req.Exec, want) {
				t.Fatalf("exec = %q, want %q", req.Exec, want)
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
