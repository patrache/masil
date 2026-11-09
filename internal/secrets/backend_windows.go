package secrets

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// defaultOS is the Windows Credential Manager (advapi32 Cred*W, no PowerShell).
func defaultOS() OSBackend { return credman{api: winCred{}} }

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1 // CRED_TYPE_GENERIC
	credPersistLocalMachine = 2 // CRED_PERSIST_LOCAL_MACHINE: this user, this PC (no roaming)

	errNotFound           = syscall.Errno(1168) // ERROR_NOT_FOUND
	errNoSuchLogonSession = syscall.Errno(1312) // ERROR_NO_SUCH_LOGON_SESSION (no profile: services, some SSH sessions)
)

// credentialW is CREDENTIALW.
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// winCred is the credAPI over advapi32.
type winCred struct{}

func (winCred) write(target, comment string, blob []byte) error {
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	user, _ := windows.UTF16PtrFromString(Service)
	c := credentialW{
		Type:               credTypeGeneric,
		TargetName:         t,
		CredentialBlobSize: uint32(len(blob)),
		Persist:            credPersistLocalMachine,
		UserName:           user,
	}
	if comment != "" {
		if c.Comment, err = windows.UTF16PtrFromString(comment); err != nil {
			return err
		}
	}
	if len(blob) > 0 {
		c.CredentialBlob = &blob[0]
	}
	r, _, e := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0)
	runtime.KeepAlive(blob)
	runtime.KeepAlive(&c)
	if r == 0 {
		return credErr(e)
	}
	return nil
}

func (winCred) read(target string) (string, []byte, error) {
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return "", nil, err
	}
	var pc *credentialW
	r, _, e := procCredReadW.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&pc)))
	if r == 0 {
		return "", nil, credErr(e)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(pc)))
	var blob []byte
	if pc.CredentialBlobSize > 0 && pc.CredentialBlob != nil {
		blob = append([]byte(nil), unsafe.Slice(pc.CredentialBlob, pc.CredentialBlobSize)...)
	}
	return windows.UTF16PtrToString(pc.Comment), blob, nil
}

func (winCred) delete(target string) error {
	t, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, _, e := procCredDeleteW.Call(uintptr(unsafe.Pointer(t)), credTypeGeneric, 0)
	if r == 0 {
		return credErr(e)
	}
	return nil
}

// credErr maps a Cred*W error: not found, no logon session (= unavailable), or
// the error as is.
func credErr(e error) error {
	var errno syscall.Errno
	if errors.As(e, &errno) {
		switch errno {
		case errNotFound:
			return ErrNotFound
		case errNoSuchLogonSession:
			return fmt.Errorf("%w: credential manager: %v", ErrUnavailable, errno)
		}
	}
	return fmt.Errorf("secrets: credential manager: %w", e)
}
