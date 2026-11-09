package termhost

import (
	"errors"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type recordingCmd struct {
	sawExclusive []bool
	err          error
}

func (r *recordingCmd) Run() error {
	r.sawExclusive = append(r.sawExclusive, Exclusive())
	return r.err
}
func (r *recordingCmd) SetStdin(io.Reader)  {}
func (r *recordingCmd) SetStdout(io.Writer) {}
func (r *recordingCmd) SetStderr(io.Writer) {}

type exitedMsg struct{ info ExitInfo }

func TestExecSetsExclusive(t *testing.T) {
	fake := &recordingCmd{}
	restore := newProcessCommand
	newProcessCommand = func(Request) tea.ExecCommand { return fake }
	t.Cleanup(func() { newProcessCommand = restore })

	req := Request{Program: "/opt/homebrew/bin/lazygit", Title: "lazygit", ToolID: "git"}
	cmd, cb := prepareExec(req, func(i ExitInfo) tea.Msg { return exitedMsg{i} })
	if Exclusive() {
		t.Fatal("Exclusive() before Run")
	}
	msg := cb(cmd.Run())
	if !slices.Equal(fake.sawExclusive, []bool{true}) {
		t.Fatalf("Exclusive during Run = %v, want [true]", fake.sawExclusive)
	}
	if Exclusive() {
		t.Fatal("Exclusive() still true after Run")
	}
	got := msg.(exitedMsg).info
	if got.Program != req.Program || got.Title != "lazygit" || got.Mode != ModeExec || got.Code != 0 || got.Err != nil {
		t.Fatalf("ExitInfo = %+v", got)
	}

	// A start failure is reported in Err with code -1.
	fake.err = errors.New("boom")
	msg = cb(cmd.Run())
	if got := msg.(exitedMsg).info; got.Code != -1 || got.Err == nil {
		t.Fatalf("failed run ExitInfo = %+v, want code -1 and Err", got)
	}

	if Exec(req, func(i ExitInfo) tea.Msg { return exitedMsg{i} }) == nil {
		t.Fatal("Exec returned a nil Cmd")
	}
}

func TestExecExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	req := Request{Program: "sh", Args: []string{"-c", "exit 3"}, Title: "sh"}
	cmd, cb := prepareExec(req, func(i ExitInfo) tea.Msg { return exitedMsg{i} })
	cmd.SetStdin(strings.NewReader(""))
	cmd.SetStdout(io.Discard)
	cmd.SetStderr(io.Discard)
	got := cb(cmd.Run()).(exitedMsg).info
	if got.Code != 3 || got.Err != nil || got.Mode != ModeExec {
		t.Fatalf("ExitInfo = %+v, want code 3", got)
	}
}

func TestExecEnvAndDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	dir := t.TempDir()
	t.Setenv("MASIL_BASE_ENV", "kept")
	req := Request{Program: "sh", Args: []string{"-c", "pwd; echo $MASIL_BASE_ENV $EXTRA"}, Dir: dir, Env: []string{"EXTRA=added"}}
	cmd, cb := prepareExec(req, func(i ExitInfo) tea.Msg { return exitedMsg{i} })
	var out strings.Builder
	cmd.SetStdin(strings.NewReader(""))
	cmd.SetStdout(&out)
	cmd.SetStderr(io.Discard)
	if got := cb(cmd.Run()).(exitedMsg).info; got.Code != 0 {
		t.Fatalf("ExitInfo = %+v", got)
	}
	real, _ := os.Stat(dir)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output = %q", out.String())
	}
	if st, err := os.Stat(lines[0]); err != nil || !os.SameFile(st, real) {
		t.Fatalf("pwd = %q, want %q", lines[0], dir)
	}
	if lines[1] != "kept added" {
		t.Fatalf("env line = %q, want %q", lines[1], "kept added")
	}
}
