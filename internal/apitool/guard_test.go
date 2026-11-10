package apitool

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/patrache/masil/internal/secrets"
)

// countServer counts the requests it gets and records X-Api-Key.
func countServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if h != nil {
			h(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// secretScope has a secret "token"/"key" allowed for hosts, and base_url.
func secretScope(t *testing.T, baseURL string, hosts ...string) *Scope {
	t.Helper()
	vault := testVault(t)
	s := &Scope{
		Env: &Env{ID: "e1", Vars: []Var{
			{Name: "base_url", Value: baseURL},
			{Name: "token", Secret: true, AllowedHosts: hosts},
			{Name: "key", Secret: true, AllowedHosts: hosts},
		}},
		Vault: vault,
	}
	for _, n := range []string{"token", "key"} {
		if err := vault.Put(SecretKey("", "e1", n), secrets.New("sv-"+n)); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestSecretToUnlistedHostBlocked(t *testing.T) {
	evil, hits := countServer(t, nil)
	s := newSender(t)
	sc := secretScope(t, evil.URL, "api.test")
	r := &Request{Method: "GET", URL: "{{base_url}}/v1", Headers: []KV{{Key: "Authorization", Value: "Bearer {{token}}"}}}
	_, err := send(t, s, r, sc)
	var he *HostNotAllowedError
	if !errors.As(err, &he) || he.Var != "token" || he.Host != "127.0.0.1" {
		t.Fatalf("err = %v, want HostNotAllowedError{token, 127.0.0.1}", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("evil server got %d requests", hits.Load())
	}
	if strings.Contains(err.Error(), "sv-token") {
		t.Errorf("error carries the secret: %v", err)
	}
	// allowing the host lets it through
	sc.Env.Vars[1].AllowedHosts = append(sc.Env.Vars[1].AllowedHosts, "127.0.0.1")
	if resp, err := send(t, s, r, sc); err != nil || resp.Status != 200 || hits.Load() != 1 {
		t.Fatalf("after allow: %v %v hits=%d", err, resp, hits.Load())
	}
}

func TestHostMatchExactHostname(t *testing.T) {
	used := []Value{{Name: "token", Secret: true, AllowedHosts: []string{"API.x.com:443", "[::1]"}}}
	for _, c := range []struct {
		url string
		ok  bool
	}{
		{"https://api.X.com:8443/a", true},
		{"https://api.x.com./a", true},
		{"http://[::1]:9/", true},
		{"https://evil.api.x.com/", false},
		{"https://x.com/", false},
		{"https://api.x.com.evil.test/", false},
	} {
		err := CheckSecretHosts(&Prepared{URL: c.url, Used: used})
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.url, err, c.ok)
		}
	}
	if err := CheckSecretHosts(&Prepared{URL: "https://api.x.com/", Used: []Value{{Name: "t", Secret: true}}}); err == nil {
		t.Error("a secret with no allowed host was sent")
	}
}

func TestNonSecretVarsNotChecked(t *testing.T) {
	p := &Prepared{URL: "https://anywhere.test/", Used: []Value{{Name: "base_url", Text: "x"}, {Name: "$uuid", Source: SourceDynamic}}}
	if err := CheckSecretHosts(p); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultHosts(t *testing.T) {
	sc := &Scope{Env: &Env{ID: "e", Vars: []Var{{Name: "base_url", Value: "https://API.Base.test:8443/v1"}}}}
	p, err := Prepare(&Request{Method: "GET", URL: "https://other.test/x"}, "", sc, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if got := DefaultHosts(p, sc, fixedDyn()); !slices.Equal(got, []string{"other.test", "api.base.test"}) {
		t.Fatalf("DefaultHosts = %v", got)
	}
	p.URL = "https://api.base.test/y"
	if got := DefaultHosts(p, sc, fixedDyn()); !slices.Equal(got, []string{"api.base.test"}) {
		t.Fatalf("DefaultHosts dedupe = %v", got)
	}
}

func TestRedirectToUnlistedHostStopsWithSecret(t *testing.T) {
	other, otherHits := countServer(t, nil)
	port := other.URL[strings.LastIndex(other.URL, ":")+1:]
	allowed, _ := countServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+port+"/steal", http.StatusFound)
	})
	s := newSender(t)
	sc := secretScope(t, allowed.URL, "127.0.0.1")
	r := &Request{Method: "GET", URL: "{{base_url}}/start?k={{token}}", Headers: []KV{{Key: "X-Api-Key", Value: "{{key}}"}}}
	resp, err := send(t, s, r, sc)
	if err != nil {
		t.Fatal(err)
	}
	if otherHits.Load() != 0 {
		t.Fatalf("other host got %d requests", otherHits.Load())
	}
	if resp.Status != 302 || resp.RedirectBlocked == nil || resp.RedirectBlocked.To != "localhost" || resp.RedirectBlocked.Var == "" {
		t.Fatalf("resp = %d %+v", resp.Status, resp.RedirectBlocked)
	}
}

func TestRedirectSameHostFollows(t *testing.T) {
	var gotKey string
	srv, _ := countServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
		gotKey = r.Header.Get("X-Api-Key")
		fmt.Fprint(w, "next")
	})
	s := newSender(t)
	sc := secretScope(t, srv.URL, "127.0.0.1")
	resp, err := send(t, s, &Request{Method: "GET", URL: "{{base_url}}/start", Headers: []KV{{Key: "X-Api-Key", Value: "{{key}}"}}}, sc)
	if err != nil || resp.Status != 200 || resp.RedirectBlocked != nil || gotKey != "sv-key" {
		t.Fatalf("err=%v status=%d block=%+v key=%q", err, resp.Status, resp.RedirectBlocked, gotKey)
	}
}

