package feed

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// guestbookRoute is RouteProfile id opened on the 방명록 tab.
func guestbookRoute(id string) ui.Route {
	return ui.Route{Kind: ui.RouteProfile, ID: id, Query: map[string]string{"tab": "guestbook"}}
}

// guestbookOptsWith opens id's guestbook tab on srv.
func guestbookOptsWith(srv *apitest.Server, id string, hostile bool) []uitest.Option {
	opts := profileOptsWith(srv, id, hostile)
	return append(opts, uitest.WithRoute(guestbookRoute(id)))
}

// hostileGuestbook is one entry per hostile timeline post (author and body),
// alternating secret, plus a hidden and a deleted entry.
func hostileGuestbook(ownerID string) []api.GuestbookEntry {
	var out []api.GuestbookEntry
	for i, it := range fixtures.HostileTimeline() {
		out = append(out, api.GuestbookEntry{
			ID: sid(200 + i), OwnerID: ownerID, Author: ptr(it.Post.Author), Body: it.Post.Body,
			IsSecret: i%2 == 1, CreatedAt: at(i),
		})
	}
	return append(out,
		api.GuestbookEntry{ID: sid(250), OwnerID: ownerID, IsSecret: true, Hidden: true, CreatedAt: at(30)},
		api.GuestbookEntry{ID: sid(251), OwnerID: ownerID, Deleted: true, CreatedAt: at(31)},
	)
}

func TestGuestbookTabGolden(t *testing.T) {
	// testdata/TestGuestbookTabGolden/<WxH>/hana.golden
	sweep(t, uitest.AllSizes(), "hana", guestbookOptsWith(profileSrv(t, false, nil), "@hana", false), nil)
}

func TestGuestbookTabHostile(t *testing.T) {
	srv := profileSrv(t, true, nil)
	owner := profileUsers(true)[hostileProfileHandle].ID
	srv.JSON("GET /v1/users/{user}/guestbook", http.StatusOK, pageOf(hostileGuestbook(owner)))
	sweep(t, uitest.AllSizes(), "", guestbookOptsWith(srv, "@"+hostileProfileHandle, true), nil)
}

func TestGuestbookTabShowsEntries(t *testing.T) {
	h := uitest.New(t, guestbookOptsWith(profileSrv(t, false, nil), "@hana", false)...)
	h.Resize(120, 40)
	if _, ok := lastReq(h, "GET", "/v1/users/"+userOf("hana").ID+"/guestbook"); !ok {
		t.Fatal("guestbook tab sent no GET /v1/users/<hana>/guestbook")
	}
	s := h.Screen()
	for _, want := range []string{"@minji", "놀러 왔어요 ✨", "누군가", "🔒 비밀글", "비밀글이에요", "@sam", "또 올게요", "3분"} {
		if !strings.Contains(s, want) {
			t.Fatalf("guestbook tab lacks %q:\n%s", want, s)
		}
	}
	if strings.Index(s, "놀러 왔어요") > strings.Index(s, "또 올게요") {
		t.Fatalf("entries out of server order:\n%s", s)
	}
}

