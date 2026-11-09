package fixtures

import (
	"slices"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/textwidth"
)

// UserData lists the user-written text of every fixture set (Seed, Hostile,
// Cards, the timelines and notifications): display names, handles ("sam" and
// "@sam"), status texts, bios, space names and descriptions, channel names
// ("개발" and "#개발"), topics, message and post bodies, poll questions and
// options, hashtags ("#golang") and notification previews — each as given and
// sanitized (textwidth.Sanitize, as drawn). The x-pseudo check (P5 Task 4)
// allows them: they are data, never catalog text. Sorted, no duplicates.
func UserData() []string {
	var out []string
	add := func(ss ...string) {
		for _, s := range ss {
			if s != "" {
				out = append(out, s, textwidth.Sanitize(s))
			}
		}
	}
	user := func(u api.UserSummary) {
		add(u.DisplayName, u.Handle, "@"+u.Handle)
		if u.Status != nil {
			add(u.Status.Text)
		}
	}
	message := func(m api.Message) {
		user(m.Author)
		add(m.Body)
		if m.Poll != nil {
			add(m.Poll.Question)
			for _, o := range m.Poll.Options {
				add(o.Text)
			}
		}
	}
	post := func(p api.Post) {
		user(p.Author)
		add(p.Body)
		for _, h := range p.Hashtags {
			add(h, "#"+h)
		}
	}

	s := store.New()
	Seed(s)
	Hostile(s)
	Cards(s)
	add(s.Me.Bio)
	for _, u := range s.Users {
		user(*u)
	}
	for _, sp := range s.Spaces {
		add(sp.Name, sp.Description)
	}
	for _, ch := range s.Channels {
		if ch.Name != nil {
			add(*ch.Name, "#"+*ch.Name)
		}
		add(ch.Topic)
		for _, u := range ch.Members {
			user(u)
		}
		for _, m := range Messages(ch.ID) {
			message(m)
		}
	}
	for _, m := range devThread() {
		message(m)
	}
	for _, it := range append(Timeline(), HostileTimeline()...) {
		post(it.Post)
		if it.RepostedBy != nil {
			user(*it.RepostedBy)
		}
	}
	for _, n := range Notifications() {
		if n.Actor != nil {
			user(*n.Actor)
		}
		add(n.Preview)
		if n.Context.SpaceName != nil {
			add(*n.Context.SpaceName)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