func foreignSend(t *testing.T, s *Sender, rawURL string, ok bool) (*Response, error) {
	t.Helper()
	p, err := Prepare(&Request{Method: "GET", URL: rawURL, Settings: Settings{TimeoutMS: 2000}}, "", nil, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	p.Foreign, p.LastBy, p.PrivateOK = true, "hana", ok
	return s.Send(context.Background(), p)
}

func TestForeignPrivateLiteralAsks(t *testing.T) {
	srv, hits := countServer(t, nil)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	s := newSender(t)
	for _, u := range []string{srv.URL, "http://10.0.0.5:" + port, "http://169.254.169.254/latest/meta-data", "http://[::1]:" + port, "http://printer.local/", "http://app.localhost/"} {
		start := time.Now()
		_, err := foreignSend(t, s, u, false)
		var pe *PrivateTargetError
		if !errors.As(err, &pe) || pe.LastBy != "hana" || pe.Host == "" {
			t.Fatalf("%s: err = %v, want PrivateTargetError", u, err)
		}
		if time.Since(start) > 500*time.Millisecond {
			t.Errorf("%s: guard took %v (connected?)", u, time.Since(start))
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("server got %d requests", hits.Load())
	}
	if resp, err := foreignSend(t, s, srv.URL, true); err != nil || resp.Status != 200 || hits.Load() != 1 {
		t.Fatalf("PrivateOK: %v hits=%d", err, hits.Load())
	}
}

func TestForeignHostnameResolvingToLoopbackAsks(t *testing.T) {
	srv, hits := countServer(t, nil)
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	s := newSender(t)
	var asked atomic.Int32
	s.Resolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
		asked.Add(1)
		if host == "internal.example" {
			return []netip.Addr{netip.MustParseAddr("::ffff:127.0.0.1")}, nil
		}
		return nil, errors.New("no such host")
	}
	_, err := foreignSend(t, s, "http://internal.example:"+port+"/", false)
	var pe *PrivateTargetError
	if !errors.As(err, &pe) || pe.Host != "internal.example" || pe.IP != netip.MustParseAddr("127.0.0.1") {
		t.Fatalf("err = %v (%+v), want PrivateTargetError with 127.0.0.1", err, pe)
	}
	if hits.Load() != 0 || asked.Load() == 0 {
		t.Fatalf("hits=%d asked=%d", hits.Load(), asked.Load())
	}
	// once allowed, it dials the resolved address
	resp, err := foreignSend(t, s, "http://internal.example:"+port+"/", true)
	if err != nil || resp.Status != 200 || hits.Load() != 1 {
		t.Fatalf("PrivateOK: %v hits=%d", err, hits.Load())
	}
}

func TestForeignRedirectToPrivateAsks(t *testing.T) {
	inner, innerHits := countServer(t, nil)
	port := inner.URL[strings.LastIndex(inner.URL, ":")+1:]
	s := newSender(t)
	s.Resolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
		switch host {
		case "public.example":
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return nil, errors.New("no such host")
	}
	outer, _ := countServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:"+port+"/", http.StatusFound)
	})
	oport := outer.URL[strings.LastIndex(outer.URL, ":")+1:]
	// the user allowed public.example (it resolves privately here); the
	// redirect to another private host still asks
	_, err := foreignSend(t, s, "http://public.example:"+oport+"/", true)
	var pe *PrivateTargetError
	if !errors.As(err, &pe) || pe.Host != "localhost" {
		t.Fatalf("err = %v", err)
	}
	if innerHits.Load() != 0 {
		t.Fatalf("inner got %d", innerHits.Load())
	}
}

