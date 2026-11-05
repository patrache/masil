package uitest

import (
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Option configures a Harness.
type Option func(*config)

// config is the harness configuration built from Options.
type config struct { /* TODO(core): Task 25 */
}

// WithTheme selects the theme.
func WithTheme(n theme.Name) Option {
	// TODO(core): Task 25
	return nil
}

// WithSeed preloads the Store (fixtures).
func WithSeed(fn func(s *store.Store)) Option {
	// TODO(core): Task 25
	return nil
}

// WithAPI serves canned REST.
func WithAPI(srv *apitest.Server) Option {
	// TODO(core): Task 25
	return nil
}

// WithRoute opens r first.
func WithRoute(r ui.Route) Option {
	// TODO(core): Task 25
	return nil
}

// WithPin pins the layout mode.
func WithPin(m layout.Mode) Option {
	// TODO(core): Task 25
	return nil
}

// WithUnicodeCore simulates tea.ModeReportMsg 2027.
func WithUnicodeCore(on bool) Option {
	// TODO(core): Task 25
	return nil
}

// WithCredentials sets the credentials (default: logged-in fixture user "sam").
func WithCredentials(c *auth.Credentials) Option {
	// TODO(core): Task 25
	return nil
}
