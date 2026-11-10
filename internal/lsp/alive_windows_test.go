//go:build windows

package lsp

import "syscall"

// processAlive reports whether pid is a running process.
func processAlive(pid int) bool {
	const queryLimitedInformation = 0x1000 // PROCESS_QUERY_LIMITED_INFORMATION
	const stillActive = 259                // STILL_ACTIVE
	h, err := syscall.OpenProcess(queryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
