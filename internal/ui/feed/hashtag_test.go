package feed

import (
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// hashtagOptsWith opens RouteHashtag{ID: tag} on srv.
func hashtagOptsWith(srv *apitest.Server, tag string, hostile bool) []uitest.Option {
	seed := fixtures.Seed
	if hostile {
		seed = func(s *store.Store) { fixtures.Seed(s); fixtures.Hostile(s) }
	}
	return []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(seed), uitest.WithRoute(ui.Route{Kind: ui.RouteHashtag, ID: tag})}
}

func hashtagOpts(t *testing.T, tag string) []uitest.Option {
	t.Helper()
	return hashtagOptsWith(snsAPI(t, false), tag, false)
}

func TestHashtagGolden(t *testing.T) {
	sweep(t, uitest.AllSizes(), "hashtag", hashtagOpts(t, "첫눈"), nil)

	h := uitest.New(t, hashtagOpts(t, "첫눈")...)
	if _, ok := lastReq(h, "GET", "/v1/hashtags/첫눈/posts"); !ok {
		t.Fatalf("no GET /v1/hashtags/첫눈/posts; requests %+v", h.API.Requests())
	}
	s := h.Screen()
	for _, want := range []string{"@hana", "#첫눈 왔다"} {
		if !strings.Contains(s, want) {
			t.Fatalf("hashtag feed lacks %q:\n%s", want, s)
		}
	}
}

func TestHashtagHostile(t *testing.T) {
	srv := snsAPI(t, true)
	srv.Handle("GET /v1/hashtags/{tag}/posts", func(w http.ResponseWriter, r *http.Request) {
		ps := []api.Post{}
		for _, it := range hashtagPosts("첫눈") {
			ps = append(ps, it.Post)
		}
		for _, it := range fixtures.HostileTimeline() {
			ps = append(ps, it.Post)
		}
		writeJSON(w, pageOf(ps))
	})
	tag := "태그\x1b[31m빨강\u202e" + strings.Repeat("긴태그", 20)
	sweep(t, uitest.AllSizes(), "", hashtagOptsWith(srv, tag, true), nil)
}

func TestHashtagEmpty(t *testing.T) {
	h := uitest.New(t, hashtagOpts(t, "아무도안씀")...)
	if s := h.Screen(); !strings.Contains(s, "이 태그의 글이 아직 없어요") {
		t.Fatalf("empty hashtag state missing:\n%s", s)
	}
	h.Check()
}

func TestHashtagActionsWork(t *testing.T) {
	srv := snsAPI(t, false)
	id := postBy("hana").ID
	srv.JSON("PUT /v1/posts/{id}/like", http.StatusOK, api.LikeResult{PostID: id, Liked: true, LikeCount: 13})
	srv.JSON("DELETE /v1/posts/{id}/like", http.StatusOK, api.LikeResult{PostID: id, Liked: false, LikeCount: 11})
	h := uitest.New(t, hashtagOptsWith(srv, "첫눈", false)...)
	h.Resize(120, 40)
	h.Press("l")
	// hana's post is already liked (fixture), so l unlikes it.
	if _, ok := lastReq(h, "DELETE", "/v1/posts/"+id+"/like"); !ok {
		t.Fatalf("l on first card sent no like request; requests %+v", h.API.Requests())
	}
	h.Press("j", "l")
	if _, ok := lastReq(h, "PUT", "/v1/posts/"+sid(70)+"/like"); !ok {
		t.Fatalf("j, l sent no PUT like for the second card; requests %+v", h.API.Requests())
	}
	h.Press("enter")
	if top := feedTop(h); top.Kind != ui.RoutePost || top.ID != sid(70) {
		t.Fatalf("enter opened %+v, want RoutePost %s", top, sid(70))
	}
}

func TestHashtagInfiniteScroll(t *testing.T) {
	srv := snsAPI(t, false)
	srv.Handle("GET /v1/hashtags/{tag}/posts", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "c2" {
			writeJSON(w, pageOf([]api.Post{myPost()}))
			return
		}
		ps := []api.Post{}
		for _, it := range hashtagPosts("첫눈") {
			ps = append(ps, it.Post)
		}
		writeJSON(w, map[string]any{"items": ps, "next_cursor": "c2"})
	})
	h := uitest.New(t, hashtagOptsWith(srv, "첫눈", false)...)
	h.Press("G")
	r, ok := lastReq(h, "GET", "/v1/hashtags/첫눈/posts")
	if !ok || !strings.Contains(r.Query, "cursor=c2") {
		t.Fatalf("last hashtag request = %+v (ok=%v), want cursor=c2", r, ok)
	}
	if !strings.Contains(h.Screen(), "점심 먹고 배포합니다") {
		t.Fatalf("second page not shown:\n%s", h.Screen())
	}
}

func TestBlockHidesInPerScreenLists(t *testing.T) {
	// Review Focus 3, per-screen lists (not FeedStore windows).
	hana := userOf("hana").ID
	block := func(h *uitest.Harness) {
		h.Event("block.updated", map[string]any{"user_id": hana, "blocked": true})
	}

	t.Run("hashtag", func(t *testing.T) {
		h := uitest.New(t, hashtagOpts(t, "첫눈")...)
		h.Resize(120, 40)
		if s := h.Screen(); !strings.Contains(s, "@hana") || !strings.Contains(s, "#첫눈 왔다") {
			t.Fatalf("hashtag feed before block:\n%s", s)
		}
		block(h)
		s := h.Screen()
		if strings.Contains(s, "@hana") {
			t.Fatalf("blocked hana still in the hashtag feed:\n%s", s)
		}
		if !strings.Contains(s, "#첫눈 왔다") {
			t.Fatalf("minji's post vanished with the block:\n%s", s)
		}
		h.Check()
	})

	t.Run("profile", func(t *testing.T) {
		h := uitest.New(t, profileOpts(t, "@dev_kim", false, nil)...)
		h.Resize(120, 40)
		s := h.Screen()
		for _, want := range []string{"bubbletea v2", "님이 리포스트", "오늘 첫 눈"} {
			if !strings.Contains(s, want) {
				t.Fatalf("dev_kim's posts tab lacks %q:\n%s", want, s)
			}
		}
		block(h)
		s = h.Screen()
		for _, gone := range []string{"@hana", "오늘 첫 눈", "님이 리포스트"} {
			if strings.Contains(s, gone) {
				t.Fatalf("blocked hana's repost still shows %q:\n%s", gone, s)
			}
		}
		if !strings.Contains(s, "bubbletea v2") {
			t.Fatalf("dev_kim's own post vanished:\n%s", s)
		}
		h.Check()
	})

	t.Run("user_list", func(t *testing.T) {
		h := uitest.New(t, feedOpts(t, "following", false)...)
		h.Resize(120, 40)
		h.API.JSON("GET /v1/posts/{id}/likes", http.StatusOK, pageOf([]api.UserSummary{userOf("hana"), userOf("minji")}))
		h.Send(ui.OpenOverlayMsg{ID: "feed.user_list", Args: "likes " + postBy("dev_kim").ID})
		rows := overlayText(h)
		if rows == "" {
			t.Fatal("feed.user_list did not open")
		}
		if !strings.Contains(rows, "@hana") || !strings.Contains(rows, "@minji") {
			t.Fatalf("likes list before block:\n%s", rows)
		}
		block(h)
		rows = overlayText(h)
		if strings.Contains(rows, "@hana") || !strings.Contains(rows, "@minji") {
			t.Fatalf("likes list after blocking hana:\n%s", rows)
		}
		h.Check()
	})
}
