package widgets_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/widgets"
)

func overlayCount(h *uitest.Harness) int { return len(h.Model.Frame().Overlays) }

func openDialog(h *uitest.Harness, o ui.Overlay) {
	h.T.Helper()
	if o == nil {
		h.T.Fatal("dialog constructor returned nil")
	}
	h.Send(ui.OpenOverlayMsg{Overlay: o})
	if overlayCount(h) != 1 {
		h.T.Fatalf("dialog not open: %d overlays", overlayCount(h))
	}
}

func TestConfirmKeys(t *testing.T) {
	const yesToast = "확인 눌렀어요"
	for _, k := range []string{"y", "enter"} {
		t.Run(k, func(t *testing.T) {
			h := uitest.New(t)
			openDialog(h, widgets.Confirm("로그아웃할까요?", "이 기기에서 로그아웃해요.", "로그아웃", "취소", ui.Toast(yesToast, ui.ToastInfo)))
			s := h.Screen()
			for _, want := range []string{"로그아웃할까요?", "이 기기에서 로그아웃해요.", "로그아웃", "취소"} {
				if !strings.Contains(s, want) {
					t.Fatalf("confirm lacks %q:\n%s", want, s)
				}
			}
			h.Check()
			h.Press(k)
			if overlayCount(h) != 0 {
				t.Fatalf("%s: confirm still open", k)
			}
			if !strings.Contains(h.Screen(), yesToast) {
				t.Fatalf("%s: onYes was not run", k)
			}
		})
	}
	for _, k := range []string{"n", "esc"} {
		t.Run(k, func(t *testing.T) {
			h := uitest.New(t)
			openDialog(h, widgets.Confirm("로그아웃할까요?", "이 기기에서 로그아웃해요.", "로그아웃", "취소", ui.Toast(yesToast, ui.ToastInfo)))
			h.Press(k)
			if overlayCount(h) != 0 {
				t.Fatalf("%s: confirm still open", k)
			}
			if strings.Contains(h.Screen(), yesToast) {
				t.Fatalf("%s: onYes ran", k)
			}
		})
	}
	t.Run("select no then enter", func(t *testing.T) {
		h := uitest.New(t)
		openDialog(h, widgets.Confirm("지울까요?", "", "삭제", "취소", ui.Toast(yesToast, ui.ToastInfo)))
		h.Press("right", "enter")
		if overlayCount(h) != 0 || strings.Contains(h.Screen(), yesToast) {
			t.Fatal("enter on the no button must only close")
		}
	})
	t.Run("nil onYes", func(t *testing.T) {
		h := uitest.New(t)
		openDialog(h, widgets.Confirm("지울까요?", "", "삭제", "취소", nil))
		h.Press("y")
		if overlayCount(h) != 0 {
			t.Fatal("nil onYes: y must still close")
		}
	})
}

func TestEmojiPicker(t *testing.T) {
	var picked []string
	onPick := func(e string) tea.Cmd {
		picked = append(picked, e)
		return nil
	}
	h := uitest.New(t)
	openDialog(h, widgets.EmojiPicker(onPick))
	h.Check()
	h.Type("박수")
	if s := h.Screen(); !strings.Contains(s, "👏") || !strings.Contains(s, "박수") {
		t.Fatalf("search 박수:\n%s", s)
	}
	h.Check()
	h.Press("enter")
	if !slices.Equal(picked, []string{"👏"}) {
		t.Fatalf("picked = %q, want 👏", picked)
	}
	if overlayCount(h) != 0 {
		t.Fatal("picking must close the picker")
	}

	openDialog(h, widgets.EmojiPicker(onPick))
	if !strings.Contains(h.Screen(), "최근") {
		t.Fatal("reopened picker has no recent row")
	}
	h.Press("enter") // the first item is the most recent one
	if !slices.Equal(picked, []string{"👏", "👏"}) {
		t.Fatalf("recent first: picked = %q", picked)
	}

	openDialog(h, widgets.EmojiPicker(onPick))
	h.Type("clap")
	h.Press("enter")
	if !slices.Equal(picked, []string{"👏", "👏", "👏"}) {
		t.Fatalf("clap: picked = %q", picked)
	}

	openDialog(h, widgets.EmojiPicker(onPick))
	h.Type("zzzz")
	if !strings.Contains(h.Screen(), "찾는 이모지가 없어요") {
		t.Fatal("no-match notice missing")
	}
	h.Press("enter") // nothing to pick: stays open
	if overlayCount(h) != 1 || len(picked) != 3 {
		t.Fatal("enter without a match must do nothing")
	}
	h.Press("esc")
	if overlayCount(h) != 0 {
		t.Fatal("esc must close the picker")
	}

	openDialog(h, widgets.EmojiPicker(nil)) // nil onPick: picking just closes
	h.Press("right", "enter")
	if overlayCount(h) != 0 || len(picked) != 3 {
		t.Fatal("nil onPick: enter must close without calling anything")
	}
}

func reportRequests(h *uitest.Harness) []apitest.Recorded {
	var out []apitest.Recorded
	for _, r := range h.API.Requests() {
		if r.Method == "POST" && r.Path == "/v1/reports" {
			out = append(out, r)
		}
	}
	return out
}

