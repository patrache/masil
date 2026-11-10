package ui

import (
	"net/url"

	"github.com/patrache/masil/internal/secrets"
)

// SecretScope is the masil account tool secrets are kept under (v0.1.3 A9.1
// rule 3): the masil server host (from the API client's base URL, else the
// configured server) and the logged-in user id. ok is false when logged out.
func (c *Context) SecretScope() (server, userID string, ok bool) {
	me := c.Me()
	if me == nil || me.ID == "" {
		return "", "", false
	}
	base := ""
	if c.API != nil {
		base = c.API.BaseURL()
	}
	if base == "" && c.Config != nil {
		base = c.Config.ServerURL()
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", "", false
	}
	return u.Host, me.ID, true
}

// SecretKey is tool's value name for the logged-in masil account
// (ctx.Secrets.Put/Get/Delete). ok is false when logged out.
func (c *Context) SecretKey(tool, name string) (secrets.Key, bool) {
	server, user, ok := c.SecretScope()
	if !ok {
		return secrets.Key{}, false
	}
	return secrets.Key{Tool: tool, Server: server, UserID: user, Name: name}, true
}
