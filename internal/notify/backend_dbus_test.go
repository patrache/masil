//go:build linux

package notify

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"
)

// TestDBusPlatformWiring: the Linux backend is dbusPlatform dialing the
// session bus with dialSessionBus; show itself is tested on every OS
// (TestDBusShowSendsBuiltCall).
func TestDBusPlatformWiring(t *testing.T) {
	b, ok := NewOSBackend("masil").(osBackend)
	if !ok {
		t.Fatalf("NewOSBackend = %T", NewOSBackend("masil"))
	}
	p, ok := b.p.(*dbusPlatform)
	if !ok {
		t.Fatalf("platform = %T, want *dbusPlatform", b.p)
	}
	if p.appName != "masil" {
		t.Errorf("appName = %q", p.appName)
	}
	if reflect.ValueOf(p.dial).Pointer() != reflect.ValueOf(dialSessionBus).Pointer() {
		t.Error("dial is not dialSessionBus")
	}
}

// fakeObject records the method calls made on the notification server object.
type fakeObject struct {
	dbus.BusObject // unused methods panic
	methods        []string
	args           [][]any
	caps           []string
}

func (o *fakeObject) CallWithContext(_ context.Context, method string, _ dbus.Flags, args ...any) *dbus.Call {
	o.methods = append(o.methods, method)
	o.args = append(o.args, args)
	if method == fdoIface+".GetCapabilities" {
		return &dbus.Call{Body: []any{o.caps}}
	}
	return &dbus.Call{Body: []any{uint32(1)}}
}

// TestDBusObjectPassesCallThrough: the godbus adapter sends the built call's
// fields, in Notify's argument order (susssasa{sv}i), with empty hints, and
// reads capabilities from GetCapabilities.
func TestDBusObjectPassesCallThrough(t *testing.T) {
	obj := &fakeObject{caps: []string{"body", "body-markup"}}
	o := dbusObject{obj}
	caps, err := o.capabilities(t.Context())
	if err != nil || !slices.Equal(caps, obj.caps) {
		t.Fatalf("capabilities = %v, %v", caps, err)
	}
	for _, n := range pairs() {
		c := dbusCall("masil", prepare(n), true)
		obj.methods, obj.args = nil, nil
		if err := o.notify(t.Context(), c); err != nil {
			t.Fatal(err)
		}
		want := []any{c.AppName, c.ReplacesID, c.Icon, c.Summary, c.Body, c.Actions, map[string]dbus.Variant{}, c.Timeout}
		if !slices.Equal(obj.methods, []string{fdoIface + ".Notify"}) || !reflect.DeepEqual(obj.args[0], want) {
			t.Fatalf("Notify call %q %#v\nwant %#v", obj.methods, obj.args, want)
		}
	}
}
