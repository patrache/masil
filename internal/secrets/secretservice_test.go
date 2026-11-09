package secrets

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// fakeBus is a dbusConn over an in-memory Secret Service with one default
// collection.
type fakeBus struct {
	calls      []string
	items      map[string]*fakeItem // path => item
	collLocked bool
	dismiss    bool   // every prompt is dismissed
	noDefault  bool   // ReadAlias("default") = "/"
	nextPrompt string // prompt path the next Unlock / CreateItem returns
	n          int
	closed     bool
}

type fakeItem struct {
	attrs  map[string]string
	secret []byte
	locked bool
}

const fakeColl = "/org/freedesktop/secrets/collection/login"

func newFakeBus() *fakeBus { return &fakeBus{items: map[string]*fakeItem{}} }

func (b *fakeBus) log(f string, a ...any) { b.calls = append(b.calls, fmt.Sprintf(f, a...)) }

func (b *fakeBus) OpenSession(context.Context, string) (string, error) {
	b.log("OpenSession(plain)")
	return "/org/freedesktop/secrets/session/1", nil
}

func (b *fakeBus) SearchItems(_ context.Context, attrs map[string]string) (unlocked, locked []string, err error) {
	keys := slices.Sorted(maps.Keys(attrs))
	var kv []string
	for _, k := range keys {
		kv = append(kv, k+"="+attrs[k])
	}
	b.log("SearchItems{%s}", strings.Join(kv, ","))
	for _, p := range slices.Sorted(maps.Keys(b.items)) {
		it := b.items[p]
		if maps.Equal(it.attrs, attrs) {
			if it.locked || b.collLocked {
				locked = append(locked, p)
			} else {
				unlocked = append(unlocked, p)
			}
		}
	}
	return unlocked, locked, nil
}

func (b *fakeBus) Unlock(_ context.Context, objects []string) ([]string, string, error) {
	b.log("Unlock(%s)", strings.Join(objects, ","))
	b.nextPrompt = "/org/freedesktop/secrets/prompt/u1"
	return nil, b.nextPrompt, nil
}

func (b *fakeBus) Prompt(_ context.Context, prompt string) (bool, error) {
	b.log("Prompt(%s) dismissed=%v", prompt, b.dismiss)
	if b.dismiss {
		return true, nil
	}
	b.collLocked = false
	for _, it := range b.items {
		it.locked = false
	}
	return false, nil
}

func (b *fakeBus) DefaultCollection(context.Context) (string, error) {
	b.log("ReadAlias(default)")
	if b.noDefault {
		return "/", nil
	}
	return fakeColl, nil
}

func (b *fakeBus) Locked(_ context.Context, path string) (bool, error) {
	b.log("Locked(%s)", path)
	return b.collLocked, nil
}

func (b *fakeBus) CreateItem(_ context.Context, coll, label string, attrs map[string]string, session string, secret []byte, replace bool) (string, string, error) {
	b.log("CreateItem(%s, %q, replace=%v)", coll, label, replace)
	if b.collLocked {
		return "", "", errors.New("locked")
	}
	for p, it := range b.items {
		if replace && maps.Equal(it.attrs, attrs) {
			it.secret = slices.Clone(secret)
			return p, "/", nil
		}
	}
	b.n++
	p := fmt.Sprintf("%s/%d", coll, b.n)
	b.items[p] = &fakeItem{attrs: maps.Clone(attrs), secret: slices.Clone(secret)}
	return p, "/", nil
}

func (b *fakeBus) SetSecret(_ context.Context, item, session string, secret []byte) error {
	b.log("SetSecret(%s)", item)
	if b.items[item].locked || b.collLocked {
		return errors.New("locked")
	}
	b.items[item].secret = slices.Clone(secret)
	return nil
}

func (b *fakeBus) GetSecret(_ context.Context, item, session string) ([]byte, error) {
	b.log("GetSecret(%s)", item)
	if b.items[item].locked || b.collLocked {
		return nil, errors.New("locked")
	}
	return slices.Clone(b.items[item].secret), nil
}

func (b *fakeBus) DeleteItem(_ context.Context, item string) (string, error) {
	b.log("Delete(%s)", item)
	delete(b.items, item)
	return "/", nil
}

func (b *fakeBus) Close() error { b.closed = true; return nil }

func ssWith(b *fakeBus) secretService {
	return secretService{
		env:  func(string) string { return "unix:path=/run/user/1000/bus" },
		dial: func(context.Context, string) (dbusConn, error) { return b, nil },
	}
}

