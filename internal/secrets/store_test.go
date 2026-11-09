package secrets

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/config"
)

// fakeOS is an in-memory OSBackend; down makes every call ErrUnavailable.
type fakeOS struct {
	mu    sync.Mutex
	down  bool
	vals  map[string]string // service + "\x00" + account => value
	calls int
	fail  error // returned by Set when set (not ErrUnavailable)
}

func newFakeOS(available bool) *fakeOS {
	return &fakeOS{down: !available, vals: map[string]string{}}
}

func (f *fakeOS) Name() Backend { return BackendKeychain }

func (f *fakeOS) Set(_ context.Context, service, account, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.down {
		return ErrUnavailable
	}
	if f.fail != nil {
		return f.fail
	}
	f.vals[service+"\x00"+account] = value
	return nil
}

func (f *fakeOS) Get(_ context.Context, service, account string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.down {
		return "", ErrUnavailable
	}
	v, ok := f.vals[service+"\x00"+account]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *fakeOS) Delete(_ context.Context, service, account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.down {
		return ErrUnavailable
	}
	k := service + "\x00" + account
	if _, ok := f.vals[k]; !ok {
		return ErrNotFound
	}
	delete(f.vals, k)
	return nil
}

func testPaths(t *testing.T) config.Paths {
	d := t.TempDir()
	return config.Paths{Dir: d, SecretsDir: filepath.Join(d, "secrets")}
}

var bg = context.Background()

func key(user, name string) Key { return Key{"leet", "masil.example", user, name} }

func TestPutGetOS(t *testing.T) {
	fos := newFakeOS(true)
	p := testPaths(t)
	s := Open(Options{OS: fos, Paths: p})
	b, err := s.Put(bg, key("U1", "cookie"), New("v1"))
	if err != nil || b != BackendKeychain {
		t.Fatalf("Put = %v, %v", b, err)
	}
	if fos.vals["masil\x00leet/masil.example/U1/cookie"] != "v1" {
		t.Fatalf("os vals = %v", fos.vals)
	}
	v, b, err := s.Get(bg, key("U1", "cookie"))
	if err != nil || v.Reveal() != "v1" || b != BackendKeychain {
		t.Fatalf("Get = %q, %v, %v", v.Reveal(), b, err)
	}
	if w := s.Where(key("U1", "cookie")); w != BackendKeychain {
		t.Errorf("Where = %q", w)
	}
	// A new Open (restart) finds it through the OS store and the index.
	s2 := Open(Options{OS: fos, Paths: p})
	if w := s2.Where(key("U1", "cookie")); w != BackendKeychain {
		t.Errorf("Where after restart = %q", w)
	}
	v, _, err = s2.Get(bg, key("U1", "cookie"))
	if err != nil || v.Reveal() != "v1" {
		t.Fatalf("Get after restart = %v", err)
	}
	if names, err := s2.List(bg, "leet", "masil.example", "U1"); err != nil || !slices.Equal(names, []string{"cookie"}) {
		t.Errorf("List = %v, %v", names, err)
	}
}

func TestPutNeedsConsentWhenUnavailable(t *testing.T) {
	p := testPaths(t)
	for _, fos := range []OSBackend{newFakeOS(false), nil} {
		o := Options{OS: fos, Paths: p}
		if fos == nil {
			o.OS = noOS{} // the "no OS store" platform
		}
		s := Open(o)
		if s.Fallback() != FallbackUnset {
			t.Fatalf("Fallback = %v", s.Fallback())
		}
		if _, err := s.Put(bg, key("U1", "c"), New("v")); !errors.Is(err, ErrNeedConsent) {
			t.Fatalf("Put = %v, want ErrNeedConsent", err)
		}
		if _, _, err := s.Get(bg, key("U1", "c")); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get = %v, want ErrNotFound", err)
		}
	}
}

func TestOSUnavailableAsksOnce(t *testing.T) {
	fos := newFakeOS(false)
	s := Open(Options{OS: fos, Paths: testPaths(t)})
	_, _ = s.Put(bg, key("U1", "c"), New("v"))
	s.SetFallback(FallbackSession)
	before := fos.calls
	if _, err := s.Put(bg, key("U1", "c"), New("v")); err != nil {
		t.Fatal(err)
	}
	if fos.calls != before {
		t.Errorf("the OS store was retried after it said unavailable (%d calls)", fos.calls-before)
	}
}

func TestOSOtherErrorIsReturned(t *testing.T) {
	fos := newFakeOS(true)
	boom := errors.New("boom")
	fos.fail = boom
	s := Open(Options{OS: fos, Paths: testPaths(t)})
	s.SetFallback(FallbackFile)
	if _, err := s.Put(bg, key("U1", "c"), New("v")); !errors.Is(err, boom) {
		t.Fatalf("Put = %v, want boom", err)
	}
}

