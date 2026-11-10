package leetcode

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/patrache/masil/internal/secrets"
)

// The LeetCode cookies masil keeps (A9.1, A10.2). The names are the value
// names in the tool secret store and the cookie names sent to LeetCode.
const (
	NameSession     = "LEETCODE_SESSION"
	NameCSRF        = "csrftoken"
	NameCFClearance = "cf_clearance"
)

// The environment variables that give this run its cookies (leetgo's names).
// They are used for this run only and never saved.
const (
	EnvSession     = "LEETCODE_SESSION"
	EnvCSRF        = "LEETCODE_CSRFTOKEN"
	EnvCFClearance = "LEETCODE_CFCLEARANCE"
)

// ToolID is the tool whose secrets these are (ui.ToolLeet).
const ToolID = "leet"

// Credentials are the user's LeetCode cookies. Each value prints, marshals
// and logs as "(redacted)". FromEnv: they came from the environment and are
// never saved.
type Credentials struct {
	Session, CSRF, CFClearance secrets.Secret
	FromEnv                    bool
}

// usable reports whether both required cookies are there.
func (c *Credentials) usable() bool {
	return c != nil && !c.Session.IsZero() && !c.CSRF.IsZero()
}

// cookieHeader is the Cookie header LeetCode wants. Only the client writes
// it, and only to the LeetCode host.
func (c *Credentials) cookieHeader() string {
	s := NameSession + "=" + c.Session.Reveal() + "; " + NameCSRF + "=" + c.CSRF.Reveal()
	if !c.CFClearance.IsZero() {
		s += "; " + NameCFClearance + "=" + c.CFClearance.Reveal()
	}
	return s
}

// CredNS is whose cookies these are: one masil account on one masil server
// (A9.1 rule 3). The UI fills it from ctx.SecretScope().
type CredNS struct{ ServerHost, UserID string }

// Key is the secret-store key of one cookie of this account.
func (ns CredNS) Key(name string) secrets.Key {
	return secrets.Key{Tool: ToolID, Server: ns.ServerHost, UserID: ns.UserID, Name: name}
}

// CredentialsFromEnv reads LEETCODE_SESSION and LEETCODE_CSRFTOKEN (both
// needed) and LEETCODE_CFCLEARANCE (optional), normalized like a paste.
func CredentialsFromEnv(getenv func(string) string) (*Credentials, bool) {
	if getenv == nil {
		return nil, false
	}
	c := &Credentials{
		Session:     secrets.New(NormalizeCookie(NameSession, getenv(EnvSession))),
		CSRF:        secrets.New(NormalizeCookie(NameCSRF, getenv(EnvCSRF))),
		CFClearance: secrets.New(NormalizeCookie(NameCFClearance, getenv(EnvCFClearance))),
		FromEnv:     true,
	}
	if !c.usable() {
		return nil, false
	}
	return c, true
}

// LoadCredentials is the environment's cookies when set, else this account's
// stored ones. Neither => secrets.ErrNotFound; a store that cannot be read
// returns its error (secrets.ErrUnavailable …).
func LoadCredentials(ctx context.Context, st *secrets.Store, ns CredNS, getenv func(string) string) (*Credentials, error) {
	if c, ok := CredentialsFromEnv(getenv); ok {
		return c, nil
	}
	if st == nil {
		return nil, secrets.ErrNotFound
	}
	var c Credentials
	var err error
	if c.Session, _, err = st.Get(ctx, ns.Key(NameSession)); err != nil {
		return nil, err
	}
	if c.CSRF, _, err = st.Get(ctx, ns.Key(NameCSRF)); err != nil {
		return nil, err
	}
	if c.CFClearance, _, err = st.Get(ctx, ns.Key(NameCFClearance)); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return nil, err
	}
	if !c.usable() {
		return nil, secrets.ErrNotFound
	}
	return &c, nil
}

// errNoCookies: SaveCredentials was given no session or CSRF cookie.
var errNoCookies = errors.New("leetcode: session and csrftoken cookies are both needed")

// SaveCredentials stores c for this account; an empty cf_clearance removes
// the stored one. Cookies from the environment are not saved (nil).
// secrets.ErrNeedConsent comes back as is: the UI asks, then saves again.
func SaveCredentials(ctx context.Context, st *secrets.Store, ns CredNS, c Credentials) error {
	if c.FromEnv {
		return nil
	}
	if !c.usable() {
		return errNoCookies
	}
	if _, err := st.Put(ctx, ns.Key(NameSession), c.Session); err != nil {
		return err
	}
	if _, err := st.Put(ctx, ns.Key(NameCSRF), c.CSRF); err != nil {
		return err
	}
	if c.CFClearance.IsZero() {
		return st.Delete(ctx, ns.Key(NameCFClearance))
	}
	_, err := st.Put(ctx, ns.Key(NameCFClearance), c.CFClearance)
	return err
}

// DeleteCredentials removes this account's cookies everywhere (a missing one
// is not an error).
func DeleteCredentials(ctx context.Context, st *secrets.Store, ns CredNS) error {
	var errs []error
	for _, name := range []string{NameSession, NameCSRF, NameCFClearance} {
		if err := st.Delete(ctx, ns.Key(name)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// NormalizeCookie keeps only the value of a pasted cookie: whitespace and
// line breaks dropped (cookie values have none), a leading "<name>=" removed
// (any case), everything from the first ";" cut, surrounding quotes removed.
func NormalizeCookie(name, pasted string) string {
	s := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, pasted)
	if p := name + "="; len(s) >= len(p) && strings.EqualFold(s[:len(p)], p) {
		s = s[len(p):]
	}
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	for len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		s = s[1 : len(s)-1]
	}
	return s
}

// Mask is how a stored cookie is shown: "••••" and its last 4 characters
// (4 or fewer => "••••").
func Mask(s secrets.Secret) string { return s.Masked() }
