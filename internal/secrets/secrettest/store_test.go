package secrettest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
)

func TestSecrettestNewStore(t *testing.T) {
	ctx := context.Background()
	k := secrets.Key{Tool: "leet", Server: "masil.example", UserID: "U1", Name: "cookie"}

	s := secrettest.NewStore(t, true)
	b, err := s.Put(ctx, k, secrets.New("v1"))
	if err != nil || b != secrets.BackendMemory {
		t.Fatalf("Put = %v, %v", b, err)
	}
	vals := secrettest.Values(s)
	if len(vals) != 1 || vals[k.Account()] != "v1" {
		t.Fatalf("Values = %v", vals)
	}
	if s.Where(k) != secrets.BackendMemory {
		t.Errorf("Where = %q", s.Where(k))
	}

	off := secrettest.NewStore(t, false)
	if _, err := off.Put(ctx, k, secrets.New("v2")); !errors.Is(err, secrets.ErrNeedConsent) {
		t.Fatalf("Put = %v, want ErrNeedConsent", err)
	}
	off.SetFallback(secrets.FallbackSession)
	if _, err := off.Put(ctx, k, secrets.New("v2")); err != nil {
		t.Fatal(err)
	}
	if vals := secrettest.Values(off); vals[k.Account()] != "v2" {
		t.Fatalf("Values = %v", vals)
	}
	off.SetFallback(secrets.FallbackFile)
	k2 := k
	k2.Name = "other"
	if b, err := off.Put(ctx, k2, secrets.New("v3")); err != nil || b != secrets.BackendFile {
		t.Fatalf("file Put = %v, %v", b, err)
	}
	if vals := secrettest.Values(off); len(vals) != 2 || vals[k2.Account()] != "v3" {
		t.Fatalf("Values = %v", vals)
	}
	// The stores are separate.
	if vals := secrettest.Values(s); len(vals) != 1 {
		t.Errorf("first store Values = %v", vals)
	}
}
