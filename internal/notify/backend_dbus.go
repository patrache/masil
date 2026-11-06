//go:build linux

package notify

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	fdoName  = "org.freedesktop.Notifications"
	fdoPath  = dbus.ObjectPath("/org/freedesktop/Notifications")
	fdoIface = "org.freedesktop.Notifications"
)

// newPlatform wires the session bus in (show: backend_platform.go).
func newPlatform(appName string) platform {
	return &dbusPlatform{appName: appName, dial: dialSessionBus}
}

// dialSessionBus connects without autolaunching a bus: no dbus-launch process
// is ever started for a notification. The connection has no context of its
// own (WithContext would close it when one call's timeout ends); each call
// carries ctx instead.
func dialSessionBus() (dbusConn, error) {
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return nil, fmt.Errorf("notify: dbus session bus: %w", err)
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("notify: dbus auth: %w", err)
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("notify: dbus hello: %w", err)
	}
	return godbusConn{dbusObject{conn.Object(fdoName, fdoPath)}, conn}, nil
}

// godbusConn is a dbusConn over a private session-bus connection.
type godbusConn struct {
	dbusObject
	conn *dbus.Conn
}

func (c godbusConn) alive() bool { return c.conn.Connected() }
func (c godbusConn) close()      { _ = c.conn.Close() }

// dbusObject is the dbusSender over a godbus object.
type dbusObject struct{ obj dbus.BusObject }

func (o dbusObject) capabilities(ctx context.Context) ([]string, error) {
	var caps []string
	err := o.obj.CallWithContext(ctx, fdoIface+".GetCapabilities", 0).Store(&caps)
	return caps, err
}

func (o dbusObject) notify(ctx context.Context, c dbusNotify) error {
	call := o.obj.CallWithContext(ctx, fdoIface+".Notify", 0,
		c.AppName, c.ReplacesID, c.Icon, c.Summary, c.Body, c.Actions, map[string]dbus.Variant{}, c.Timeout)
	if call.Err != nil {
		return fmt.Errorf("notify: dbus Notify: %w", call.Err)
	}
	return nil
}
