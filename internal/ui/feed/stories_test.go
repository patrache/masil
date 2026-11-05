package feed

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/store/feedstore"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// Story fixture IDs (snsfix storyGroups / devKimStories).
var (
	storySam   = sid(60)
	storyHana1 = sid(61) // "눈 온다 ❄", unseen
	storyHana2 = sid(62) // photo, bg accent2, unseen
	storyDev   = sid(65) // dev_kim (not in the ring)
)

// ringSeed seeds the core fixtures (+ Hostile) and the story ring groups.
func ringSeed(hostile bool, groups func() []api.StoryGroup) func(s *store.Store) {
	return func(s *store.Store) {
		fixtures.Seed(s)
		if hostile {
			fixtures.Hostile(s)
		}
		if groups != nil {
			feedstore.Get(s).SetStories(groups())
		}
	}
}

// storyOptsWith opens RouteStories{userID} on srv with the story ring seeded
// (the feed surface below it is the default following timeline).
func storyOptsWith(srv *apitest.Server, userID string, hostile bool, groups func() []api.StoryGroup) []uitest.Option {
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(ringSeed(hostile, groups)),
		uitest.WithRoute(ui.Route{Kind: ui.RouteStories, ID: userID})}
}

func storyOpts(t *testing.T, handle string) []uitest.Option {
	t.Helper()
	return storyOptsWith(snsAPI(t, false), userOf(handle).ID, false, storyGroups)
}

// ringOf is the FeedStore story ring of h.
func ringOf(h *uitest.Harness) []api.StoryGroup {
	return feedstore.Get(h.Context().Store).Stories()
}

// hostileStoryGroups is one group by the hostile long-name user whose stories
// carry the hostile message bodies (controls, emoji soup, a 4000-rune body), an
// unknown bg and a photo with absurd dimensions.
func hostileStoryGroups() []api.StoryGroup {
	msgs := fixtures.HostileMessages(fixtures.ChGeneral)
	author := msgs[0].Author
	var out []api.Story
	for i, m := range msgs {
		if i >= 4 {
			break
		}
		out = append(out, api.Story{ID: sid(160 + i), Author: author, Body: m.Body, BG: "\x1b[31mred", CreatedAt: at(5 + i), ExpiresAt: storyExpiry})
	}
	out[1].Media = &api.Media{ID: sid(170), URL: "/v1/media/" + sid(170), ContentType: "image/png", Width: ptr(100000), Height: ptr(1)}
	out[2].Media = &api.Media{ID: sid(171), URL: "/v1/media/" + sid(171), ContentType: "image/png", Width: ptr(1), Height: ptr(100000)}
	return append([]api.StoryGroup{{User: author, Stories: out, HasUnseen: true, LatestAt: at(5)}}, storyGroups()...)
}

func TestStoryViewerGolden(t *testing.T) {
	// testdata/TestStoryViewerGolden/<WxH>/hana.golden
	sweep(t, uitest.AllSizes(), "hana", storyOpts(t, "hana"), nil)
}

func TestStoryViewerHostile(t *testing.T) {
	author := hostileStoryGroups()[0].User.ID
	sweep(t, uitest.AllSizes(), "", storyOptsWith(snsAPI(t, true), author, true, hostileStoryGroups), func(h *uitest.Harness) {
		h.Check()
		for range 3 {
			h.Press("n") // every hostile story (photo ones included) is drawn
			h.Check()
		}
	})
}

