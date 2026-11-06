package markup_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/textwidth"
)

// hiddenSeps are runes the screen never draws (textwidth.Invisible): format
// characters (ZWJ included), variation selectors, the combining grapheme
// joiner and other default ignorables, and the noncharacters (U+FFFE/U+FFFF
// are zero-width in both width models).
var hiddenSeps = []string{"\u200b", "\u200c", "\u200d", "\u2060", "\ufeff", "\u00ad", "\u202e", "\u034f", "\ufe00", "\ufe0f", "\U000e0100", "\u180b", "\u17b4",
	"\ufffe", "\uffff", "\ufdd0"}

// TestTokenBoundaryIgnoresInvisible (pending-fixes 1 review ④): Parse runs on
// the raw body and Sanitize only later, so an invisible rune used to cut a
// mention or hashtag that the screen shows whole: "#공지\u200b사항" read
// "#공지사항" but was the hashtag #공지, "@sam\u200b_admin" read "@sam_admin" but
// mentioned @sam. Token boundaries are now decided as the screen shows them:
// a token that an invisible run would cut short is plain text, and an
// invisible rune before "@"/"#"/"http" is no word boundary.
func TestTokenBoundaryIgnoresInvisible(t *testing.T) {
	tags := markup.ParseOptions{Hashtags: true}
	for _, sep := range hiddenSeps {
		for _, c := range []struct {
			body string
			opts markup.ParseOptions
		}{
			{"#공지" + sep + "사항", tags},
			{"보기 #공지" + sep + "사항 끝", tags},
			{"#" + sep + "공지", tags},
			{"가" + sep + "#공지", tags},
			{"@sam" + sep + "_admin", markup.ParseOptions{}},
			{"@sam" + sep + "admin 확인", markup.ParseOptions{}},
			{"@" + sep + "sam", markup.ParseOptions{}},
			{"sam" + sep + "@minji", markup.ParseOptions{}},
			{"@her" + sep + "e", markup.ParseOptions{}},
			{"x" + sep + "https://evil.example/", markup.ParseOptions{}},
		} {
			d := markup.Parse(c.body, c.opts)
			if got := nonText(d); len(got) != 0 {
				t.Errorf("Parse(%q) = %+v, want plain text: the screen shows %q", c.body, got, textwidth.Sanitize(c.body))
			}
			if got, want := allText(d), c.body; got != want {
				t.Errorf("Parse(%q) text = %q, want the body unchanged", c.body, got)
			}
		}
	}
}

// An invisible rune next to a real boundary (space, end, punctuation) does
// not change the token: the screen shows the same boundary.
func TestTokenBoundaryInvisibleAtBoundary(t *testing.T) {
	tags := markup.ParseOptions{Hashtags: true}
	for _, sep := range hiddenSeps {
		for _, c := range []struct {
			body string
			opts markup.ParseOptions
			want []markup.Inline
		}{
			{"#공지" + sep + " 사항", tags, []markup.Inline{{Kind: markup.Hashtag, Text: "#공지", Handle: "공지"}}},
			{sep + "#공지", tags, []markup.Inline{{Kind: markup.Hashtag, Text: "#공지", Handle: "공지"}}},
			{"#공지" + sep, tags, []markup.Inline{{Kind: markup.Hashtag, Text: "#공지", Handle: "공지"}}},
			{"@sam" + sep + " 안녕", markup.ParseOptions{}, []markup.Inline{{Kind: markup.Mention, Text: "@sam", Handle: "sam"}}},
			{"@sam" + sep + ", 안녕", markup.ParseOptions{}, []markup.Inline{{Kind: markup.Mention, Text: "@sam", Handle: "sam"}}},
			{"안녕 " + sep + "@sam", markup.ParseOptions{}, []markup.Inline{{Kind: markup.Mention, Text: "@sam", Handle: "sam"}}},
		} {
			if got := nonText(markup.Parse(c.body, c.opts)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Parse(%q) = %+v, want %+v", c.body, got, c.want)
			}
		}
	}
}

// A bare URL holding an invisible rune is plain text as a whole, like one
// holding a control (pending-fixes 0-b ①): the screen reads one address while
// the link would open another. VS15/VS16 only pick the presentation of the
// emoji before them, so an emoji URL stays a link.
func TestBareURLHiddenRune(t *testing.T) {
	for _, sep := range hiddenSeps {
		if sep == "\ufe0f" {
			continue
		}
		u := "https://secure-paypa1.com" + sep + ".paypal.com/login"
		for _, body := range []string{u, "보기 " + u + " 끝"} {
			if got := nonText(markup.Parse(body, markup.ParseOptions{})); len(got) != 0 {
				t.Errorf("Parse(%q) = %+v, want plain text", body, got)
			}
		}
	}
	got := nonText(markup.Parse("https://emojipedia.org/❤\ufe0f 끝", markup.ParseOptions{}))
	want := []markup.Inline{{Kind: markup.Link, Text: "https://emojipedia.org/❤\ufe0f", URL: "https://emojipedia.org/❤\ufe0f"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("emoji URL = %+v, want %+v", got, want)
	}
	if strings.Contains(want[0].URL, "\u200b") {
		t.Fatal("unreachable")
	}
}
