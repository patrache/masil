package leet_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/leet"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

const spaceTwo = "01JB00000000000000000000S2"

// solveOK answers PUT /v1/leet/solves/{date}.
func solveOK(h *uitest.Harness) {
	h.API.JSON("PUT /v1/leet/solves/{date}", http.StatusOK, map[string]any{
		"solve": map[string]any{"date": "2026-11-05", "slug": slug1606, "lang": "golang", "frontend_id": "1606", "status": "accepted"},
		"cards": []any{},
	})
}

// accept submits and lets the fake judge say Accepted.
func accept(t *testing.T, e *env, h *uitest.Harness) {
	t.Helper()
	e.lc.QueueCheck(submitID, "check_submit_accepted.json")
	h.Press("s")
	h.Advance(time.Second)
	if s := h.Screen(); !strings.Contains(s, "Accepted") {
		t.Fatalf("not accepted:\n%s", s)
	}
}

// shareReady: today's problem open, signed in, LeetMe with two #daily spaces.
func shareReady(t *testing.T, opts ...uitest.Option) (*env, *uitest.Harness) {
	t.Helper()
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t, opts...)
	leetMe(h, api.LeetMe{Current: 2, DailySpaces: []api.LeetDailySpace{
		{SpaceID: fixtures.SpaceNova, ChannelID: "01JB0000000000000000000DLY"},
		{SpaceID: spaceTwo, ChannelID: "01JB0000000000000000000DL2"},
	}})
	dailyReport(h, "2026-11-05", slug1606)
	solveOK(h)
	signIn(t, e, h)
	openToday(t, h)
	return e, h
}

// solveBodies are the PUT /v1/leet/solves bodies, decoded.
func solveBodies(t *testing.T, h *uitest.Harness) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, b := range apiRequests(h, "PUT", "/v1/leet/solves/2026-11-05") {
		var m map[string]any
		if err := json.Unmarshal([]byte(b), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func TestAcceptedRecordsSolveWithoutCards(t *testing.T) {
	e, h := shareReady(t)
	accept(t, e, h)
	got := solveBodies(t, h)
	if len(got) != 1 {
		t.Fatalf("solve records = %d", len(got))
	}
	b := got[0]
	if _, ok := b["share_space_ids"]; ok {
		t.Fatalf("cards without c: %v", b)
	}
	if _, ok := b["code"]; ok {
		t.Fatalf("code without c: %v", b)
	}
	if b["slug"] != slug1606 || b["lang"] != "golang" || b["frontend_id"] != "1606" || b["submission_id"] != submitID || b["runtime_percentile"] != 85.31 {
		t.Fatalf("record = %v", b)
	}
	if s := h.Screen(); !strings.Contains(s, "c 카드 남기기") {
		t.Fatalf("no share hint:\n%s", s)
	}
}

func TestShareDialogDefaults(t *testing.T) {
	e, h := shareReady(t)
	accept(t, e, h)
	h.Press("c")
	if s := h.Screen(); !strings.Contains(s, "코드도 스포일러로 붙이기") {
		t.Fatalf("no share dialog:\n%s", s)
	}
	h.Press("enter")
	got := solveBodies(t, h)
	if len(got) != 2 {
		t.Fatalf("solve records = %d", len(got))
	}
	ids, _ := got[1]["share_space_ids"].([]any)
	if len(ids) != 2 || ids[0] != fixtures.SpaceNova || ids[1] != spaceTwo {
		t.Fatalf("shared to %v", got[1]["share_space_ids"])
	}
	if _, ok := got[1]["code"]; ok {
		t.Fatalf("code sent by default: %v", got[1])
	}
}

func TestShareWithCode(t *testing.T) {
	e, h := shareReady(t)
	accept(t, e, h)
	h.Press("c")
	if s := h.Screen(); !strings.Contains(overlayText(t, h), compact("코드는 masil 서버에 그대로 저장되고, 이 채널을 읽을 수 있는 사람과 운영자가 볼 수 있어요.")) {
		t.Fatalf("no code warning:\n%s", s)
	}
	h.Press("up", "space") // the code row is the last; up from the button
	h.Press("enter")
	got := solveBodies(t, h)
	if len(got) != 2 {
		t.Fatalf("solve records = %d:\n%s", len(got), h.Screen())
	}
	b, err := os.ReadFile(solutionPath(e))
	if err != nil {
		t.Fatal(err)
	}
	if got[1]["code"] != string(b) {
		t.Fatalf("code = %v, want the file %q", got[1]["code"], b)
	}
}

func TestShareNeverSendsCookies(t *testing.T) {
	e, h := shareReady(t)
	accept(t, e, h)
	h.Press("c", "up", "space", "enter")
	for _, r := range h.API.Requests() {
		for _, v := range []string{"sess-SECRET-abcd", "csrf-XYZ-9876"} {
			if strings.Contains(string(r.Body), v) || strings.Contains(r.Query, v) || strings.Contains(r.Path, v) {
				t.Fatalf("%s %s carries a cookie", r.Method, r.Path)
			}
			for k, hv := range r.Header {
				if strings.Contains(strings.Join(hv, ","), v) {
					t.Fatalf("%s %s header %s carries a cookie", r.Method, r.Path, k)
				}
			}
		}
	}
}

func TestAutoStatusOffNoRequests(t *testing.T) {
	e, h := shareReady(t)
	accept(t, e, h)
	h.Send(ui.CloseToolMsg{InstanceID: "leet:today"})
	for _, m := range []string{"PUT", "DELETE"} {
		if got := apiRequests(h, m, "/v1/me/status"); len(got) != 0 {
			t.Fatalf("%s /v1/me/status with auto_status off: %q", m, got)
		}
	}
}

func TestAutoStatusRestores(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	writeConfig(t, h, "[tools.leet]\nauto_status = true\n")
	coffee := "☕"
	h.Context().Store.Me.Status = &api.UserStatus{Emoji: &coffee, Text: "커피"}
	h.API.Handle("PUT /v1/me/status", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"emoji": b["emoji"], "text": b["text"], "expires_at": nil}})
	})
	dailyReport(h, "2026-11-05", slug1606)
	solveOK(h)
	signIn(t, e, h)
	openToday(t, h)
	got := apiRequests(h, "PUT", "/v1/me/status")
	if len(got) != 1 || !strings.Contains(got[0], `"emoji":"🧩"`) || !strings.Contains(got[0], `"text":"1606 푸는 중"`) {
		t.Fatalf("status at open = %q", got)
	}
	accept(t, e, h)
	got = apiRequests(h, "PUT", "/v1/me/status")
	if len(got) != 2 || !strings.Contains(got[1], `"emoji":"☕"`) || !strings.Contains(got[1], `"text":"커피"`) {
		t.Fatalf("status after accepted = %q", got)
	}
	h.Send(ui.CloseToolMsg{InstanceID: "leet:today"})
	if n := len(apiRequests(h, "PUT", "/v1/me/status")) + len(apiRequests(h, "DELETE", "/v1/me/status")); n != 2 {
		t.Fatalf("restored twice: %d", n)
	}
}

