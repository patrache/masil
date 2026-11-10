package apitool

import (
	"errors"
	"testing"

	"github.com/patrache/masil/internal/secrets"
)

// An extract rule in a shared (someone else's) request must not overwrite my
// personal/global secret, and fills a shared-layer secret only from a
// response host the secret is allowed for.
func TestSharedExtractCannotOverwritePersonalSecret(t *testing.T) {
	vault := testVault(t)
	gkey := SecretKey("", "", "token")
	if err := vault.Put(gkey, secrets.New("my-real-token")); err != nil {
		t.Fatal(err)
	}
	s := &Scope{
		DocID:   "d1",
		Env:     &Env{ID: "e1", Vars: []Var{{Name: "sess", Secret: true}, {Name: "user_id"}}},
		Globals: []Var{{Name: "token", Secret: true, AllowedHosts: []string{"api.me"}}},
		Mine:    &Mine{Hosts: map[string][]string{"e1/sess": {"api.test"}}},
		Vault:   vault,
	}
	for _, host := range []string{"evil.test", "api.me"} {
		changed, err := ApplyExtracts(map[string]string{"token": "attacker"}, s, host)
		var be *ExtractBlockedError
		if !errors.As(err, &be) || be.Var != "token" || be.Reason != ExtractNotShared || len(changed) != 0 {
			t.Fatalf("%s: changed=%v err=%v, want ExtractBlockedError(not shared)", host, changed, err)
		}
		if sec, _ := vault.Get(gkey); sec.Reveal() != "my-real-token" {
			t.Fatalf("%s: global secret overwritten: %q", host, sec.Reveal())
		}
	}
	// shared-layer secret from a host it is not allowed for: refused, others go on
	skey := SecretKey("d1", "e1", "sess")
	changed, err := ApplyExtracts(map[string]string{"sess": "evil-session", "user_id": "u9"}, s, "evil.test")
	var be *ExtractBlockedError
	if !errors.As(err, &be) || be.Var != "sess" || be.Reason != ExtractHostNotAllowed || be.Host != "evil.test" {
		t.Fatalf("err = %v, want ExtractBlockedError(host)", err)
	}
	if len(changed) != 1 || changed[0] != "user_id" {
		t.Errorf("changed = %v", changed)
	}
	if _, err := vault.Get(skey); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("shared secret written from a disallowed host: %v", err)
	}
	// from an allowed host it is stored (spec acceptance 6)
	if ch, err := ApplyExtracts(map[string]string{"sess": "good"}, s, "API.test:443"); err != nil || len(ch) != 1 {
		t.Fatalf("allowed host: %v %v", ch, err)
	}
	if sec, err := vault.Get(skey); err != nil || sec.Reveal() != "good" {
		t.Fatalf("shared secret = %q %v", sec.Reveal(), err)
	}
	// ExtractSecretKey is what the UI uses before its own vault put
	if k, secret, err := s.ExtractSecretKey("sess", "api.test"); err != nil || !secret || k != skey {
		t.Errorf("ExtractSecretKey(sess) = %q %v %v", k, secret, err)
	}
	if _, secret, err := s.ExtractSecretKey("token", "api.me"); !secret || err == nil {
		t.Errorf("ExtractSecretKey(token) = %v %v, want blocked", secret, err)
	}
	if _, secret, err := s.ExtractSecretKey("user_id", "x"); secret || err != nil {
		t.Errorf("ExtractSecretKey(user_id) = %v %v", secret, err)
	}
	// a personal collection keeps writing its own secrets (no host rule)
	p := &Scope{Globals: []Var{{Name: "token", Secret: true, AllowedHosts: []string{"api.me"}}}, Vault: vault}
	if ch, err := ApplyExtracts(map[string]string{"token": "fresh"}, p, "auth.me"); err != nil || len(ch) != 1 {
		t.Fatalf("personal: %v %v", ch, err)
	}
}

func TestResponseHostIsFinalHost(t *testing.T) {
	srv, _ := countServer(t, nil)
	resp, err := send(t, newSender(t), &Request{Method: "GET", URL: srv.URL + "/x"}, nil)
	if err != nil || resp.Host != "127.0.0.1" {
		t.Fatalf("resp.Host = %q %v", resp.Host, err)
	}
}
