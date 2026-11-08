package fun

// ui/feed cannot be imported here (CA 2.2), so the dialog is opened directly
// with ui.OpenOverlayMsg, as the profile's guestbook tab does.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func writeOpts(t *testing.T) []uitest.Option {
	t.Helper()
	return []uitest.Option{uitest.WithAPI(funAPI(t)), uitest.WithSeed(fixtures.Seed)}
}

func openWrite(h *uitest.Harness, ownerID string) {
	h.Send(ui.OpenOverlayMsg{ID: "fun.guestbook_write", Args: ownerID})
}

func TestGuestbookWriteSubmits(t *testing.T) {
	srv := funAPI(t)
	hanaID := userOf("hana").ID
	entry := api.GuestbookEntry{ID: sid(56), OwnerID: hanaID, Author: ptr(userOf("sam")), Body: "놀러 왔어요", IsSecret: true, CreatedAt: fixtureNow}
	srv.Handle("POST /v1/users/{user}/guestbook", func(w http.ResponseWriter, r *http.Request) {
		writeStatus(w, http.StatusCreated, map[string]any{"entry": entry})
	})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed))
	h.Resize(120, 40)
	openWrite(h, hanaID)
	if s := overlayText(h); !strings.Contains(s, "방명록 남기기 · @hana") || !strings.Contains(s, "[ ] 비밀글로 남기기") {
		t.Fatalf("no guestbook dialog:\n%s", s)
	}
	h.Type("놀러 왔어요")
	h.Press("tab", "space")
	if s := overlayText(h); !strings.Contains(s, "[x] 비밀글로 남기기") {
		t.Fatalf("space did not toggle secret:\n%s", s)
	}
	h.Press("tab", "enter")
	r, ok := lastReq(h, "POST", "/v1/users/"+hanaID+"/guestbook")
	if !ok {
		t.Fatal("enter sent no POST /v1/users/<hana>/guestbook")
	}
	body := postBody(t, r)
	if body["body"] != "놀러 왔어요" || body["is_secret"] != true || len(body) != 2 {
		t.Fatalf("POST body %s, want {body:놀러 왔어요, is_secret:true}", r.Body)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("dialog still open (%d overlays)", n)
	}
	if !strings.Contains(toastText(h), squash("방명록을 남겼어요")) {
		t.Fatalf("no written toast:\n%s", h.Screen())
	}
}

func TestGuestbookWritePublic(t *testing.T) {
	srv := funAPI(t)
	hanaID := userOf("hana").ID
	srv.Handle("POST /v1/users/{user}/guestbook", func(w http.ResponseWriter, r *http.Request) {
		writeStatus(w, http.StatusCreated, map[string]any{"entry": api.GuestbookEntry{ID: sid(57), OwnerID: hanaID, Author: ptr(userOf("sam")), Body: "안녕", CreatedAt: fixtureNow}})
	})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed))
	h.Resize(120, 40)
	openWrite(h, hanaID)
	h.Type("안녕")
	h.Press("enter")
	r, ok := lastReq(h, "POST", "/v1/users/"+hanaID+"/guestbook")
	if !ok {
		t.Fatal("no POST guestbook")
	}
	if body := postBody(t, r); body["body"] != "안녕" {
		t.Fatalf("POST body %s", r.Body)
	} else if _, has := body["is_secret"]; has {
		t.Fatalf("POST body %s sends is_secret for a public entry", r.Body)
	}
}

func TestGuestbookWriteEmptyAndTooLong(t *testing.T) {
	h := uitest.New(t, writeOpts(t)...)
	h.Resize(120, 40)
	hanaID := userOf("hana").ID
	openWrite(h, hanaID)
	h.Press("enter")
	if !strings.Contains(toastText(h), squash("내용을 입력해 주세요")) {
		t.Fatalf("no empty toast:\n%s", h.Screen())
	}
	h.Paste(strings.Repeat("가", 301))
	if s := overlayText(h); !strings.Contains(s, "301/300") {
		t.Fatalf("no 301/300 counter:\n%s", s)
	}
	h.Press("enter")
	if !strings.Contains(toastText(h), squash("300자를 넘었어요")) {
		t.Fatalf("no too-long toast:\n%s", h.Screen())
	}
	if _, ok := lastReq(h, "POST", "/v1/users/"+hanaID+"/guestbook"); ok {
		t.Fatal("an invalid entry was sent")
	}
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want the dialog kept", n)
	}
}

func TestGuestbookWritePrivateError(t *testing.T) {
	for _, code := range []string{"private_account", "action_not_allowed"} {
		t.Run(code, func(t *testing.T) {
			srv := funAPI(t)
			srv.Handle("POST /v1/users/{user}/guestbook", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, `{"error":{"code":%q,"message":%q,"details":{}}}`, code, code)
			})
			h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed))
			h.Resize(120, 40)
			hanaID := userOf("hana").ID
			openWrite(h, hanaID)
			h.Type("놀러 왔어요")
			h.Press("enter")
			if _, ok := lastReq(h, "POST", "/v1/users/"+hanaID+"/guestbook"); !ok {
				t.Fatal("no POST guestbook")
			}
			want := api.Describe(&api.Error{Code: code})
			if !strings.Contains(toastText(h), squash(want)) {
				t.Fatalf("no %q toast:\n%s", want, h.Screen())
			}
			if n := len(h.Model.Frame().Overlays); n != 1 {
				t.Fatalf("overlays = %d, want the dialog kept", n)
			}
			if s := overlayText(h); !strings.Contains(s, "놀러 왔어요") {
				t.Fatalf("draft lost after the error:\n%s", s)
			}
		})
	}
}

func TestGuestbookWriteCursor(t *testing.T) {
	h := uitest.New(t, writeOpts(t)...)
	h.Resize(120, 40)
	openWrite(h, userOf("hana").ID)
	if h.Cursor() == nil {
		t.Fatal("no real cursor in the input")
	}
	h.Press("tab")
	if h.Cursor() != nil {
		t.Fatal("cursor shown while the toggle has the focus")
	}
}

func TestGuestbookWriteOverlayGolden(t *testing.T) {
	// testdata/TestGuestbookWriteOverlayGolden/<WxH>/write.golden
	sizes := append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6})
	sweep(t, sizes, "write", writeOpts(t), func(h *uitest.Harness) {
		openWrite(h, userOf("hana").ID)
		h.Type("놀러 왔어요")
	})
}
