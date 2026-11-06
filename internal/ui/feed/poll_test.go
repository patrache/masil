package feed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// pollFirst is a following timeline with p on top (selected: made the newest)
// then fixtures.Timeline().
func pollFirst(p api.Post) snsOpt {
	p.CreatedAt = fixtureNow
	return withFollowing(append([]api.TimelineItem{itemOf(p)}, fixtures.Timeline()...))
}

// pollWith is a copy of base's poll with the given option counts and my_votes.
func pollWith(base api.Post, counts []int, mine []int) *api.Poll {
	pl := clonePoll(base.Poll)
	voters := 0
	for i := range pl.Options {
		pl.Options[i].VoteCount = counts[i]
		voters += counts[i]
	}
	pl.VoterCount = voters
	pl.MyVotes = append([]int{}, mine...)
	return pl
}

var lastInt = regexp.MustCompile(`(\d+)\D*$`)

// pollRow reads the bar row of label in text: the vote count (last integer
// field after the label) and whether the row carries my "✓" mark.
func pollRow(t *testing.T, text, label string) (count int, mine bool) {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		i := strings.Index(line, label)
		if i < 0 || !strings.ContainsAny(line, "█░") {
			continue
		}
		count = -1
		if m := lastInt.FindStringSubmatch(line[i+len(label):]); m != nil {
			count, _ = strconv.Atoi(m[1])
		}
		return count, strings.Contains(line[:i], "✓")
	}
	t.Fatalf("no poll bar row for %q:\n%s", label, text)
	return 0, false
}

// voteBody decodes a PUT /v1/polls/{id}/vote body.
func voteBody(t *testing.T, r []byte) []int {
	t.Helper()
	var b struct {
		OptionIdxs []int `json:"option_idxs"`
	}
	if err := json.Unmarshal(r, &b); err != nil {
		t.Fatalf("vote body %q: %v", r, err)
	}
	return b.OptionIdxs
}

func TestVotePollLocalChecks(t *testing.T) {
	ctx := &ui.Context{Now: time.Now}
	p := pollPost()
	p.Poll.Closed = true
	cmd := votePoll(ctx, &p, []int{0})
	if cmd == nil {
		t.Fatal("votePoll on a closed poll: nil Cmd, want a toast")
	}
	if m, ok := cmd().(ui.ToastMsg); !ok || m.Text != "마감된 투표예요" {
		t.Fatalf("votePoll on a closed poll = %#v, want toast 마감된 투표예요", cmd())
	}
	d := deletedPost()
	if m, ok := votePoll(ctx, &d, []int{0})().(ui.ToastMsg); !ok || m.Text != textDeleted {
		t.Fatalf("votePoll on a deleted post = %#v", m)
	}
	q := postBy("hana")
	if votePoll(ctx, &q, []int{0}) != nil {
		t.Fatal("votePoll on a post without a poll returned a Cmd")
	}
}

func TestPollVoteSingleClick(t *testing.T) {
	items := append(append([]api.TimelineItem{}, longTimeline()[:3]...), itemOf(pollPost()))
	srv := snsAPI(t, false, withFollowing(items))
	pollID := pollPost().Poll.ID
	srv.JSON("PUT /v1/polls/{id}/vote", http.StatusOK, map[string]any{"poll": pollWith(pollPost(), []int{4, 3}, []int{1})})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h.Press("G")
	x, y := cellOf(t, h.Screen(), "돈까스")
	h.Click(x+1, y)
	r, ok := lastReq(h, "PUT", "/v1/polls/"+pollID+"/vote")
	if !ok {
		t.Fatalf("click on 돈까스 sent no PUT vote:\n%s", h.Screen())
	}
	if got := voteBody(t, r.Body); !slices.Equal(got, []int{1}) {
		t.Fatalf("option_idxs = %v, want [1]", got)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("single-choice click opened %d overlays", n)
	}
	s := h.Screen()
	if c, mine := pollRow(t, s, "돈까스"); c != 3 || !mine {
		t.Fatalf("돈까스 row: count=%d mine=%v, want 3 ✓:\n%s", c, mine, s)
	}
	if c, mine := pollRow(t, s, "국밥"); c != 4 || mine {
		t.Fatalf("국밥 row: count=%d mine=%v, want 4 without ✓:\n%s", c, mine, s)
	}
}