func TestStoryAutoAdvance(t *testing.T) {
	h := uitest.New(t, storyOpts(t, "hana")...)
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana1+"/view"); !ok {
		t.Fatal("first story shown without POST /v1/stories/<s1>/view")
	}
	if s := h.Screen(); !strings.Contains(s, "눈 온다") || !strings.Contains(s, "@hana") {
		t.Fatalf("first story not shown:\n%s", s)
	}
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); ok {
		t.Fatal("second story marked viewed before it was shown")
	}
	h.Advance(4 * time.Second)
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); ok {
		t.Fatal("advanced before 5 s")
	}
	h.Advance(time.Second)
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); !ok {
		t.Fatal("5 s later: no POST /v1/stories/<s2>/view")
	}
	if s := h.Screen(); strings.Contains(s, "눈 온다") {
		t.Fatalf("5 s later the first story is still shown:\n%s", s)
	}
	if n := len(reqsTo(h, "POST", "/v1/stories/"+storyHana1+"/view")); n != 1 {
		t.Fatalf("POST view s1 sent %d times, want 1", n)
	}
	for _, g := range ringOf(h) {
		if g.User.ID == userOf("hana").ID && g.HasUnseen {
			t.Fatalf("hana's group still unseen after both stories were shown: %+v", g)
		}
	}
}

func TestStoryManualResetsTimer(t *testing.T) {
	h := uitest.New(t, storyOpts(t, "hana")...)
	h.Advance(4 * time.Second)
	h.Press("n")
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); !ok {
		t.Fatal("n: second story not shown")
	}
	h.Advance(4 * time.Second) // the first story's timer (due at 5 s) must not skip the second
	if top := feedTop(h); top.Kind != ui.RouteStories {
		t.Fatalf("double skip: feed top = %+v, want still the stories", top)
	}
	if s := h.Screen(); !strings.Contains(s, "2/2") {
		t.Fatalf("not on the second story:\n%s", s)
	}
	h.Advance(time.Second) // 5 s after "n": the group ends, no unseen group is left
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("5 s after n: feed top = %+v, want RouteFeed", top)
	}
}

func TestStoryEndGoesBack(t *testing.T) {
	h := uitest.New(t, storyOpts(t, "hana")...)
	h.Press("n")
	if top := feedTop(h); top.Kind != ui.RouteStories {
		t.Fatalf("n on the first story left the viewer: %+v", top)
	}
	h.Press("p")
	if s := h.Screen(); !strings.Contains(s, "눈 온다") {
		t.Fatalf("p: not back on the first story:\n%s", s)
	}
	h.Press("space", "right") // last story of the last unseen group, then past it
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("past the last unseen group: feed top = %+v, want RouteFeed", top)
	}
}

func TestStoryNextUnseenGroup(t *testing.T) {
	// junho's (seen) group ends => the next unseen group (hana), not my own.
	h := uitest.New(t, storyOpts(t, "junho")...)
	if s := h.Screen(); !strings.Contains(s, "커피") {
		t.Fatalf("junho's story not shown:\n%s", s)
	}
	h.Press("n")
	if s := h.Screen(); !strings.Contains(s, "눈 온다") || !strings.Contains(s, "@hana") {
		t.Fatalf("n at the end of junho's group: not on hana's first unseen story:\n%s", s)
	}
	if top := feedTop(h); top.Kind != ui.RouteStories {
		t.Fatalf("feed top = %+v", top)
	}
}

func TestStoryReport(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("POST /v1/reports", http.StatusCreated, map[string]string{"report_id": sid(99)})
	h := uitest.New(t, storyOptsWith(srv, userOf("hana").ID, false, storyGroups)...)
	h.Press("m")
	if s := overlayText(h); strings.Contains(s, "삭제") || strings.Contains(s, "본 사람") {
		t.Fatalf("someone else's story offers my actions:\n%s", s)
	}
	h.Press("esc")
	menuPick(t, h, "신고")
	if s := overlayText(h); !strings.Contains(s, "스팸") || !strings.Contains(s, "하나의 스토리") {
		t.Fatalf("no story report dialog:\n%s", s)
	}
	h.Press("tab", "enter")
	r, ok := lastReq(h, "POST", "/v1/reports")
	if !ok {
		t.Fatal("report dialog sent no POST /v1/reports")
	}
	if body := postBody(t, r); body["target_type"] != "story" || body["target_id"] != storyHana1 {
		t.Fatalf("report body %s, want target story %s", r.Body, storyHana1)
	}
}

