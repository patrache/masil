package widgets

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
)

// TestReportErrorLineSanitized (pending-fixes 0-b): the inline error line is
// sanitized where it is drawn, whatever string ended up in errMsg.
func TestReportErrorLineSanitized(t *testing.T) {
	d := ReportDialog("message", "M1", "민지").(*reportDialog)
	d.size = ui.Size{W: reportW, H: reportH}
	d.errMsg = "오류 \x1b[2J\x1b]52;c;ZXZpbA==\x07\u009b31m\x1b]0;PWNED\x07 끝"
	rows, _, _ := d.layout(theme.Get(theme.Amber))
	var status string
	for _, r := range rows {
		if strings.Contains(ansi.Strip(r), "오류") {
			status = r
		}
	}
	if status == "" {
		t.Fatalf("no error row in %q", rows)
	}
	for _, bad := range []string{"[2J", "]52;", "]0;", "\x07", "\u009b"} {
		if strings.Contains(status, bad) {
			t.Errorf("error row carries %q: %q", bad, status)
		}
	}
	if got := ansi.Strip(status); !strings.Contains(got, "✕ 오류") || !strings.Contains(got, "끝") {
		t.Errorf("error row = %q", got)
	}
}
