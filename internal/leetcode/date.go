// Package leetcode is masil's UI-free access to LeetCode (v0.1.3 spec A10):
// the UTC day, the language table and solution files, the GraphQL client,
// remote run / submit / check, the local cache and the cookie credentials.
//
// It never imports the masil server client, the UI, the realtime socket or the
// local store (TestNoForbiddenImports). LeetCode cookies live only in the tool
// secret store and are sent only to the LeetCode host.
package leetcode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// SlugRe is a LeetCode problem slug as masil accepts it (the server checks the
// same): links and file names are built from it only after it matches.
var SlugRe = regexp.MustCompile(`^[a-z0-9-]{1,100}$`)

// FrontendIDRe is a LeetCode problem number as shown on the site.
var FrontendIDRe = regexp.MustCompile(`^[0-9]{1,6}$`)

// dateRe is a UTC day YYYY-MM-DD (also checked by time.Parse).
var dateRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// ErrBadSlug: a slug, problem number, day or extension that fails its check.
// No path or link is made from it.
var ErrBadSlug = errors.New("leetcode: bad slug")

// ValidSlug reports whether s matches SlugRe.
func ValidSlug(s string) bool { return SlugRe.MatchString(s) }

// validDate is a real calendar day written YYYY-MM-DD.
func validDate(s string) bool {
	if !dateRe.MatchString(s) {
		return false
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// UTCDate is the LeetCode day of t: its UTC date, YYYY-MM-DD.
func UTCDate(t time.Time) string { return t.UTC().Format(time.DateOnly) }

// NextReset is the time from now to the next UTC midnight (24h at midnight).
func NextReset(now time.Time) time.Duration {
	u := now.UTC()
	next := time.Date(u.Year(), u.Month(), u.Day()+1, 0, 0, 0, 0, time.UTC)
	return next.Sub(u)
}

// DefaultDir is where solution files go by default: <home>/masil/leetcode.
func DefaultDir(home string) string { return filepath.Join(home, "masil", "leetcode") }

// SolutionPath is <dir>/<date>-<id>-<slug><ext>. Every part is checked first
// (date YYYY-MM-DD, id FrontendIDRe, slug SlugRe, ext "." + [a-z0-9]+), so a
// hostile card or server value never becomes a path: ErrBadSlug.
func SolutionPath(dir, date, frontendID, slug string, l Lang) (string, error) {
	if !validDate(date) || !FrontendIDRe.MatchString(frontendID) || !ValidSlug(slug) || !extRe.MatchString(l.Ext) {
		return "", fmt.Errorf("%w: %q %q %q %q", ErrBadSlug, date, frontendID, slug, l.Ext)
	}
	return filepath.Join(dir, date+"-"+frontendID+"-"+slug+l.Ext), nil
}

// extRe is a solution file extension.
var extRe = regexp.MustCompile(`^\.[a-z0-9]{1,8}$`)

// EnsureSolutionFile makes path with snippet when it does not exist yet (the
// folder 0755, the file 0644, O_EXCL). An existing file is the user's work and
// is left untouched: created=false, nil.
func EnsureSolutionFile(path, snippet string) (created bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(snippet); err != nil {
		_ = f.Close()
		return true, err
	}
	return true, f.Close()
}
