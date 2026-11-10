package fun

// Status message and presence mode commands (plan 05 Task 16). The commands run
// through the app's ui.RunCommandMsg dispatch (CR-B), as the chat input's
// /status does.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// statusAPI is funAPI plus PUT/DELETE /v1/me/status (the PUT echoes the body
// back as the UserStatus) and PUT /v1/me/presence (echoes the mode).
func statusAPI(t *testing.T) *apitest.Server {
	t.Helper()
	srv := funAPI(t)
	srv.Handle("PUT /v1/me/status", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Emoji        *string `json:"emoji"`
			Text         string  `json:"text"`
			ExpiresInSec *int    `json:"expires_in_sec"`
		}
		if err := decodeBody(r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		st := api.UserStatus{Emoji: body.Emoji, Text: body.Text}
		if body.ExpiresInSec != nil {
			exp := fixtureNow.Add(time.Duration(*body.ExpiresInSec) * time.Second)
			st.ExpiresAt = &exp
		}
		writeJSON(w, map[string]any{"status": st})
	})
	srv.Handle("DELETE /v1/me/status", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv.Handle("PUT /v1/me/presence", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Mode string `json:"mode"`
		}
		if err := decodeBody(r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		eff := api.PresenceOnline
		switch body.Mode {
		case "away":
			eff = api.PresenceAway
		case "dnd":
			eff = api.PresenceDND
		}
		writeJSON(w, map[string]any{"presence": api.PresenceState{Status: eff, Mode: body.Mode}})
	})
	return srv
}

func decodeBody(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }

func statusHarness(t *testing.T, seed func(*testing.T) []uitest.Option) *uitest.Harness {
	t.Helper()
	opts := []uitest.Option{uitest.WithAPI(statusAPI(t)), uitest.WithSeed(fixtures.Seed)}
	if seed != nil {
		opts = append(opts, seed(t)...)
	}
	h := uitest.New(t, opts...)
	h.Resize(120, 40)
	return h
}

func runCommand(h *uitest.Harness, id, args string) {
	h.Send(ui.RunCommandMsg{ID: id, Args: args})
}

func myStatus(h *uitest.Harness) *api.UserStatus {
	return h.Context().Store.Me.Status
}

func TestParseStatusArgs(t *testing.T) {
	for _, tc := range []struct {
		in, emoji, text string
		hasEmoji, err   bool
	}{
		{in: "🎧 듣는 중", emoji: "🎧", text: "듣는 중", hasEmoji: true},
		{in: "  🏖   휴가 중  ", emoji: "🏖", text: "휴가 중", hasEmoji: true},
		{in: "🎧듣는 중", emoji: "🎧", text: "듣는 중", hasEmoji: true},
		{in: "☕ 커피", emoji: "☕", text: "커피", hasEmoji: true},
		{in: "👍🏽 좋아", emoji: "👍", text: "좋아", hasEmoji: true}, // SanitizeInput drops the skin tone (CA 8.4)
		{in: "🔨", emoji: "🔨", text: "", hasEmoji: true},
		{in: "배포 중", text: "배포 중"},
		{in: "a🎧", text: "a🎧"},
		{in: "줄\n바꿈", text: "줄 바꿈"},
		{in: strings.Repeat("가", 64), text: strings.Repeat("가", 64)},
		{in: "🎧 " + strings.Repeat("가", 64), emoji: "🎧", text: strings.Repeat("가", 64), hasEmoji: true},
		{in: strings.Repeat("가", 65), err: true},
	} {
		emoji, text, err := parseStatusArgs(tc.in)
		if (err != nil) != tc.err {
			t.Fatalf("parseStatusArgs(%q) err = %v, want err %v", tc.in, err, tc.err)
		}
		if tc.err {
			continue
		}
		if emoji.Set != tc.hasEmoji || emoji.Value != tc.emoji || text != tc.text {
			t.Fatalf("parseStatusArgs(%q) = (%+v, %q), want (%q set=%v, %q)", tc.in, emoji, text, tc.emoji, tc.hasEmoji, tc.text)
		}
	}
}

