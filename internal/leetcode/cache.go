package leetcode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/patrache/masil/internal/config"
)

// DefaultKeep is how long Prune keeps a cached day or problem.
const DefaultKeep = 30 * 24 * time.Hour

// maxCacheFile caps one cache file read (a broken or foreign file is a miss).
const maxCacheFile = 4 << 20

// Cache is the local problem cache (A10.1: <UserCacheDir>/masil/leetcode/,
// folders 0700, files 0600, written atomically). Problem bodies live only
// here — never on the masil server. A missing, broken or hostile entry reads
// as a miss.
type Cache struct{ Dir string }

// OpenCache is the cache under <cacheRoot>/leetcode (made on first write).
func OpenCache(cacheRoot string) *Cache { return &Cache{Dir: filepath.Join(cacheRoot, "leetcode")} }

// cachedDay is one daily/<date>.json.
type cachedDay struct {
	Date     string    `json:"date"`
	Question *Question `json:"question"`
}

// fetchMark is one fetched/<day>.json: on the clock's UTC day Day, LeetCode
// named Date as today (they differ), after Tries fetches, the last at At.
type fetchMark struct {
	Day   string    `json:"day"`
	Date  string    `json:"date"`
	At    time.Time `json:"at"`
	Tries int       `json:"tries"`
}

func (c *Cache) fetchedPath(day string) string { return filepath.Join(c.Dir, "fetched", day+".json") }

// fetched is the mark of the clock day day.
func (c *Cache) fetched(day string) (fetchMark, bool) {
	var m fetchMark
	if !validDate(day) || !c.read(c.fetchedPath(day), &m) || m.Day != day || !validDate(m.Date) || m.Tries < 1 {
		return fetchMark{}, false
	}
	return m, true
}

func (c *Cache) putFetched(m fetchMark) error {
	if !validDate(m.Day) || !validDate(m.Date) {
		return fmt.Errorf("%w: cache day %q", ErrBadSlug, m.Day)
	}
	return c.write(c.fetchedPath(m.Day), m)
}

func (c *Cache) dailyPath(date string) string { return filepath.Join(c.Dir, "daily", date+".json") }
func (c *Cache) questionPath(slug string) string {
	return filepath.Join(c.Dir, "questions", slug+".json")
}

// usable is a cached or fetched problem masil can name files and links after.
func usable(q *Question) bool {
	return q != nil && ValidSlug(q.Slug) && FrontendIDRe.MatchString(q.FrontendID)
}

// Daily is the cached problem of the UTC day date.
func (c *Cache) Daily(date string) (*Question, bool) {
	if !validDate(date) {
		return nil, false
	}
	var d cachedDay
	if !c.read(c.dailyPath(date), &d) || d.Date != date || !usable(d.Question) {
		return nil, false
	}
	return d.Question, true
}

// PutDaily caches q as the problem of date.
func (c *Cache) PutDaily(date string, q *Question) error {
	if !validDate(date) || !usable(q) {
		return fmt.Errorf("%w: cache day %q", ErrBadSlug, date)
	}
	return c.write(c.dailyPath(date), cachedDay{Date: date, Question: q})
}

// Question is the cached problem slug.
func (c *Cache) Question(slug string) (*Question, bool) {
	if !ValidSlug(slug) {
		return nil, false
	}
	var q Question
	if !c.read(c.questionPath(slug), &q) || q.Slug != slug || !usable(&q) {
		return nil, false
	}
	return &q, true
}

// PutQuestion caches q under its slug.
func (c *Cache) PutQuestion(q *Question) error {
	if !usable(q) {
		return fmt.Errorf("%w: cache problem", ErrBadSlug)
	}
	return c.write(c.questionPath(q.Slug), q)
}

// Prune removes cache files not written for keep (<= 0 => DefaultKeep).
func (c *Cache) Prune(now time.Time, keep time.Duration) error {
	if keep <= 0 {
		keep = DefaultKeep
	}
	cutoff := now.Add(-keep)
	var errs []error
	for _, sub := range []string{"daily", "fetched", "questions"} {
		entries, err := os.ReadDir(filepath.Join(c.Dir, sub))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(cutoff) {
				continue
			}
			if err := os.Remove(filepath.Join(c.Dir, sub, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (c *Cache) read(path string, v any) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() || fi.Size() > maxCacheFile {
		return false
	}
	return json.NewDecoder(f).Decode(v) == nil
}

func (c *Cache) write(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	for _, d := range []string{c.Dir, filepath.Dir(path)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(d, 0o700); err != nil {
			return err
		}
	}
	return config.WriteFileAtomic(path, data, 0o600)
}
