package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLocales writes files (path "ko/x.toml" => body) under a temp locales dir.
func writeLocales(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "locales")
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// problems loads and checks dir, returning every error text joined.
func problems(t *testing.T, dir string) (errs string, warns []string) {
	t.Helper()
	c, lerrs := load(dir)
	var all []string
	for _, e := range lerrs {
		all = append(all, e.Error())
	}
	if c != nil {
		cerrs, w := check(c)
		for _, e := range cerrs {
			all = append(all, e.Error())
		}
		warns = w
	}
	return strings.Join(all, "\n"), warns
}

func TestCheckRejects(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"key only in en", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nother=\"가\"\n",
			"en/tools.toml": "[a]\nother=\"A\"\n[b]\nother=\"B\"\n",
		}, "not in ko"},
		{"params differ", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nparams={ n = \"int\" }\nother=\"{n}개\"\n",
			"en/tools.toml": "[a]\nother=\"{count} items\"\n",
		}, "params differ"},
		{"ko uses undeclared", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nother=\"{n}개\"\n",
		}, "params differ"},
		{"missing plural one", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nparams={ n = \"int\" }\ncount=\"n\"\nother=\"{n}개\"\n",
			"en/tools.toml": "[a]\nother=\"{n} items\"\n",
		}, "missing plural one"},
		{"one outside en", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nparams={ n = \"int\" }\ncount=\"n\"\none=\"{n}개\"\nother=\"{n}개\"\n",
		}, "plural one only in en"},
		{"particle outside ko", map[string]string{
			"ko/tools.toml":      "[a]\ndesc=\"d\"\nparams={ x = \"string\" }\nother=\"{x|이/가} 없어요\"\n",
			"zh-Hans/tools.toml": "[a]\nother=\"{x|이/가} 没有\"\n",
		}, "particle outside ko"},
		{"D8", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nparams={ name = \"string\" }\nother=\"{name} 님이 왔어요\"\n",
		}, "D8"},
		{"ansi", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nother=\"\\u001b[1m굵게\"\n",
		}, "control"},
		{"bidi", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nother=\"가\u202e나\"\n",
		}, "control"},
		{"tab", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nother=\"가\\t나\"\n",
		}, "control"},
		{"exceeds max", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nmax=4\nother=\"알림\"\n",
			"en/tools.toml": "[a]\nother=\"Notifications\"\n",
		}, "exceeds max 4"},
		{"max counts int args as 999", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nparams={ n = \"int\" }\nmax=4\nother=\"{n}개\"\n",
		}, "exceeds max 4"},
		{"missing desc", map[string]string{
			"ko/tools.toml": "[a]\nother=\"가\"\n",
		}, "missing desc"},
		{"missing other", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\n",
		}, "missing other"},
		{"bad key", map[string]string{
			"ko/tools.toml": "[Bad-Key]\ndesc=\"d\"\nother=\"가\"\n",
		}, "bad key"},
		{"bad param type", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nparams={ n = \"float\" }\nother=\"{n}\"\n",
		}, "bad param type"},
		{"count not int", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nparams={ n = \"string\" }\ncount=\"n\"\nother=\"{n}\"\n",
		}, "count must name an int param"},
		{"meta outside ko", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nother=\"가\"\n",
			"en/tools.toml": "[a]\ndesc=\"x\"\nother=\"A\"\n",
		}, "only in ko"},
		{"short without ko short", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nother=\"가\"\n",
			"en/tools.toml": "[a]\nother=\"A\"\nshort=\"a\"\n",
		}, "short not in ko"},
		{"method name collision", map[string]string{
			"ko/tools.toml": "[a]\ndesc=\"d\"\nother=\"가\"\nshort=\"가\"\n[a_short]\ndesc=\"d\"\nother=\"나\"\n",
		}, "name collision"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := problems(t, writeLocales(t, c.files))
			if !strings.Contains(got, c.want) {
				t.Fatalf("problems = %q, want it to contain %q", got, c.want)
			}
		})
	}
}

func TestCheckAcceptsValid(t *testing.T) {
	dir := writeLocales(t, map[string]string{
		"ko/tools.toml": "[members]\ndesc=\"d\"\nparams={ n = \"int\" }\ncount=\"n\"\nmax=12\nother=\"멤버 {n}명\"\nshort=\"{n}명\"\n" +
			"[into]\ndesc=\"d\"\nparams={ space = \"string\" }\nother=\"{space|으로/로} 가요\"\n",
		"en/tools.toml": "[members]\none=\"{n} member\"\nother=\"{n} members\"\nshort=\"{n}\"\n" +
			"[into]\nother=\"Going to {space}\"\n",
		"zh-Hans/tools.toml": "[members]\nother=\"{n} 位成员\"\n",
	})
	got, warns := problems(t, dir)
	if got != "" {
		t.Fatalf("valid catalog rejected: %s", got)
	}
	if strings.Join(warns, "\n") != "tools.into: zh-Hans missing" {
		t.Fatalf("warnings = %q", warns)
	}
}