func TestOwnRequestNotAsked(t *testing.T) {
	srv, hits := countServer(t, nil)
	s := newSender(t)
	p, err := Prepare(&Request{Method: "GET", URL: srv.URL}, "", nil, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := s.Send(context.Background(), p); err != nil || resp.Status != 200 || hits.Load() != 1 {
		t.Fatalf("own request: %v hits=%d", err, hits.Load())
	}
}

func TestIsPrivate(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "100.64.0.1", "169.254.169.254", "0.0.0.0", "0.1.2.3", "::1", "::", "fc00::1", "fd12::1", "fe80::1", "::ffff:10.0.0.1", "64:ff9b::7f00:1"} {
		if !IsPrivateAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s not private", a)
		}
	}
	for _, a := range []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111", "100.128.0.1", "172.32.0.1"} {
		if IsPrivateAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s private", a)
		}
	}
	for _, h := range []string{"localhost", "LOCALHOST.", "a.localhost", "printer.local", "127.0.0.1", "[::1]"} {
		if !IsPrivateHostname(h) {
			t.Errorf("%s not private", h)
		}
	}
	for _, h := range []string{"example.com", "local.example.com", "localhost.example", "8.8.8.8"} {
		if IsPrivateHostname(h) {
			t.Errorf("%s private", h)
		}
	}
}

func TestSecretWithHostOverrideBlocked(t *testing.T) {
	srv, hits := countServer(t, nil)
	s := newSender(t)
	sc := secretScope(t, srv.URL, "127.0.0.1")
	r := &Request{Method: "GET", URL: "{{base_url}}/x", Headers: []KV{{Key: "Host", Value: "Attacker.Example:443"}, {Key: "X-Api-Key", Value: "{{key}}"}}}
	_, err := send(t, s, r, sc)
	var he *HostNotAllowedError
	if !errors.As(err, &he) || he.Var != "key" || he.Host != "attacker.example" {
		t.Fatalf("err = %v, want HostNotAllowedError{key, attacker.example}", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("server got %d requests", hits.Load())
	}
	// an override naming an allowed host goes
	r.Headers[0].Value = "127.0.0.1"
	if resp, err := send(t, s, r, sc); err != nil || resp.Status != 200 || hits.Load() != 1 {
		t.Fatalf("allowed override: %v hits=%d", err, hits.Load())
	}
	// no secret: the override is not checked
	r.Headers = []KV{{Key: "Host", Value: "attacker.example"}}
	if resp, err := send(t, s, r, sc); err != nil || resp.Status != 200 {
		t.Fatalf("no secret: %v", err)
	}
}

func TestRedirectKeepsHostOverrideChecked(t *testing.T) {
	g := &guard{p: &Prepared{Used: []Value{{Name: "key", Secret: true, AllowedHosts: []string{"127.0.0.1"}}}}, proxies: map[string]bool{}}
	req, _ := http.NewRequest("GET", "http://127.0.0.1:1/next", nil)
	req.Host = "attacker.example"
	if err := g.redirect(req); err != http.ErrUseLastResponse {
		t.Fatalf("redirect err = %v, want ErrUseLastResponse", err)
	}
	if b := g.redirectBlock(); b == nil || b.To != "attacker.example" || b.Var != "key" {
		t.Fatalf("block = %+v", b)
	}
}

// withProxy routes every request through proxyURL for the test.
func withProxy(t *testing.T, proxyURL string) {
	t.Helper()
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	old := proxyFromEnv
	proxyFromEnv = func(*http.Request) (*url.URL, error) { return u, nil }
	t.Cleanup(func() { proxyFromEnv = old })
}

func TestForeignProxiedNameResolvingPrivateAsks(t *testing.T) {
	proxy, proxyHits := countServer(t, nil)
	withProxy(t, proxy.URL)
	s := newSender(t)
	s.Resolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
		switch host {
		case "internal.example":
			return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.5")}, nil
		case "public.example":
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		case "127.0.0.1":
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return nil, errors.New("no such host")
	}
	_, err := foreignSend(t, s, "http://internal.example/admin", false)
	var pe *PrivateTargetError
	if !errors.As(err, &pe) || pe.Host != "internal.example" || pe.IP != netip.MustParseAddr("10.0.0.5") {
		t.Fatalf("err = %v (%+v), want PrivateTargetError 10.0.0.5", err, pe)
	}
	if proxyHits.Load() != 0 {
		t.Fatalf("proxy got %d requests", proxyHits.Load())
	}
	// consented, public, or not resolvable here: through the proxy
	for i, c := range []struct {
		url string
		ok  bool
	}{{"http://internal.example/admin", true}, {"http://public.example/", false}, {"http://only-proxy-knows.example/", false}} {
		if resp, err := foreignSend(t, s, c.url, c.ok); err != nil || resp.Status != 200 || proxyHits.Load() != int32(i+1) {
			t.Fatalf("%s: %v hits=%d", c.url, err, proxyHits.Load())
		}
	}
}
