package app_test

import (
	"testing"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/uitest"
)

var secretsKey = secrets.Key{Tool: "leet", Server: "masil.example", UserID: "U1", Name: "cookie"}

// TestContextSecretsNeverNil: the app opens the tool-secret store at start
// (v0.1.3 A9); a tool never sees a nil ctx.Secrets.
func TestContextSecretsNeverNil(t *testing.T) {
	m := newModel(t, config.Defaults())
	if m.Context().Secrets == nil {
		t.Fatal("app.New left ctx.Secrets nil")
	}
	if m.Context().Secrets.Where(secretsKey) != "" {
		t.Error("a fresh store already knows a value")
	}
	// Without paths too (a bare app).
	if app.New(app.Options{DisableRealtime: true}).Context().Secrets == nil {
		t.Fatal("app.New without paths left ctx.Secrets nil")
	}
	// The harness passes a memory store.
	h := uitest.New(t)
	if h.Context().Secrets == nil {
		t.Fatal("harness ctx.Secrets nil")
	}
	if len(secrettest.Values(h.Context().Secrets)) != 0 {
		t.Error("harness store not empty")
	}
}
