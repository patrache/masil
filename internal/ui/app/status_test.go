package app_test

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// statusRow is the last screen row (the status line).
func statusRow(h *uitest.Harness) string {
	lines := strings.Split(h.Screen(), "\n")
	return lines[len(lines)-1]
}

// rateLimit makes POST /v1/channels/{id}/messages answer 429 for limiter with
// retry_after_sec = secs, then sends one message through the app's own client so the
// cooldown is recorded on the harness clock.
func rateLimit(t *testing.T, h *uitest.Harness, limiter string, secs int) {
	t.Helper()
	h.API.Handle("POST /v1/channels/{channel_id}/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down","details":{"limiter":"` + limiter + `","retry_after_sec":` + strconv.Itoa(secs) + `}}}`))
	})
	_, _, err := h.Context().API.SendMessage(context.Background(), fixtures.ChDev, api.SendMessageRequest{ClientNonce: "n1", Kind: "text", Body: "hi"})
	if api.CodeOf(err) != "rate_limited" {
		t.Fatalf("SendMessage err = %v, want rate_limited", err)
	}
}

func TestStatusRightItems(t *testing.T) {
	h := uitest.New(t)
	h.Resize(120, 40)
	now := h.Clock().Now()

	// Focus: remaining mm:ss, re-rendered each second through ui.After.
	h.Event("focus.started", realtime.FocusStarted{Focus: api.Focus{StartedAt: now, EndsAt: now.Add(18*time.Minute + 20*time.Second)}})
	if s := statusRow(h); !strings.Contains(s, "🎯 18:20") {
		t.Fatalf("focus: status = %q, want 🎯 18:20", s)
	}
	if !slices.Contains(h.Clock().Pending(), time.Second) {
		t.Fatalf("focusing must arm a 1s status tick, pending = %v", h.Clock().Pending())
	}
	h.Advance(time.Second)
	if s := statusRow(h); !strings.Contains(s, "🎯 18:19") {
		t.Fatalf("focus +1s: status = %q, want 🎯 18:19", s)
	}
	if n := len(h.Clock().Pending()); n != 1 {
		t.Fatalf("exactly one status tick must stay armed while focusing, pending = %v", h.Clock().Pending())
	}

	// 429 cooldown: the longest remaining limiter, seconds rounded up.
	rateLimit(t, h, "RL_MESSAGE", 12)
	h.Resize(120, 40)
	if s := statusRow(h); !strings.Contains(s, "⏳ 12초") {
		t.Fatalf("cooldown: status = %q, want ⏳ 12초", s)
	}
	h.Advance(2 * time.Second)
	if s := statusRow(h); !strings.Contains(s, "⏳ 10초") {
		t.Fatalf("cooldown +2s: status = %q, want ⏳ 10초", s)
	}
	if s := statusRow(h); !strings.Contains(s, "🎯 18:17 · ⏳ 10초 · ● 온라인") {
		t.Fatalf("right items order: status = %q", s)
	}
	h.Advance(10 * time.Second)
	if s := statusRow(h); strings.Contains(s, "⏳") {
		t.Fatalf("cooldown over: status = %q still shows ⏳", s)
	}

	// Focus over => no 🎯 and the tick stops re-arming.
	h.Event("focus.ended", map[string]any{"summary": map[string]any{}})
	h.Advance(2 * time.Second)
	if s := statusRow(h); strings.Contains(s, "🎯") {
		t.Fatalf("focus ended: status = %q still shows 🎯", s)
	}
	if p := h.Clock().Pending(); len(p) != 0 {
		t.Fatalf("no focus/cooldown => no status tick, pending = %v", p)
	}

	// Syncing.
	app.SetRealtimeForTest(h.Model, &fakeRT{st: realtime.State{Phase: realtime.PhaseSyncing}})
	if s := statusRow(h); !strings.Contains(s, "◌ 동기화 중…") {
		t.Fatalf("syncing: status = %q", s)
	}
	h.Check()
}

func TestStatusPinnedItem(t *testing.T) {
	h := uitest.New(t, uitest.WithPin(layout.ModeStandard))
	h.Resize(120, 40)
	if s := statusRow(h); !strings.Contains(s, "📌 · ● 온라인") {
		t.Fatalf("pinned: status = %q, want 📌 before the connection item", s)
	}
	h.Check()
}

func TestStatusNarrowKeepsConnectionOnly(t *testing.T) {
	h := uitest.New(t, uitest.WithPin(layout.ModeWidget), uitest.WithRoute(devRoute))
	h.Resize(50, 24)
	now := h.Clock().Now()
	h.Event("focus.started", realtime.FocusStarted{Focus: api.Focus{StartedAt: now, EndsAt: now.Add(25 * time.Minute)}})
	rateLimit(t, h, "RL_POST", 30)
	h.Press("i") // the stub chat captures input => the long input hints
	if got := h.Context().Mode; got != layout.ModeWidget {
		t.Fatalf("mode = %v, want widget", got)
	}
	s := statusRow(h)
	if !strings.HasSuffix(strings.TrimRight(s, " "), "● 온라인") {
		t.Fatalf("narrow: status = %q, want the connection item flush right", s)
	}
	for _, gone := range []string{"🎯", "⏳", "📌"} {
		if strings.Contains(s, gone) {
			t.Fatalf("narrow: status = %q must drop %s", s, gone)
		}
	}
	if !strings.Contains(s, "…") || !strings.Contains(s, "enter 보내기") {
		t.Fatalf("narrow: status = %q, want the input hints cut with …", s)
	}
	for i, l := range strings.Split(h.Screen(), "\n") {
		if w := ansi.StringWidth(l); w != 50 {
			t.Fatalf("line %d width %d, want 50", i, w)
		}
	}
	h.Check()
}

func TestPinFallbackHint(t *testing.T) {
	h := uitest.New(t, uitest.WithPin(layout.ModeCommand))
	h.Resize(120, 40)
	if got := h.Context().Mode; got != layout.ModeStandard {
		t.Fatalf("mode = %v, want standard (pin does not fit)", got)
	}
	s := statusRow(h)
	if !strings.Contains(s, "📌 지휘소 고정 · 창이 작아 표준으로 표시") {
		t.Fatalf("status = %q, want the pin fallback notice", s)
	}
	if strings.Contains(s, "📌 · ") {
		t.Fatalf("status = %q: a pin that did not apply must not show the 📌 item", s)
	}
	h.Check()

	h.Resize(200, 60) // the pin fits now
	s = statusRow(h)
	if strings.Contains(s, "창이 작아") || !strings.Contains(s, "📌 · ● 온라인") {
		t.Fatalf("pin applied: status = %q", s)
	}
}
