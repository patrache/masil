package secrets

import (
	"errors"
	"strings"
	"unicode"
)

// Service is the OS-store service name of every masil tool secret.
const Service = "masil"

// Key names one secret: a tool's value for one masil account on one masil
// server (A9.1 rule 3). Tool, Server and UserID never contain "/"; Name may
// (an API environment and variable name).
type Key struct {
	Tool   string // tool id (ui.ToolLeet …)
	Server string // masil server host
	UserID string // masil user id
	Name   string // value name ("cookie.session", "env/prod/TOKEN" …)
}

// Account is the OS-store account name "<tool>/<host>/<user_id>/<name>"
// (service Service).
func (k Key) Account() string {
	return k.Tool + "/" + k.Server + "/" + k.UserID + "/" + k.Name
}

// errInvalidKey is returned for a key that cannot be stored.
var errInvalidKey = errors.New("secrets: invalid key")

// validate rejects empty parts, "/" in Tool/Server/UserID and control
// characters anywhere.
func (k Key) validate() error {
	parts := []string{k.Tool, k.Server, k.UserID, k.Name}
	for i, p := range parts {
		if p == "" || strings.ContainsFunc(p, unicode.IsControl) || (i < 3 && strings.Contains(p, "/")) {
			return errInvalidKey
		}
	}
	return nil
}

// parseAccount is Account in reverse.
func parseAccount(a string) (Key, bool) {
	p := strings.SplitN(a, "/", 4)
	if len(p) != 4 {
		return Key{}, false
	}
	k := Key{p[0], p[1], p[2], p[3]}
	return k, k.validate() == nil
}
