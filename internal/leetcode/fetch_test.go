package leetcode_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/leetcode/leetcodetest"
)

// clock is a settable fake time.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func dailyQueries(srv *leetcodetest.Server) int {
	n := 0
	for _, r := range srv.Requests() {
		if strings.Contains(r.Body, "activeDailyCodingChallengeQuestion") {
			n++
		}
	}
	return n
}

func newFetcher(t *testing.T, srv *leetcodetest.Server, c *clock) (*leetcode.Fetcher, string) {
	root := t.TempDir()
	return &leetcode.Fetcher{C: newClient(srv, nil, nil), Cache: leetcode.OpenCache(root), Now: c.now}, root
}

func TestTodayOncePerUTCDay(t *testing.T) {
	srv := leetcodetest.New(t)
	srv.SetDaily("daily_1606.json") // 2026-11-09
	c := &clock{time.Date(2026, 11, 9, 2, 5, 0, 0, time.UTC)}
	f, _ := newFetcher(t, srv, c)
	q, date, cached, err := f.Today(t.Context())
	if err != nil || cached || date != "2026-11-09" || q.FrontendID != "1606" || q.ContentHTML == "" {
		t.Fatalf("first = %+v %q %v %v", q, date, cached, err)
	}
	q, date, cached, err = f.Today(t.Context())
	if err != nil || !cached || date != "2026-11-09" || q.Snippets["golang"] == "" {
		t.Fatalf("second = %+v %q %v %v", q, date, cached, err)
	}
	if n := dailyQueries(srv); n != 1 {
		t.Fatalf("%d daily queries, want 1", n)
	}
	total := len(srv.Requests())
	c.t = time.Date(2026, 11, 9, 23, 59, 0, 0, time.UTC)
	if _, _, cached, err := f.Today(t.Context()); err != nil || !cached {
		t.Fatalf("23:59Z: cached %v err %v", cached, err)
	}
	if n := len(srv.Requests()); n != total {
		t.Fatalf("23:59Z sent %d requests", n-total)
	}
	srv.SetDaily("premium.json") // 2026-11-10
	c.t = time.Date(2026, 11, 10, 0, 1, 0, 0, time.UTC)
	q, date, cached, err = f.Today(t.Context())
	if err != nil || cached || date != "2026-11-10" || q.Slug != "meeting-rooms-ii" {
		t.Fatalf("next day = %+v %q %v %v", q, date, cached, err)
	}
	if n := dailyQueries(srv); n != 2 {
		t.Fatalf("%d daily queries, want 2", n)
	}
	if _, _, cached, _ := f.Today(t.Context()); !cached {
		t.Fatal("next day not cached")
	}
	if n := dailyQueries(srv); n != 2 {
		t.Fatalf("%d daily queries after the cache hit", n)
	}
}

