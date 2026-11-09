package secrets

import (
	"context"
	"fmt"
)

// The Secret Service flow (org.freedesktop.secrets: GNOME Keyring, KWallet)
// over a small dbusConn so it is tested on every OS; backend_linux.go is the
// godbus adapter. Items carry the attributes {service, account}; new items go
// into the default collection. A locked item or collection is unlocked through
// the OS prompt; a dismissed prompt (or no bus, no service, no default
// collection) is ErrUnavailable, so the store asks for consent.

// dbusConn is one session-bus connection to the Secret Service. Object paths
// are strings; "/" is the D-Bus "no object" (no prompt, no alias).
type dbusConn interface {
	OpenSession(ctx context.Context, algorithm string) (session string, err error)
	SearchItems(ctx context.Context, attrs map[string]string) (unlocked, locked []string, err error)
	Unlock(ctx context.Context, objects []string) (unlocked []string, prompt string, err error)
	// Prompt shows the OS prompt and waits for its Completed signal.
	Prompt(ctx context.Context, prompt string) (dismissed bool, err error)
	DefaultCollection(ctx context.Context) (string, error)
	Locked(ctx context.Context, collection string) (bool, error)
	CreateItem(ctx context.Context, collection, label string, attrs map[string]string, session string, secret []byte, replace bool) (item, prompt string, err error)
	SetSecret(ctx context.Context, item, session string, secret []byte) error
	GetSecret(ctx context.Context, item, session string) ([]byte, error)
	DeleteItem(ctx context.Context, item string) (prompt string, err error)
	Close() error
}

// busEnv is the session bus address variable; without it the store never
// autolaunches a bus (dbus-launch).
const busEnv = "DBUS_SESSION_BUS_ADDRESS"

// secretService is the Secret Service OSBackend.
type secretService struct {
	env  func(string) string
	dial func(ctx context.Context, address string) (dbusConn, error)
}

func (secretService) Name() Backend { return BackendSecretService }

// unavailable wraps a Secret Service failure as ErrUnavailable.
func unavailable(what string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: secret service: %s", ErrUnavailable, what)
	}
	return fmt.Errorf("%w: secret service: %s: %v", ErrUnavailable, what, err)
}

// open connects and opens a plain session (the bus is local to this user).
func (s secretService) open(ctx context.Context) (dbusConn, string, error) {
	addr := s.env(busEnv)
	if addr == "" {
		return nil, "", unavailable("no session bus", nil)
	}
	c, err := s.dial(ctx, addr)
	if err != nil {
		return nil, "", unavailable("connect", err)
	}
	session, err := c.OpenSession(ctx, "plain")
	if err != nil {
		_ = c.Close()
		return nil, "", unavailable("open session", err)
	}
	return c, session, nil
}

func attrsFor(service, account string) map[string]string {
	return map[string]string{"service": service, "account": account}
}

// unlock unlocks objects, waiting for the OS prompt when there is one.
func unlock(ctx context.Context, c dbusConn, objects []string) error {
	_, prompt, err := c.Unlock(ctx, objects)
	if err != nil {
		return unavailable("unlock", err)
	}
	return waitPrompt(ctx, c, prompt)
}

func waitPrompt(ctx context.Context, c dbusConn, prompt string) error {
	if prompt == "" || prompt == "/" {
		return nil
	}
	dismissed, err := c.Prompt(ctx, prompt)
	if err != nil {
		return unavailable("prompt", err)
	}
	if dismissed {
		return unavailable("the prompt was dismissed", nil)
	}
	return nil
}

// find returns the first item with the attributes, unlocked ("" = none).
func find(ctx context.Context, c dbusConn, attrs map[string]string) (string, error) {
	unlocked, locked, err := c.SearchItems(ctx, attrs)
	if err != nil {
		return "", unavailable("search", err)
	}
	if len(unlocked) > 0 {
		return unlocked[0], nil
	}
	if len(locked) == 0 {
		return "", nil
	}
	if err := unlock(ctx, c, locked[:1]); err != nil {
		return "", err
	}
	return locked[0], nil
}

// Set, Get and Delete: a failure while ctx is done is ctx's error (ctxErr),
// not ErrUnavailable.
func (s secretService) Set(ctx context.Context, service, account, value string) error {
	return ctxErr(ctx, s.set(ctx, service, account, value))
}

func (s secretService) set(ctx context.Context, service, account, value string) error {
	c, session, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	attrs := attrsFor(service, account)
	secret := []byte(value)
	defer clear(secret)
	item, err := find(ctx, c, attrs)
	if err != nil {
		return err
	}
	if item != "" {
		if err := c.SetSecret(ctx, item, session, secret); err != nil {
			return unavailable("set secret", err)
		}
		return nil
	}
	coll, err := c.DefaultCollection(ctx)
	if err != nil {
		return unavailable("default collection", err)
	}
	if coll == "" || coll == "/" {
		return unavailable("no default collection", nil)
	}
	locked, err := c.Locked(ctx, coll)
	if err != nil {
		return unavailable("collection state", err)
	}
	if locked {
		if err := unlock(ctx, c, []string{coll}); err != nil {
			return err
		}
	}
	_, prompt, err := c.CreateItem(ctx, coll, service+": "+account, attrs, session, secret, true)
	if err != nil {
		return unavailable("create item", err)
	}
	return waitPrompt(ctx, c, prompt)
}

func (s secretService) Get(ctx context.Context, service, account string) (string, error) {
	v, err := s.get(ctx, service, account)
	return v, ctxErr(ctx, err)
}

func (s secretService) get(ctx context.Context, service, account string) (string, error) {
	c, session, err := s.open(ctx)
	if err != nil {
		return "", err
	}
	defer c.Close()
	item, err := find(ctx, c, attrsFor(service, account))
	if err != nil {
		return "", err
	}
	if item == "" {
		return "", ErrNotFound
	}
	v, err := c.GetSecret(ctx, item, session)
	if err != nil {
		return "", unavailable("get secret", err)
	}
	defer clear(v)
	return string(v), nil
}

func (s secretService) Delete(ctx context.Context, service, account string) error {
	return ctxErr(ctx, s.del(ctx, service, account))
}

func (s secretService) del(ctx context.Context, service, account string) error {
	c, _, err := s.open(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	attrs := attrsFor(service, account)
	deleted := false
	for range credMaxChunks { // every matching item, bounded
		item, err := find(ctx, c, attrs)
		if err != nil {
			return err
		}
		if item == "" {
			break
		}
		prompt, err := c.DeleteItem(ctx, item)
		if err != nil {
			return unavailable("delete", err)
		}
		if err := waitPrompt(ctx, c, prompt); err != nil {
			return err
		}
		deleted = true
	}
	if !deleted {
		return ErrNotFound
	}
	return nil
}
