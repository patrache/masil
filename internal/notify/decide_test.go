package notify_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/notify"
)

var now = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

func alertAt(created time.Time, suppressed bool) api.Alert {
	return api.Alert{ID: "A1", Kind: api.AlertMention, Title: "t", Body: "b", CreatedAt: created, Suppressed: suppressed}
}

func TestDecideTable(t *testing.T) {
	fresh := now.Add(-time.Minute)
	base := func(role notify.Role) notify.Input {
		return notify.Input{Role: role, Now: now, DesktopEnabled: true}
	}
	type row struct {
		name      string
		a         api.Alert
		in        func(notify.Input) notify.Input
		tui, daem notify.Decision
	}
	id := func(in notify.Input) notify.Input { return in }
	rows := []row{
		{"stale replay 11m", alertAt(now.Add(-11*time.Minute), false),
			func(in notify.Input) notify.Input { in.Replayed = true; return in }, notify.Silent, notify.Skip},
		{"replay exactly 10m continues", alertAt(now.Add(-10*time.Minute), false),
			func(in notify.Input) notify.Input { in.Replayed = true; return in }, notify.Popup, notify.Popup},
		{"replay 9m continues", alertAt(now.Add(-9*time.Minute), false),
			func(in notify.Input) notify.Input { in.Replayed = true; return in }, notify.Popup, notify.Popup},
		{"old but live (not replayed) continues", alertAt(now.Add(-30*time.Minute), false), id, notify.Popup, notify.Popup},
		{"stale replay beats suppressed", alertAt(now.Add(-11*time.Minute), true),
			func(in notify.Input) notify.Input { in.Replayed = true; return in }, notify.Silent, notify.Skip},
		{"suppressed", alertAt(fresh, true), id, notify.Silent, notify.Skip},
		{"desktop disabled", alertAt(fresh, false),
			func(in notify.Input) notify.Input { in.DesktopEnabled = false; return in }, notify.Silent, notify.Skip},
		{"tui present (daemon only)", alertAt(fresh, false),
			func(in notify.Input) notify.Input { in.TUIPresent = true; return in }, notify.Popup, notify.Skip},
		{"focused and viewing", alertAt(fresh, false),
			func(in notify.Input) notify.Input {
				in.TerminalFocusKnown, in.TerminalFocused, in.ViewingTarget = true, true, true
				return in
			}, notify.Silent, notify.Popup},
		{"blurred but viewing", alertAt(fresh, false),
			func(in notify.Input) notify.Input {
				in.TerminalFocusKnown, in.TerminalFocused, in.ViewingTarget = true, false, true
				return in
			}, notify.Popup, notify.Popup},
		{"focus unknown, viewing", alertAt(fresh, false),
			func(in notify.Input) notify.Input {
				in.TerminalFocusKnown, in.TerminalFocused, in.ViewingTarget = false, true, true
				return in
			}, notify.Popup, notify.Popup},
		{"focused, not viewing", alertAt(fresh, false),
			func(in notify.Input) notify.Input {
				in.TerminalFocusKnown, in.TerminalFocused = true, true
				return in
			}, notify.Popup, notify.Popup},
		{"otherwise", alertAt(fresh, false), id, notify.Popup, notify.Popup},
	}
	for _, r := range rows {
		if got := notify.Decide(r.a, r.in(base(notify.RoleTUI))); got != r.tui {
			t.Errorf("%s: TUI = %v, want %v", r.name, got, r.tui)
		}
		if got := notify.Decide(r.a, r.in(base(notify.RoleDaemon))); got != r.daem {
			t.Errorf("%s: daemon = %v, want %v", r.name, got, r.daem)
		}
	}
}

func TestFormat(t *testing.T) {
	n := notify.Format(api.Alert{ID: "01JC9ZA", Title: "민지\x1b[31m님이\x07 ❤️", Body: "안녕\r\n\x00하세요\u0085"}, nil)
	if n.AlertID != "01JC9ZA" {
		t.Errorf("AlertID = %q", n.AlertID)
	}
	if n.Title != "민지님이 ❤" {
		t.Errorf("Title = %q", n.Title)
	}
	if n.Body != "안녕하세요" {
		t.Errorf("Body = %q", n.Body)
	}

	long := notify.Format(api.Alert{Title: strings.Repeat("가", 81), Body: strings.Repeat("b", 201)}, nil)
	if c := utf8.RuneCountInString(long.Title); c != 80 || !strings.HasSuffix(long.Title, "…") ||
		long.Title != strings.Repeat("가", 79)+"…" {
		t.Errorf("title %d cps: %q", c, long.Title)
	}
	if c := utf8.RuneCountInString(long.Body); c != 200 || long.Body != strings.Repeat("b", 199)+"…" {
		t.Errorf("body %d cps: %q", c, long.Body)
	}

	exact := notify.Format(api.Alert{Title: strings.Repeat("가", 80), Body: strings.Repeat("b", 200)}, nil)
	if exact.Title != strings.Repeat("가", 80) || exact.Body != strings.Repeat("b", 200) {
		t.Errorf("exact limits must not be truncated: %q / %q", exact.Title, exact.Body)
	}

	// Sanitize may expand (tab -> 4 spaces); the limit still holds afterwards.
	tabs := notify.Format(api.Alert{Title: strings.Repeat("\t", 30)}, nil)
	if c := utf8.RuneCountInString(tabs.Title); c > 80 {
		t.Errorf("tab title %d cps > 80", c)
	}
}
