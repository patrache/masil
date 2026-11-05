//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// daemonChildEnv marks the hidden child started by Detach so it runs the
// daemon itself instead of respawning again.
const daemonChildEnv = "MASIL_DAEMON_CHILD"

// Detach re-launches the current executable with args (and MASIL_DAEMON_CHILD=1)
// as a hidden, window-less process and reports (true, nil): the HKCU Run key
// starts `masil daemon run` from a console subsystem binary, and this keeps a
// console window from staying open for the whole session. Inside that child
// (MASIL_DAEMON_CHILD=1) it does nothing and reports (false, nil).
func Detach(args []string) (detached bool, err error) {
	if os.Getenv(daemonChildEnv) == "1" {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("daemon: detach: %w", err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), daemonChildEnv+"=1")
	// Stdin/Stdout/Stderr stay nil: the child gets the null device and keeps no
	// handle to this console.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("daemon: detach: %w", err)
	}
	_ = cmd.Process.Release() // the parent exits at once; the child outlives it
	return true, nil
}
