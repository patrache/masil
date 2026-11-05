// Package authui is the login / signup screen (RouteLogin, CA 5.8).
package authui

import (
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
)

// LoggedInMsg is emitted after a successful login/signup (credentials already saved);
// the app sets the API token, Store.Me and starts realtime.
type LoggedInMsg struct {
	Credentials *auth.Credentials
	Me          *api.Me
}

// TODO(core): Task 33 — RouteLogin component registered in init().
