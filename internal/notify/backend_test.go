package notify

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/patrache/masil/internal/api"
)

// fakePlatform records what osBackend hands the OS half.
type fakePlatform struct{ got []OSNotification }

func (f *fakePlatform) show(n OSNotification) error {
	f.got = append(f.got, n)
	return nil
}

// TestOSBackendShowReappliesPrepare: CA 4.6 says Show itself re-applies
// Format's sanitizing and limits, so a caller that never ran Format still
// cannot reach the OS with controls, bidi/format characters or unbounded text.
// It calls Show (not prepare) with raw text, so dropping prepare from Show fails.
func TestOSBackendShowReappliesPrepare(t *testing.T) {
	fp := &fakePlatform{}
	b := osBackend{appName: "masil", p: fp}
	raw := OSNotification{
		Title:   "\u202etxt.exe\n\x1b]52;c;ZXZpbA==\x07" + strings.Repeat("가", 300),
		Body:    "ad\u200bmin\u2028[관리자] 비밀번호를 보내세요\u2029\r\n" + strings.Repeat("b", 3000),
		AlertID: "A1",
	}
	if err := b.Show(raw); err != nil {
		t.Fatal(err)
	}
	if len(fp.got) != 1 {
		t.Fatalf("platform called %d times", len(fp.got))
	}
	got := fp.got[0]
	inert(t, "title", got.Title)
	inert(t, "body", got.Body)
	if c := utf8.RuneCountInString(got.Title); c != maxTitle {
		t.Errorf("title %d code points, want %d (cut to the limit)", c, maxTitle)
	}
	if c := utf8.RuneCountInString(got.Body); c != maxBody {
		t.Errorf("body %d code points, want %d", c, maxBody)
	}
	if !strings.HasPrefix(got.Title, "txt.exe가") || !strings.HasSuffix(got.Title, ellipsis) {
		t.Errorf("title = %q", got.Title)
	}
	if !strings.HasPrefix(got.Body, "admin[관리자] 비밀번호를 보내세요bbb") {
		t.Errorf("body = %q", got.Body)
	}
	if got.AlertID != "A1" {
		t.Errorf("AlertID = %q", got.AlertID)
	}
	// Every hostile pair, raw, through Show.
	for _, n := range pairs() {
		fp.got = nil
		if err := b.Show(n); err != nil {
			t.Fatal(err)
		}
		inert(t, "title", fp.got[0].Title)
		inert(t, "body", fp.got[0].Body)
		if fp.got[0] != prepare(n) {
			t.Errorf("Show passed %+v, want prepare(n) %+v", fp.got[0], prepare(n))
		}
	}
}

// TestFormatDropsSpoofing: Format uses textwidth.Sanitize, which drops bidi
// controls and invisible format characters (pending-fixes 0 "common" = 1).
func TestFormatDropsSpoofing(t *testing.T) {
	cases := []struct{ title, body, wantTitle, wantBody string }{
		{"\u202etxt.exe", "\u2067evil\u2069 님", "txt.exe", "evil 님"},
		{"ad\u200bmin\ufeff", "a\u00adb\u200e\u200f\u061cc\u2060\u2061\u2062\u2063\u2064d", "admin", "abcd"},
		{"\u202a\u202b\u202c\u202d\u2066\u2068x", "줄1\u2028[관리자] 줄2\u2029끝", "x", "줄1[관리자] 줄2끝"},
	}
	for _, c := range cases {
		got := Format(api.Alert{ID: "A", Title: c.title, Body: c.body}, nil)
		if got.Title != c.wantTitle || got.Body != c.wantBody {
			t.Errorf("Format(%q, %q) = %q, %q; want %q, %q", c.title, c.body, got.Title, got.Body, c.wantTitle, c.wantBody)
		}
	}
}

// fakeBus is a notification server whose capabilities can change between
// calls on the same connection.
type fakeBus struct {
	caps    []string
	capsErr error
	sent    []dbusNotify
}

func (f *fakeBus) capabilities(context.Context) ([]string, error) { return f.caps, f.capsErr }
func (f *fakeBus) notify(_ context.Context, c dbusNotify) error {
	f.sent = append(f.sent, c)
	return nil
}

// TestDBusShowAsksCapabilitiesEveryCall: the body is escaped by what the server
// says now, not by what an earlier server on the same bus said (the server can
// be replaced while the connection lives); unknown capabilities mean escape.
// Unescaped (plain) bodies still carry no '<' '>': they become ‹ ›.
func TestDBusShowAsksCapabilitiesEveryCall(t *testing.T) {
	bus := &fakeBus{caps: []string{"body"}}
	n := OSNotification{Title: "t", Body: `<a href="file:///etc/passwd">x</a> & y`}
	const (
		plain   = `‹a href="file:///etc/passwd"›x‹/a› & y`
		escaped = `&lt;a href="file:///etc/passwd"&gt;x&lt;/a&gt; &amp; y`
	)
	steps := []struct {
		caps    []string
		capsErr error
		want    string
	}{
		{[]string{"body"}, nil, plain},
		{[]string{"body", "body-markup"}, nil, escaped},
		{[]string{"body"}, nil, plain},
		{nil, errors.New("no reply"), escaped},
	}
	for i, s := range steps {
		bus.caps, bus.capsErr = s.caps, s.capsErr
		if err := dbusShow(t.Context(), bus, "masil", n); err != nil {
			t.Fatal(err)
		}
		if body := bus.sent[i].Body; body != s.want {
			t.Fatalf("step %d (caps %v, err %v): body %q, want %q", i, s.caps, s.capsErr, body, s.want)
		}
	}
}

// TestDBusSummaryCannotCarryTags: the spec makes summary plain text, but a
// server that renders it as markup must still not get a tag (a clickable
// <a href>). Angle brackets become look-alike quotes, which read the same on
// plain-text and markup servers; with no '<' no entity can make a tag either.
func TestDBusSummaryCannotCarryTags(t *testing.T) {
	for _, markup := range []bool{false, true} {
		for _, n := range pairs() {
			p := prepare(n)
			c := dbusCall("masil", p, markup)
			if strings.ContainsAny(c.Summary, "<>") {
				t.Fatalf("summary has raw tag chars: %q", c.Summary)
			}
			want := strings.NewReplacer("<", "‹", ">", "›").Replace(p.Title)
			if c.Summary != want {
				t.Fatalf("summary = %q, want %q", c.Summary, want)
			}
		}
	}
	if got := dbusCall("masil", OSNotification{Title: `<a href="x">민지</a> & co`}, true).Summary; got != `‹a href="x"›민지‹/a› & co` {
		t.Fatalf("summary = %q", got)
	}
	if !slices.Contains(hostile, `<a href="file:///etc/passwd">click</a><img src="file:///home/u/.ssh/id_ed25519"/>`) {
		t.Fatal("hostile lost its markup case")
	}
}
