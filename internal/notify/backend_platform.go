package notify

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// The platforms' show methods live here, compiled on every OS, so the tests
// can run each one with hostile text on any machine with only the final OS
// call (exec, wintoast, the D-Bus connection) replaced. The GOOS files
// (backend_darwin.go, backend_windows.go, backend_dbus.go) only plug in those
// calls; they never see an OSNotification (TestPlatformFilesOnlyWire).

// showTimeout bounds one OS notifier call (it runs inside a tea.Cmd / the
// daemon's notify goroutine).
const showTimeout = 10 * time.Second

// runFunc runs one process (no shell) and returns its combined output.
type runFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// execRun is the real runFunc: exactly name and args, never a shell.
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// darwinPlatform shows notifications with osascript.
type darwinPlatform struct{ run runFunc }

// show runs osascript directly (no shell) with the text as argv data.
func (p darwinPlatform) show(n OSNotification) error {
	ctx, cancel := context.WithTimeout(context.Background(), showTimeout)
	defer cancel()
	name, args := darwinCommand(n)
	if out, err := p.run(ctx, name, args...); err != nil {
		return fmt.Errorf("notify: osascript: %w (%d bytes of output)", err, len(out))
	}
	return nil
}

// windowsPlatform shows WinRT toasts. setAppID and push are the wintoast COM
// calls (backend_windows.go); push gets the finished document only.
type windowsPlatform struct {
	appName  string
	setAppID func(appID string) error
	push     func(doc string) error
}

// show pushes the escaped toast XML.
func (p windowsPlatform) show(n OSNotification) error {
	if err := p.setAppID(p.appName); err != nil {
		return fmt.Errorf("notify: toast app data: %w", err)
	}
	if err := p.push(toastXML(n)); err != nil {
		return fmt.Errorf("notify: toast: %w", err)
	}
	return nil
}

// dbusConn is a live connection to the notification server.
type dbusConn interface {
	dbusSender
	alive() bool
	close()
}

// dbusPlatform talks to the notification server directly: the text is a D-Bus
// string argument, never part of a command line. dial opens the session bus
// (backend_dbus.go).
type dbusPlatform struct {
	appName string
	dial    func() (dbusConn, error)

	mu   sync.Mutex
	conn dbusConn
}

// show sends one Notify call, connecting first (once, reconnecting after a
// failure or a disconnect).
func (p *dbusPlatform) show(n OSNotification) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), showTimeout)
	defer cancel()
	if p.conn == nil || !p.conn.alive() {
		p.drop()
		conn, err := p.dial()
		if err != nil {
			return err
		}
		p.conn = conn
	}
	if err := dbusShow(ctx, p.conn, p.appName, n); err != nil {
		p.drop()
		return err
	}
	return nil
}

func (p *dbusPlatform) drop() {
	if p.conn != nil {
		p.conn.close()
		p.conn = nil
	}
}
