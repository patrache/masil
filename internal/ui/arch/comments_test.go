package arch

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/collab/collabtest"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func TestAnchorRoundTrip(t *testing.T) {
	for _, a := range []commentAnchor{
		{kind: anchorNode, id: "n_a"},
		{kind: anchorEdge, id: "e_x"},
		{kind: anchorBox, id: "az-a"},
		{kind: anchorAt, x: 12, y: -3},
		{kind: anchorAt, x: 0, y: 0},
	} {
		s := encodeAnchor(a)
		got, ok := decodeAnchor(s)
		if !ok || got != a {
			t.Errorf("%+v → %q → %+v, %v", a, s, got, ok)
		}
	}
	for _, s := range []string{
		"node:" + strings.Repeat("a", 124), // 129 characters
		"", "node:", "node:a b", "edge:é", "box:a/b", "at:1", "at:x,2", "at:+1,2", "at:01,2", "at:1,2,3",
		"at:2000000,1", "n:abc", "abc", "nodes:abc", "NODE:abc",
	} {
		if a, ok := decodeAnchor(s); ok {
			t.Errorf("decodeAnchor(%q) = %+v, want false", s, a)
		}
	}
	if s := encodeAnchor(commentAnchor{}); s != "" {
		t.Errorf("an unknown anchor encodes as %q", s)
	}
}

// commentDoc seeds the pay_v3 design as room document D1 of SpaceNova and
// opens it at 120x40.
func commentDoc(t *testing.T) (*uitest.Harness, *canvasModel, *sharedSink, *collabtest.Hub) {
	t.Helper()
	hub := collabtest.New(t)
	sharedFixture(t, hub, api.Doc{SpaceID: fixtures.SpaceNova, Title: fixtureName})
	h, c, sh := docHarness(t, hub)
	// the room's members (the @ suggestions)
	h.API.JSON("GET /v1/spaces/{space_id}/members", http.StatusOK, map[string]any{"items": []api.SpaceMember{
		{User: api.UserSummary{ID: fixtures.UserHana, Handle: "hana", DisplayName: "하나"}, Role: api.RoleMember},
		{User: api.UserSummary{ID: fixtures.UserMinji, Handle: "minji", DisplayName: "민지"}, Role: api.RoleMember},
	}, "next_cursor": nil})
	waitUntil(t, h, "the comments", func() bool { return c.cmt.loaded })
	return h, c, sh, hub
}

// commentRequests are the recorded requests of method on the comment paths.
func commentRequests(h *uitest.Harness, method string) []apitest.Recorded {
	var out []apitest.Recorded
	for _, r := range h.API.Requests() {
		if r.Method == method && strings.HasPrefix(r.Path, "/v1/docs/D1/comments") {
			out = append(out, r)
		}
	}
	return out
}

// commentEvent delivers doc.comment_created of D1.
func commentEvent(h *uitest.Harness, c api.DocComment) {
	h.T.Helper()
	c.DocID = "D1"
	if c.CreatedAt.IsZero() {
		c.CreatedAt = h.Clock().Now()
	}
	h.Event("doc.comment_created", realtime.DocCommentCreatedEvent{DocID: "D1", Comment: c})
}

func TestMentionAlertOpensDoc(t *testing.T) {
	hub := collabtest.New(t)
	sharedFixture(t, hub, api.Doc{SpaceID: fixtures.SpaceNova, Title: fixtureName})
	h := newHarness(t, uitest.WithCollab(hub))
	h.Resize(120, 40)
	a := api.Alert{ID: "RD1", Kind: api.AlertMention, Title: "멘션", Body: "@sam 확인",
		Target: api.AlertTarget{Type: "doc", ID: "D1", DocType: "arch", SpaceID: strp(fixtures.SpaceNova)}, CreatedAt: h.Clock().Now()}
	h.Event("alert", map[string]any{"alert": a})
	h.Press("enter")
	nav := h.Nav()
	var r *ui.Route
	for i := range nav.Tools {
		if nav.Tools[i].Kind == ui.RouteTool && nav.Tools[i].Arg == ui.ToolArch {
			r = &nav.Tools[i]
		}
	}
	if r == nil || r.ID != keyDoc+"D1" || r.Query[queryDoc] != "D1" {
		t.Fatalf("the alert did not open D1 on the tool surface: %+v", nav.Tools)
	}
	c := canvasOf(t, h, *r)
	if sh := c.shared(); sh != nil {
		t.Cleanup(sh.close)
	}
	waitUntil(t, h, "the comments", func() bool { return c.cmt.loaded })
	if c.mode != modeComments {
		t.Fatalf("the comments panel is not open (mode %d):\n%s", c.mode, h.Screen())
	}
	if !strings.Contains(h.Screen(), h.Context().T.Archcanvas.CommentsTitle(0)) {
		t.Errorf("no comments panel on screen:\n%s", h.Screen())
	}

	// the flag off: the target opens nothing
	t.Setenv("MASIL_FEATURES", "")
	if cmd, ok := ui.AlertTargetCmd(h.Context(), a); !ok || cmd != nil {
		t.Errorf("flag off: AlertTargetCmd = %v, %v; want a registered nil Cmd", cmd != nil, ok)
	}
}

