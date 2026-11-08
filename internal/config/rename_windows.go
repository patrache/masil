//go:build windows

package config

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// isTransientRenameErr reports Windows rename errors that usually clear up
// once another process closes the target or finishes its own rename
// (mirrors cmd/go/internal/robustio).
func isTransientRenameErr(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case windows.ERROR_ACCESS_DENIED, windows.ERROR_SHARING_VIOLATION, windows.ERROR_FILE_NOT_FOUND:
		return true
	}
	return false
}
