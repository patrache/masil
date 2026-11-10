package gittool_test

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/ui/uitest"
)

// 0.1.3 patch (tools-nav fix 4): after lazygit exits the status line read
// "x x 닫기" (and "enter 종료됨 · enter 다시 열기"): the hint texts carried
// their key and the status line put the key in front again. Each key shows
// once now: "enter 다시 열기 · x 닫기". The body keeps its full sentences.
func TestExitedStatusHintsNotDoubled(t *testing.T) {
	f, _ := running(t, macEnv, "/r/masil")
	h := uitest.New(t)
	h.Resize(120, 40)
	openMasil(t, h)
	h.Send(f.Exit("git:"+native("/r/masil"), 0))
	lines := strings.Split(h.Screen(), "\n")
	status := lines[len(lines)-1]
	if !strings.Contains(status, "enter 다시 열기 · x 닫기") {
		t.Errorf("status = %q, want %q", status, "enter 다시 열기 · x 닫기")
	}
	for _, bad := range []string{"x x", "enter 종료됨", "enter enter"} {
		if strings.Contains(status, bad) {
			t.Errorf("status = %q has %q", status, bad)
		}
	}
	body := strings.Join(lines[:len(lines)-1], "\n")
	if !strings.Contains(body, "종료됨 · enter 다시 열기") || !strings.Contains(body, "x 닫기") {
		t.Errorf("the body lost its lines:\n%s", body)
	}
}
