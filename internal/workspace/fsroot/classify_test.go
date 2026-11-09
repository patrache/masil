package fsroot

import (
	"errors"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		content string
		kind    Kind
		notText bool
	}{
		{"small.txt", strings.Repeat("a", 100), KindText, false},
		{"large.txt", strings.Repeat("a", 600<<10), KindReadOnlyLarge, false},
		{"huge.txt", strings.Repeat("a", MaxView+1), KindTooLarge, true},
		{"nul.bin", "a\x00b", KindBinary, true},
		{"bad.txt", "a\xff\xfeb", KindBinary, true},
		{"mixed.txt", "a\r\nb\nc", KindReadOnlyMixedEOL, false},
		{"exact.txt", strings.Repeat("a", MaxCollab), KindText, false},
		{"maxview.txt", strings.Repeat("a", MaxView), KindReadOnlyLarge, false},
	}
	for _, c := range cases {
		writeFile(t, dir, c.name, c.content)
	}
	r := openRoot(t, dir)
	for _, c := range cases {
		text, info, err := r.ReadText(c.name)
		if info.Kind != c.kind {
			t.Errorf("%s: kind = %v, want %v", c.name, info.Kind, c.kind)
		}
		if c.notText {
			if !errors.Is(err, ErrNotText) || text != "" {
				t.Errorf("%s: err = %v, text len %d; want ErrNotText and no text", c.name, err, len(text))
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: err = %v", c.name, err)
		}
		if info.Size != int64(len(c.content)) || info.Rel != c.name || info.FP.Size != info.Size {
			t.Errorf("%s: info = %+v", c.name, info)
		}
	}
	text, _, _ := r.ReadText("mixed.txt")
	if text != "a\r\nb\nc" {
		t.Errorf("mixed EOL text changed: %q", text)
	}
}

func TestReadTextCRLFAndBOM(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "w.txt", "\ufeffa\r\nb\r\n")
	writeFile(t, dir, "u.txt", "x\ny\n")
	r := openRoot(t, dir)
	text, info, err := r.ReadText("w.txt")
	if err != nil || text != "a\nb\n" || info.EOL != EOLCRLF || !info.BOM || info.Kind != KindText {
		t.Errorf("ReadText(w) = %q, %+v, %v", text, info, err)
	}
	text, info, err = r.ReadText("u.txt")
	if err != nil || text != "x\ny\n" || info.EOL != EOLLF || info.BOM {
		t.Errorf("ReadText(u) = %q, %+v, %v", text, info, err)
	}
}

func TestReadTextRefusesEscape(t *testing.T) {
	r := openRoot(t, t.TempDir())
	if _, _, err := r.ReadText("../x"); err == nil {
		t.Errorf("ReadText(../x) succeeded")
	}
}
