package secrets

import (
	"errors"
	"syscall"
	"testing"
)

func TestCredManIsDefault(t *testing.T) {
	b := defaultOS()
	if b == nil || b.Name() != BackendCredMan {
		t.Fatalf("defaultOS = %v", b)
	}
}

func TestCredErrMapping(t *testing.T) {
	if !errors.Is(credErr(syscall.Errno(1168)), ErrNotFound) { // ERROR_NOT_FOUND
		t.Error("ERROR_NOT_FOUND")
	}
	if !errors.Is(credErr(syscall.Errno(1312)), ErrUnavailable) { // ERROR_NO_SUCH_LOGON_SESSION
		t.Error("ERROR_NO_SUCH_LOGON_SESSION")
	}
	if err := credErr(syscall.Errno(87)); errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnavailable) || err == nil {
		t.Errorf("ERROR_INVALID_PARAMETER = %v", err)
	}
}