func TestStoryAuthorProfile(t *testing.T) {
	h := uitest.New(t, storyOpts(t, "hana")...)
	menuPick(t, h, "작성자 프로필")
	if top := feedTop(h); top.Kind != ui.RouteProfile || top.ID != userOf("hana").ID {
		t.Fatalf("작성자 프로필: feed top = %+v", top)
	}
}

func TestStoryMineViewersAndDelete(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("GET /v1/stories/{id}/viewers", http.StatusOK, pageOf([]api.StoryViewer{{User: userOf("minji"), ViewedAt: at(1)}}))
	srv.Handle("DELETE /v1/stories/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := uitest.New(t, storyOptsWith(srv, userOf("sam").ID, false, storyGroups)...)
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storySam+"/view"); ok {
		t.Fatal("my own story sent POST view")
	}
	h.Press("m")
	if s := overlayText(h); !strings.Contains(s, "본 사람 3") || !strings.Contains(s, "삭제") || strings.Contains(s, "신고") {
		t.Fatalf("my story menu:\n%s", s)
	}
	h.Press("esc")
	menuPick(t, h, "본 사람")
	if _, ok := lastReq(h, "GET", "/v1/stories/"+storySam+"/viewers"); !ok {
		t.Fatal("본 사람: no GET /v1/stories/<id>/viewers")
	}
	if s := overlayText(h); !strings.Contains(s, "@minji") {
		t.Fatalf("viewers list:\n%s", s)
	}
	h.Press("esc")
	menuPick(t, h, "삭제")
	if s := overlayText(h); !strings.Contains(s, "이 스토리를 삭제할까요?") {
		t.Fatalf("no delete confirm:\n%s", s)
	}
	h.Press("y")
	if _, ok := lastReq(h, "DELETE", "/v1/stories/"+storySam); !ok {
		t.Fatal("no DELETE /v1/stories/<id>")
	}
	for _, g := range ringOf(h) {
		for _, st := range g.Stories {
			if st.ID == storySam {
				t.Fatal("deleted story still in the ring right after the 204")
			}
		}
	}
	if !strings.Contains(toastText(h), "스토리를삭제했어요") {
		t.Fatalf("no delete toast:\n%s", h.Screen())
	}
}

func TestStoryExpiredPruned(t *testing.T) {
	h := uitest.New(t, feedOpts(t, "following", false)...)
	if s := h.Screen(); !strings.Contains(s, "◉ 하나") {
		t.Fatalf("ring lacks hana before expiry:\n%s", s)
	}
	h.Advance(21 * time.Hour) // nothing else happens meanwhile: the ring is pruned when drawn
	if s := h.Screen(); strings.Contains(s, "◉ 하나") || strings.Contains(s, "○ 하나") {
		t.Fatalf("ring still lists hana after expiry:\n%s", s)
	}
	for _, g := range ringOf(h) {
		if g.User.ID == userOf("hana").ID {
			t.Fatalf("hana's expired group still in the ring: %+v", g)
		}
	}

	// the viewer prunes before drawing too: an expired group is never shown
	soon := func() []api.StoryGroup {
		gs := storyGroups()
		gs[2].Stories[0].ExpiresAt = fixtureNow.Add(3 * time.Second) // junho
		return gs
	}
	h = uitest.New(t, storyOptsWith(snsAPI(t, false), userOf("junho").ID, false, soon)...)
	if s := h.Screen(); !strings.Contains(s, "커피") {
		t.Fatalf("junho's story not shown before expiry:\n%s", s)
	}
	h.Advance(3 * time.Second) // no timer is due: only View can prune
	if s := h.Screen(); strings.Contains(s, "커피") {
		t.Fatalf("expired story drawn:\n%s", s)
	}
}

