//go:build windows

package config

import (
	"errors"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION (winerror.h, 32). The std
// syscall package has ERROR_ACCESS_DENIED and ERROR_FILE_NOT_FOUND but not
// this one. It is spelled out here instead of importing golang.org/x/sys/windows
// because that package imports "net" (SocketAddress.IP), and config sits under
// the pure arch packages (archdoc, archsim, ...), whose import tests ban net.
// rename_windows_test.go pins the value to windows.ERROR_SHARING_VIOLATION.
const errSharingViolation syscall.Errno = 32

// isTransientRenameErr reports Windows rename errors that usually clear up
// once another process closes the target or finishes its own rename
// (mirrors cmd/go/internal/robustio).
func isTransientRenameErr(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case syscall.ERROR_ACCESS_DENIED, errSharingViolation, syscall.ERROR_FILE_NOT_FOUND:
		return true
	}
	return false
}