func TestAutoStatusClearsWhenNoneBefore(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	h := uitest.New(t)
	writeConfig(t, h, "[tools.leet]\nauto_status = true\n")
	h.Context().Store.Me.Status = nil
	h.API.JSON("PUT /v1/me/status", http.StatusOK, map[string]any{"status": map[string]any{"emoji": "🧩", "text": "1606 푸는 중", "expires_at": nil}})
	h.API.JSON("DELETE /v1/me/status", http.StatusNoContent, nil)
	openToday(t, h)
	h.Send(ui.CloseToolMsg{InstanceID: "leet:today"})
	if got := apiRequests(h, "DELETE", "/v1/me/status"); len(got) != 1 {
		t.Fatalf("DELETE /v1/me/status = %d", len(got))
	}
}

// writeConfig replaces config.toml and reloads it into the context.
func writeConfig(t *testing.T, h *uitest.Harness, toml string) {
	t.Helper()
	ctx := h.Context()
	if err := os.WriteFile(ctx.Paths.Config, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := leet.ReloadConfigForTest(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestShareGolden(t *testing.T) {
	e := setup(t)
	e.lc.SetDailyDate("2026-11-05")
	uitest.ForEachSize(t, leetSizes, func(t *testing.T, h *uitest.Harness) {
		leetMe(h, api.LeetMe{DailySpaces: []api.LeetDailySpace{{SpaceID: fixtures.SpaceNova, ChannelID: "01JB0000000000000000000DLY"}}})
		solveOK(h)
		signIn(t, e, h)
		openToday(t, h)
		accept(t, e, h)
		h.Press("c")
		h.Check()
		h.Golden("leet_share")
	})
}
