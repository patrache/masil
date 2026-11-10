package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

const wipeLabel = "이 기기의 도구 로그인도 지우기"

// logoutWithStore opens a harness whose store holds two values of sam on this
// server and one of another account, then opens the logout dialog.
func logoutWithStore(t *testing.T) (*uitest.Harness, *secrets.Store, []secrets.Key, secrets.Key) {
	t.Helper()
	st := secrettest.NewStore(t, true)
	h := uitest.New(t, uitest.WithSecrets(st))
	h.API.Handle("POST /v1/auth/logout", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	u, err := url.Parse(h.API.URL)
	if err != nil {
		t.Fatal(err)
	}
	mine := []secrets.Key{
		{Tool: ui.ToolLeet, Server: u.Host, UserID: fixtures.UserSam, Name: "cookie.session"},
		{Tool: ui.ToolAPI, Server: u.Host, UserID: fixtures.UserSam, Name: "env/prod/TOKEN"},
	}
	other := secrets.Key{Tool: ui.ToolLeet, Server: u.Host, UserID: fixtures.UserHana, Name: "cookie.session"}
	for _, k := range append(mine, other) {
		if _, err := st.Put(context.Background(), k, secrets.New("v")); err != nil {
			t.Fatal(err)
		}
	}
	h.Send(ui.RunCommandMsg{ID: "logout"})
	return h, st, mine, other
}

func loggedOut(h *uitest.Harness) bool {
	return h.Model.Frame().Routes[layout.SlotMain].Kind == ui.RouteLogin
}

func TestLogoutWipeOption(t *testing.T) {
	t.Run("checked", func(t *testing.T) {
		t.Setenv("MASIL_FEATURES", "all")
		h, st, mine, other := logoutWithStore(t)
		if !strings.Contains(h.Screen(), wipeLabel) || !strings.Contains(h.Screen(), "[ ] ") {
			t.Fatalf("the wipe option (off) is missing:\n%s", h.Screen())
		}
		h.Press("space")
		if !strings.Contains(h.Screen(), "[x] ") {
			t.Fatalf("space must check the option:\n%s", h.Screen())
		}
		h.Press("enter")
		if !loggedOut(h) {
			t.Fatalf("not logged out:\n%s", h.Screen())
		}
		vals := secrettest.Values(st)
		for _, k := range mine {
			if _, ok := vals[k.Account()]; ok {
				t.Errorf("%s kept", k.Account())
			}
		}
		if _, ok := vals[other.Account()]; !ok {
			t.Error("another account's value was wiped")
		}
	})
	t.Run("checked, the server never answers", func(t *testing.T) {
		// The logout request times out; the wipe must not run on its spent context.
		t.Setenv("MASIL_FEATURES", "all")
		app.SetLogoutTimeoutForTest(t, 50*time.Millisecond)
		h, st, mine, other := logoutWithStore(t)
		h.API.Handle("POST /v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		})
		h.Press("space", "enter")
		if !loggedOut(h) {
			t.Fatalf("not logged out:\n%s", h.Screen())
		}
		vals := secrettest.Values(st)
		for _, k := range mine {
			if _, ok := vals[k.Account()]; ok {
				t.Errorf("%s kept after a stalled logout request", k.Account())
			}
		}
		if _, ok := vals[other.Account()]; !ok {
			t.Error("another account's value was wiped")
		}
	})
	t.Run("checked, the store cannot delete", func(t *testing.T) {
		// A wipe that fails is told, not only logged: the user believes the logins are gone.
		t.Setenv("MASIL_FEATURES", "all")
		h, st, _, _ := logoutWithStore(t)
		secrettest.FailDeletes(st)
		h.Press("space", "enter")
		if !loggedOut(h) {
			t.Fatalf("not logged out:\n%s", h.Screen())
		}
		if !strings.Contains(h.Screen(), "이 기기의 도구 로그인을 지우지 못했어요") {
			t.Fatalf("no wipe-failure toast:\n%s", h.Screen())
		}
	})
	t.Run("default off", func(t *testing.T) {
		t.Setenv("MASIL_FEATURES", "all")
		h, st, mine, _ := logoutWithStore(t)
		h.Press("y")
		if !loggedOut(h) {
			t.Fatal("not logged out")
		}
		if len(secrettest.Values(st)) != len(mine)+1 {
			t.Fatalf("values wiped without the option: %v", secrettest.Values(st))
		}
	})
	t.Run("flags off: the 0.1.2 dialog", func(t *testing.T) {
		t.Setenv("MASIL_FEATURES", "none") // the release turns every tool on by default
		h, st, mine, _ := logoutWithStore(t)
		if strings.Contains(h.Screen(), wipeLabel) {
			t.Fatalf("the wipe option shows with every tool flag off:\n%s", h.Screen())
		}
		h.Press("y")
		if !loggedOut(h) || len(secrettest.Values(st)) != len(mine)+1 {
			t.Fatal("plain logout")
		}
	})
	t.Run("cancel", func(t *testing.T) {
		t.Setenv("MASIL_FEATURES", "all")
		h, st, _, _ := logoutWithStore(t)
		h.Press("space", "n")
		if loggedOut(h) || len(secrettest.Values(st)) != 3 {
			t.Fatal("cancel logged out or wiped")
		}
	})
}
