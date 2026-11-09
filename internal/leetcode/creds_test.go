package leetcode

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
)

func TestNormalizeCookie(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{NameSession, " LEETCODE_SESSION=abc123;\n", "abc123"},
		{NameSession, "leetcode_session=abc123", "abc123"},
		{NameSession, "\"abc\"", "abc"},
		{NameSession, "'abc'", "abc"},
		{NameSession, "  abc\r\n", "abc"},
		{NameSession, "ab\nc", "abc"},
		{NameCSRF, "csrftoken=XYZ; Path=/", "XYZ"},
		{NameCSRF, "CSRFTOKEN=\"XYZ\"; Secure", "XYZ"},
		{NameCFClearance, "cf_clearance=v.1-2; Domain=.leetcode.com", "v.1-2"},
		{NameCSRF, "", ""},
		{NameCSRF, ";", ""},
	} {
		if got := NormalizeCookie(c.name, c.in); got != c.want {
			t.Errorf("NormalizeCookie(%q, %q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestMask(t *testing.T) {
	for in, want := range map[string]string{"abcdefgh": "••••efgh", "abcd": "••••", "": "••••", "abcde": "••••bcde"} {
		if got := Mask(secrets.New(in)); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCredsNamespacedPerUser(t *testing.T) {
	st := secrettest.NewStore(t, true)
	a := CredNS{ServerHost: "masil.test", UserID: "U_A"}
	b := CredNS{ServerHost: "masil.test", UserID: "U_B"}
	k := a.Key(NameSession)
	if k != (secrets.Key{Tool: "leet", Server: "masil.test", UserID: "U_A", Name: "LEETCODE_SESSION"}) {
		t.Fatalf("key = %+v", k)
	}
	if k.Account() != "leet/masil.test/U_A/LEETCODE_SESSION" {
		t.Fatalf("account = %q", k.Account())
	}
	in := Credentials{Session: secrets.New("sess-SECRET"), CSRF: secrets.New("csrf-SECRET")}
	if err := SaveCredentials(t.Context(), st, a, in); err != nil {
		t.Fatal(err)
	}
	noEnv := func(string) string { return "" }
	got, err := LoadCredentials(t.Context(), st, a, noEnv)
	if err != nil || got.Session.Reveal() != "sess-SECRET" || got.CSRF.Reveal() != "csrf-SECRET" || !got.CFClearance.IsZero() || got.FromEnv {
		t.Fatalf("load A = %+v, %v", got, err)
	}
	if _, err := LoadCredentials(t.Context(), st, b, noEnv); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("load B: %v", err)
	}
	vals := secrettest.Values(st)
	if len(vals) != 2 || vals["leet/masil.test/U_A/LEETCODE_SESSION"] != "sess-SECRET" || vals["leet/masil.test/U_A/csrftoken"] != "csrf-SECRET" {
		t.Fatalf("stored = %v", vals)
	}
	// cf_clearance saved, then a save without it removes the old one
	in.CFClearance = secrets.New("cf-SECRET")
	if err := SaveCredentials(t.Context(), st, a, in); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadCredentials(t.Context(), st, a, noEnv); got.CFClearance.Reveal() != "cf-SECRET" {
		t.Fatalf("cf = %q", got.CFClearance.Reveal())
	}
	in.CFClearance = secrets.Secret{}
	if err := SaveCredentials(t.Context(), st, a, in); err != nil {
		t.Fatal(err)
	}
	if _, ok := secrettest.Values(st)["leet/masil.test/U_A/cf_clearance"]; ok {
		t.Fatal("old cf_clearance kept")
	}
	if err := DeleteCredentials(t.Context(), st, a); err != nil {
		t.Fatal(err)
	}
	if n := len(secrettest.Values(st)); n != 0 {
		t.Fatalf("%d values after delete", n)
	}
	if _, err := LoadCredentials(t.Context(), st, a, noEnv); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestEnvCredsNotSaved(t *testing.T) {
	env := map[string]string{"LEETCODE_SESSION": "env-sess", "LEETCODE_CSRFTOKEN": "env-csrf", "LEETCODE_CFCLEARANCE": "env-cf"}
	getenv := func(k string) string { return env[k] }
	st := secrettest.NewStore(t, true)
	ns := CredNS{ServerHost: "masil.test", UserID: "U_A"}
	c, err := LoadCredentials(t.Context(), st, ns, getenv)
	if err != nil || !c.FromEnv || c.Session.Reveal() != "env-sess" || c.CSRF.Reveal() != "env-csrf" || c.CFClearance.Reveal() != "env-cf" {
		t.Fatalf("env creds = %+v, %v", c, err)
	}
	if err := SaveCredentials(t.Context(), st, ns, *c); err != nil {
		t.Fatal(err)
	}
	if n := len(secrettest.Values(st)); n != 0 {
		t.Fatalf("env creds stored: %d values", n)
	}
	// both cookies are needed; cf_clearance alone is optional
	delete(env, "LEETCODE_CFCLEARANCE")
	if c, ok := CredentialsFromEnv(getenv); !ok || !c.CFClearance.IsZero() {
		t.Fatalf("without cf: %+v %v", c, ok)
	}
	delete(env, "LEETCODE_CSRFTOKEN")
	if _, ok := CredentialsFromEnv(getenv); ok {
		t.Fatal("session alone accepted")
	}
	if _, ok := CredentialsFromEnv(nil); ok {
		t.Fatal("nil getenv accepted")
	}
	// env values are normalized like pasted ones
	env["LEETCODE_CSRFTOKEN"] = " csrftoken=abc; Path=/ "
	if c, _ := CredentialsFromEnv(getenv); c.CSRF.Reveal() != "abc" {
		t.Fatalf("csrf = %q", c.CSRF.Reveal())
	}
}

func TestSaveNeedsConsent(t *testing.T) {
	st := secrettest.NewStore(t, false)
	err := SaveCredentials(t.Context(), st, CredNS{ServerHost: "masil.test", UserID: "U_A"},
		Credentials{Session: secrets.New("s"), CSRF: secrets.New("c")})
	if !errors.Is(err, secrets.ErrNeedConsent) {
		t.Fatalf("err = %v", err)
	}
	if err := SaveCredentials(t.Context(), st, CredNS{ServerHost: "masil.test", UserID: "U_A"}, Credentials{}); err == nil {
		t.Fatal("empty credentials saved")
	}
}

func TestCredentialsNeverStringify(t *testing.T) {
	c := Credentials{Session: secrets.New("sess-SECRET"), CSRF: secrets.New("csrf-SECRET"), CFClearance: secrets.New("cf-SECRET")}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{fmt.Sprintf("%v %+v %#v %v", c, c, c, &c), string(b)} {
		for _, v := range []string{"sess-SECRET", "csrf-SECRET", "cf-SECRET"} {
			if strings.Contains(s, v) {
				t.Fatalf("%q leaks %q", s, v)
			}
		}
	}
}