func TestPollVoteOverlayMulti(t *testing.T) {
	mp := multiPollPost()
	srv := snsAPI(t, false, pollFirst(mp))
	srv.JSON("PUT /v1/polls/{id}/vote", http.StatusOK, map[string]any{"poll": pollWith(mp, []int{5, 3}, []int{0, 1})})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	menuPick(t, h, "투표하기")
	s := overlayText(h)
	if !strings.Contains(s, "[ ]") || strings.Contains(s, "[x]") || strings.Contains(s, "( )") {
		t.Fatalf("multi-choice overlay without unchecked boxes:\n%s", s)
	}
	h.Press("space", "j", "space")
	if s := overlayText(h); strings.Count(s, "[x]") != 2 {
		t.Fatalf("two picks not checked:\n%s", s)
	}
	h.Press("enter")
	r, ok := lastReq(h, "PUT", "/v1/polls/"+mp.Poll.ID+"/vote")
	if !ok {
		t.Fatal("enter sent no PUT vote")
	}
	if got := voteBody(t, r.Body); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("option_idxs = %v, want [0 1]", got)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("overlay still open after enter (%d)", n)
	}
	s = h.Screen()
	if c, mine := pollRow(t, s, "국밥"); c != 5 || !mine {
		t.Fatalf("국밥 row: count=%d mine=%v:\n%s", c, mine, s)
	}
	if c, mine := pollRow(t, s, "돈까스"); c != 3 || !mine {
		t.Fatalf("돈까스 row: count=%d mine=%v:\n%s", c, mine, s)
	}

	// A click on a multi-choice option opens the overlay with it pre-checked, no request.
	h2 := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	before := len(reqsTo(h2, "PUT", "/v1/polls/"+mp.Poll.ID+"/vote"))
	x, y := cellOf(t, h2.Screen(), "돈까스")
	h2.Click(x+1, y)
	if n := len(h2.Model.Frame().Overlays); n != 1 {
		t.Fatalf("multi click: %d overlays, want the vote overlay", n)
	}
	s = overlayText(h2)
	if strings.Count(s, "[x]") != 1 {
		t.Fatalf("clicked option not pre-checked:\n%s", s)
	}
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "국밥") && strings.Contains(l, "[x]") {
			t.Fatalf("국밥 checked instead of 돈까스:\n%s", s)
		}
	}
	if after := len(reqsTo(h2, "PUT", "/v1/polls/"+mp.Poll.ID+"/vote")); after != before {
		t.Fatal("multi click sent a vote")
	}
}

func TestPollRetractAndClose(t *testing.T) {
	p := pollPost()
	srv := snsAPI(t, false, pollFirst(p))
	srv.JSON("DELETE /v1/polls/{id}/vote", http.StatusOK, map[string]any{"poll": pollWith(p, []int{3, 2}, []int{})})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	menuPick(t, h, "투표하기")
	if s := overlayText(h); !strings.Contains(s, "(•)") || !strings.Contains(s, "( )") {
		t.Fatalf("single-choice overlay without radio marks (my vote 국밥 picked):\n%s", s)
	}
	h.Press("c") // not the creator: no close
	if _, ok := lastReq(h, "POST", "/v1/polls/"+p.Poll.ID+"/close"); ok {
		t.Fatal("c on someone else's poll sent POST close")
	}
	h.Press("x")
	if _, ok := lastReq(h, "DELETE", "/v1/polls/"+p.Poll.ID+"/vote"); !ok {
		t.Fatal("x sent no DELETE vote")
	}
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays after retract = %d, want the vote overlay still open", n)
	}
	s := overlayText(h)
	if strings.Contains(s, "(•)") || strings.Contains(s, "✓") {
		t.Fatalf("my vote still marked after retract:\n%s", s)
	}
	if c, _ := pollRow(t, s, "국밥"); c != 3 {
		t.Fatalf("국밥 count after retract = %d, want 3:\n%s", c, s)
	}

	mine := myPollPost()
	closed := pollWith(mine, []int{4, 2}, []int{0})
	closed.Closed = true
	srv2 := snsAPI(t, false, pollFirst(mine))
	srv2.JSON("POST /v1/polls/{id}/close", http.StatusOK, map[string]any{"poll": closed})
	h2 := uitest.New(t, uitest.WithAPI(srv2), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	h2.Send(ui.OpenOverlayMsg{ID: "feed.poll_vote", Args: mine.ID})
	h2.Press("c")
	if s := overlayText(h2); !strings.Contains(s, "투표를 마감할까요?") {
		t.Fatalf("no close confirm:\n%s", s)
	}
	h2.Press("y")
	if _, ok := lastReq(h2, "POST", "/v1/polls/"+mine.Poll.ID+"/close"); !ok {
		t.Fatal("c, y sent no POST close")
	}
	if n := len(h2.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays after close = %d, want the vote overlay back", n)
	}
	s = overlayText(h2)
	if !strings.Contains(s, "마감된 투표예요") || strings.Contains(s, "( )") || strings.Contains(s, "(•)") {
		t.Fatalf("closed poll overlay not read-only:\n%s", s)
	}
	h2.Press("x", "enter")
	for _, r := range h2.API.Requests() {
		if strings.HasPrefix(r.Path, "/v1/polls/") && r.Method != "POST" {
			t.Fatalf("closed overlay sent %s %s", r.Method, r.Path)
		}
	}
}