func TestCheckWarnsMissing(t *testing.T) {
	dir := writeLocales(t, map[string]string{
		"ko/tools.toml": "[hub_title]\ndesc=\"d\"\nother=\"도구\"\n",
	})
	got, warns := problems(t, dir)
	if got != "" {
		t.Fatalf("missing en must not be an error: %s", got)
	}
	if !strings.Contains(strings.Join(warns, "\n"), "tools.hub_title: en missing") {
		t.Fatalf("warnings = %q, want tools.hub_title: en missing", warns)
	}
}

func TestAreaFieldNames(t *testing.T) {
	for file, want := range map[string][2]string{
		"cli":        {"Cli", "CliT"},
		"archcanvas": {"Archcanvas", "ArchcanvasT"},
		"apitest":    {"Apitest", "ApitestT"},
		"errors":     {"Errors", "ErrorsT"},
		"x2":         {"X2", "X2T"},
	} {
		field, typ, err := areaNames(file)
		if err != nil || field != want[0] || typ != want[1] {
			t.Errorf("areaNames(%q) = %q,%q,%v; want %q,%q", file, field, typ, err, want[0], want[1])
		}
	}
	for _, bad := range []string{"a_b", "Chat", "a-b", "1x", ""} {
		if _, _, err := areaNames(bad); err == nil || !strings.Contains(err.Error(), "bad area name") {
			t.Errorf("areaNames(%q) err = %v, want bad area name", bad, err)
		}
	}
	got, _ := problems(t, writeLocales(t, map[string]string{"ko/a_b.toml": ""}))
	if !strings.Contains(got, "bad area name") {
		t.Fatalf("a_b.toml: %q", got)
	}
	got, _ = problems(t, writeLocales(t, map[string]string{"ko/Chat.toml": ""}))
	if !strings.Contains(got, "bad area name") {
		t.Fatalf("Chat.toml: %q", got)
	}
	got, _ = problems(t, writeLocales(t, map[string]string{"ko/tools.toml": "", "en/x.toml": ""}))
	if !strings.Contains(got, "area x not in ko") {
		t.Fatalf("en-only x.toml: %q", got)
	}

	// Adding one area file adds exactly one T field.
	base := map[string]string{"ko/tools.toml": "", "ko/common.toml": ""}
	before := catalogSource(t, writeLocales(t, base))
	base["ko/cli.toml"] = "[hello]\ndesc=\"d\"\nother=\"안녕\"\n"
	after := catalogSource(t, writeLocales(t, base))
	for _, s := range []string{"\tCli ", "CliT"} {
		if strings.Contains(before, s) || !strings.Contains(after, s) {
			t.Fatalf("field %q: before has=%v after has=%v", s, strings.Contains(before, s), strings.Contains(after, s))
		}
	}
	if fields(before)+1 != fields(after) {
		t.Fatalf("T fields %d -> %d, want +1", fields(before), fields(after))
	}
}

func catalogSource(t *testing.T, dir string) string {
	t.Helper()
	c, errs := load(dir)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	files, err := emit(c)
	if err != nil {
		t.Fatal(err)
	}
	return string(files["zz_catalog_gen.go"])
}

// fields counts the area fields of the generated T struct.
func fields(src string) int {
	i := strings.Index(src, "type T struct {")
	j := strings.Index(src[i:], "\n}")
	n := 0
	for _, line := range strings.Split(src[i:i+j], "\n")[1:] {
		f := strings.Fields(line)
		if len(f) == 2 && strings.HasSuffix(f[1], "T") && f[0] != "locale" {
			n++
		}
	}
	return n
}

func TestEmitMethodsAndParamOrder(t *testing.T) {
	dir := writeLocales(t, map[string]string{
		"ko/chat.toml": "[members_count]\ndesc=\"d\"\nparams={ n = \"int\", who = \"string\" }\ncount=\"n\"\nother=\"{who} {n}명\"\nshort=\"{n}명\"\n",
		"en/chat.toml": "[members_count]\none=\"{who} {n} member\"\nother=\"{who} {n} members\"\n",
	})
	c, errs := load(dir)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	files, err := emit(c)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files["zz_chat_gen.go"])
	for _, want := range []string{
		"func (a_ ChatT) MembersCount(n int, who string) string",
		"func (a_ ChatT) MembersCountShort(n int, who string) string",
		"// Code generated",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("zz_chat_gen.go lacks %q:\n%s", want, src)
		}
	}
}

func TestFormatTOMLSortsAndKeepsHeader(t *testing.T) {
	dir := writeLocales(t, map[string]string{
		"ko/tools.toml": "# header line\n\n[zeta]\nother=\"z\"\ndesc=\"마지막\"\n[alpha]\nparams={ b = \"string\", a = \"int\" }\nother=\"{a}{b}\"\ndesc=\"처음\"\n",
	})
	c, errs := load(dir)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	got := string(formatTOML(c.Areas[0].files["ko"]))
	want := "# header line\n\n[alpha]\ndesc = \"처음\"\nparams = { b = \"string\", a = \"int\" }\nother = \"{a}{b}\"\n\n[zeta]\ndesc = \"마지막\"\nother = \"z\"\n"
	if got != want {
		t.Fatalf("formatTOML =\n%s\nwant\n%s", got, want)
	}
}
