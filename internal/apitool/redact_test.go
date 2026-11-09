package apitool

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
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
