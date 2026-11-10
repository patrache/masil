package leetcode

import (
	"strings"
	"testing"
)

func TestLanguagesTable(t *testing.T) {
	if len(Languages) != 19 {
		t.Fatalf("len = %d", len(Languages))
	}
	want := []string{"golang", "python3", "python", "cpp", "java", "c", "csharp", "javascript", "typescript", "php",
		"swift", "kotlin", "dart", "ruby", "scala", "rust", "racket", "erlang", "elixir"}
	seen := map[string]bool{}
	for i, l := range Languages {
		if l.Slug != want[i] {
			t.Errorf("Languages[%d] = %q, want %q", i, l.Slug, want[i])
		}
		if seen[l.Slug] {
			t.Errorf("duplicate %q", l.Slug)
		}
		seen[l.Slug] = true
		if !strings.HasPrefix(l.Ext, ".") || l.Name == "" {
			t.Errorf("%+v", l)
		}
	}
	if l, ok := LangBySlug(DefaultLang); !ok || l.Name != "Go" || l.Ext != ".go" {
		t.Fatalf("default = %+v %v", l, ok)
	}
	if l, ok := LangBySlug("csharp"); !ok || l.Name != "C#" || l.Ext != ".cs" {
		t.Fatalf("csharp = %+v", l)
	}
	if _, ok := LangBySlug("cobol"); ok {
		t.Fatal("cobol found")
	}
}
