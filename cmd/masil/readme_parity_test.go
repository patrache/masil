package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

// P5 Task 21: README.md is English and README.ko.md is Korean. From here on a
// README change touches both files in the same PR, and this test keeps their
// skeleton equal: the language line, the ##/### heading levels in order, the
// number of code blocks and the commands inside them. A block that is
// legitimately different per language (sample output in that language) has
// "<!-- parity:skip -->" on the line right above its opening fence in both
// files. English that is not written yet is marked TODO(i18n), which must be
// gone before English opens.

const paritySkip = "<!-- parity:skip -->"

// readmeShape is what TestReadmeParity compares between the two READMEs.
type readmeShape struct {
	headings []int      // levels of ## / ### headings, in order
	blocks   [][]string // command lines of each compared code block
	skipped  int        // code blocks under parity:skip
}

// parseReadme reads the heading levels and fenced code blocks of a README.
// Lines inside a fence are never headings; a fence may be indented (inside a
// list item).
func parseReadme(doc string) (readmeShape, error) {
	var s readmeShape
	lines := strings.Split(doc, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "```") {
			skip := i > 0 && strings.TrimSpace(lines[i-1]) == paritySkip
			var cmds []string
			closed := false
			for i++; i < len(lines); i++ {
				l := strings.TrimSpace(lines[i])
				if strings.HasPrefix(l, "```") {
					closed = true
					break
				}
				if c := commandLine(l); c != "" {
					cmds = append(cmds, c)
				}
			}
			if !closed {
				return s, fmt.Errorf("unclosed code block")
			}
			if skip {
				s.skipped++
			} else {
				s.blocks = append(s.blocks, cmds)
			}
			continue
		}
		switch {
		case strings.HasPrefix(lines[i], "### "):
			s.headings = append(s.headings, 3)
		case strings.HasPrefix(lines[i], "## "):
			s.headings = append(s.headings, 2)
		}
	}
	return s, nil
}

// commandLine is a code-block line without its trailing " # …" comment; ""
// for an empty line or a whole-line comment.
func commandLine(l string) string {
	if l == "" || strings.HasPrefix(l, "#") {
		return ""
	}
	if i := strings.Index(l, " # "); i >= 0 {
		l = l[:i]
	}
	return strings.TrimSpace(l)
}

func TestReadmeParity(t *testing.T) {
	en := readRepo(t, "README.md")
	ko := readRepo(t, "README.ko.md")

	for _, c := range []struct{ rel, doc, want string }{
		{"README.md", en, "**English** · [한국어](README.ko.md)"},
		{"README.ko.md", ko, "[English](README.md) · **한국어**"},
	} {
		lines := strings.SplitN(c.doc, "\n", 3)
		if len(lines) < 2 || lines[1] != c.want {
			t.Errorf("%s: second line must be %q", c.rel, c.want)
		}
	}

	es, err := parseReadme(en)
	if err != nil {
		t.Fatalf("README.md: %v", err)
	}
	ks, err := parseReadme(ko)
	if err != nil {
		t.Fatalf("README.ko.md: %v", err)
	}
	if len(es.headings) == 0 || len(es.blocks) == 0 {
		t.Fatalf("README.md: %d headings, %d code blocks — nothing to compare", len(es.headings), len(es.blocks))
	}
	if !slices.Equal(es.headings, ks.headings) {
		t.Errorf("heading levels differ:\nREADME.md    %v\nREADME.ko.md %v", es.headings, ks.headings)
	}
	if es.skipped != ks.skipped {
		t.Errorf("parity:skip blocks: README.md %d, README.ko.md %d", es.skipped, ks.skipped)
	}
	if len(es.blocks) != len(ks.blocks) {
		t.Fatalf("code blocks: README.md %d, README.ko.md %d", len(es.blocks), len(ks.blocks))
	}
	for i := range es.blocks {
		if !slices.Equal(es.blocks[i], ks.blocks[i]) {
			t.Errorf("code block %d commands differ:\nREADME.md    %q\nREADME.ko.md %q", i+1, es.blocks[i], ks.blocks[i])
		}
	}

	if i18n.IsOpen(i18n.En) && strings.Contains(en, "TODO(i18n)") {
		t.Error("README.md: English is open, so no TODO(i18n) may remain")
	}
}

