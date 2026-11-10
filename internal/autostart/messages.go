package autostart

import (
	"errors"

	"github.com/patrache/masil/internal/i18n"
)

// ExeErrKind says what is wrong with the executable given to autostart.
type ExeErrKind int

const (
	ExeNotAbsolute ExeErrKind = iota // the path is relative
	ExeMissing                       // os.Stat failed (Err holds why)
	ExeNotRegular                    // a directory or a device
	ExeTemporary                     // a `go run` build (path contains "go-build")
)

// ExeError is a refused executable path. Error() is the Korean sentence; a
// screen renders it with Message in its own language.
type ExeError struct {
	Kind ExeErrKind
	Exe  string
	Err  error // the os.Stat error for ExeMissing
}

func (e *ExeError) Error() string { return Message(nil, e) }
func (e *ExeError) Unwrap() error { return e.Err }

// sentinel is an autostart error whose text comes from the catalog.
type sentinel struct{ text func(t *i18n.T) string }

func (s *sentinel) Error() string { return s.text(i18n.KoT()) }

var (
	// ErrRunKeyConfigDir: the Windows Run key cannot carry MASIL_CONFIG_DIR.
	ErrRunKeyConfigDir error = &sentinel{func(t *i18n.T) string { return t.Daemon.RunkeyConfigDir() }}
	// ErrNoSystemd: Linux has no systemd user session to register with.
	ErrNoSystemd error = &sentinel{func(t *i18n.T) string { return t.Daemon.NoSystemd() }}
)

// noSystemdError is ErrNoSystemd with the systemctl failure that showed it.
type noSystemdError struct{ cause error }

func noSystemd(cause error) error { return &noSystemdError{cause} }

func (e *noSystemdError) Error() string        { return Message(nil, e) }
func (e *noSystemdError) Is(target error) bool { return target == ErrNoSystemd }

// Message is err in t's language (nil t = ko): the autostart errors above are
// rendered from the catalog, anything else is err.Error() as it is.
func Message(t *i18n.T, err error) string {
	if t == nil {
		t = i18n.KoT()
	}
	var ee *ExeError
	var ns *noSystemdError
	var s *sentinel
	switch {
	case err == nil:
		return ""
	case errors.As(err, &ee):
		switch ee.Kind {
		case ExeNotAbsolute:
			return t.Daemon.ExeNotAbsolute(ee.Exe)
		case ExeMissing:
			cause := ""
			if ee.Err != nil {
				cause = ee.Err.Error()
			}
			return t.Daemon.ExeMissing(ee.Exe, cause)
		case ExeNotRegular:
			return t.Daemon.ExeNotRegular(ee.Exe)
		case ExeTemporary:
			return t.Daemon.ExeTemporary(ee.Exe)
		}
		return ee.Exe // an unknown Kind (never made here); not err.Error(), which calls Message
	case errors.As(err, &ns):
		if ns.cause == nil {
			return t.Daemon.NoSystemd()
		}
		return t.Daemon.NoSystemd() + " (" + ns.cause.Error() + ")"
	case errors.As(err, &s):
		return s.text(t)
	}
	return err.Error()
}
