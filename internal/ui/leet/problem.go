package leet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/third_party/goeditor"
	"github.com/patrache/masil/internal/ui"
)

// Loading a problem (A10.4): today's through the local cache (one LeetCode
// fetch a day), or a card's "<date>/<slug>" by slug. Today's facts — never
// the body — are reported to masil once per date per run; the answer's slug
// is remembered when it differs from mine (the cards show it, Task 23).

// phase is where the instance's problem is.
type phase int

const (
	phaseLoading phase = iota
	phaseReady
	phasePremium // facts only: the body is never requested (A10.1 JA27)
	phaseFailed
)

// loaded is a problem load's outcome (err may come with q: premium).
type loaded struct {
	q    *leetcode.Question
	date string
	err  error
}

// loadTag is the ui.Do tag of instance id's problem load.
func loadTag(id string) string { return "leet.load:" + id }

// reportTag is the ui.Do tag of a day's fact report.
func reportTag(date string) string { return "leet.report:" + date }

// splitTarget splits a card target "<date>/<slug>" (ok=false: malformed).
func splitTarget(target string) (date, slug string, ok bool) {
	date, slug, ok = strings.Cut(target, "/")
	if !ok || !leetcode.ValidSlug(slug) || len(date) != len("2006-01-02") {
		return "", "", false
	}
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return "", "", false
	}
	return date, slug, true
}

// loadProblem fetches instance id's problem: today's ("" target) or the
// target's. The result is ui.ResultMsg[*loaded] tagged loadTag(id).
func loadProblem(ctx *ui.Context, id, target string) tea.Cmd {
	f := fetcher(ctx)
	return ui.Do(ctx, loadTag(id), func(c context.Context, _ *api.Client) (*loaded, error) {
		if target == "" {
			q, date, _, err := f.Today(c)
			return &loaded{q: q, date: date, err: err}, nil
		}
		date, slug, ok := splitTarget(target)
		if !ok {
			return &loaded{err: leetcode.ErrBadSlug}, nil
		}
		q, err := f.Open(c, slug)
		return &loaded{q: q, date: date, err: err}, nil
	})
}

// report sends date's facts to masil unless this run already did (or is
// doing) it for this account.
func report(ctx *ui.Context, date string, q *leetcode.Question) tea.Cmd {
	if q == nil || date == "" || ctx.API == nil {
		return nil
	}
	first := withMemo(ctx, func(m *memo) bool {
		if m.reported[date] {
			return false
		}
		m.reported[date] = true
		return true
	})
	if !first {
		return nil
	}
	facts := api.LeetDailyFacts{FrontendID: q.FrontendID, Slug: q.Slug, Title: q.Title, Difficulty: q.Difficulty,
		Tags: append([]string{}, q.Tags...), IsPaidOnly: q.IsPaidOnly}
	return ui.Do(ctx, reportTag(date), func(c context.Context, a *api.Client) (*api.LeetDailyReport, error) {
		return a.ReportLeetDaily(c, date, facts)
	})
}

// reported takes a report's answer: a failure lets a later open try again;
// masil's slug is remembered when it is not mine.
func reported(ctx *ui.Context, date, mine string, m ui.ResultMsg[*api.LeetDailyReport]) {
	withMemo(ctx, func(mm *memo) struct{} {
		if m.Err != nil || m.Value == nil {
			delete(mm.reported, date)
			return struct{}{}
		}
		if s := m.Value.Daily.Slug; s != "" && mine != "" && s != mine {
			mm.mismatch[date] = s
		} else if s == mine {
			delete(mm.mismatch, date)
		}
		return struct{}{}
	})
}

// mismatchOn is masil's slug for date when it differs from the one this
// device got ("" = same or unknown).
func mismatchOn(ctx *ui.Context, date string) string {
	return withMemo(ctx, func(m *memo) string { return m.mismatch[date] })
}

// countdown is the time to the next problem (UTC midnight) as H:MM.
func countdown(now time.Time) string {
	d := leetcode.NextReset(now)
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("%d:%02d", int(d/time.Hour), int(d%time.Hour/time.Minute))
}

// nextTick is how long until the countdown's minute changes (the last one
// lands exactly on the reset).
func nextTick(now time.Time) time.Duration {
	d := leetcode.NextReset(now) % time.Minute
	if d <= 0 {
		d = time.Minute
	}
	return d
}

// tickMsg redraws instance id's countdown.
type tickMsg struct{ id string }

// ---- the browser ----

var (
	openMu  sync.Mutex
	openURL = goeditor.OpenWithSystem // open/xdg-open/rundll32 with one argument, no shell
)

// problemURL is the problem's page on leetcode.com ("" for a bad slug).
func problemURL(slug string) string {
	if !leetcode.ValidSlug(slug) {
		return ""
	}
	return leetcode.Origin + "/problems/" + slug + "/"
}

// openFailedMsg: the browser did not open; the link goes to the clipboard.
type openFailedMsg struct{ id, url string }

// openInBrowser opens slug's page; when no browser opens the link is copied.
func openInBrowser(id, slug string) tea.Cmd {
	u := problemURL(slug)
	if u == "" {
		return nil
	}
	openMu.Lock()
	open := openURL
	openMu.Unlock()
	return func() tea.Msg {
		if err := open(u); err != nil {
			return openFailedMsg{id: id, url: u}
		}
		return nil
	}
}

// loadErrorText is the sentence for a failed load.
func loadErrorText(ctx *ui.Context, err error) string {
	t := ctx.T
	switch {
	case errors.Is(err, leetcode.ErrBlocked):
		return t.Leetcode.Blocked()
	case errors.Is(err, leetcode.ErrRateLimited):
		return t.Leetcode.RateLimited()
	}
	return t.Leetcode.LoadFailed()
}
