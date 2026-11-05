package daemon

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "time/tzdata"

	"github.com/patrache/masil/internal/config"
)

// t0 is the fixed test clock (plan 06 Global Constraints).
var t0 = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

// fakeClock is a settable clock safe for concurrent use.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// testPaths points MASIL_CONFIG_DIR at t.TempDir(), resolves and creates it.
func testPaths(t *testing.T) config.Paths {
	t.Helper()
	root := t.TempDir()
	t.Setenv("MASIL_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("MASIL_CACHE_DIR", filepath.Join(root, "cache"))
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	if err := config.EnsureDir(p); err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	return p
}
