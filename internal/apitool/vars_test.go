package apitool

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
)

func testVault(t *testing.T) Vault {
	t.Helper()
	return NewVault(context.Background(), secrettest.NewStore(t, true), "masil.test", "u1")
}

func fixedDyn() Dyn {
	return Dyn{
		Now:  func() time.Time { return time.Date(2026, 11, 10, 9, 8, 7, 654_000_000, time.FixedZone("KST", 9*3600)) },
		Rand: bytes.NewReader(bytes.Repeat([]byte{0xAB, 0x01, 0x7F, 0x33}, 64)),
	}
}

func TestResolutionOrder(t *testing.T) {
	s := &Scope{
		Temp:       map[string]string{"x": "temp"},
		Mine:       &Mine{Values: map[string]string{"e1/x": "mine"}},
		Env:        &Env{ID: "e1", Name: "staging", Vars: []Var{{Name: "x", Value: "env"}}},
		Collection: []Var{{Name: "x", Value: "col"}},
		Globals:    []Var{{Name: "x", Value: "glob"}},
		Vault:      testVault(t),
		DocID:      "doc1",
	}
	steps := []struct {
		text string
		src  Source
		drop func()
	}{
		{"temp", SourceTemp, func() { s.Temp = nil }},
		{"mine", SourceMine, func() { s.Mine = nil }},
		{"env", SourceSharedEnv, func() { s.Env = nil }},
		{"col", SourceCollection, func() { s.Collection = nil }},
		{"glob", SourceGlobal, func() { s.Globals = nil }},
	}
	for _, st := range steps {
		v, err := s.Lookup("x")
		if err != nil {
			t.Fatal(err)
		}
		if v.Text != st.text || v.Source != st.src || v.Secret {
			t.Fatalf("Lookup = %+v, want %q from %v", v, st.text, st.src)
		}
		st.drop()
	}
	var ue *UndefinedError
	if _, err := s.Lookup("x"); !errors.As(err, &ue) || ue.Name != "x" {
		t.Fatalf("Lookup after all dropped = %v", err)
	}
}

func TestSecretComesOnlyFromVault(t *testing.T) {
	vault := testVault(t)
	s := &Scope{
		Temp:  map[string]string{"token": "temp-x"},
		Mine:  &Mine{Values: map[string]string{"e1/token": "mine-x"}, Hosts: map[string][]string{"e1/token": {"api.test"}}},
		Env:   &Env{ID: "e1", Vars: []Var{{Name: "token", Secret: true, Value: "shared-x", AllowedHosts: []string{"evil.test"}}}},
		Vault: vault,
		DocID: "doc1",
	}
	var ue *UndefinedError
	if _, err := s.Lookup("token"); !errors.As(err, &ue) || ue.Name != "token" {
		t.Fatalf("no vault value: Lookup err = %v, want UndefinedError", err)
	}
	if err := vault.Put(SecretKey("doc1", "e1", "token"), secrets.New("vault-x")); err != nil {
		t.Fatal(err)
	}
	v, err := s.Lookup("token")
	if err != nil {
		t.Fatal(err)
	}
	if v.Text != "vault-x" || !v.Secret || !slices.Equal(v.AllowedHosts, []string{"api.test"}) {
		t.Fatalf("Lookup = %+v (shared secret: vault value, hosts from Mine)", v)
	}
	// personal environment: hosts come from the Var
	p := &Scope{Env: &Env{ID: "e2", Vars: []Var{{Name: "k", Secret: true, AllowedHosts: []string{"api.test"}}}}, Vault: vault}
	if err := vault.Put(SecretKey("", "e2", "k"), secrets.New("pv")); err != nil {
		t.Fatal(err)
	}
	v, err = p.Lookup("k")
	if err != nil || v.Text != "pv" || !slices.Equal(v.AllowedHosts, []string{"api.test"}) {
		t.Fatalf("personal Lookup = %+v, %v", v, err)
	}
	// globals
	g := &Scope{Globals: []Var{{Name: "pw", Secret: true}}, Vault: vault}
	if err := vault.Put(SecretKey("", "", "pw"), secrets.New("gv")); err != nil {
		t.Fatal(err)
	}
	if v, err := g.Lookup("pw"); err != nil || v.Text != "gv" || v.Source != SourceGlobal {
		t.Fatalf("global Lookup = %+v, %v", v, err)
	}
	// no vault at all: a secret is undefined, never a shared value
	n := &Scope{Env: &Env{ID: "e1", Vars: []Var{{Name: "token", Secret: true, Value: "shared-x"}}}}
	if _, err := n.Lookup("token"); !errors.As(err, &ue) {
		t.Fatalf("nil vault Lookup err = %v", err)
	}
}

