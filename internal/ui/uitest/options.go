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
type config struct {
	theme    theme.Name
	seed     func(s *store.Store) // nil => fixtures.Seed
	api      *apitest.Server      // nil => a fresh server with fixtures.RegisterAPI
	route    *ui.Route
	pin      *layout.Mode
	unicode  *bool
	creds    *auth.Credentials
	credsSet bool // WithCredentials was given (nil => logged out)
	keys     map[string][]string
}

// WithKeys sets config.toml [keys] overrides (action id => keys).
func WithKeys(keys map[string][]string) Option {
	return func(c *config) { c.keys = keys }
}

// WithTheme selects the theme.
func WithTheme(n theme.Name) Option {
	return func(c *config) { c.theme = n }
}

// WithSeed preloads the Store (fixtures); it replaces the default fixtures.Seed.
func WithSeed(fn func(s *store.Store)) Option {
	return func(c *config) { c.seed = fn }
}

// WithAPI serves canned REST from srv instead of the default fixtures server.
func WithAPI(srv *apitest.Server) Option {
	return func(c *config) { c.api = srv }
}

// WithRoute opens r first (app.Options.InitialRoute).
func WithRoute(r ui.Route) Option {
	return func(c *config) { c.route = &r }
}

// WithPin pins the layout mode ([layout] pin).
func WithPin(m layout.Mode) Option {
	return func(c *config) { c.pin = &m }
}

// WithUnicodeCore simulates tea.ModeReportMsg 2027 (set or reset).
func WithUnicodeCore(on bool) Option {
	return func(c *config) { c.unicode = &on }
}

// WithCredentials sets the credentials (default: logged-in fixture user "sam");
// nil means logged out.
func WithCredentials(cr *auth.Credentials) Option {
	return func(c *config) { c.creds, c.credsSet = cr, true }
}
