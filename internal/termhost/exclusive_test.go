package termhost

import (
	"io"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ExclusiveProgram names the exec program that owns (or last owned) the
// terminal: the app's return toast reads it after the true -> false edge.
func TestExclusiveProgramDuringRun(t *testing.T) {
	var during [2]string
	restore := newProcessCommand
	newProcessCommand = func(Request) tea.ExecCommand {
		return &hookCmd{run: func() { during[0], during[1] = ExclusiveProgram() }}
	}
	t.Cleanup(func() { newProcessCommand = restore })
	cmd, _ := prepareExec(Request{Program: "/opt/homebrew/bin/lazygit", Title: "lazygit"}, nil)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if during != [2]string{"/opt/homebrew/bin/lazygit", "lazygit"} {
		t.Fatalf("during Run = %q", during)
	}
	if p, title := ExclusiveProgram(); p != "/opt/homebrew/bin/lazygit" || title != "lazygit" {
		t.Fatalf("after Run = %q %q, want the last program", p, title)
	}
}

func TestSetExclusiveForTest(t *testing.T) {
	prevP, prevT := ExclusiveProgram()
	restore := SetExclusiveForTest(true, Request{Program: "nvim", Title: "편집"})
	if !Exclusive() {
		t.Fatal("Exclusive() = false")
	}
	if p, title := ExclusiveProgram(); p != "nvim" || title != "편집" {
		t.Fatalf("program = %q %q", p, title)
	}
	restore()
	restore() // idempotent
	if Exclusive() {
		t.Fatal("restore left Exclusive() true")
	}
	if p, title := ExclusiveProgram(); p != prevP || title != prevT {
		t.Fatalf("restore: program = %q %q, want %q %q", p, title, prevP, prevT)
	}
	r2 := SetExclusiveForTest(true) // no request: program unchanged
	if p, _ := ExclusiveProgram(); p != prevP {
		t.Fatalf("SetExclusiveForTest(true) changed the program to %q", p)
	}
	r2()
}

type hookCmd struct{ run func() }

func (h *hookCmd) Run() error {
	h.run()
	return nil
}
func (h *hookCmd) SetStdin(io.Reader)  {}
func (h *hookCmd) SetStdout(io.Writer) {}
func (h *hookCmd) SetStderr(io.Writer) {}
