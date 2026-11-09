package archexport

import (
	"bytes"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
)

var tfHeader = []string{"자동 생성된 뼈대 · 실행 전 검토 필요 · masil 은 이 파일을 실행하지 않음", "${not} %{code}"}

func TestTerraformGolden(t *testing.T) {
	for _, name := range []string{"three_tier", "autoscale", "mixed_providers"} {
		t.Run(name, func(t *testing.T) {
			golden.RequireEqual(t, Terraform(loadDesign(t, name), Options{Header: tfHeader}))
		})
	}
}

// hclLiterals scans HCL source: it returns every double-quoted string literal
// (raw, escapes kept), skips # and // comments, counts braces outside strings
// and reports a newline inside a literal.
func hclLiterals(t *testing.T, label, src string) []string {
	t.Helper()
	var lits []string
	depth := 0
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '#' || c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '"':
			start := i + 1
			for i++; i < len(src) && src[i] != '"'; i++ {
				switch src[i] {
				case '\\':
					i++
				case '\n':
					t.Fatalf("label %q: newline inside a string literal", label)
				}
			}
			if i >= len(src) {
				t.Fatalf("label %q: unterminated string", label)
			}
			lits = append(lits, src[start:i])
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth < 0 {
				t.Fatalf("label %q: unbalanced braces", label)
			}
		}
	}
	if depth != 0 {
		t.Fatalf("label %q: unbalanced braces (%d)", label, depth)
	}
	return lits
}

// interpolates lexes a literal left to right the way HCL does (checked with
// terraform console: "a$$${" ends the string, "%%%{ if }" is no directive): a
// backslash escapes one byte, "$${" and "%%{" are a literal "${" and "%{", and
// any other "${" or "%{" starts an interpolation or directive.
func interpolates(lit string) bool {
	for i := 0; i < len(lit); i++ {
		c := lit[i]
		switch {
		case c == '\\':
			i++
		case c != '$' && c != '%':
		case i+2 < len(lit) && lit[i+1] == c && lit[i+2] == '{':
			i += 2
		case i+1 < len(lit) && lit[i+1] == '{':
			return true
		}
	}
	return false
}

func TestTerraformHostileLabels(t *testing.T) {
	for _, label := range hostileLabels(t) {
		out := string(Terraform(hostileDesign(t, label), Options{Header: []string{label}}))
		checkClean(t, label, out)
		for _, lit := range hclLiterals(t, label, out) {
			if interpolates(lit) {
				t.Fatalf("label %q: interpolation in %q", label, lit)
			}
		}
	}
}

func TestInterpolatesHelper(t *testing.T) {
	for lit, want := range map[string]bool{
		"${x}": true, "$${x}": false, "$$${x}": false, "$$$${x}": false, "%{if}": true, "%%{if}": false, "%%%{if}": false,
		"$Latest": false, "a$b{": false, `\\${x}`: true, `a\"${x}`: true,
	} {
		if interpolates(lit) != want {
			t.Fatalf("interpolates(%q) = %v", lit, !want)
		}
	}
}

func TestHCLStringEscapes(t *testing.T) {
	for in, want := range map[string]string{
		`a"b\c`:          `"a\"b\\c"`,
		"${file(\"x\")}": `"$${file(\"x\")}"`,
		"%{ if }":        `"%%{ if }"`,
		"$${already}":    `"$$${already}"`,
		"a\nb\x1b[31m‮":  `"a b"`,
	} {
		if got := hclString(in); got != want {
			t.Fatalf("hclString(%q) = %s, want %s", in, got, want)
		}
	}
}

var digits12 = regexp.MustCompile(`[0-9]{12}`)

func TestTerraformNoSecrets(t *testing.T) {
	for _, name := range []string{"three_tier", "autoscale", "mixed_providers"} {
		out := string(Terraform(loadDesign(t, name), Options{Header: tfHeader}))
		if strings.Contains(out, "ami-") || digits12.MatchString(out) {
			t.Fatalf("%s: AMI or account-like number in output", name)
		}
		for _, v := range []string{`variable "ami_id" {`, `variable "account_id" {`, `variable "db_username" {`} {
			if i := strings.Index(out, v); i >= 0 {
				block := out[i : i+strings.Index(out[i:], "\n}")]
				if strings.Contains(block, "default") {
					t.Fatalf("%s: %s has a default", name, v)
				}
			}
		}
		if !strings.Contains(out, "var.ami_id") || !strings.Contains(out, `variable "ami_id" {`) {
			t.Fatalf("%s: launch templates do not use var.ami_id", name)
		}
	}
}

func TestTFIdentUnique(t *testing.T) {
	used := map[string]int{}
	var got []string
	for _, l := range []string{"API 서버", "api-서버", "123", ""} {
		got = append(got, tfIdent(l, "api", used))
	}
	if strings.Join(got, ",") != "api,api_2,r_123,api_3" {
		t.Fatalf("got %v", got)
	}
	ok := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	for _, l := range append(hostileLabels(t), "api_2", "Ünïcode", "___", "9lives") {
		if id := tfIdent(l, "worker", used); !ok.MatchString(id) {
			t.Fatalf("tfIdent(%q) = %q", l, id)
		}
	}
	if id := tfIdent("api_2", "api", map[string]int{"api": 2, "api_2": 1}); id != "api_2_2" {
		t.Fatalf("taken name reused: %q", id)
	}
}

func TestTerraformFmt(t *testing.T) {
	tf, err := exec.LookPath("terraform")
	if err != nil {
		t.Skip("terraform not installed")
	}
	var hostile []byte // every hostile output must parse and be formatted too
	for _, label := range hostileLabels(t) {
		hostile = append(hostile, Terraform(hostileDesign(t, label), Options{Header: []string{label}})...)
	}
	srcs := map[string][]byte{"hostile": hostile}
	for _, name := range []string{"three_tier", "autoscale", "mixed_providers"} {
		srcs[name] = Terraform(loadDesign(t, name), Options{Header: tfHeader})
	}
	for name, src := range srcs {
		cmd := exec.Command(tf, "fmt", "-check", "-")
		cmd.Stdin = bytes.NewReader(src)
		if out, err := cmd.CombinedOutput(); err != nil {
			fix := exec.Command(tf, "fmt", "-")
			fix.Stdin = bytes.NewReader(src)
			formatted, _ := fix.CombinedOutput()
			t.Fatalf("%s: terraform fmt -check: %v\n%s\nformatted:\n%s", name, err, out, formatted)
		}
	}
}