func TestPollClosedConflict(t *testing.T) {
	p := pollPost()
	closed := pollWith(p, []int{4, 2}, []int{0})
	closed.Closed = true
	srv := snsAPI(t, false, pollFirst(p))
	srv.Error("PUT /v1/polls/{id}/vote", http.StatusConflict, "poll_closed")
	srv.JSON("GET /v1/polls/{id}", http.StatusOK, map[string]any{"poll": closed})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	x, y := cellOf(t, h.Screen(), "돈까스")
	h.Click(x+1, y)
	if _, ok := lastReq(h, "PUT", "/v1/polls/"+p.Poll.ID+"/vote"); !ok {
		t.Fatal("click sent no PUT vote")
	}
	if !strings.Contains(toastText(h), "마감된투표예요") {
		t.Fatalf("no poll_closed toast:\n%s", h.Screen())
	}
	if _, ok := lastReq(h, "GET", "/v1/polls/"+p.Poll.ID); !ok {
		t.Fatal("poll_closed did not refresh with GET /v1/polls/<id>")
	}
	if s := h.Screen(); !strings.Contains(s, "(마감)") {
		t.Fatalf("refreshed poll not shown closed:\n%s", s)
	}
	// A closed poll answers the next click locally.
	n := len(reqsTo(h, "PUT", "/v1/polls/"+p.Poll.ID+"/vote"))
	x, y = cellOf(t, h.Screen(), "국밥")
	h.Click(x+1, y)
	if m := len(reqsTo(h, "PUT", "/v1/polls/"+p.Poll.ID+"/vote")); m != n {
		t.Fatal("click on a closed poll sent another vote")
	}
}

func TestPollUpdatedEventKeepsMine(t *testing.T) {
	// Review Focus 2: poll.updated (my_votes always []) never clears my mark.
	p := pollPost()
	p.Poll.MyVotes = []int{}
	srv := snsAPI(t, false, pollFirst(p))
	srv.JSON("PUT /v1/polls/{id}/vote", http.StatusOK, map[string]any{"poll": pollWith(p, []int{5, 2}, []int{0})})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed),
		uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"}))
	x, y := cellOf(t, h.Screen(), "국밥")
	h.Click(x+1, y)
	if r, ok := lastReq(h, "PUT", "/v1/polls/"+p.Poll.ID+"/vote"); !ok || !slices.Equal(voteBody(t, r.Body), []int{0}) {
		t.Fatal("click on 국밥 did not vote [0]")
	}
	if _, mine := pollRow(t, h.Screen(), "국밥"); !mine {
		t.Fatalf("my vote not marked:\n%s", h.Screen())
	}
	h.Event("poll.updated", map[string]any{"poll": pollWith(p, []int{9, 2}, []int{})})
	s := h.Screen()
	if c, mine := pollRow(t, s, "국밥"); c != 9 || !mine {
		t.Fatalf("after poll.updated 국밥: count=%d mine=%v, want 9 ✓:\n%s", c, mine, s)
	}
	if c, mine := pollRow(t, s, "돈까스"); c != 2 || mine {
		t.Fatalf("after poll.updated 돈까스: count=%d mine=%v:\n%s", c, mine, s)
	}
}

func TestPollVoteOverlayGolden(t *testing.T) {
	// One "WxH" subtest per size holding three goldens
	// (testdata/TestPollVoteOverlayGolden/<WxH>/{single,multi,closed}.golden).
	sizes := append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6})
	mp := multiPollPost()
	cl := myPollPost()
	cl.Poll.Closed = true
	srv := snsAPI(t, false, withFollowing(append([]api.TimelineItem{itemOf(pollPost()), itemOf(mp), itemOf(cl)}, fixtures.Timeline()...)))
	opts := []uitest.Option{uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed), uitest.WithRoute(ui.Route{Kind: ui.RouteFeed, Arg: "following"})}
	for _, s := range sizes {
		t.Run(fmt.Sprintf("%dx%d", s.W, s.H), func(t *testing.T) {
			for _, c := range []struct{ name, args string }{
				{"single", pollPost().ID},
				{"multi", mp.ID + " 1"},
				{"closed", cl.ID},
			} {
				h := uitest.New(t, opts...)
				h.Resize(s.W, s.H)
				h.Send(ui.OpenOverlayMsg{ID: "feed.poll_vote", Args: c.args})
				if n := len(h.Model.Frame().Overlays); n != 1 && s.W >= 44 {
					t.Fatalf("%s: %d overlays", c.name, n)
				}
				h.Golden(c.name)
			}
		})
	}
}
