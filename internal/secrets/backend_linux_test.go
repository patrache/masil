package secrets

import (
	"errors"
	"testing"
)

// TestSecretServiceRealNoBus: the real backend without a session bus address
// is unavailable (no dbus-launch autolaunch) and the store asks for consent.
func TestSecretServiceRealNoBus(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	b := defaultOS()
	if b == nil || b.Name() != BackendSecretService {
		t.Fatalf("defaultOS = %v", b)
	}
	if err := b.Set(bg, Service, "a/b/c/d", "v"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Set = %v, want ErrUnavailable", err)
	}
	s := Open(Options{Paths: testPaths(t)})
	if _, err := s.Put(bg, key("U1", "c"), New("v")); !errors.Is(err, ErrNeedConsent) {
		t.Fatalf("Put = %v, want ErrNeedConsent", err)
	}
}
