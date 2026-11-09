package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

// langServer records "METHOD PATH => Accept-Language" for every request.
func langServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" => "+r.Header.Get("Accept-Language"))
		mu.Unlock()
		switch r.URL.Path {
		case "/v1/auth/logout":
			w.WriteHeader(http.StatusNoContent)
		case "/v1/auth/login":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(authResult("msl_langtoken"))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// Every CLI REST request carries Accept-Language (api.md 0.1): the effective UI
// language for the user's commands, ko for the Korean-only admin CLI.
func TestCLIRequestsCarryAcceptLanguage(t *testing.T) {
	i18n.SetOpenForTest(t, i18n.Ko, i18n.En)
	t.Setenv("MASIL_LANG", "en")
	srv, seen := langServer(t)
	e := newEnv(t, srv.URL)

	// masil logout
	saveCreds(t, e.Paths, srv.URL)
	if code := e.run("logout"); code != 0 {
		t.Fatalf("logout => %d, stderr %q", code, e.err)
	}

	// masil login --password-stdin whose credentials cannot be saved: the
	// cleanup logout too.
	if err := os.MkdirAll(filepath.Join(e.Paths.Credentials, "block"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.Stdin = strings.NewReader("pw\n")
	if code := e.run("login", "--handle", "sam", "--password-stdin"); code == 0 {
		t.Fatal("login with an unwritable credentials path must fail")
	}
	if err := os.RemoveAll(e.Paths.Credentials); err != nil {
		t.Fatal(err)
	}

	// masil admin
	saveCreds(t, e.Paths, srv.URL)
	c, err := adminClient(e.Env)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.GetMe(context.Background())

	want := []string{
		"POST /v1/auth/logout => en",
		"POST /v1/auth/login => en",
		"POST /v1/auth/logout => en",
		"GET /v1/me => ko",
	}
	got := seen()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
