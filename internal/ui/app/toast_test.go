package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

func TestToastShowsAndExpires(t *testing.T) {
	uitest.ForEachSize(t, uitest.Required, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.ToastMsg{Text: "복사했어요", Kind: ui.ToastSuccess})
		h.Golden("toast") // box top-right of body, bordered, FrameInfo.Toast set
		if h.Context().Mode != layout.ModeTiny {
			tb := h.Model.Frame().Toast
			if tb == nil || !tb.Bordered {
				t.Fatalf("FrameInfo.Toast = %+v, want a bordered box", tb)
			}
			l := h.Model.Frame().Layout
			if tb.Rect.X+tb.Rect.W != l.Cols || tb.Rect.Y != 0 || tb.Rect.W != min(44, l.Cols) || tb.Rect.H != 3 {
				t.Fatalf("toast rect = %+v, want top-right of the body, w=min(44,W), h=3", tb.Rect)
			}
		}
		h.Advance(3 * time.Second)
		if !strings.Contains(h.Screen(), "복사했어요") {
			t.Fatal("toast must still show after 3s")
		}
		h.Advance(time.Second)
		if h.Model.Frame().Toast != nil || strings.Contains(h.Screen(), "복사했어요") {
			t.Fatal("toast must expire after 4s")
		}
	})
}

func TestToastTinyUsesStatusLine(t *testing.T) {
	h := uitest.New(t)
	h.Resize(50, 20)
	if h.Context().Mode != layout.ModeTiny {
		t.Fatalf("mode = %v, want tiny", h.Context().Mode)
	}
	h.Send(ui.ToastMsg{Text: "복사했어요", Kind: ui.ToastSuccess})
	if h.Model.Frame().Toast != nil {
		t.Fatalf("tiny: FrameInfo.Toast = %+v, want nil", h.Model.Frame().Toast)
	}
	if s := statusRow(h); !strings.Contains(s, "복사했어요") {
		t.Fatalf("tiny: status = %q, want the toast text", s)
	}
	if strings.Count(h.Screen(), "복사했어요") != 1 {
		t.Fatal("tiny: the toast must only be on the status line")
	}
	h.Check()
	h.Advance(4 * time.Second)
	if strings.Contains(h.Screen(), "복사했어요") {
		t.Fatal("tiny: toast must expire after 4s")
	}
}

func TestToastStackMax3(t *testing.T) {
	h := uitest.New(t)
	h.Resize(120, 40)
	for i := 1; i <= 5; i++ {
		h.Send(ui.ToastMsg{Text: fmt.Sprintf("알림%d", i), Kind: ui.ToastInfo})
		h.Advance(100 * time.Millisecond)
	}
	scr := h.Screen()
	for i := 1; i <= 5; i++ {
		if want := i >= 3; strings.Contains(scr, fmt.Sprintf("알림%d", i)) != want {
			t.Fatalf("알림%d visible = %v, want %v\n%s", i, !want, want, scr)
		}
	}
	// newest first (top of the box)
	if strings.Index(scr, "알림5") > strings.Index(scr, "알림3") {
		t.Fatalf("newest toast must be on top\n%s", scr)
	}
	if tb := h.Model.Frame().Toast; tb == nil || tb.Rect.H != 5 {
		t.Fatalf("toast box = %+v, want 3 lines + border", tb)
	}
	h.Check()
}

func TestErrorMsgBecomesToast(t *testing.T) {
	h := uitest.New(t)
	h.Resize(120, 40)
	h.Send(ui.ErrorMsg{Err: &api.Error{Status: 404, Code: "not_found"}, Context: "채널"})
	if !strings.Contains(h.Screen(), "✕ 채널: 찾을 수 없어요") {
		t.Fatalf("screen lacks the error toast\n%s", h.Screen())
	}
	h.Check()
	h.Advance(4 * time.Second)
	h.Send(ui.ErrorMsg{Err: &api.Error{Status: 404, Code: "not_found"}})
	scr := h.Screen()
	if !strings.Contains(scr, "✕ 찾을 수 없어요") || strings.Contains(scr, ": 찾을") {
		t.Fatalf("empty Context => Describe only\n%s", scr)
	}
}

func TestToastAlertClickNavigates(t *testing.T) {
	h := uitest.New(t)
	h.Resize(120, 40)
	if got := h.Nav().Tabs[h.Nav().Active].Stack[0].Kind; got == ui.RouteChannel {
		t.Fatalf("precondition: active tab root is already a channel")
	}
	h.Send(ui.ToastMsg{Text: "#개발 새 멘션", Kind: ui.ToastAlert, Route: &ui.Route{Kind: ui.RouteChannel, ID: fixtures.ChDev}})
	tb := h.Model.Frame().Toast
	if tb == nil {
		t.Fatal("no toast box")
	}
	h.Click(tb.Rect.X+tb.Rect.W/2, tb.Rect.Y+1)
	n := h.Nav()
	if r := n.Tabs[n.Active].Stack[0]; r.Kind != ui.RouteChannel || r.ID != fixtures.ChDev {
		t.Fatalf("after click: active tab root = %+v, want channel %s", r, fixtures.ChDev)
	}
	if h.Model.Frame().Toast != nil {
		t.Fatal("a clicked toast is dismissed")
	}
}

func TestToastClickWithoutRouteIsInert(t *testing.T) {
	h := uitest.New(t)
	h.Resize(120, 40)
	before := h.Nav()
	h.Send(ui.ToastMsg{Text: "복사했어요", Kind: ui.ToastSuccess})
	tb := h.Model.Frame().Toast
	h.Click(tb.Rect.X+2, tb.Rect.Y+1)
	if after := h.Nav(); after.Active != before.Active || len(after.Tabs) != len(before.Tabs) || after.Tabs[after.Active].Stack[0].Key() != before.Tabs[before.Active].Stack[0].Key() {
		t.Fatalf("a toast without a route must not navigate: %+v -> %+v", before, after)
	}
}
