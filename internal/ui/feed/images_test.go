package feed

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// photoPost is a post with one 64x48 PNG served by snsAPI's GET /v1/media/{id}.
func photoPost(n int) api.Post {
	p := mkPost(n, "minji", fmt.Sprintf("사진 글 %d", n), n)
	id := sid(1000 + n)
	p.Media = []api.Media{{ID: id, URL: "/v1/media/" + id, ContentType: "image/png", Width: ptr(64), Height: ptr(48)}}
	return p
}

// photoTimeline is n photo posts, newest (photoPost(1)) first.
func photoTimeline(n int) []api.TimelineItem {
	out := make([]api.TimelineItem, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, itemOf(photoPost(i)))
	}
	return out
}

// mediaGets is how many distinct media a test loaded so far.
func mediaGets(h *uitest.Harness) int {
	seen := map[string]bool{}
	for _, r := range h.API.Requests() {
		if r.Method == "GET" && strings.HasPrefix(r.Path, "/v1/media/") {
			seen[r.Path] = true
		}
	}
	return len(seen)
}

// maxRetained bounds the images a card screen keeps: the cards of about three
// view heights around the screen and the selection plus the margins.
const maxRetained = 16

// Scrolling a long photo timeline keeps only the images near the screen
// (finding: every decoded image used to stay for the instance's life).
func TestTimelineImagesStayBounded(t *testing.T) {
	const n = 80
	h := uitest.New(t, feedOpts(t, "following", false, withFollowing(photoTimeline(n)))...)
	h.Resize(120, 40)
	inst := h.Instance(keyFollowingRoute)
	if got := ImagesRetained(inst); got <= 0 {
		t.Fatalf("no image loaded for the first screen (retained %d)", got)
	}
	peak := 0
	for i := 0; i < n-1; i++ {
		h.Press("j")
		h.Screen()
		peak = max(peak, ImagesRetained(inst))
	}
	if peak > maxRetained {
		t.Fatalf("retained images peaked at %d while scrolling %d photo cards, want <= %d", peak, n, maxRetained)
	}
	if got := mediaGets(h); got < n/2 {
		t.Fatalf("only %d media loaded while scrolling to the end; the near-screen window did not follow", got)
	}
	last := photoTimeline(n)[n-1].Post.Media[0].ID
	if inst.(*timeline).imgs.image(last) == nil {
		t.Fatal("the selected last card's image is not loaded")
	}
}

// scrollBounded presses j through n cards of the screen at route, drawing each
// step, and checks the retained image peak and that loading followed the selection.
func scrollBounded(t *testing.T, h *uitest.Harness, route ui.Route, n int) {
	t.Helper()
	h.Resize(120, 40)
	inst := h.Instance(route.Key())
	if inst == nil {
		t.Fatalf("no instance for %s", route.Key())
	}
	if got := ImagesRetained(inst); got <= 0 {
		t.Fatalf("no image loaded for the first screen (retained %d)", got)
	}
	peak := 0
	for i := 0; i < n-1; i++ {
		h.Press("j")
		h.Screen()
		peak = max(peak, ImagesRetained(inst))
	}
	if peak > maxRetained {
		t.Fatalf("retained images peaked at %d while scrolling %d photo cards, want <= %d", peak, n, maxRetained)
	}
	if got := mediaGets(h); got < n/2 {
		t.Fatalf("only %d media loaded while scrolling to the end; the near-screen window did not follow", got)
	}
}

func TestHashtagImagesStayBounded(t *testing.T) {
	const n = 60
	srv := snsAPI(t, false)
	srv.Handle("GET /v1/hashtags/{tag}/posts", func(w http.ResponseWriter, r *http.Request) {
		ps := make([]api.Post, 0, n)
		for _, it := range photoTimeline(n) {
			ps = append(ps, it.Post)
		}
		writeJSON(w, pageOf(ps))
	})
	route := ui.Route{Kind: ui.RouteHashtag, ID: "사진"}
	h := uitest.New(t, hashtagOptsWith(srv, route.ID, false)...)
	scrollBounded(t, h, route, n)
}

func TestProfileImagesStayBounded(t *testing.T) {
	const n = 60
	srv := profileSrv(t, false, nil)
	srv.Handle("GET /v1/users/{user}/posts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, pageOf(photoTimeline(n)))
	})
	route := ui.Route{Kind: ui.RouteProfile, ID: "@minji"}
	h := uitest.New(t, profileOptsWith(srv, route.ID, false)...)
	scrollBounded(t, h, route, n)
}

func TestPostImagesStayBounded(t *testing.T) {
	const n = 60
	parent := postBy("dev_kim")
	srv := snsAPI(t, false)
	srv.Handle("GET /v1/posts/{id}/replies", func(w http.ResponseWriter, r *http.Request) {
		ps := make([]api.Post, 0, n)
		if r.PathValue("id") == parent.ID {
			for i := n; i >= 1; i-- { // oldest first
				p := photoPost(i)
				p.ReplyToID, p.RootID, p.ReplyToAuthor = ptr(parent.ID), ptr(parent.ID), ptr(parent.Author)
				ps = append(ps, p)
			}
		}
		writeJSON(w, pageOf(ps))
	})
	route := ui.Route{Kind: ui.RoutePost, ID: parent.ID}
	h := uitest.New(t, postOptsWith(srv, route.ID, false)...)
	h.Resize(120, 40)
	h.Press("j") // from the main post into the photo replies
	scrollBounded(t, h, route, n)
}