func TestSecretKeyNames(t *testing.T) {
	for _, c := range []struct{ doc, env, name, want string }{
		{"", "e1", "token", "local/e1/token"},
		{"d1", "e1", "token", "doc/d1/e1/token"},
		{"", "", "pw", "globals/pw"},
		{"d1", "", "pw", "globals/pw"},
	} {
		if got := SecretKey(c.doc, c.env, c.name); got != c.want {
			t.Errorf("SecretKey(%q,%q,%q) = %q, want %q", c.doc, c.env, c.name, got, c.want)
		}
	}
}

func TestDynamicVarsDeterministic(t *testing.T) {
	s := &Scope{}
	out, used, err := Expand("{{$uuid}}|{{ $timestamp }}|{{$isoTimestamp}}|{{$randomInt}}", s, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`^([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\|(\d+)\|(.+)\|(\d+)$`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("Expand = %q", out)
	}
	if m[2] != "1794269287" {
		t.Errorf("$timestamp = %s", m[2])
	}
	if m[3] != "2026-11-10T00:08:07.654Z" {
		t.Errorf("$isoTimestamp = %s", m[3])
	}
	var n int
	for _, c := range m[4] {
		n = n*10 + int(c-'0')
	}
	if n < 0 || n > 1000 {
		t.Errorf("$randomInt = %d", n)
	}
	again, _, _ := Expand("{{$uuid}}|{{ $timestamp }}|{{$isoTimestamp}}|{{$randomInt}}", s, fixedDyn())
	if again != out {
		t.Errorf("not deterministic: %q vs %q", again, out)
	}
	if len(used) != 4 || used[0].Source != SourceDynamic || used[0].Name != "$uuid" {
		t.Errorf("used = %+v", used)
	}
	// defaults (nil Now / Rand) still work
	if out, _, err := Expand("{{$uuid}}", s, Dyn{}); err != nil || len(out) != 36 {
		t.Errorf("default Dyn: %q, %v", out, err)
	}
}

func TestUnknownDynamicIsUndefined(t *testing.T) {
	var ue *UndefinedError
	if _, _, err := Expand("a {{$foo}} b", &Scope{}, fixedDyn()); !errors.As(err, &ue) || ue.Name != "$foo" {
		t.Fatalf("err = %v", err)
	}
}

func TestExpandNoRecursion(t *testing.T) {
	s := &Scope{Collection: []Var{{Name: "x", Value: "{{y}}"}, {Name: "y", Value: "boom"}}}
	out, _, err := Expand("[{{x}}] {{ not a var }} {{y", s, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if out != "[{{y}}] {{ not a var }} {{y" {
		t.Fatalf("Expand = %q", out)
	}
}

func TestExpandReportsUsedSecrets(t *testing.T) {
	vault := testVault(t)
	s := &Scope{
		Env:   &Env{ID: "e1", Vars: []Var{{Name: "token", Secret: true, AllowedHosts: []string{"api.test"}}, {Name: "base_url", Value: "https://api.test"}}},
		Vault: vault,
	}
	if err := vault.Put(SecretKey("", "e1", "token"), secrets.New("tok-1")); err != nil {
		t.Fatal(err)
	}
	out, used, err := Expand("Bearer {{token}} {{base_url}} {{token}}", s, fixedDyn())
	if err != nil {
		t.Fatal(err)
	}
	if out != "Bearer tok-1 https://api.test tok-1" {
		t.Fatalf("Expand = %q", out)
	}
	if len(used) != 2 {
		t.Fatalf("used = %+v (each name once)", used)
	}
	if u := used[0]; u.Name != "token" || !u.Secret || u.Text != "tok-1" || !slices.Equal(u.AllowedHosts, []string{"api.test"}) {
		t.Errorf("used[0] = %+v", u)
	}
	if used[1].Secret {
		t.Errorf("base_url marked secret")
	}
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