func TestGuestbookTabEmpty(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.JSON("GET /v1/users/{user}/guestbook", http.StatusOK, pageOf([]api.GuestbookEntry{}))
	h := uitest.New(t, guestbookOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	if s := h.Screen(); !strings.Contains(s, "아직 방명록이 비어 있어요") || !strings.Contains(s, "w 로 첫 글을 남겨 보세요") {
		t.Fatalf("empty guestbook:\n%s", s)
	}
}

func TestGuestbookTabKeyLoads(t *testing.T) {
	h := uitest.New(t, profileOpts(t, "@hana", false, nil)...)
	h.Resize(120, 40)
	path := "/v1/users/" + userOf("hana").ID + "/guestbook"
	if _, ok := lastReq(h, "GET", path); ok {
		t.Fatal("the guestbook loaded before its tab was opened")
	}
	h.Press("4")
	if _, ok := lastReq(h, "GET", path); !ok {
		t.Fatal("4 sent no GET guestbook")
	}
	if s := h.Screen(); !strings.Contains(s, "놀러 왔어요") {
		t.Fatalf("guestbook tab:\n%s", s)
	}
	n := len(reqsTo(h, "GET", path))
	h.Press("1", "4")
	if got := len(reqsTo(h, "GET", path)); got != n {
		t.Fatalf("an already loaded guestbook refetched (%d -> %d)", n, got)
	}
}

func TestGuestbookHidesDeleted(t *testing.T) {
	srv := profileSrv(t, false, nil)
	entries := guestbookEntries(userOf("hana").ID)
	entries = append(entries, api.GuestbookEntry{ID: sid(53), OwnerID: userOf("hana").ID, Author: ptr(userOf("junho")), Deleted: true, CreatedAt: at(90)})
	srv.JSON("GET /v1/users/{user}/guestbook", http.StatusOK, pageOf(entries))
	h := uitest.New(t, guestbookOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	if s := h.Screen(); strings.Contains(s, "@junho") {
		t.Fatalf("deleted entry drawn:\n%s", s)
	}
}

func TestGuestbookHidesBlocked(t *testing.T) {
	h := uitest.New(t, guestbookOptsWith(profileSrv(t, false, nil), "@hana", false)...)
	h.Resize(120, 40)
	h.Event("block.updated", map[string]any{"user_id": userOf("minji").ID, "blocked": true})
	if s := h.Screen(); strings.Contains(s, "놀러 왔어요") || !strings.Contains(s, "또 올게요") {
		t.Fatalf("blocked minji's entry still listed:\n%s", s)
	}
}

func TestGuestbookDeleteConfirm(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.JSON("DELETE /v1/guestbook/{id}", http.StatusNoContent, nil)
	h := uitest.New(t, guestbookOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	// minji's entry (selected first) is neither mine nor on my guestbook.
	h.Press("d")
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("d on someone else's entry opened %d overlays:\n%s", n, overlayText(h))
	}
	h.Press("j", "j") // my own entry (sid 52)
	h.Press("d")
	if s := overlayText(h); !strings.Contains(s, "방명록 글을 지울까요?") {
		t.Fatalf("no delete confirm:\n%s", s)
	}
	if _, ok := lastReq(h, "DELETE", "/v1/guestbook/"+sid(52)); ok {
		t.Fatal("deleted before the confirm")
	}
	h.Press("y")
	if _, ok := lastReq(h, "DELETE", "/v1/guestbook/"+sid(52)); !ok {
		t.Fatal("y sent no DELETE /v1/guestbook/<id>")
	}
	if s := h.Screen(); strings.Contains(s, "또 올게요") || !strings.Contains(s, "놀러 왔어요") {
		t.Fatalf("deleted row still shown:\n%s", s)
	}
}

func TestGuestbookOwnerDeletes(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.JSON("DELETE /v1/guestbook/{id}", http.StatusNoContent, nil)
	h := uitest.New(t, guestbookOptsWith(srv, "@me", false)...)
	h.Resize(120, 40)
	h.Press("d") // minji's entry on my own guestbook
	if s := overlayText(h); !strings.Contains(s, "방명록 글을 지울까요?") {
		t.Fatalf("owner: no delete confirm:\n%s", s)
	}
	h.Press("y")
	if _, ok := lastReq(h, "DELETE", "/v1/guestbook/"+sid(50)); !ok {
		t.Fatal("owner: no DELETE /v1/guestbook/<id>")
	}
}

func TestGuestbookReport(t *testing.T) {
	srv := profileSrv(t, false, nil)
	srv.JSON("POST /v1/reports", http.StatusCreated, map[string]string{"report_id": sid(99)})
	h := uitest.New(t, guestbookOptsWith(srv, "@hana", false)...)
	h.Resize(120, 40)
	h.Press("m")
	if s := overlayText(h); !strings.Contains(s, "신고") || !strings.Contains(s, "민지의 방명록 글") {
		t.Fatalf("no guestbook report dialog:\n%s", s)
	}
	h.Press("tab", "enter")
	r, ok := lastReq(h, "POST", "/v1/reports")
	if !ok {
		t.Fatal("report dialog sent no POST /v1/reports")
	}
	body := postBody(t, r)
	if body["target_type"] != "guestbook_entry" || body["target_id"] != sid(50) {
		t.Fatalf("report body %s, want target guestbook_entry %s", r.Body, sid(50))
	}
}

func TestGuestbookWriteKey(t *testing.T) {
	// ui/fun owns fun.guestbook_write (and cannot be imported here): a stub
	// registered under the same id records the args the tab opens it with.
	t.Cleanup(ui.SnapshotRegistryForTest())
	var got []string
	ui.RegisterOverlay("fun.guestbook_write", func(_ *ui.Context, args string) ui.Overlay {
		got = append(got, args)
		return nil
	})
	h := uitest.New(t, guestbookOptsWith(profileSrv(t, false, nil), "@hana", false)...)
	h.Resize(120, 40)
	h.Press("w")
	if len(got) != 1 || got[0] != userOf("hana").ID {
		t.Fatalf("w opened fun.guestbook_write with %q, want [hana id]", got)
	}
}

func TestGuestbookWrittenResultPrepends(t *testing.T) {
	h := uitest.New(t, guestbookOptsWith(profileSrv(t, false, nil), "@hana", false)...)
	h.Resize(120, 40)
	hanaID := userOf("hana").ID
	e := api.GuestbookEntry{ID: sid(54), OwnerID: hanaID, Author: ptr(userOf("sam")), Body: "새로 남긴 글", CreatedAt: fixtureNow}
	h.Send(ui.ResultMsg[*api.GuestbookEntry]{Tag: "fun.guestbook_write:" + hanaID, Value: &e})
	s := h.Screen()
	i, j := strings.Index(s, "새로 남긴 글"), strings.Index(s, "놀러 왔어요")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("written entry not first:\n%s", s)
	}
	// Someone else's guestbook result changes nothing here.
	other := api.GuestbookEntry{ID: sid(55), OwnerID: userOf("minji").ID, Author: ptr(userOf("sam")), Body: "다른 방명록", CreatedAt: fixtureNow}
	h.Send(ui.ResultMsg[*api.GuestbookEntry]{Tag: "fun.guestbook_write:" + userOf("minji").ID, Value: &other})
	if s := h.Screen(); strings.Contains(s, "다른 방명록") {
		t.Fatalf("another owner's entry shown:\n%s", s)
	}
}

// My guestbook tab picks up a new entry live: the owner's only signal is
// notification.created{type guestbook_entry} (api.md 5.20), which refreshes the
// first page and keeps the older ones (M4 gate: bob wrote in alice's guestbook
// and alice's open tab stayed empty). Someone else's guestbook ignores it — that
// notification is always about mine.
func TestGuestbookLiveOnMyNotification(t *testing.T) {
	for _, c := range []struct {
		name, handle string
		reload       bool
	}{{"mine", "sam", true}, {"theirs", "hana", false}} {
		t.Run(c.name, func(t *testing.T) {
			srv := profileSrv(t, false, nil)
			owner := userOf(c.handle).ID
			entries := guestbookEntries(owner)
			fresh := api.GuestbookEntry{ID: sid(260), OwnerID: owner, Author: ptr(userOf("minji")), Body: "실시간 방명록 🌸", CreatedAt: at(0)}
			var n atomic.Int32
			gets := func() int { return int(n.Load()) }
			srv.Handle("GET /v1/users/{user}/guestbook", func(w http.ResponseWriter, r *http.Request) {
				page := entries
				if n.Add(1) > 1 {
					page = append([]api.GuestbookEntry{fresh}, entries...)
				}
				writeJSON(w, pageOf(page))
			})
			h := uitest.New(t, guestbookOptsWith(srv, "@"+c.handle, false)...)
			h.Resize(120, 40)
			if gets() != 1 || strings.Contains(h.Screen(), "실시간 방명록") {
				t.Fatalf("first load: %d GETs", gets())
			}
			h.Event("notification.created", map[string]any{
				"notification": api.Notification{
					ID: sid(261), Type: "guestbook_entry", Actor: ptr(userOf("minji")),
					Subject: api.NotificationSubject{Type: "guestbook_entry", ID: fresh.ID}, Preview: fresh.Body, CreatedAt: at(0),
				},
				"unread_count": 1,
			})
			s := h.Screen()
			if !c.reload {
				if gets() != 1 || strings.Contains(s, "실시간 방명록") {
					t.Fatalf("someone else's guestbook reloaded on my notification (%d GETs):\n%s", gets(), s)
				}
				return
			}
			if gets() != 2 {
				t.Fatalf("my guestbook: %d GETs after the notification, want 2", gets())
			}
			iNew, iOld := strings.Index(s, "실시간 방명록"), strings.Index(s, "놀러 왔어요")
			if iNew < 0 || iOld < 0 || iNew > iOld {
				t.Fatalf("new entry not on top of the kept ones:\n%s", s)
			}
		})
	}
}