func TestReadmeParityParser(t *testing.T) {
	doc := strings.Join([]string{
		"# masil",
		"## A",
		"```sh",
		"## not a heading",
		"masil login   # comment",
		"# whole-line comment",
		"",
		"```",
		"### B",
		paritySkip,
		"```",
		"output",
		"```",
		"- item",
		"  ```sh",
		"  masil daemon run --foreground   # x",
		"  ```",
	}, "\n")
	s, err := parseReadme(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.headings, []int{2, 3}) || s.skipped != 1 || len(s.blocks) != 2 ||
		!slices.Equal(s.blocks[0], []string{"masil login"}) || !slices.Equal(s.blocks[1], []string{"masil daemon run --foreground"}) {
		t.Fatalf("parseReadme = %+v", s)
	}
	if _, err := parseReadme("## A\n```sh\nmasil\n"); err == nil {
		t.Fatal("unclosed block accepted")
	}
}

// archivesFiles is the archives[].files list of .goreleaser.yaml.
func archivesFiles(t *testing.T) []string {
	t.Helper()
	gr := readRepo(t, ".goreleaser.yaml")
	i := strings.Index(gr, "\narchives:")
	if i < 0 {
		t.Fatal(".goreleaser.yaml: no archives section")
	}
	section := gr[i+1:]
	if end := strings.Index(section, "\n\n"); end >= 0 {
		section = section[:end]
	}
	_, files, ok := strings.Cut(section, "\n    files:\n")
	if !ok {
		t.Fatalf(".goreleaser.yaml: archives has no files:\n%s", section)
	}
	var out []string
	for _, l := range strings.Split(files, "\n") {
		if v, ok := strings.CutPrefix(l, "      - "); ok {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

func TestGoreleaserArchivesReadmes(t *testing.T) {
	files := archivesFiles(t)
	for _, want := range []string{"README.md", "README.ko.md", "LICENSE"} {
		if !slices.Contains(files, want) {
			t.Errorf(".goreleaser.yaml: archives[].files lacks %s (has %q)", want, files)
		}
	}
}

// The Homebrew formula is read by people in any language: desc is English and
// every caveat is an English line followed by the same thing in Korean.
func TestHomebrewDescEnglish(t *testing.T) {
	formula := readRepo(t, "packaging/homebrew/masil.rb.tmpl")
	if !strings.Contains(formula, "\n  desc \"Terminal chat + social feed\"\n") {
		t.Error(`masil.rb.tmpl: desc must be "Terminal chat + social feed"`)
	}
	_, caveats, ok := strings.Cut(formula, "<<~EOS\n")
	if !ok {
		t.Fatal("masil.rb.tmpl: no caveats heredoc")
	}
	caveats, _, ok = strings.Cut(caveats, "\n    EOS\n")
	if !ok {
		t.Fatal("masil.rb.tmpl: caveats heredoc not closed")
	}
	var lines []string
	for _, l := range strings.Split(caveats, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 || len(lines)%2 != 0 {
		t.Fatalf("masil.rb.tmpl: caveats must be English/Korean line pairs, got %d lines:\n%s", len(lines), caveats)
	}
	for i := 0; i < len(lines); i += 2 {
		if hasHangul(lines[i]) {
			t.Errorf("caveat line %d must be English: %q", i+1, lines[i])
		}
		if !hasHangul(lines[i+1]) {
			t.Errorf("caveat line %d must be the Korean of %q: %q", i+2, lines[i], lines[i+1])
		}
	}
}

// TestReadmeLanguageRowConditional: the Settings language row only appears
// while i18n.Selectable() (two or more languages open), so the README line
// that names its path must say so — otherwise a 0.1.3 reader (Korean only)
// looks for a row that is not there. The wording holds before and after the
// other languages open, so it is not tied to the gate.
func TestReadmeLanguageRowConditional(t *testing.T) {
	for _, c := range []struct{ rel, path, cond string }{
		{"README.md", "Display → **Language**", "Once more than one language is open"},
		{"README.ko.md", "화면 → **언어**", "언어가 둘 이상 열리면"},
	} {
		doc := readRepo(t, c.rel)
		found := false
		for _, l := range strings.Split(doc, "\n") {
			if !strings.Contains(l, c.path) {
				continue
			}
			found = true
			if !strings.Contains(l, c.cond) {
				t.Errorf("%s: the line naming %q must say %q (the row is hidden while one language is open):\n%s", c.rel, c.path, c.cond, l)
			}
		}
		if !found {
			t.Errorf("%s: no line names the Settings path %q", c.rel, c.path)
		}
	}
}
