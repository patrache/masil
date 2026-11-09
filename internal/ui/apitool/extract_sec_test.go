package apitool

import (
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/collab/collabtest"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui/uitest"
)

// An extract rule in a shared request naming my own (global) secret does
// not overwrite it; I am told instead (v0.1.3 security d).
func TestSharedExtractKeepsMyGlobalSecret(t *testing.T) {
	srv, hits := hitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"t":"attacker-token-0077"}`))
	})
	c := sharedCollection(srv.URL)
	c.Requests["r-get"].Extracts = []apitool.Extract{{ID: "x", Var: "gtoken", From: "json", Path: "$.t"}}
	hub := collabtest.New(t)
	seedShared(hub, c)
	h := openAPI(t, uitest.WithCollab(hub), uitest.WithSecrets(secrettest.NewStore(t, true)))
	tl := openShared(t, h, "r-get")
	tl.globals = []apitool.Var{{Name: "gtoken", Secret: true, AllowedHosts: []string{"127.0.0.1"}}}
	gkey := apitool.SecretKey("", "", "gtoken")
	if err := tl.vault(h.Context()).Put(gkey, secrets.New("my-real-token")); err != nil {
		t.Fatal(err)
	}
	tl.focus = paneRequest
	h.Press("enter")
	waitFor(t, h, "the private-target question", func() bool { return hasWhole(overlayText(h), ko.PrivateTargetUnknown()) })
	h.Press("enter") // 그래도 보내기
	seen := false
	waitFor(t, h, "the response and its toast", func() bool {
		// (the toast wraps: its {{gtoken}} is the only one on the screen)
		seen = seen || strings.Contains(h.Screen(), "{{gtoken}}")
		return hits.Load() == 1 && seen
	})
	settle(h)
	if len(tl.pendingPuts) != 0 {
		t.Fatalf("pending vault puts: %d", len(tl.pendingPuts))
	}
	if v, err := tl.vault(h.Context()).Get(gkey); err != nil || v.Reveal() != "my-real-token" {
		t.Fatalf("my global secret = %q %v", v.Reveal(), err)
	}
}
