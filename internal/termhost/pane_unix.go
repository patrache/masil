//go:build !windows

package termhost

import (
	"errors"
	"os/exec"
	"syscall"

	"github.com/charmbracelet/x/xpty"
)

func init() { startProc = startUnixProc }

// unixProc is a child on a Unix PTY. The child is a session leader with the
// PTY as its controlling terminal (Setsid/Setctty): xpty does not do this,
// and without it resizes never reach the child as SIGWINCH and the child is
// not hung up when masil dies.
type unixProc struct {
	pty *xpty.UnixPty
	cmd *exec.Cmd
}

func startUnixProc(req Request, size Size, env []string) (proc, error) {
	pty, err := xpty.NewUnixPty(size.W, size.H)
	if err != nil {
		return nil, err
	}
	prog, args := req.command(ModePane)
	cmd := exec.Command(prog, args...)
	cmd.Dir = req.dir(ModePane)
	cmd.Env = env
	slave := pty.Slave()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		_ = pty.Close()
		return nil, err
	}
	// Only the child keeps the slave open: when it (and its children) exit,
	// reads on the master end instead of waiting forever.
	_ = slave.Close()
	return &unixProc{pty: pty, cmd: cmd}, nil
}

func (u *unixProc) Read(b []byte) (int, error)  { return u.pty.Master().Read(b) }
func (u *unixProc) Write(b []byte) (int, error) { return u.pty.Master().Write(b) }
func (u *unixProc) Resize(s Size) error         { return u.pty.Resize(s.W, s.H) }
func (u *unixProc) Pid() int                    { return u.cmd.Process.Pid }

func (u *unixProc) Signal(kill bool) error {
	sig := syscall.SIGHUP
	if kill {
		sig = syscall.SIGKILL
	}
	err := syscall.Kill(-u.cmd.Process.Pid, sig) // the whole process group
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (u *unixProc) Wait() (int, error) { return exitResult(u.cmd.Wait()) }

func (u *unixProc) Close() error { return u.pty.Master().Close() }