func TestReportDialogSubmit(t *testing.T) {
	submit := func(h *uitest.Harness) {
		h.T.Helper()
		openDialog(h, widgets.ReportDialog("message", "M1", "민지의 메시지"))
		s := h.Screen()
		for _, want := range []string{"민지의 메시지", "스팸·광고", "괴롭힘·혐오", "불법 콘텐츠", "음란물", "폭력·위협", "기타"} {
			if !strings.Contains(s, want) {
				h.T.Fatalf("report dialog lacks %q:\n%s", want, s)
			}
		}
		h.Press("enter") // choose 스팸·광고 (the first reason) => note
		h.Type("광고")
		h.Press("enter") // submit
	}
	t.Run("201", func(t *testing.T) {
		h := uitest.New(t)
		h.API.JSON("POST /v1/reports", 201, map[string]string{"report_id": "R1"})
		submit(h)
		reqs := reportRequests(h)
		if len(reqs) != 1 {
			t.Fatalf("%d report requests, want 1", len(reqs))
		}
		var body map[string]any
		if err := json.Unmarshal(reqs[0].Body, &body); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"target_type": "message", "target_id": "M1", "reason": "spam", "note": "광고"}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("body = %v, want %v", body, want)
		}
		if !strings.Contains(h.Screen(), "신고했어요") || overlayCount(h) != 0 {
			t.Fatalf("201: toast/close:\n%s", h.Screen())
		}
	})
	t.Run("other reason without note", func(t *testing.T) {
		h := uitest.New(t)
		h.API.JSON("POST /v1/reports", 201, map[string]string{"report_id": "R2"})
		openDialog(h, widgets.ReportDialog("post", "P1", "글"))
		h.Press("down", "down", "tab", "enter")
		reqs := reportRequests(h)
		if len(reqs) != 1 {
			t.Fatalf("%d report requests", len(reqs))
		}
		var body map[string]any
		json.Unmarshal(reqs[0].Body, &body)
		want := map[string]any{"target_type": "post", "target_id": "P1", "reason": "illegal"}
		if !reflect.DeepEqual(body, want) {
			t.Fatalf("body = %v, want %v", body, want)
		}
	})
	t.Run("409", func(t *testing.T) {
		h := uitest.New(t)
		h.API.Error("POST /v1/reports", 409, "already_exists")
		submit(h)
		if !strings.Contains(h.Screen(), "이미 신고한 대상이에요") || overlayCount(h) != 0 {
			t.Fatalf("409: toast/close:\n%s", h.Screen())
		}
	})
	t.Run("500", func(t *testing.T) {
		h := uitest.New(t)
		h.API.Error("POST /v1/reports", 500, "internal_error")
		submit(h)
		if overlayCount(h) != 1 {
			t.Fatal("500: the dialog must stay open")
		}
		if !strings.Contains(h.Screen(), "서버 오류가 났어요") {
			t.Fatalf("500: no inline error:\n%s", h.Screen())
		}
		h.Check()
	})
	// pending-fixes 0-b: a hostile server's error code and details.fields keys
	// and values reach the inline error line without any escape sequence.
	// The app's lipgloss Compositor already drops OSC 52, OSC 0/2 and CSI 2J,
	// so each case also carries what it lets through (APC kitty graphics, DCS,
	// SOS, PM, 8-bit C1 CSI, OSC 8): with api.Describe's and the dialog's
	// Sanitize both removed this test fails; either one alone keeps it green
	// (TestDescribeSanitizesServerStrings and TestReportErrorLineSanitized pin
	// each layer on its own). FormatSoup adds the invisible format characters
	// h.Check's rune check rejects.
	const passThrough = "\x1b_Gf=100;AAAA\x1b\\\x1bP1$qm\x1b\\\x1bXs\x1b\\\x1b^p\x1b\\\u009b2J\x1b]8;;https://evil.example/\x07L\x1b]8;;\x07" +
		fixtures.FormatSoup // bidi/zero-width/line separators (pending-fixes 1): check 3
	for name, env := range map[string]map[string]any{
		"hostile code": {"error": map[string]any{"code": "x\x1b[2J\x1b]52;c;ZXZpbA==\x07" + passThrough, "message": "\x1b]0;PWNED\x07"}},
		"hostile fields": {"error": map[string]any{"code": "invalid_request", "message": "m",
			"details": map[string]any{"fields": map[string]string{"\x1b[2J" + passThrough: "\x1b]52;c;ZXZpbA==\x07\u009b31m" + passThrough}}}},
	} {
		t.Run(name, func(t *testing.T) {
			h := uitest.New(t)
			h.API.JSON("POST /v1/reports", 400, env)
			submit(h)
			if overlayCount(h) != 1 {
				t.Fatal("400: the dialog must stay open")
			}
			h.Check() // check 6: only SGR / clean OSC 8 escapes
			raw := h.Styled()
			for _, bad := range []string{"\x1b[2J", "]52;", "]0;", "\x07", "\u009b", "\x1b_", "\x1bP", "\x1bX", "\x1b^", "evil.example"} {
				if strings.Contains(raw, bad) {
					t.Fatalf("frame carries %q:\n%q", bad, raw)
				}
			}
		})
	}
	t.Run("note limit and newline", func(t *testing.T) {
		h := uitest.New(t)
		h.API.JSON("POST /v1/reports", 201, map[string]string{"report_id": "R3"})
		openDialog(h, widgets.ReportDialog("user", "U1", "민지"))
		h.Press("tab")
		h.Paste("줄\r\n바꿈\x1b[31m" + strings.Repeat("가", 1200))
		h.Check()
		h.Press("enter")
		reqs := reportRequests(h)
		if len(reqs) != 1 {
			t.Fatalf("%d report requests", len(reqs))
		}
		var body struct{ Note string }
		json.Unmarshal(reqs[0].Body, &body)
		if strings.ContainsAny(body.Note, "\r\n\x1b") || !strings.HasPrefix(body.Note, "줄 바꿈") {
			t.Fatalf("note not normalized: %q", body.Note[:min(len(body.Note), 40)])
		}
		if n := len([]rune(body.Note)); n != 1000 {
			t.Fatalf("note has %d runes, want the 1000 cap", n)
		}
	})
}