func TestStoryDeletedEventSkips(t *testing.T) {
	h := uitest.New(t, storyOpts(t, "hana")...)
	h.Event("story.deleted", map[string]any{"user_id": userOf("hana").ID, "story_id": storyHana1})
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); !ok {
		t.Fatal("story.deleted of the current story: next story not shown")
	}
	if s := h.Screen(); strings.Contains(s, "눈 온다") || !strings.Contains(s, "1/1") {
		t.Fatalf("after story.deleted:\n%s", s)
	}
	// someone else's deletion leaves the current story alone
	h.Event("story.deleted", map[string]any{"user_id": userOf("junho").ID, "story_id": sid(64)})
	if top := feedTop(h); top.Kind != ui.RouteStories {
		t.Fatalf("unrelated story.deleted moved the viewer: %+v", top)
	}
}

func TestStoryBlockedGroupSkipped(t *testing.T) {
	h := uitest.New(t, storyOpts(t, "junho")...)
	h.Event("block.updated", map[string]any{"user_id": userOf("hana").ID, "blocked": true})
	h.Press("n") // junho's group ends; hana (the only unseen group) is blocked
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("blocked hana's group shown: feed top = %+v", top)
	}
}

func TestStoryUserFallbackLoads(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("GET /v1/users/{user}/stories", http.StatusOK, itemsOf(devKimStories()))
	dev := userOf("dev_kim").ID
	h := uitest.New(t, storyOptsWith(srv, dev, false, storyGroups)...)
	if _, ok := lastReq(h, "GET", "/v1/users/"+dev+"/stories"); !ok {
		t.Fatal("no GET /v1/users/<dev_kim>/stories")
	}
	if s := h.Screen(); !strings.Contains(s, "배포 끝") || !strings.Contains(s, "@dev_kim") {
		t.Fatalf("dev_kim's story not shown:\n%s", s)
	}
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyDev+"/view"); !ok {
		t.Fatal("fallback story shown without POST view")
	}
	if got := ringOf(h); !reflect.DeepEqual(got, storyGroups()) {
		t.Fatalf("the temporary group reached the FeedStore ring:\n%+v", got)
	}
	h.Press("n") // the temporary group ends: back, never into the ring's unseen groups
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("end of the temporary group: feed top = %+v, want RouteFeed", top)
	}
}

func TestStoryUserFallbackEmpty(t *testing.T) {
	srv := snsAPI(t, false)
	srv.JSON("GET /v1/users/{user}/stories", http.StatusOK, itemsOf([]api.Story{}))
	h := uitest.New(t, storyOptsWith(srv, userOf("dev_kim").ID, false, storyGroups)...)
	if !strings.Contains(toastText(h), "지금볼수있는스토리가없어요") {
		t.Fatalf("no empty toast:\n%s", h.Screen())
	}
	if top := feedTop(h); top.Kind == ui.RouteStories {
		t.Fatalf("empty fallback stayed on the viewer: %+v", top)
	}
}

