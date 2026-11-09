package apitool

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

// Non-canonical IPv4 literals (inet_aton forms) that a proxy or the OS
// resolver may read as a private address are private for a Foreign request.
func TestNonCanonicalIPv4IsPrivate(t *testing.T) {
	for _, h := range []string{"2130706433", "127.1", "0x7f.1", "017700000001", "0x7f000001", "0177.0.0.1", "10.1", "0x0a.0.0.1", "127.0.0.01", "4294967296", "1.2.3.0x1ff"} {
		if !IsPrivateHostname(h) {
			t.Errorf("%s not private", h)
		}
	}
	for _, h := range []string{"example.com", "8.8.8.8", "1x.example", "123.example.com", "a1.b2"} {
		if IsPrivateHostname(h) {
			t.Errorf("%s private", h)
		}
	}
}

func TestForeignProxiedNonCanonicalIPv4Asks(t *testing.T) {
	proxy, proxyHits := countServer(t, nil)
	withProxy(t, proxy.URL)
	s := newSender(t)
	s.Resolver = func(ctx context.Context, host string) ([]netip.Addr, error) { return nil, errors.New("no such host") }
	for _, u := range []string{"http://2130706433/", "http://127.1/", "http://0x7f.1/", "http://017700000001/"} {
		_, err := foreignSend(t, s, u, false)
		var pe *PrivateTargetError
		if !errors.As(err, &pe) {
			t.Errorf("%s: err = %v, want PrivateTargetError", u, err)
		}
	}
	if proxyHits.Load() != 0 {
		t.Fatalf("proxy got %d requests", proxyHits.Load())
	}
}
