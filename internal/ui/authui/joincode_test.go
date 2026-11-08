package authui_test

import (
	"net/http"
	"testing"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// C5 (v0.1.1, cross-track note 14): `masil join <code>` while logged out shows
// the login screen; after logging in (or signing up instead) the invite code is
// still there and its invite screen opens.
func TestJoinCodeSurvivesLogin(t *testing.T) {
	const code = "AbC12345"
	invite := ui.Route{Kind: ui.RouteInvite, ID: code}
	activeTop := func(t *testing.T, h *uitest.Harness) ui.Route {
		t.Helper()
		nav := h.Nav()
		if nav.Active < 0 || nav.Active >= len(nav.Tabs) {
			t.Fatalf("no active tab: %+v", nav)
		}
		st := nav.Tabs[nav.Active].Stack
		return st[len(st)-1]
	}

	t.Run("login", func(t *testing.T) {
		h := loggedOut(t, uitest.WithRoute(invite))
		h.API.JSON("POST /v1/auth/login", http.StatusOK, authResult("msl_new"))
		h.Type("sam")
		h.Press("tab")
		h.Type("correct horse")
		h.Press("enter")
		if r := activeTop(t, h); r.Key() != invite.Key() {
			t.Fatalf("after login the top route is %+v, want the invite %q", r, code)
		}
	})

	t.Run("signup", func(t *testing.T) {
		h := loggedOut(t, uitest.WithRoute(invite))
		h.Press("shift+tab", "enter") // to the signup form
		h.API.JSON("POST /v1/auth/signup", http.StatusCreated, authResult("msl_signed"))
		h.Type("sam")
		h.Press("tab")
		h.Type("샘")
		h.Press("tab")
		h.Type("correct horse")
		h.Press("tab")
		h.Type("correct horse")
		h.Press("enter")
		if r := activeTop(t, h); r.Key() != invite.Key() {
			t.Fatalf("after signup the top route is %+v, want the invite %q", r, code)
		}
	})
}
