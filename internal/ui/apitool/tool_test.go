package apitool

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/app"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
)

// firstSizes are the first screen's golden sizes (C3.15 1).
var firstSizes = append(slices.Clone(uitest.Required), uitest.Size{W: 60, H: 24}, uitest.Size{W: 44, H: 24}, uitest.Tiny)

func TestGoldenAPIFirstScreen(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "api")
	uitest.ForEachSize(t, firstSizes, func(t *testing.T, h *uitest.Harness) {
		openIn(h)
		seedFirstScreen(h)
		h.Golden("first")
	})
}

// squeeze drops spaces and box-drawing characters: a text wrapped over
// several lines (or inside a box) still matches.
func squeeze(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || (r >= 0x2500 && r <= 0x257F) {
			return -1
		}
		return r
	}, s)
}

// hasWhole reports whether the screen shows text whole (maybe wrapped).
func hasWhole(screen, text string) bool { return strings.Contains(squeeze(screen), squeeze(text)) }

func TestHostileResponseSanitized(t *testing.T) {
	h := openAPI(t)
	seedFirstScreen(h)
	tl := toolOf(h)
	hostile := "title \x1b]0;pwn\x07 clear \x1b[2J rtl \u202eevil\n"
	resp := &apitool.Response{
		Status: 200, StatusText: "OK\x1b[31m", Proto: "HTTP/1.1", ContentType: "text/plain",
		Header: http.Header{"X-Evil": {"\x1b]0;pwn\x07\u202e"}, "Content-Type": {"text/plain"}},
		Body:   []byte(hostile + `{"k\u001b[2J":"\u001b]0;pwn\u0007"}`), Size: 64,
	}
	tl.showResponse(tl.respKey(), tl.req, resp)
	tl.focus = paneResponse
	jsonResp := &apitool.Response{Status: 200, StatusText: "OK", ContentType: "application/json",
		Header: http.Header{}, Body: []byte(`{"k\u001b[2J":"\u001b]0;pwn\u0007 \u202e"}`)}
	for _, r := range []*apitool.Response{resp, jsonResp} {
		tl.showResponse(tl.respKey(), tl.req, r)
		for _, tab := range []string{"1", "2", "3", "4"} {
			h.Press(tab)
			s := h.Styled()
			screen := h.Screen()
			for _, bad := range []string{"\x1b]", "\x1b[2J", "\u202e", "\x07"} {
				if strings.Contains(s, bad) || strings.Contains(screen, bad) {
					t.Fatalf("tab %s: the screen carries %q:\n%s", tab, bad, screen)
				}
			}
			h.Check()
		}
	}
	for _, m := range h.TeaMsgs() {
		if strings.Contains(fmt.Sprintf("%#v", m), "pwn") {
			t.Fatalf("a terminal message carries the response: %#v", m)
		}
	}
	if title := h.Model.View().WindowTitle; strings.Contains(title, "pwn") || strings.ContainsRune(title, 0x1b) {
		t.Fatalf("window title = %q", title)
	}
}

func TestOpenFromHubAndPalette(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "api")
	h := uitest.New(t)
	h.Press("ctrl+t", "5")
	n := h.Nav()
	if n.Front != app.FrontTools || len(n.Tools) != 2 || n.Tools[1].Arg != ui.ToolAPI {
		t.Fatalf("ctrl+t 5: front=%v tools=%v, want the API tool", n.Front, n.Tools)
	}
	if !strings.Contains(h.Screen(), i18n.KoT().Apitest.TreeLocal()) {
		t.Fatalf("ctrl+t 5: no API screen:\n%s", h.Screen())
	}
	h.Press("ctrl+t") // back to chat
	if h.Nav().Front == app.FrontTools {
		t.Fatalf("ctrl+t: still on the tool surface")
	}
	h.Press("ctrl+k")
	h.Type("api")
	h.Press("enter")
	n = h.Nav()
	if n.Front != app.FrontTools || len(n.Tools) != 2 || n.Tools[1].Arg != ui.ToolAPI || n.Tools[1].ID != instanceID {
		t.Fatalf("palette api: front=%v tools=%v, want the same API instance", n.Front, n.Tools)
	}
}