func TestSessionFallback(t *testing.T) {
	fos := newFakeOS(false)
	p := testPaths(t)
	s := Open(Options{OS: fos, Paths: p})
	s.SetFallback(FallbackSession)
	b, err := s.Put(bg, key("U1", "c"), New("v"))
	if err != nil || b != BackendMemory {
		t.Fatalf("Put = %v, %v", b, err)
	}
	v, b, err := s.Get(bg, key("U1", "c"))
	if err != nil || v.Reveal() != "v" || b != BackendMemory {
		t.Fatalf("Get = %v, %v", b, err)
	}
	if s.Where(key("U1", "c")) != BackendMemory {
		t.Errorf("Where = %q", s.Where(key("U1", "c")))
	}
	if names, _ := s.List(bg, "leet", "masil.example", "U1"); !slices.Equal(names, []string{"c"}) {
		t.Errorf("List = %v", names)
	}
	s2 := Open(Options{OS: fos, Paths: p})
	if _, _, err := s2.Get(bg, key("U1", "c")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after restart = %v, want ErrNotFound", err)
	}
	if names, _ := s2.List(bg, "leet", "masil.example", "U1"); len(names) != 0 {
		t.Errorf("List after restart = %v", names)
	}
	if s2.Where(key("U1", "c")) != "" {
		t.Errorf("Where after restart = %q", s2.Where(key("U1", "c")))
	}
}

func TestUsersAreSeparated(t *testing.T) {
	s := Open(Options{OS: newFakeOS(true), Paths: testPaths(t)})
	if _, err := s.Put(bg, key("U1", "c"), New("v")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get(bg, key("U2", "c")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("U2 Get = %v, want ErrNotFound", err)
	}
	if names, _ := s.List(bg, "leet", "masil.example", "U2"); len(names) != 0 {
		t.Errorf("U2 List = %v", names)
	}
	other := Key{"leet", "other.example", "U1", "c"}
	if _, _, err := s.Get(bg, other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other server Get = %v", err)
	}
}

func TestDeleteAllScopesToUser(t *testing.T) {
	fos := newFakeOS(true)
	p := testPaths(t)
	s := Open(Options{OS: fos, Paths: p})
	keep := []Key{key("U2", "c"), {"leet", "other.example", "U1", "c"}}
	gone := []Key{key("U1", "a"), key("U1", "b"), {"api", "masil.example", "U1", "env/x"}}
	for _, k := range append(slices.Clone(keep), gone...) {
		if _, err := s.Put(bg, k, New("v-"+k.Account())); err != nil {
			t.Fatal(err)
		}
	}
	// One value only in this run's memory.
	fos.down = true
	s.SetFallback(FallbackSession)
	sess := key("U1", "session-only")
	if _, err := s.Put(bg, sess, New("m")); err != nil {
		t.Fatal(err)
	}
	fos.down = false // Delete always tries the OS store (a wipe must be thorough)

	if err := s.DeleteAll(bg, "masil.example", "U1"); err != nil {
		t.Fatal(err)
	}
	for _, k := range append(gone, sess) {
		if _, _, err := s.Get(bg, k); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s still there (%v)", k.Account(), err)
		}
	}
	s = Open(Options{OS: fos, Paths: p}) // a new run: the keychain is back
	for _, k := range keep {
		if v, _, err := s.Get(bg, k); err != nil || v.Reveal() != "v-"+k.Account() {
			t.Errorf("%s lost (%v)", k.Account(), err)
		}
	}
	if len(fos.vals) != len(keep) {
		t.Errorf("os vals = %v", slices.Collect(maps.Keys(fos.vals)))
	}
}

func TestDeleteOne(t *testing.T) {
	fos := newFakeOS(true)
	p := testPaths(t)
	s := Open(Options{OS: fos, Paths: p})
	if _, err := s.Put(bg, key("U1", "a"), New("v")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(bg, key("U1", "a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(bg, key("U1", "a")); err != nil {
		t.Fatalf("second Delete = %v (missing is fine)", err)
	}
	if len(fos.vals) != 0 || s.Where(key("U1", "a")) != "" {
		t.Errorf("left %v / %q", fos.vals, s.Where(key("U1", "a")))
	}
	if names, _ := Open(Options{OS: fos, Paths: p}).List(bg, "leet", "masil.example", "U1"); len(names) != 0 {
		t.Errorf("index kept %v", names)
	}
}

func TestPutReplacesOtherPlaces(t *testing.T) {
	fos := newFakeOS(false)
	p := testPaths(t)
	s := Open(Options{OS: fos, Paths: p})
	s.SetFallback(FallbackFile)
	if _, err := s.Put(bg, key("U1", "a"), New("old")); err != nil {
		t.Fatal(err)
	}
	// The keychain comes back (a new run): the next Put goes there and the file copy is gone.
	fos.down = false
	s2 := Open(Options{OS: fos, Paths: p})
	if b, err := s2.Put(bg, key("U1", "a"), New("new")); err != nil || b != BackendKeychain {
		t.Fatalf("Put = %v, %v", b, err)
	}
	if vals := ValuesForTest(s2); len(vals) != 0 {
		t.Errorf("file/session still hold %v", vals)
	}
	v, b, err := Open(Options{OS: fos, Paths: p}).Get(bg, key("U1", "a"))
	if err != nil || v.Reveal() != "new" || b != BackendKeychain {
		t.Errorf("Get = %q %v %v", v.Reveal(), b, err)
	}
}

func TestInvalidKey(t *testing.T) {
	s := Open(Options{OS: newFakeOS(true), Paths: testPaths(t)})
	if _, err := s.Put(bg, Key{"leet", "h", "", "n"}, New("v")); err == nil {
		t.Error("Put with an empty user id succeeded")
	}
}
