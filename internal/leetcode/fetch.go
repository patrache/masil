package leetcode

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Fetcher gets problems with the fewest LeetCode requests (A10.1 JA27):
// today's problem at most once per UTC day (none on a cache hit) and only the
// problems the user opens.
type Fetcher struct {
	C     *Client
	Cache *Cache
	Now   func() time.Time // nil => time.Now
}

func (f *Fetcher) now() time.Time {
	if f.Now == nil {
		return time.Now()
	}
	return f.Now()
}

// Today is today's problem and its UTC day. A problem cached for
// UTCDate(Now()) sends nothing (fromCache). Otherwise one fetch; its result is
// cached under the day LeetCode names, and also by slug for Open. A failed
// fetch returns the error and no problem — a day-old cached problem is never
// shown as today's.
func (f *Fetcher) Today(ctx context.Context) (q *Question, date string, fromCache bool, err error) {
	today := UTCDate(f.now())
	if q, ok := f.Cache.Daily(today); ok {
		return q, today, true, nil
	}
	q, date, err = f.C.Daily(ctx)
	if err != nil {
		return nil, "", false, err
	}
	// a cache that cannot be written is not the user's problem: still show it
	_ = f.Cache.PutDaily(date, q)
	_ = f.Cache.PutQuestion(q)
	return q, date, false, nil
}

// Open is the problem slug, from the cache when it is there. A premium
// problem comes back with its facts only and ErrPremiumOnly.
func (f *Fetcher) Open(ctx context.Context, slug string) (*Question, error) {
	if !ValidSlug(slug) {
		return nil, fmt.Errorf("%w: %q", ErrBadSlug, slug)
	}
	if q, ok := f.Cache.Question(slug); ok {
		if q.IsPaidOnly {
			return q, ErrPremiumOnly
		}
		return q, nil
	}
	q, err := f.C.Question(ctx, slug)
	if err != nil && !(errors.Is(err, ErrPremiumOnly) && q != nil) {
		return nil, err
	}
	_ = f.Cache.PutQuestion(q)
	return q, err
}