func TestStatusSetArgs(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "status.set", "🎧 듣는 중")
	r, ok := lastReq(h, "PUT", "/v1/me/status")
	if !ok {
		t.Fatal("status.set sent no PUT /v1/me/status")
	}
	body := postBody(t, r)
	if body["emoji"] != "🎧" || body["text"] != "듣는 중" || len(body) != 2 {
		t.Fatalf("PUT body %s, want {emoji:🎧, text:듣는 중}", r.Body)
	}
	st := myStatus(h)
	if st == nil || st.Text != "듣는 중" || st.Emoji == nil || *st.Emoji != "🎧" {
		t.Fatalf("Me.Status = %+v, want 🎧 듣는 중", st)
	}
	me := h.Context().Store.Me
	if u := h.Context().Store.Users[me.ID]; u == nil || u.Status == nil || u.Status.Text != "듣는 중" {
		t.Fatalf("Users[me] = %+v, want the new status observed", u)
	}
	if !strings.Contains(toastText(h), squash("상태를 바꿨어요")) {
		t.Fatalf("no success toast:\n%s", h.Screen())
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("overlays = %d, want none for status.set with args", n)
	}
}

func TestStatusSetTextOnly(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "status.set", "배포 중")
	r, ok := lastReq(h, "PUT", "/v1/me/status")
	if !ok {
		t.Fatal("no PUT /v1/me/status")
	}
	body := postBody(t, r)
	if _, has := body["emoji"]; has {
		t.Fatalf("PUT body %s has an emoji key", r.Body)
	}
	if body["text"] != "배포 중" || len(body) != 1 {
		t.Fatalf("PUT body %s, want {text:배포 중}", r.Body)
	}
	if st := myStatus(h); st == nil || st.Text != "배포 중" || st.Emoji != nil {
		t.Fatalf("Me.Status = %+v", st)
	}
}

func TestStatusSetTooLong(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "status.set", strings.Repeat("가", 65))
	if _, ok := lastReq(h, "PUT", "/v1/me/status"); ok {
		t.Fatal("a 65-character status was sent")
	}
	if !strings.Contains(toastText(h), squash("상태 메시지는 64자까지예요")) {
		t.Fatalf("no too-long toast:\n%s", h.Screen())
	}
	if myStatus(h) != nil {
		t.Fatal("Me.Status changed")
	}
}

func TestStatusSetError(t *testing.T) {
	srv := funAPI(t)
	srv.Handle("PUT /v1/me/status", func(w http.ResponseWriter, r *http.Request) {
		writeStatus(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"code": "invalid_request", "message": "bad", "details": map[string]any{}}})
	})
	h := uitest.New(t, uitest.WithAPI(srv), uitest.WithSeed(fixtures.Seed))
	h.Resize(120, 40)
	runCommand(h, "status.set", "🎧 듣는 중")
	if myStatus(h) != nil {
		t.Fatal("Me.Status set after a failed PUT")
	}
	want := errtext.Describe(i18n.KoT(), &api.Error{Code: "invalid_request"})
	if !strings.Contains(toastText(h), squash(want)) {
		t.Fatalf("no %q toast:\n%s", want, h.Screen())
	}
}

func TestStatusOverlayPresetWithExpiry(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "status.set", "")
	s := overlayText(h)
	for _, want := range []string{"🎧 듣는 중", "🔨 배포 중", "🍚 점심 먹는 중", "🤒 아파요", "🏖 휴가 중", "없음", "30분", "1시간", "4시간", "오늘", "1주"} {
		if !strings.Contains(s, want) {
			t.Fatalf("status dialog lacks %q:\n%s", want, s)
		}
	}
	h.Press("j", "tab", "right", "right", "enter")
	r, ok := lastReq(h, "PUT", "/v1/me/status")
	if !ok {
		t.Fatal("enter sent no PUT /v1/me/status")
	}
	body := postBody(t, r)
	if body["emoji"] != "🔨" || body["text"] != "배포 중" || body["expires_in_sec"] != float64(3600) || len(body) != 3 {
		t.Fatalf("PUT body %s, want {emoji:🔨, text:배포 중, expires_in_sec:3600}", r.Body)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("dialog still open (%d overlays)", n)
	}
	if st := myStatus(h); st == nil || st.Text != "배포 중" || st.ExpiresAt == nil {
		t.Fatalf("Me.Status = %+v", st)
	}
	if !strings.Contains(toastText(h), squash("상태를 바꿨어요")) {
		t.Fatalf("no success toast:\n%s", h.Screen())
	}
}

