//go:build linux

package notify

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	showTimeout = 10 * time.Second

	fdoName  = "org.freedesktop.Notifications"
	fdoPath  = dbus.ObjectPath("/org/freedesktop/Notifications")
	fdoIface = "org.freedesktop.Notifications"
)

// dbusPlatform talks to the notification server directly: the text is a D-Bus
// string argument, never part of a command line.
type dbusPlatform struct {
	appName string

	mu   sync.Mutex
	conn *dbus.Conn
}

func newPlatform(appName string) platform { return &dbusPlatform{appName: appName} }

func (p *dbusPlatform) show(n OSNotification) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), showTimeout)
	defer cancel()
	obj, err := p.object()
	if err != nil {
		return err
	}
	if err := dbusShow(ctx, dbusObject{obj}, p.appName, n); err != nil {
		p.drop()
		return err
	}
	return nil
}

// object connects (once, reconnecting after a failure) without autolaunching a
// bus: no dbus-launch process is ever started for a notification. The
// connection has no context of its own (WithContext would close it when one
// call's timeout ends); each call carries ctx instead.
func (p *dbusPlatform) object() (dbus.BusObject, error) {
	if p.conn == nil || !p.conn.Connected() {
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
		p.conn = conn
	}
	return p.conn.Object(fdoName, fdoPath), nil
}

func (p *dbusPlatform) drop() {
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
}

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
