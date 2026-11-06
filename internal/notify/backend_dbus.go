//go:build linux

package notify

import (
	"context"
	"fmt"
	"slices"
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

	mu     sync.Mutex
	conn   *dbus.Conn
	markup bool // server capability "body-markup"
}

func newPlatform(appName string) platform { return &dbusPlatform{appName: appName} }

func (p *dbusPlatform) show(n OSNotification) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), showTimeout)
	defer cancel()
	obj, err := p.object(ctx)
	if err != nil {
		return err
	}
	c := dbusCall(p.appName, n, p.markup)
	call := obj.CallWithContext(ctx, fdoIface+".Notify", 0,
		c.AppName, c.ReplacesID, c.Icon, c.Summary, c.Body, c.Actions, map[string]dbus.Variant{}, c.Timeout)
	if call.Err != nil {
		p.drop()
		return fmt.Errorf("notify: dbus Notify: %w", call.Err)
	}
	return nil
}

// object connects (once, reconnecting after a failure) without autolaunching a
// bus: no dbus-launch process is ever started for a notification. The
// connection has no context of its own (WithContext would close it when one
// call's timeout ends); each call carries ctx instead.
func (p *dbusPlatform) object(ctx context.Context) (dbus.BusObject, error) {
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
		var caps []string
		if err := conn.Object(fdoName, fdoPath).CallWithContext(ctx, fdoIface+".GetCapabilities", 0).Store(&caps); err == nil {
			p.markup = slices.Contains(caps, "body-markup")
		} else {
			p.markup = true // unknown: escaping is the safe side
		}
	}
	return p.conn.Object(fdoName, fdoPath), nil
}

func (p *dbusPlatform) drop() {
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
}
