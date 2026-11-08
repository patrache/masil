//go:build windows

package config

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsTransientRenameErrWindows(t *testing.T) {
	for _, errno := range []error{windows.ERROR_ACCESS_DENIED, windows.ERROR_SHARING_VIOLATION, windows.ERROR_FILE_NOT_FOUND} {
		err := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: errno}
		if !isTransientRenameErr(err) {
			t.Errorf("isTransientRenameErr(%v) = false, want true", err)
		}
	}
	if isTransientRenameErr(&os.LinkError{Op: "rename", Old: "a", New: "b", Err: windows.ERROR_INVALID_NAME}) {
		t.Error("ERROR_INVALID_NAME treated as transient")
	}
	if isTransientRenameErr(errors.New("x")) {
		t.Error("plain error treated as transient")
	}
}