func TestCommentPanelFlow(t *testing.T) {
	h, c, _, _ := commentDoc(t)
	n := c.node("api-a")
	x, y := worldScr(t, h, c, n.Pos[0], n.Pos[1])
	h.Send(tea.MouseClickMsg{X: x + 2, Y: y + 1, Button: tea.MouseRight})
	h.Send(tea.MouseReleaseMsg{X: x + 2, Y: y + 1, Button: tea.MouseRight})
	if c.menu == nil {
		t.Fatal("no menu")
	}
	for c.menu != nil && c.menu.actions[c.menu.sel] != menuComment {
		h.Press("j")
	}
	h.Press("enter")
	if c.mode != modeComments || !c.cmt.composing {
		t.Fatalf("댓글 did not start a comment (mode %d):\n%s", c.mode, h.Screen())
	}
	h.Type("@ha")
	if len(c.cmt.sugg) == 0 || c.cmt.sugg[0].Handle != "hana" {
		t.Fatalf("no @hana suggestion: %+v\n%s", c.cmt.sugg, h.Screen())
	}
	if !strings.Contains(h.Screen(), "@hana") {
		t.Errorf("the suggestion is not shown:\n%s", h.Screen())
	}
	h.Press("enter") // takes the suggestion
	h.Type("확인")
	if got := c.cmt.ti.Value(); got != "@hana 확인" {
		t.Fatalf("input %q", got)
	}
	h.Check()
	h.Press("enter")
	posts := commentRequests(h, "POST")
	if len(posts) != 1 {
		t.Fatalf("%d POSTs", len(posts))
	}
	var body map[string]any
	if err := json.Unmarshal(posts[0].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["anchor"] != "node:api-a" || body["body"] != "@hana 확인" {
		t.Errorf("POST body %s", posts[0].Body)
	}
	if c.commentCounts()["api-a"] != 1 {
		t.Errorf("counts %v", c.commentCounts())
	}
	if !strings.Contains(h.Screen(), "✎1") {
		t.Errorf("no ✎1 on the node:\n%s", h.Screen())
	}
	if !strings.Contains(h.Screen(), "확인") {
		t.Errorf("the comment is not in the panel:\n%s", h.Screen())
	}

	// a reply (enter on the thread) keeps the anchor and names the root
	h.Press("enter")
	h.Type("네")
	h.Press("enter")
	posts = commentRequests(h, "POST")
	if len(posts) != 2 {
		t.Fatalf("%d POSTs after the reply", len(posts))
	}
	var reply map[string]any
	_ = json.Unmarshal(posts[1].Body, &reply)
	if reply["anchor"] != "node:api-a" || reply["root_id"] == nil || reply["root_id"] == "" {
		t.Errorf("reply body %s", posts[1].Body)
	}
	if c.commentCounts()["api-a"] != 2 {
		t.Errorf("counts after the reply %v", c.commentCounts())
	}

	// x resolves the thread: PATCH {resolved:true}, the mark goes
	h.Press("x")
	patches := commentRequests(h, "PATCH")
	if len(patches) != 1 || !strings.Contains(string(patches[0].Body), `"resolved":true`) {
		t.Fatalf("PATCH %+v", patches)
	}
	if c.commentCounts()["api-a"] != 0 || strings.Contains(h.Screen(), "✎2") {
		t.Errorf("a resolved thread still counts:\n%s", h.Screen())
	}
	h.Press("esc")
	if c.mode != modeNormal {
		t.Errorf("esc left mode %d", c.mode)
	}
	h.Check()
}

func TestCommentKeyOnEmptySpot(t *testing.T) {
	h, c, _, _ := commentDoc(t)
	h.Press("C")
	if !c.cmt.composing {
		t.Fatalf("C did not start a comment:\n%s", h.Screen())
	}
	a, ok := decodeAnchor(c.cmt.anchor)
	if !ok || a.kind != anchorAt {
		t.Fatalf("anchor %q, want a spot", c.cmt.anchor)
	}
	h.Type("여기에 캐시")
	h.Press("enter")
	if posts := commentRequests(h, "POST"); len(posts) != 1 || !strings.Contains(string(posts[0].Body), `"anchor":"at:`) {
		t.Fatalf("POSTs %+v", posts)
	}
	if !strings.Contains(h.Screen(), h.Context().T.Archcanvas.CommentsGroupAt(a.x, a.y)) {
		t.Errorf("no spot group:\n%s", h.Screen())
	}
}

func TestCommentEventUpdatesBadge(t *testing.T) {
	h, c, _, _ := commentDoc(t)
	gets := len(commentRequests(h, "GET"))
	commentEvent(h, api.DocComment{ID: "K9", Anchor: "node:api-a", Author: api.UserSummary{ID: fixtures.UserHana, Handle: "hana"}, Body: "봐 주세요"})
	if got := c.commentCounts()["api-a"]; got != 1 {
		t.Fatalf("count %d after the event", got)
	}
	if !strings.Contains(h.Screen(), "✎1") {
		t.Errorf("no ✎1:\n%s", h.Screen())
	}
	// the same comment again (my own POST answer and its event) counts once
	commentEvent(h, api.DocComment{ID: "K9", Anchor: "node:api-a", Author: api.UserSummary{ID: fixtures.UserHana}, Body: "봐 주세요"})
	commentEvent(h, api.DocComment{ID: "K10", Anchor: "node:api-a", Author: api.UserSummary{ID: fixtures.UserJunho}, Body: "저도"})
	if got := c.commentCounts()["api-a"]; got != 2 {
		t.Errorf("count %d, want 2", got)
	}
	// another document's comment is not ours
	h.Event("doc.comment_created", realtime.DocCommentCreatedEvent{DocID: "D2", Comment: api.DocComment{ID: "K11", DocID: "D2", Anchor: "node:api-a", Body: "x", CreatedAt: h.Clock().Now()}})
	if got := c.commentCounts()["api-a"]; got != 2 {
		t.Errorf("another document's comment counted: %d", got)
	}
	if n := len(commentRequests(h, "GET")); n != gets {
		t.Errorf("the events refetched the comments (%d → %d GETs)", gets, n)
	}
}

func TestOrphanComments(t *testing.T) {
	h, c, _, hub := commentDoc(t)
	commentEvent(h, api.DocComment{ID: "K1", Anchor: "node:api-b", Author: api.UserSummary{ID: fixtures.UserHana}, Body: "지울 노드"})
	commentEvent(h, api.DocComment{ID: "K2", Anchor: "zzz", Author: api.UserSummary{ID: fixtures.UserHana}, Body: "어디였지"})
	commentEvent(h, api.DocComment{ID: "K3", Anchor: "node:api-a", Author: api.UserSummary{ID: fixtures.UserHana}, Body: "남는 노드"})
	if c.commentCounts()["api-b"] != 1 {
		t.Fatalf("counts %v", c.commentCounts())
	}
	hub.Inject("D1", fixtures.UserHana, []collab.Op{{Op: collab.OpDel, ID: "n:api-b"}})
	waitUntil(t, h, "the remote delete", func() bool { return c.node("api-b") == nil })
	h.Press("C", "esc") // the panel, no new comment
	if c.mode != modeComments {
		t.Fatalf("mode %d", c.mode)
	}
	a := h.Context().T.Archcanvas
	screen := h.Screen()
	del := strings.Index(screen, a.DeletedNodeComment())
	if del < 0 {
		t.Fatalf("no %q group:\n%s", a.DeletedNodeComment(), screen)
	}
	if i := strings.Index(screen, "지울 노드"); i < del {
		t.Errorf("the orphan comment is not under its group:\n%s", screen)
	}
	unk := strings.Index(screen, a.CommentsGroupUnknown())
	if unk < 0 || strings.Index(screen, "어디였지") < unk {
		t.Errorf("the unknown anchor is not under %q:\n%s", a.CommentsGroupUnknown(), screen)
	}
	if strings.Index(screen, "남는 노드") > del {
		t.Errorf("a live node's comment is listed as an orphan:\n%s", screen)
	}
	if _, ok := c.commentCounts()["api-b"]; ok {
		t.Error("a deleted node keeps a count")
	}
	h.Check()
}

func TestCommentHostileText(t *testing.T) {
	h, c, _, _ := commentDoc(t)
	commentEvent(h, api.DocComment{ID: "K1", Anchor: "node:api-a",
		Author: api.UserSummary{ID: "U_EVIL", Handle: "evil", DisplayName: "\x1b]0;pwn\x07악\u202E당"},
		Body:   "\x1b[31m빨강\x1b[0m \u202Eevil\u200F 끝\n\t줄"})
	h.Press("C", "esc")
	if c.mode != modeComments {
		t.Fatalf("mode %d", c.mode)
	}
	screen := h.Screen()
	if strings.ContainsAny(screen, "\x1b\u202e\u200f\x07") {
		t.Errorf("control characters on screen:\n%q", screen)
	}
	if !strings.Contains(screen, "빨강") || !strings.Contains(screen, "악") {
		t.Errorf("the hostile comment lost its text:\n%s", screen)
	}
	h.Check()
	for _, sz := range []uitest.Size{{W: 200, H: 60}, {W: 80, H: 24}, {W: 60, H: 24}} {
		h.Resize(sz.W, sz.H)
		h.Check()
	}
}

func TestCommentsOnPersonalDesign(t *testing.T) {
	r := toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
	h := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	h.Resize(120, 40)
	c := canvasOf(t, h, r)
	h.Press("C")
	if c.mode == modeComments || c.cmt.composing {
		t.Error("a personal design took a comment")
	}
	if !shown(h.Screen(), h.Context().T.Archcanvas.CommentsPersonal()) {
		t.Errorf("no notice:\n%s", h.Screen())
	}
}

// TestMentionsRoomMembersOnly: @ suggests the room's members only — every
// page of them — and nobody until they load (a failed list is asked again
// the next time the comments open).
func TestMentionsRoomMembersOnly(t *testing.T) {
	h, c, _, _ := commentDoc(t)
	h.API.Error("GET /v1/spaces/{space_id}/members", http.StatusInternalServerError, "internal")
	h.Press("C")
	h.Type("@ha")
	if len(c.cmt.sugg) != 0 {
		t.Fatalf("suggestions before the members loaded: %+v", c.cmt.sugg)
	}
	h.Press("esc", "esc")

	member := func(id, handle string) api.SpaceMember {
		return api.SpaceMember{User: api.UserSummary{ID: id, Handle: handle}, Role: api.RoleMember}
	}
	h.API.Handle("GET /v1/spaces/{space_id}/members", func(w http.ResponseWriter, r *http.Request) {
		page := map[string]any{"items": []api.SpaceMember{member(fixtures.UserMinji, "minji")}, "next_cursor": "p2"}
		if r.URL.Query().Get("cursor") == "p2" {
			page = map[string]any{"items": []api.SpaceMember{member(fixtures.UserHana, "hana")}, "next_cursor": nil}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(page)
	})
	h.Press("C")
	waitUntil(t, h, "the members", func() bool { return len(c.cmt.members) == 2 })
	h.Type("@ha")
	if len(c.cmt.sugg) != 1 || c.cmt.sugg[0].Handle != "hana" {
		t.Errorf("@ha (second page) = %+v", c.cmt.sugg)
	}
	h.Press("backspace", "backspace")
	h.Type("ju") // 준호 is in the store but not a member
	if len(c.cmt.sugg) != 0 {
		t.Errorf("@ju suggests a non-member: %+v", c.cmt.sugg)
	}
}