func TestStoryUserFallbackError(t *testing.T) {
	srv := snsAPI(t, false)
	srv.Handle("GET /v1/users/{user}/stories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":"private_account","message":"private_account","details":{}}}`))
	})
	h := uitest.New(t, storyOptsWith(srv, userOf("dev_kim").ID, false, storyGroups)...)
	if top := feedTop(h); top.Kind == ui.RouteStories {
		t.Fatalf("403 stayed on the viewer: %+v", top)
	}
}

func TestStoryAutoAdvanceEveryMode(t *testing.T) {
	for _, s := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 120, H: 30}, {W: 80, H: 24}, {W: 50, H: 20}} {
		h := uitest.New(t, storyOpts(t, "hana")...)
		h.Resize(s.W, s.H)
		h.Advance(5 * time.Second)
		if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); !ok {
			t.Fatalf("%dx%d: no auto-advance to the second story:\n%s", s.W, s.H, h.Screen())
		}
	}
}

func TestStoryLeftViewerStops(t *testing.T) {
	// A viewer left with esc stays cached and keeps getting broadcasts: its
	// timer must neither advance nor send Back for a screen it no longer shows.
	h := uitest.New(t, feedOpts(t, "following", false)...)
	h.Press("s")
	if top := feedTop(h); top.Kind != ui.RouteStories {
		t.Fatalf("s: feed top = %+v", top)
	}
	h.Press("esc")
	if top := feedTop(h); top.Kind != ui.RouteFeed {
		t.Fatalf("esc: feed top = %+v", top)
	}
	h.Press("enter") // hana's post
	if top := feedTop(h); top.Kind != ui.RoutePost {
		t.Fatalf("enter: feed top = %+v", top)
	}
	h.Advance(20 * time.Second)
	if top := feedTop(h); top.Kind != ui.RoutePost {
		t.Fatalf("the hidden viewer's timer moved the feed: top = %+v", top)
	}
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); ok {
		t.Fatal("the hidden viewer advanced to the second story")
	}
	h.Press("esc", "s") // back on the feed, open hana's stories again: starts at the first unseen
	if s := h.Screen(); !strings.Contains(s, "2/2") {
		t.Fatalf("reopened viewer not on the first unseen story:\n%s", s)
	}
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); !ok {
		t.Fatal("reopened viewer sent no POST view for the story it shows")
	}
}

func TestStoryMenuPausesTimer(t *testing.T) {
	h := uitest.New(t, storyOpts(t, "hana")...)
	h.Press("m")
	h.Advance(10 * time.Second)
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); ok {
		t.Fatal("advanced under the open menu")
	}
	h.Press("esc") // menu closed; the next key resumes the timer
	h.Press("x")
	h.Advance(5 * time.Second)
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); !ok {
		t.Fatal("timer not resumed after the menu")
	}
}

func TestStoryMenuIsBox(t *testing.T) {
	// a two-item menu is still a bordered box (the app draws an overlay shorter
	// than 6 rows full screen without a border)
	h := uitest.New(t, storyOpts(t, "hana")...)
	h.Press("m")
	f := h.Model.Frame()
	if len(f.Overlays) != 1 || !f.Overlays[0].Bordered || f.Overlays[0].Rect.W >= 120 {
		t.Fatalf("story menu overlay = %+v, want one bordered box", f.Overlays)
	}
	if s := overlayText(h); !strings.Contains(s, "› 신고") || !strings.Contains(s, "작성자 프로필") {
		t.Fatalf("story menu:\n%s", s)
	}
}

func TestStoryHiddenViewerWaits(t *testing.T) {
	// The viewer is below the author's profile: a story.deleted moves it on
	// silently; the next story is marked viewed only once it is on screen.
	h := uitest.New(t, storyOpts(t, "hana")...)
	menuPick(t, h, "작성자 프로필")
	if top := feedTop(h); top.Kind != ui.RouteProfile {
		t.Fatalf("feed top = %+v", top)
	}
	h.Event("story.deleted", map[string]any{"user_id": userOf("hana").ID, "story_id": storyHana1})
	h.Advance(10 * time.Second)
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); ok {
		t.Fatal("the hidden viewer marked the next story viewed")
	}
	if top := feedTop(h); top.Kind != ui.RouteProfile {
		t.Fatalf("the hidden viewer moved the feed: top = %+v", top)
	}
	h.Press("esc")
	if top := feedTop(h); top.Kind != ui.RouteStories {
		t.Fatalf("esc: feed top = %+v", top)
	}
	if _, ok := lastReq(h, "POST", "/v1/stories/"+storyHana2+"/view"); !ok {
		t.Fatal("back on the viewer: the story it shows was not marked viewed")
	}
	if s := h.Screen(); !strings.Contains(s, "1/1") {
		t.Fatalf("back on the viewer:\n%s", s)
	}
}
