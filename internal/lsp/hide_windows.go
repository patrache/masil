//go:build windows

package lsp

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a console server from flashing a window on Windows.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
