package fun

// ui/fun test fixtures (plan 05 Task 12). Test code cannot be shared across
// packages, so this is a copy of the one ui/feed fixture these tests use
// (guestbookEntries) and the helpers it needs. Clock reference = harness clock
// 2026-11-05T02:05:00Z.

import (
	"fmt"
	"sync"
	"time"

	"github.com/patrache/masil/internal/api"
)

// fixtureNow is the harness clock start (uitest.Start).
var fixtureNow = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

// sid is a 26-char sns-fun fixture ID ("01JDSNS" + 19 digits).
func sid(n int) string { return fmt.Sprintf("01JDSNS%019d", n) }

func ptr[T any](v T) *T { return &v }

// at is 02:05Z minus minAgo minutes (negative = future).
func at(minAgo int) time.Time { return fixtureNow.Add(-time.Duration(minAgo) * time.Minute) }

var fixtureUsers = sync.OnceValue(func() map[string]api.UserSummary {
	out := map[string]api.UserSummary{}
	for _, u := range seedStore(false).Users {
		out[u.Handle] = *u
	}
	return out
})

// userOf is the seeded user with handle (sam, minji, junho, hana, dev_kim).
func userOf(handle string) api.UserSummary {
	u, ok := fixtureUsers()[handle]
	if !ok {
		panic("snsfix: no seeded user @" + handle)
	}
	return u
}

// guestbookEntries is the ui/feed fixture of the same name.
func guestbookEntries(ownerID string) []api.GuestbookEntry {
	return []api.GuestbookEntry{
		{ID: sid(50), OwnerID: ownerID, Author: ptr(userOf("minji")), Body: "놀러 왔어요 ✨", CreatedAt: at(3)},
		{ID: sid(51), OwnerID: ownerID, Author: nil, Body: "", IsSecret: true, Hidden: true, CreatedAt: at(10)},
		{ID: sid(52), OwnerID: ownerID, Author: ptr(userOf("sam")), Body: "또 올게요", CreatedAt: at(60)},
	}
}
