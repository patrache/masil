//go:build windows

package notify

import (
	"reflect"
	"testing"
)

// TestWindowsPlatformWiring: the Windows backend is windowsPlatform with the
// wintoast COM wrappers (setToastAppID, pushToast); show itself is tested on
// every OS (TestWindowsShowPushesEscapedToast).
func TestWindowsPlatformWiring(t *testing.T) {
	b, ok := NewOSBackend("masil").(osBackend)
	if !ok {
		t.Fatalf("NewOSBackend = %T", NewOSBackend("masil"))
	}
	p, ok := b.p.(windowsPlatform)
	if !ok {
		t.Fatalf("platform = %T, want windowsPlatform", b.p)
	}
	if p.appName != "masil" {
		t.Errorf("appName = %q", p.appName)
	}
	if reflect.ValueOf(p.setAppID).Pointer() != reflect.ValueOf(setToastAppID).Pointer() {
		t.Error("setAppID is not setToastAppID")
	}
	if reflect.ValueOf(p.push).Pointer() != reflect.ValueOf(pushToast).Pointer() {
		t.Error("push is not pushToast")
	}
}
