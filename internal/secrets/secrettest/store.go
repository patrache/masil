// Package secrettest is the tool-secret test kit: a memory store for other
// packages' tests (P1 cookies, P4 secret variables) and leak assertions.
package secrettest

import (
	"context"
	"maps"
	"path/filepath"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/secrets"
)

// fakeOS is the in-memory OS store of NewStore; it reports BackendMemory. A
// call made with a done context fails with the context's error.
type fakeOS struct {
	mu        sync.Mutex
	available bool
	vals      map[string]string // account => value
}

func (f *fakeOS) Name() secrets.Backend { return secrets.BackendMemory }

func (f *fakeOS) Set(ctx context.Context, _, account, value string) error {
	if err := ctx.Err(); err != nil { // like a real store: a done context fails the call
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available {
		return secrets.ErrUnavailable
	}
	f.vals[account] = value
	return nil
}

func (f *fakeOS) Get(ctx context.Context, _, account string) (string, error) {
	if err := ctx.Err(); err != nil { // like a real store: a done context fails the call
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available {
		return "", secrets.ErrUnavailable
	}
	v, ok := f.vals[account]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}

func (f *fakeOS) Delete(ctx context.Context, _, account string) error {
	if err := ctx.Err(); err != nil { // like a real store: a done context fails the call
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.available {
		return secrets.ErrUnavailable
	}
	if _, ok := f.vals[account]; !ok {
		return secrets.ErrNotFound
	}
	delete(f.vals, account)
	return nil
}

// fakes maps each NewStore store to its OS fake (Values reads it).
var fakes sync.Map // *secrets.Store => *fakeOS

// NewStore is a store on an in-memory OS fake (osAvailable=false: every OS call
// is unavailable, so Put asks for consent with ErrNeedConsent); the file
// fallback lives in tb.TempDir().
func NewStore(tb testing.TB, osAvailable bool) *secrets.Store {
	tb.Helper()
	f := &fakeOS{available: osAvailable, vals: map[string]string{}}
	dir := tb.TempDir()
	s := secrets.Open(secrets.Options{OS: f, Paths: config.Paths{Dir: dir, SecretsDir: filepath.Join(dir, "secrets")}})
	fakes.Store(s, f)
	tb.Cleanup(func() { fakes.Delete(s) })
	return s
}

// Values is every value s holds — in its OS fake (NewStore stores only), this
// run's memory and the file — as Account() => value: for leak and storage checks.
func Values(s *secrets.Store) map[string]string {
	out := secrets.ValuesForTest(s)
	if v, ok := fakes.Load(s); ok {
		f := v.(*fakeOS)
		f.mu.Lock()
		maps.Copy(out, f.vals)
		f.mu.Unlock()
	}
	return out
}
