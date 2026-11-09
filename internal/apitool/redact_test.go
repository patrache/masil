package apitool

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/secrets"
)

func TestSensitiveHeaderNames(t *testing.T) {
	for _, n := range []string{"Authorization", "proxy-authorization", "COOKIE", "Set-Cookie", "x-api-key", "X-Auth-Token", "X-Session-Id", "My-Token", "X-Client-Secret", "X-Password", "Api-Key"} {
		if !SensitiveHeader(n) {
			t.Errorf("%s not sensitive", n)
		}
	}
	for _, n := range []string{"Content-Type", "Accept", "User-Agent", "X-Request-Id", "Keep-Alive", ""} {
		if SensitiveHeader(n) {
			t.Errorf("%s sensitive", n)
		}
	}
}

func TestRedactSecretValueAnywhere(t *testing.T) {
	sec := []string{"s3cr3t", "", "s3cr3t-longer", "a b&c"}
	in := "body s3cr3t-longer and s3cr3t; url ?k=" + url.QueryEscape("a b&c") + " path /" + url.PathEscape("a b&c") + " raw a b&c"
	got := RedactString(in, sec)
	if strings.Contains(got, "s3cr3t") || strings.Contains(got, "a b&c") || strings.Contains(got, url.QueryEscape("a b&c")) || strings.Contains(got, url.PathEscape("a b&c")) {
		t.Fatalf("RedactString = %q", got)
	}
	if got != "body ••• and •••; url ?k=••• path /••• raw •••" {
		t.Errorf("RedactString = %q", got)
	}
	if RedactString("nothing here", []string{""}) != "nothing here" {
		t.Error("empty secret changed the text")
	}
	h := http.Header{
		"Authorization": {"Bearer abc"},
		"Content-Type":  {"application/json"},
		"X-Echo":        {"has s3cr3t inside"},
		"Set-Cookie":    {"a=1", "b=2"},
	}
	r := RedactHeader(h, sec)
	if r.Get("Authorization") != Mask || r.Get("Content-Type") != "application/json" || r.Get("X-Echo") != "has ••• inside" {
		t.Errorf("RedactHeader = %v", r)
	}
	if v := r.Values("Set-Cookie"); len(v) != 2 || v[0] != Mask || v[1] != Mask {
		t.Errorf("Set-Cookie = %v", v)
	}
	if h.Get("Authorization") != "Bearer abc" {
		t.Error("RedactHeader changed its input")
	}
	if RedactHeader(nil, sec) != nil {
		t.Error("nil header")
	}
}

// TestRedactSecretInExpandedPath: a secret with '/', ';' or ',' plus a
// character to escape is written by url.URL with its path rules — neither
// QueryEscape nor PathEscape gives that form.
func TestRedactSecretInExpandedPath(t *testing.T) {
	for _, v := range []string{"a/b c", "x;y z", "abc/défg", "p,q r", "plain"} {
		sc := &Scope{Env: &Env{ID: "e1", Vars: []Var{{Name: "k", Secret: true, AllowedHosts: []string{"h.test"}}}}, Vault: testVault(t)}
		if err := sc.Vault.Put(SecretKey("", "e1", "k"), secrets.New(v)); err != nil {
			t.Fatal(err)
		}
		p, err := Prepare(&Request{Method: "GET", URL: "https://h.test/v1/{{k}}/x?q={{k}}"}, "", sc, fixedDyn())
		if err != nil {
			t.Fatal(err)
		}
		if got := RedactString(p.URL, []string{v}); got != "https://h.test/v1/•••/x?q=•••" {
			t.Errorf("%q: URL %q redacted to %q", v, p.URL, got)
		}
	}
}
