package daemon

import (
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/notify"
)

// The daemon pops other users' text through the same notify backend as the TUI
// (pending-fixes 0). Its sink must hand the backend Format's output only:
// no control characters (newline, ESC, BEL, C1), bounded length, and the text
// never reaches daemon.log.
func TestLedgerSinkShowsFormattedTextOnly(t *testing.T) {
	p := testPaths(t)
	rec := &lockedRecorder{}
	var logs syncBuffer
	s := newLedgerSink(rec, notify.OpenLedger(p), slog.New(slog.NewTextHandler(&logs, nil)))
	title := "x\n\"@\nStart-Process calc\n@\" $(touch /tmp/pwn) `id` ]]> \x1b]52;c;ZXZpbA==\x07" + strings.Repeat("가", 200)
	body := `" & do shell script "id" & "` + "\r\n\u009b2J<a href=\"file:///etc/passwd\">x</a>" + strings.Repeat("b", 500)
	in := notify.Input{Role: notify.RoleDaemon, Now: t0, DesktopEnabled: true}
	d, shown, err := s.Handle(api.Alert{ID: "H1", Kind: api.AlertDM, Title: title, Body: body, CreatedAt: t0}, in)
	if err != nil || !shown || d != notify.Popup {
		t.Fatalf("Handle = %v, %v, %v", d, shown, err)
	}
	all := rec.all()
	if len(all) != 1 {
		t.Fatalf("shown %d", len(all))
	}
	want := notify.Format(api.Alert{ID: "H1", Title: title, Body: body})
	if all[0] != want {
		t.Fatalf("backend got %+v, want Format output %+v", all[0], want)
	}
	for _, s := range []string{all[0].Title, all[0].Body} {
		for _, r := range s {
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
				t.Fatalf("control %U reached the backend: %q", r, s)
			}
		}
	}
	if utf8.RuneCountInString(all[0].Title) > 80 || utf8.RuneCountInString(all[0].Body) > 200 {
		t.Fatalf("limits not applied: %d / %d", utf8.RuneCountInString(all[0].Title), utf8.RuneCountInString(all[0].Body))
	}
	if out := logs.String(); strings.Contains(out, "Start-Process") || strings.Contains(out, "do shell script") {
		t.Fatalf("title/body logged:\n%s", out)
	}
}