func TestWideDefaultStandard(t *testing.T) {
	h := openAPI(t)
	h.Resize(120, 40)
	tl := toolOf(h)
	if tl.size.W < 116 {
		t.Fatalf("API tool width at 120x40 = %d, want the wide view (no sidebar)", tl.size.W)
	}
	if got := h.Nav().ToolZoom; got[toolRoute.Key()] {
		t.Logf("zoom remembered: %v", got)
	}
}

func TestFeatureFlagOffHidden(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "")
	h := uitest.New(t)
	if _, ok := ui.ToolByID(ui.ToolAPI); ok {
		t.Fatalf("ToolByID(api) with the flag off")
	}
	h.Press("ctrl+t")
	if h.Nav().Front == app.FrontTools {
		t.Fatalf("ctrl+t opened the tool surface with no flag on")
	}
	for _, id := range []string{"api.open", "api.new", "api.import", "api.env", "api.runner", "api.send", "api.history.clear"} {
		c, ok := palette.Lookup(id)
		if !ok {
			t.Fatalf("palette command %s not registered", id)
		}
		if c.Available(h.Context()) {
			t.Fatalf("%s available with the flag off", id)
		}
	}
	h.Press("ctrl+k")
	h.Type("api")
	if s := h.Screen(); strings.Contains(s, i18n.KoT().Apitest.ToolName()) {
		t.Fatalf("palette lists the API tool with the flag off:\n%s", s)
	}
}

func TestModeSwitchKeepsState(t *testing.T) {
	h := openAPI(t)
	seedFirstScreen(h)
	tl := toolOf(h)
	tl.selectRequest(tl.colName, "list-repos")
	tl.showResponse(tl.respKey(), tl.req, &apitool.Response{Status: 201, StatusText: "Created", Header: http.Header{}, Body: []byte(`[1,2,3]`), Size: 7})
	want := tl.rv.current()
	check := func(size string, wantText ...string) {
		t.Helper()
		if tl.req == nil || tl.req.ID != "list-repos" || tl.rv.current() != want {
			t.Fatalf("%s: request/response changed: req=%v", size, tl.req)
		}
		s := h.Screen()
		for _, w := range wantText {
			if !strings.Contains(s, w) {
				t.Fatalf("%s: screen lacks %q:\n%s", size, w, s)
			}
		}
		h.Check()
	}
	check("120x40", "/user/repos", "201 Created")
	h.Resize(80, 24)
	h.Press("]") // the response tab
	check("80x24", "/user/repos", "201 Created")
	h.Resize(40, 14)
	check("40x14", "GET /user/repos", "201 ·")
	h.Resize(120, 40)
	check("120x40 again", "/user/repos", "201 Created")
}

func TestHubStatusLine(t *testing.T) {
	h := openAPI(t)
	seedFirstScreen(h)
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	if s := h.Screen(); !strings.Contains(s, "깃허브 API (로컬) · staging") {
		t.Fatalf("hub row lacks the status:\n%s", s)
	}
}

func TestHelpShowsTermsLine(t *testing.T) {
	t.Setenv("MASIL_FEATURES", "api")
	terms := i18n.KoT().Apitest.HelpTerms()
	uitest.ForEachSize(t, firstSizes, func(t *testing.T, h *uitest.Harness) {
		openIn(h)
		h.Press("?")
		if s := h.Screen(); !hasWhole(s, terms) {
			t.Fatalf("help lacks the whole terms line %q:\n%s", terms, s)
		}
		h.Check()
		h.Press("esc")
		if s := h.Screen(); hasWhole(s, terms) {
			t.Fatalf("esc did not close the help:\n%s", s)
		}
	})
}
