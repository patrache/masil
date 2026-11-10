package settings_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// valueOffset is, on the line holding label, the cells from the label's start
// to the first cell of value after it (-1 = not found).
func valueOffset(line, label, value string) int {
	i := strings.Index(line, label)
	if i < 0 {
		return -1
	}
	j := strings.Index(line[i+len(label):], value)
	if j < 0 {
		return -1
	}
	return textwidth.Width(line[i : i+len(label)+j])
}

// TestSettingsColumnWidthFromWidest: the row-name column is as wide as the
// widest row name of the section in the shown language (not a fixed number),
// and the en screens keep their borders at 120x40, 80x24 and 50x20.
func TestSettingsColumnWidthFromWidest(t *testing.T) {
	for _, tc := range []struct {
		loc            i18n.Locale
		short, widest  string
		on, off, clock string
	}{
		{i18n.Ko, "마우스", "배경색 칠하기", "켜짐", "꺼짐", "시계"},
		{i18n.En, "Mouse", "Paint background", "On", "Off", "Clock"},
	} {
		h, _ := newSettings(t, "display", uitest.WithLocale(tc.loc))
		want := textwidth.Width(tc.widest) + 1
		for _, label := range []string{tc.short, tc.widest} {
			l := lineWith(h, label)
			off := valueOffset(l, label, tc.on)
			if off < 0 {
				off = valueOffset(l, label, tc.off)
			}
			if off != want {
				t.Errorf("%s %q: value at +%d cells, want +%d (widest name %q):\n%s", tc.loc, label, off, want, tc.widest, h.Screen())
			}
		}
		if lineWith(h, tc.clock) == "" {
			t.Errorf("%s: no clock row:\n%s", tc.loc, h.Screen())
		}
	}
	h, _ := newSettings(t, "", uitest.WithLocale(i18n.En))
	for _, sec := range []string{"theme", "layout", "display", "notify", "privacy", "focus", "blocks", "sessions", "password", "logout"} {
		open(h, sec)
		for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 80, H: 24}, {W: 50, H: 20}} {
			h.Resize(sz.W, sz.H)
			h.Check()
			if h.Model.Frame().Layout.Mode == layout.ModeTooSmall {
				continue
			}
			// The settings pane, minus user data (names, device names) and
			// the border title, which the shell draws (P5 Task 19).
			pane := uitest.PseudoRegions(h)["pane/main/settings"]
			if _, body, ok := strings.Cut(pane, "\n"); ok {
				pane = body
			}
			for _, u := range fixtures.UserData() {
				if u != "" {
					pane = strings.ReplaceAll(pane, u, "")
				}
			}
			if strings.ContainsFunc(pane, func(r rune) bool { return r >= 0xAC00 && r <= 0xD7A3 }) {
				t.Errorf("en %s %dx%d: Korean in the settings pane:\n%s", sec, sz.W, sz.H, pane)
			}
		}
	}
}

// TestPrivacyRowsLegal: the privacy section's texts are legal keys (the
// reviewer checks their meaning separately), and the screen shows them.
func TestPrivacyRowsLegal(t *testing.T) {
	var keys []i18n.Entry
	for _, e := range i18n.Entries() {
		if e.Area == "settings" && strings.HasPrefix(e.Key, "privacy_") {
			keys = append(keys, e)
		}
	}
	if len(keys) < 5 {
		t.Fatalf("settings has %d privacy_ keys, want at least 5", len(keys))
	}
	for _, e := range keys {
		if !e.Legal {
			t.Errorf("settings.%s: privacy text without legal = true", e.Key)
		}
	}
	h, _ := newSettings(t, "privacy")
	for _, e := range keys {
		if e.Key == "privacy_dm_friends_only" { // the value not selected now
			continue
		}
		if !strings.Contains(h.Screen(), e.Text[i18n.Ko].Other) {
			t.Errorf("privacy screen lacks %s %q:\n%s", e.Key, e.Text[i18n.Ko].Other, h.Screen())
		}
	}
}

// TestAnchorRow: a "section.row" anchor opens with that row selected.
func TestAnchorRow(t *testing.T) {
	h, _ := newSettings(t, "display.clock")
	if l := selectedLine(h); !strings.Contains(l, "시계") {
		t.Fatalf("Anchor display.clock selected %q, want the clock row:\n%s", l, h.Screen())
	}
	open(h, "display.nope") // unknown row: the section's first row
	if l := selectedLine(h); !strings.Contains(l, "마우스") {
		t.Fatalf("Anchor display.nope selected %q, want the first display row:\n%s", l, h.Screen())
	}
	open(h, "nope.clock") // unknown section: ignored
	if l := selectedLine(h); !strings.Contains(l, "마우스") {
		t.Fatalf("Anchor nope.clock moved the selection to %q", l)
	}
}

// TestErrorsFollowLocaleSwitch: error lines made when something failed (load
// errors, inline row errors) are rendered when drawn, so a language switch
// redraws them in the new language at once (P5 Review Focus 1, Task 9 M4).
func TestErrorsFollowLocaleSwitch(t *testing.T) {
	t.Setenv("MASIL_LANG", "")
	i18n.SetOpenForTest(t, i18n.Ko, i18n.En)
	h := uitest.New(t)
	h.API.Error("GET /v1/me/settings", http.StatusInternalServerError, "internal_error")
	h.API.Error("GET /v1/blocks", http.StatusInternalServerError, "internal_error")
	h.API.Error("GET /v1/auth/sessions", http.StatusInternalServerError, "internal_error")
	open(h, "password")
	selectRow(t, h, "비밀번호 바꾸기")
	h.Press("enter") // empty form: inline row error
	if !strings.Contains(h.Screen(), "현재 비밀번호를 입력해 주세요") {
		t.Fatalf("ko row error missing:\n%s", h.Screen())
	}
	gets := len(h.API.Requests())
	h.SetLocale("en")
	for _, sec := range []string{"password", "notify", "blocks", "sessions"} {
		open(h, sec)
		pane := uitest.PseudoRegions(h)["pane/main/settings"]
		if _, body, ok := strings.Cut(pane, "\n"); ok { // the shell's border title (Task 19)
			pane = body
		}
		if strings.ContainsFunc(pane, func(r rune) bool { return r >= 0xAC00 && r <= 0xD7A3 }) {
			t.Errorf("en %s after the switch: Korean left in the settings pane:\n%s", sec, pane)
		}
	}
	if n := len(h.API.Requests()); n != gets {
		t.Fatalf("the switch or the section moves refetched (%d -> %d requests): the check would not see stale text", gets, n)
	}
}