func TestSecretServiceFlow(t *testing.T) {
	b := newFakeBus()
	ss := ssWith(b)
	if ss.Name() != BackendSecretService {
		t.Fatalf("Name = %q", ss.Name())
	}
	acct := "leet/masil.example/U1/cookie"
	if err := ss.Set(bg, Service, acct, "v1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"OpenSession(plain)",
		"SearchItems{account=" + acct + ",service=masil}",
		"ReadAlias(default)",
		"Locked(" + fakeColl + ")",
		`CreateItem(` + fakeColl + `, "masil: ` + acct + `", replace=true)`,
	}
	if !slices.Equal(b.calls, want) {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(b.calls, "\n"), strings.Join(want, "\n"))
	}
	if !b.closed {
		t.Error("connection left open")
	}

	// Set again: the found item's secret is replaced.
	b.calls = nil
	if err := ss.Set(bg, Service, acct, "v2"); err != nil {
		t.Fatal(err)
	}
	if len(b.items) != 1 || !slices.Contains(b.calls, "SetSecret("+fakeColl+"/1)") {
		t.Fatalf("calls = %v, items %d", b.calls, len(b.items))
	}
	v, err := ss.Get(bg, Service, acct)
	if err != nil || v != "v2" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	if _, err := ss.Get(bg, Service, "leet/masil.example/U2/cookie"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get other = %v, want ErrNotFound", err)
	}

	// A locked collection: Unlock, wait for the prompt, then read.
	b.collLocked = true
	b.calls = nil
	v, err = ss.Get(bg, Service, acct)
	if err != nil || v != "v2" {
		t.Fatalf("locked Get = %q, %v", v, err)
	}
	if !slices.Contains(b.calls, "Unlock("+fakeColl+"/1)") || !slices.Contains(b.calls, "Prompt(/org/freedesktop/secrets/prompt/u1) dismissed=false") {
		t.Fatalf("calls = %v", b.calls)
	}

	if err := ss.Delete(bg, Service, acct); err != nil {
		t.Fatal(err)
	}
	if len(b.items) != 0 {
		t.Fatalf("items = %d", len(b.items))
	}
	if err := ss.Delete(bg, Service, acct); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete = %v, want ErrNotFound", err)
	}
}

func TestSecretServiceLockedNewItem(t *testing.T) {
	b := newFakeBus()
	b.collLocked = true
	if err := ssWith(b).Set(bg, Service, "a/b/c/d", "v"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(b.calls, "Unlock("+fakeColl+")") || len(b.items) != 1 {
		t.Fatalf("calls = %v", b.calls)
	}
}

func TestSecretServiceDismissed(t *testing.T) {
	b := newFakeBus()
	b.collLocked, b.dismiss = true, true
	if err := ssWith(b).Set(bg, Service, "a/b/c/d", "v"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Set = %v, want ErrUnavailable", err)
	}
	if len(b.items) != 0 {
		t.Fatal("an item was created after the unlock was dismissed")
	}
	// The store then asks for consent instead of retrying.
	s := Open(Options{OS: ssWith(b), Paths: testPaths(t)})
	if _, err := s.Put(bg, key("U1", "c"), New("v")); !errors.Is(err, ErrNeedConsent) {
		t.Fatalf("Put = %v, want ErrNeedConsent", err)
	}
}

func TestSecretServiceNoDefaultCollection(t *testing.T) {
	b := newFakeBus()
	b.noDefault = true
	if err := ssWith(b).Set(bg, Service, "a/b/c/d", "v"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Set = %v, want ErrUnavailable", err)
	}
}

func TestSecretServiceNoBus(t *testing.T) {
	dialed := false
	ss := secretService{
		env:  func(string) string { return "" },
		dial: func(context.Context, string) (dbusConn, error) { dialed = true; return newFakeBus(), nil },
	}
	if err := ss.Set(bg, Service, "a/b/c/d", "v"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Set = %v, want ErrUnavailable", err)
	}
	if _, err := ss.Get(bg, Service, "a/b/c/d"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Get = %v, want ErrUnavailable", err)
	}
	if dialed {
		t.Error("dialed without DBUS_SESSION_BUS_ADDRESS")
	}
	broken := secretService{
		env:  func(string) string { return "unix:path=/nope" },
		dial: func(context.Context, string) (dbusConn, error) { return nil, errors.New("connection refused") },
	}
	if err := broken.Set(bg, Service, "a/b/c/d", "v"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Set = %v, want ErrUnavailable", err)
	}
}
