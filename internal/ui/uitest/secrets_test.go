package uitest_test

import (
	"context"
	"testing"

	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui/uitest"
)

func TestWithSecrets(t *testing.T) {
	s := secrettest.NewStore(t, false)
	h := uitest.New(t, uitest.WithSecrets(s))
	if h.Context().Secrets != s {
		t.Fatal("WithSecrets store not on ctx.Secrets")
	}
	k := secrets.Key{Tool: "leet", Server: "h", UserID: "U1", Name: "c"}
	if _, err := h.Context().Secrets.Put(context.Background(), k, secrets.New("v")); err != secrets.ErrNeedConsent {
		t.Fatalf("Put = %v, want ErrNeedConsent", err)
	}
}