func TestStatusOverlayExpiryToday(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "status.set", "")
	h.Press("tab", "right", "right", "right", "right", "enter")
	r, ok := lastReq(h, "PUT", "/v1/me/status")
	if !ok {
		t.Fatal("no PUT /v1/me/status")
	}
	// Harness clock 11:05:00 Seoul => today ends 23:59:59, 12h54m59s later.
	want := float64(12*3600 + 54*60 + 59)
	if body := postBody(t, r); body["expires_in_sec"] != want || body["emoji"] != "🎧" {
		t.Fatalf("PUT body %s, want 🎧 with expires_in_sec %v", r.Body, want)
	}
}

func TestStatusOverlayCustom(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "status.set", "")
	// Presets 1-5, then the emoji chip, then the text field.
	h.Press("down", "down", "down", "down", "down")
	h.Press("enter") // chip -> emoji picker
	if n := len(h.Model.Frame().Overlays); n != 2 {
		t.Fatalf("overlays = %d, want the emoji picker on top", n)
	}
	h.Type("coffee")
	h.Press("enter")
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want the picker closed", n)
	}
	h.Press("down")
	if h.Cursor() == nil {
		t.Fatal("no real cursor in the custom text field")
	}
	h.Type("커피 jk 마시는 중")
	if s := overlayText(h); !strings.Contains(s, "☕") || !strings.Contains(s, "커피 jk 마시는 중") {
		t.Fatalf("custom row lacks the picked emoji or the text:\n%s", s)
	}
	h.Press("enter")
	r, ok := lastReq(h, "PUT", "/v1/me/status")
	if !ok {
		t.Fatal("no PUT /v1/me/status")
	}
	if body := postBody(t, r); body["emoji"] != "☕" || body["text"] != "커피 jk 마시는 중" || len(body) != 2 {
		t.Fatalf("PUT body %s", r.Body)
	}
}

func TestStatusOverlayCustomTooLong(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "status.set", "")
	h.Press("down", "down", "down", "down", "down", "down")
	h.Paste(strings.Repeat("가", 65))
	h.Press("enter")
	if _, ok := lastReq(h, "PUT", "/v1/me/status"); ok {
		t.Fatal("a 65-character status was sent")
	}
	if !strings.Contains(toastText(h), squash("상태 메시지는 64자까지예요")) {
		t.Fatalf("no too-long toast:\n%s", h.Screen())
	}
	if n := len(h.Model.Frame().Overlays); n != 1 {
		t.Fatalf("overlays = %d, want the dialog kept", n)
	}
}

func withStatus(t *testing.T) []uitest.Option {
	t.Helper()
	return []uitest.Option{uitest.WithSeed(func(s *store.Store) {
		fixtures.Seed(s)
		e := "🎧"
		s.Me.Status = &api.UserStatus{Emoji: &e, Text: "듣는 중"}
		s.ObserveUser(s.Me.UserSummary)
	})}
}

func TestStatusClear(t *testing.T) {
	h := statusHarness(t, withStatus)
	if myStatus(h) == nil {
		t.Fatal("seeded status missing")
	}
	runCommand(h, "status.clear", "")
	if _, ok := lastReq(h, "DELETE", "/v1/me/status"); !ok {
		t.Fatal("status.clear sent no DELETE /v1/me/status")
	}
	if st := myStatus(h); st != nil {
		t.Fatalf("Me.Status = %+v, want nil", st)
	}
	me := h.Context().Store.Me
	if u := h.Context().Store.Users[me.ID]; u == nil || u.Status != nil {
		t.Fatalf("Users[me] = %+v, want the status cleared", u)
	}
	if !strings.Contains(toastText(h), squash("상태 메시지를 지웠어요")) {
		t.Fatalf("no cleared toast:\n%s", h.Screen())
	}
}

func TestStatusClearUnavailable(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "status.clear", "")
	if _, ok := lastReq(h, "DELETE", "/v1/me/status"); ok {
		t.Fatal("status.clear without a status sent a DELETE")
	}
}

func TestStatusOverlayClear(t *testing.T) {
	h := statusHarness(t, withStatus)
	runCommand(h, "status.set", "")
	if s := overlayText(h); !strings.Contains(s, "지금") || !strings.Contains(s, "듣는 중") {
		t.Fatalf("dialog does not show the current status:\n%s", s)
	}
	h.Press("ctrl+x")
	if _, ok := lastReq(h, "DELETE", "/v1/me/status"); !ok {
		t.Fatal("ctrl+x sent no DELETE /v1/me/status")
	}
	if myStatus(h) != nil {
		t.Fatal("Me.Status not cleared")
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("dialog still open (%d overlays)", n)
	}
}

