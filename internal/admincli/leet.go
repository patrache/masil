package admincli

import (
	"errors"
	"flag"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// `masil admin leet-daily set <date> --id … --slug … --title … --difficulty …
// [--tags a,b] [--paid]` (v0.1.3 A10.3, P1 Task 7): the operator fixes the
// stored facts of a LeetCode day; the server rewrites that day's #daily roots.
// Every check runs before a request is sent. Its texts come from the catalog
// (leetcode area).

// The same checks as the server (api.md 5.25) and internal/leetcode.
var (
	leetSlugRe       = regexp.MustCompile(`^[a-z0-9-]{1,100}$`)
	leetFrontendIDRe = regexp.MustCompile(`^[0-9]{1,6}$`)
	leetDateRe       = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	leetDifficulties = []string{"Easy", "Medium", "Hard"}
)

// leetUsage writes the leet-daily usage line to Stderr and returns ErrUsage.
func (r *runner) leetUsage(detail string) error {
	fmt.Fprintln(r.o.Stderr, i18n.For(i18n.Ko).Leetcode.AdminUsage())
	return fmt.Errorf("%w: leet-daily: %s", ErrUsage, detail)
}

// validLeetDate is a real calendar day written YYYY-MM-DD.
func validLeetDate(s string) bool {
	if !leetDateRe.MatchString(s) {
		return false
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

func (r *runner) leetDaily(_ []string, rest []string) error {
	for _, a := range rest {
		if a == "-h" || a == "-help" || a == "--help" {
			fmt.Fprintln(r.o.Stdout, i18n.For(i18n.Ko).Leetcode.AdminUsage())
			return nil
		}
	}
	if len(rest) < 2 || rest[0] != "set" || strings.HasPrefix(rest[1], "-") {
		return r.leetUsage("want set <date>")
	}
	date := rest[1]
	var f api.LeetDailyFacts
	var tags string
	fs := flag.NewFlagSet("leet-daily", flag.ContinueOnError)
	fs.SetOutput(r.o.Stderr)
	fs.Usage = func() {}
	fs.StringVar(&f.FrontendID, "id", "", "")
	fs.StringVar(&f.Slug, "slug", "", "")
	fs.StringVar(&f.Title, "title", "", "")
	fs.StringVar(&f.Difficulty, "difficulty", "", "")
	fs.StringVar(&tags, "tags", "", "")
	fs.BoolVar(&f.IsPaidOnly, "paid", false, "")
	if err := fs.Parse(rest[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(r.o.Stdout, i18n.For(i18n.Ko).Leetcode.AdminUsage())
			return nil
		}
		return r.leetUsage(err.Error())
	}
	f.Title = strings.TrimSpace(f.Title)
	f.Tags = []string{}
	for t := range strings.SplitSeq(tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			f.Tags = append(f.Tags, t)
		}
	}
	switch {
	case fs.NArg() > 0:
		return r.leetUsage("extra arguments")
	case !validLeetDate(date):
		return r.leetUsage("bad date")
	case !leetFrontendIDRe.MatchString(f.FrontendID):
		return r.leetUsage("bad --id")
	case !leetSlugRe.MatchString(f.Slug):
		return r.leetUsage("bad --slug")
	case f.Title == "":
		return r.leetUsage("missing --title")
	case !slices.Contains(leetDifficulties, f.Difficulty):
		return r.leetUsage("bad --difficulty")
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	d, err := c.AdminSetLeetDaily(r.ctx, date, f)
	if err != nil {
		return r.apiErr(err)
	}
	fmt.Fprintln(r.o.Stdout, i18n.For(i18n.Ko).Leetcode.AdminDailySet(clean(d.Date), textwidth.Sanitize(d.Slug)))
	return nil
}
