package apitool

import (
	"strings"
	"testing"
)

func TestDetectPatterns(t *testing.T) {
	cases := []struct {
		name, value, rule string
	}{
		{"Authorization", "Bearer " + strings.Repeat("Ab3", 13) + "x", "bearer"},
		{"Authorization", "Basic dXNlcjpwYXNzd29yZDEyMw==", "basic"},
		{"note", "token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U here", "jwt"},
		{"cfg", "AKIAIOSFODNN7EXAMPLE", "aws_access_key"},
		{"body", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----", "private_key"},
		{"body", "-----BEGIN PRIVATE KEY-----\nMIIE", "private_key"},
		{"x", "ghp_" + strings.Repeat("a1B2", 9), "github_token"},
		{"x", "github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz", "github_token"},
		{"x", "xoxb-1234567890-abcdefghij", "slack_token"},
		{"X-Api-Key", "q8Zr2Lm9Xv4Tn7Kp1Wd6Hs3Jf5Bc0Gy", "named_high_entropy"},
		{"session_id", "Zq9X2mL7vT4nK1pW8dH6sJ3fB5cG0yRe", "named_high_entropy"},
	}
	for _, c := range cases {
		got := Detect(c.name, c.value)
		if len(got) == 0 {
			t.Errorf("%s=%q: no finding, want %s", c.name, c.value, c.rule)
			continue
		}
		f := got[0]
		if f.Rule != c.rule || f.Start < 0 || f.End > len(c.value) || f.Start >= f.End {
			t.Errorf("%s=%q: %+v, want %s", c.name, c.value, got, c.rule)
		}
	}
	// the bearer span is the token, not the scheme
	v := "Bearer " + strings.Repeat("Ab3", 13) + "x"
	if f := Detect("Authorization", v)[0]; v[f.Start:f.End] != strings.Repeat("Ab3", 13)+"x" {
		t.Errorf("bearer span = %q", v[f.Start:f.End])
	}
	// one finding per span (a JWT bearer is one finding)
	jwtBearer := "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	if got := Detect("Authorization", jwtBearer); len(got) != 1 || got[0].Rule != "jwt" {
		t.Errorf("jwt bearer = %+v", got)
	}
}

func TestDetectSkipsTemplatesAndLowEntropy(t *testing.T) {
	for _, c := range []struct{ name, value string }{
		{"Authorization", "Bearer {{token}}"},
		{"Authorization", "Basic {{basic}}"},
		{"Authorization", "Bearer short"},
		{"password", "hello"},
		{"key", "aaaaaaaaaaaaaaaaaaaa"},
		{"X-Api-Key", "{{api_key}}"},
		{"X-Api-Key", "{{a}}{{b}}{{c}}{{d}}{{e}}"},
		{"Content-Type", "q8Zr2Lm9Xv4Tn7Kp1Wd6Hs3Jf5Bc0Gy"},
		{"url", "https://api.example.com/v1/payments?limit=10"},
		{"token", "ghp_short"},
	} {
		if got := Detect(c.name, c.value); len(got) != 0 {
			t.Errorf("%s=%q: %+v", c.name, c.value, got)
		}
	}
}

func FuzzDetect(f *testing.F) {
	f.Add("Authorization", "Bearer eyJ.a.b")
	f.Add("x", "-----BEGIN PRIVATE KEY-----")
	f.Fuzz(func(t *testing.T, name, value string) {
		for _, fd := range Detect(name, value) {
			if fd.Start < 0 || fd.End > len(value) || fd.Start >= fd.End {
				t.Fatalf("bad span %+v for %q", fd, value)
			}
		}
		_ = RedactString(value, []string{name})
	})
}