func TestTodayNeverShowsYesterday(t *testing.T) {
	srv := leetcodetest.New(t)
	c := &clock{time.Date(2026, 11, 9, 2, 5, 0, 0, time.UTC)}
	f, _ := newFetcher(t, srv, c)
	if _, _, _, err := f.Today(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(24 * time.Hour)
	srv.Fail("/graphql/", 429, "application/json", "{}")
	q, _, _, err := f.Today(t.Context())
	if !errors.Is(err, leetcode.ErrRateLimited) || q != nil {
		t.Fatalf("network down, yesterday cached: q=%+v err=%v", q, err)
	}
}

func TestOpenUsesCache(t *testing.T) {
	srv := leetcodetest.New(t)
	c := &clock{time.Date(2026, 11, 9, 2, 5, 0, 0, time.UTC)}
	f, _ := newFetcher(t, srv, c)
	q, err := f.Open(t.Context(), "two-sum")
	if err != nil || q.Title != "Two Sum" {
		t.Fatalf("open = %+v, %v", q, err)
	}
	n := len(srv.Requests())
	if q, err := f.Open(t.Context(), "two-sum"); err != nil || q.Title != "Two Sum" || len(q.ExampleTestcases) != 3 {
		t.Fatalf("cached open = %+v, %v", q, err)
	}
	if len(srv.Requests()) != n {
		t.Fatal("cached open went to the network")
	}
	// today's problem is openable by slug from the cache after Today
	if _, _, _, err := f.Today(t.Context()); err != nil {
		t.Fatal(err)
	}
	n = len(srv.Requests())
	if q, err := f.Open(t.Context(), "find-servers-that-handled-most-number-of-requests"); err != nil || q.FrontendID != "1606" {
		t.Fatalf("open daily = %+v, %v", q, err)
	}
	if len(srv.Requests()) != n {
		t.Fatal("open of today's problem went to the network")
	}
	// premium: meta cached, still ErrPremiumOnly, never a body request
	srv.SetQuestion("meeting-rooms-ii", "premium.json")
	for range 2 {
		q, err := f.Open(t.Context(), "meeting-rooms-ii")
		if !errors.Is(err, leetcode.ErrPremiumOnly) || q == nil || q.Title != "Meeting Rooms II" {
			t.Fatalf("premium open = %+v, %v", q, err)
		}
	}
	if _, err := f.Open(t.Context(), "../x"); !errors.Is(err, leetcode.ErrBadSlug) {
		t.Fatalf("hostile slug: %v", err)
	}
}

func TestCacheFilesAre0600(t *testing.T) {
	srv := leetcodetest.New(t)
	c := &clock{time.Date(2026, 11, 9, 2, 5, 0, 0, time.UTC)}
	f, root := newFetcher(t, srv, c)
	if _, _, _, err := f.Today(t.Context()); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "leetcode")
	if f.Cache.Dir != dir {
		t.Fatalf("cache dir %q", f.Cache.Dir)
	}
	files := 0
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if runtime.GOOS == "windows" {
			if !fi.IsDir() {
				files++
			}
			return nil
		}
		switch {
		case fi.IsDir() && fi.Mode().Perm() != 0o700:
			t.Errorf("%s: dir mode %v", p, fi.Mode().Perm())
		case !fi.IsDir():
			files++
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("%s: file mode %v", p, fi.Mode().Perm())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 2 {
		t.Fatalf("%d cache files, want the day and the problem", files)
	}
}

func TestPrune(t *testing.T) {
	root := t.TempDir()
	cache := leetcode.OpenCache(root)
	q := &leetcode.Question{QuestionID: "1", FrontendID: "1", Slug: "two-sum", Title: "Two Sum", Difficulty: "Easy"}
	if err := cache.PutDaily("2026-10-01", q); err != nil {
		t.Fatal(err)
	}
	if err := cache.PutQuestion(q); err != nil {
		t.Fatal(err)
	}
	if err := cache.PutDaily("2026-11-08", q); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 2, 5, 0, 0, time.UTC)
	old := now.Add(-31 * 24 * time.Hour)
	_ = filepath.Walk(cache.Dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && !strings.Contains(p, "2026-11-08") {
			_ = os.Chtimes(p, old, old)
		}
		return nil
	})
	if err := cache.Prune(now, 0); err != nil { // 0 = the default 30 days
		t.Fatal(err)
	}
	if _, ok := cache.Daily("2026-10-01"); ok {
		t.Error("old day kept")
	}
	if _, ok := cache.Question("two-sum"); ok {
		t.Error("old problem kept")
	}
	if got, ok := cache.Daily("2026-11-08"); !ok || got.Slug != "two-sum" {
		t.Errorf("recent day = %+v %v", got, ok)
	}
	if err := leetcode.OpenCache(t.TempDir()).Prune(now, time.Hour); err != nil {
		t.Fatalf("empty cache prune: %v", err)
	}
}

func TestCacheRejectsHostileKeys(t *testing.T) {
	cache := leetcode.OpenCache(t.TempDir())
	if err := cache.PutDaily("../x", &leetcode.Question{Slug: "two-sum", FrontendID: "1"}); !errors.Is(err, leetcode.ErrBadSlug) {
		t.Fatalf("bad date: %v", err)
	}
	if err := cache.PutQuestion(&leetcode.Question{Slug: "../../x", FrontendID: "1"}); !errors.Is(err, leetcode.ErrBadSlug) {
		t.Fatalf("bad slug: %v", err)
	}
	if _, ok := cache.Question("../../x"); ok {
		t.Fatal("hostile read")
	}
	// a broken file reads as a miss
	if err := os.MkdirAll(filepath.Join(cache.Dir, "questions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache.Dir, "questions", "two-sum.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Question("two-sum"); ok {
		t.Fatal("broken file read as a hit")
	}
}
