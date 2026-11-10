//go:build unix

package lsp

import "syscall"

// processAlive reports whether pid is a running process (a zombie counts
// until it is reaped).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
