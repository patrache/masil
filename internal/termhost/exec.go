package termhost

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
)

// ExitInfo reports how a program ended. Title is Request.Title (the return
// toast and tab titles use it). Code is the exit code, -1 when the program
// did not start or was killed by a signal; Err is set only when the program
// could not be run (a normal non-zero exit has Err == nil).
type ExitInfo struct {
	Program, Title string
	Mode           Mode
	Code           int
	Err            error
}

// exclusive is true while an exec-mode program owns the terminal.
var exclusive atomic.Bool

// Exclusive reports whether an exec-mode program currently owns the terminal
// (the UI is suspended; the realtime bridge must not wait on it).
func Exclusive() bool { return exclusive.Load() }

// Exec runs req in exec mode: masil's program is suspended and the terminal
// is handed to the program (tea.Exec). Exclusive() is true while it runs.
// onExit turns the result into the message delivered when masil resumes.
func Exec(req Request, onExit func(ExitInfo) tea.Msg) tea.Cmd {
	cmd, cb := prepareExec(req, onExit)
	return tea.Exec(cmd, cb)
}

// newProcessCommand builds the command that actually runs req (a seam for tests).
var newProcessCommand = func(req Request) tea.ExecCommand { return &processCommand{req: req} }

// prepareExec returns the ExecCommand and callback Exec hands to tea.Exec.
func prepareExec(req Request, onExit func(ExitInfo) tea.Msg) (tea.ExecCommand, tea.ExecCallback) {
	cmd := &exclusiveCommand{inner: newProcessCommand(req), req: req}
	cb := func(err error) tea.Msg {
		info := ExitInfo{Program: req.Program, Title: req.Title, Mode: ModeExec}
		info.Code, info.Err = exitResult(err)
		if onExit == nil {
			return nil
		}
		return onExit(info)
	}
	return cmd, cb
}

// exitResult turns a Run/Wait error into (code, err).
func exitResult(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil // -1 when killed by a signal
	}
	return -1, err
}

// exclusiveCommand marks the terminal as owned for the duration of Run.
type exclusiveCommand struct {
	inner tea.ExecCommand
	req   Request
}

func (c *exclusiveCommand) Run() error {
	setExclusiveRequest(c.req)
	exclusive.Store(true)
	defer exclusive.Store(false)
	return c.inner.Run()
}
func (c *exclusiveCommand) SetStdin(r io.Reader)  { c.inner.SetStdin(r) }
func (c *exclusiveCommand) SetStdout(w io.Writer) { c.inner.SetStdout(w) }
func (c *exclusiveCommand) SetStderr(w io.Writer) { c.inner.SetStderr(w) }

// processCommand runs req as a child process.
type processCommand struct {
	req            Request
	stdin          io.Reader
	stdout, stderr io.Writer
}

func (c *processCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *processCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *processCommand) SetStderr(w io.Writer) { c.stderr = w }

func (c *processCommand) Run() error {
	prog, args := c.req.command()
	cmd := exec.Command(prog, args...)
	cmd.Dir = c.req.Dir
	cmd.Env = append(os.Environ(), c.req.Env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.stdin, c.stdout, c.stderr
	return cmd.Run()
}
