package apitool

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
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

// An https request using a secret does not follow a redirect to plain http,
// even on the same (allowed) host.
func TestRedirectHTTPSToHTTPStopsWithSecret(t *testing.T) {
	plain, plainHits := countServer(t, nil)
	pport := plain.URL[strings.LastIndex(plain.URL, ":")+1:]
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:"+pport+"/next", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	s := newSender(t)
	s.TLS = &tls.Config{RootCAs: pool}
	sc := secretScope(t, srv.URL, "127.0.0.1")
	resp, err := send(t, s, &Request{Method: "GET", URL: "{{base_url}}/start", Headers: []KV{{Key: "X-Api-Key", Value: "{{key}}"}}}, sc)
	if err != nil {
		t.Fatal(err)
	}
	if plainHits.Load() != 0 {
		t.Fatalf("plain http got %d requests", plainHits.Load())
	}
	if resp.Status != 302 || resp.RedirectBlocked == nil || resp.RedirectBlocked.Var != "key" || !resp.RedirectBlocked.Insecure {
		t.Fatalf("resp = %d %+v", resp.Status, resp.RedirectBlocked)
	}
	// without a secret the downgrade is followed as before
	resp, err = send(t, s, &Request{Method: "GET", URL: srv.URL + "/start"}, sc)
	if err != nil || resp.Status != 200 || resp.RedirectBlocked != nil || plainHits.Load() != 1 {
		t.Fatalf("no secret: %v %+v hits=%d", err, resp, plainHits.Load())
	}
}