func TestPresenceSet(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "presence.set", "dnd")
	r, ok := lastReq(h, "PUT", "/v1/me/presence")
	if !ok {
		t.Fatal("presence.set sent no PUT /v1/me/presence")
	}
	if body := postBody(t, r); body["mode"] != "dnd" || len(body) != 1 {
		t.Fatalf("PUT body %s, want {mode:dnd}", r.Body)
	}
	if self := h.Context().Store.Self; self.Mode != "dnd" || self.Status != api.PresenceDND {
		t.Fatalf("Store.Self = %+v, want dnd", self)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("overlays = %d, want none for presence.set with args", n)
	}
}

func TestPresenceSetInvalid(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "presence.set", "busy")
	if _, ok := lastReq(h, "PUT", "/v1/me/presence"); ok {
		t.Fatal("an unknown mode was sent")
	}
	if !strings.Contains(toastText(h), squash("auto · away · dnd 중 하나를 골라 주세요")) {
		t.Fatalf("no invalid-mode toast:\n%s", h.Screen())
	}
}

func TestPresenceOverlay(t *testing.T) {
	h := statusHarness(t, nil)
	runCommand(h, "presence.set", "")
	s := overlayText(h)
	for _, want := range []string{"● 자동", "◐ 자리비움", "⊘ 방해금지", "지금"} {
		if !strings.Contains(s, want) {
			t.Fatalf("presence dialog lacks %q:\n%s", want, s)
		}
	}
	h.Press("j", "enter")
	r, ok := lastReq(h, "PUT", "/v1/me/presence")
	if !ok {
		t.Fatal("enter sent no PUT /v1/me/presence")
	}
	if body := postBody(t, r); body["mode"] != "away" {
		t.Fatalf("PUT body %s, want {mode:away}", r.Body)
	}
	if h.Context().Store.Self.Mode != "away" {
		t.Fatalf("Store.Self = %+v", h.Context().Store.Self)
	}
	if n := len(h.Model.Frame().Overlays); n != 0 {
		t.Fatalf("dialog still open (%d overlays)", n)
	}
}

func overlaySizes() []uitest.Size {
	return append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 44, H: 16}, uitest.Size{W: 20, H: 6})
}

func TestStatusOverlaysGolden(t *testing.T) {
	// testdata/TestStatusOverlaysGolden/{status,presence}/<WxH>/<name>.golden
	opts := []uitest.Option{uitest.WithAPI(statusAPI(t)), uitest.WithSeed(fixtures.Seed)}
	t.Run("status", func(t *testing.T) {
		sweep(t, overlaySizes(), "status", opts, func(h *uitest.Harness) {
			runCommand(h, "status.set", "")
			h.Press("j", "tab", "right", "right")
		})
	})
	t.Run("presence", func(t *testing.T) {
		sweep(t, overlaySizes(), "presence", opts, func(h *uitest.Harness) {
			runCommand(h, "presence.set", "")
		})
	})
}

func TestStatusOverlaysHostile(t *testing.T) {
	seed := func(s *store.Store) {
		fixtures.Seed(s)
		fixtures.Hostile(s)
		e := "👨‍👩‍👧"
		s.Me.Status = &api.UserStatus{Emoji: &e, Text: "\x1b[31m빨강\x1b]8;;http://x\x07링크\r\t" + fixtures.FormatSoup + strings.Repeat("긴", 60)}
	}
	opts := []uitest.Option{uitest.WithAPI(statusAPI(t)), uitest.WithSeed(seed)}
	sizes := append(overlaySizes(), uitest.AllSizes()...)
	t.Run("status", func(t *testing.T) {
		sweep(t, sizes, "", opts, func(h *uitest.Harness) {
			runCommand(h, "status.set", "")
			h.Press("down", "down", "down", "down", "down", "down")
			h.Paste("\x1b[2J" + fixtures.FormatSoup + strings.Repeat("👍🏽", 40))
		})
	})
	t.Run("presence", func(t *testing.T) {
		sweep(t, sizes, "", opts, func(h *uitest.Harness) {
			runCommand(h, "presence.set", "")
		})
	})
}
