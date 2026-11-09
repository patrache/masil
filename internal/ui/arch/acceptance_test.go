package arch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/ui/uitest"
)

// P3 Task 27: the acceptance sweep (spec C2.16) across sizes and modes.

const longLabel = "결제-API-서버-한국"

// longLabelDesign is the fixture with its payment API named longLabel.
func longLabelDesign(t *testing.T) *archdoc.Design {
	d := loadFixture(t, "pay_v3")
	for i := range d.Nodes {
		if d.Nodes[i].ID == "api-a" {
			d.Nodes[i].Label = longLabel
		}
	}
	return d
}

// Acceptance #3: borders and Hangul labels line up at every size; the long
// label is cut with "…" on the canvas.
func TestAcceptanceSizes(t *testing.T) {
	r := fileRoute()
	h := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, longLabelDesign(t)))
	cut := regexp.MustCompile(`결제-[^ │┃]*…`)
	sawCut := false
	for _, s := range []uitest.Size{{W: 200, H: 60}, {W: 120, H: 40}, {W: 80, H: 24}, {W: 60, H: 24}, {W: 40, H: 14}} {
		h.Resize(s.W, s.H)
		h.Golden(fmt.Sprintf("%dx%d", s.W, s.H))
		h.Check()
		scr := h.Screen()
		checkEstimate(t, scr)
		if cut.MatchString(scr) {
			sawCut = true
		}
	}
	if !sawCut {
		t.Errorf("the long label %q is never cut with …", longLabel)
	}
}

// A mode switch (standard → widget → command) keeps the selection, the
// viewport's middle and the open settings panel.
func TestModeSwitchKeepsState(t *testing.T) {
	h, c := panelHarness(t, loadFixture(t, "pay_v3"))
	openPanelOn(t, h, c, "primary")
	center := func() [2]int {
		x, y := c.vp.ToWorld(c.vp.W/2, c.vp.H/2)
		return [2]int{x, y}
	}
	before := center()
	for _, s := range []uitest.Size{{W: 80, H: 24}, {W: 200, H: 60}} {
		h.Resize(s.W, s.H)
		h.Check()
		if c.sel != "primary" {
			t.Errorf("%dx%d: selection %q", s.W, s.H, c.sel)
		}
		if c.mode != modePanel || c.panel.target != "primary" {
			t.Errorf("%dx%d: panel mode %d on %q", s.W, s.H, c.mode, c.panel.target)
		}
	}
	if c.regs.Panel.W == 0 {
		t.Error("200x60: the panel is not shown")
	}
	if got := center(); got != before {
		t.Errorf("viewport middle %v, was %v", got, before)
	}
}

// Acceptance #15: the tiny mode shows the summary, runs and stops the timed
// run, and answers every edit with "창을 키워 주세요".
func TestTinySummaryEditBlocked(t *testing.T) {
	h, c := panelHarness(t, loadFixture(t, "pay_v3"))
	before := string(mustEncode(t, designOf(t, c)))
	for _, s := range []uitest.Size{{W: 50, H: 20}, {W: 40, H: 14}} {
		h.Resize(s.W, s.H)
		if c.regs.View != viewSummary {
			t.Fatalf("%dx%d: view %d, want the summary", s.W, s.H, c.regs.View)
		}
		scr := h.Screen()
		for _, want := range []string{"p99", "추정"} {
			if !strings.Contains(scr, want) {
				t.Errorf("%dx%d: summary lacks %q:\n%s", s.W, s.H, want, scr)
			}
		}
		for _, k := range []string{"a", "d", "m", "e", "c", "x"} {
			h.Press(k)
			if !strings.Contains(h.Screen(), "창을 키워 주세요") {
				t.Errorf("%dx%d: %s is not answered with the too-small note:\n%s", s.W, s.H, k, h.Screen())
			}
		}
		h.Press("space")
		if !c.pb.running {
			t.Errorf("%dx%d: space did not start the timed run", s.W, s.H)
		}
		h.Press("space")
		if c.pb.running {
			t.Errorf("%dx%d: space did not stop the timed run", s.W, s.H)
		}
		h.Check()
	}
	if string(mustEncode(t, designOf(t, c))) != before {
		t.Error("the tiny mode changed the design")
	}
}

// Acceptance #8: wherever a cost shows, "추정" and the price table date show
// with it, never cut.
func TestCostNeverTruncated(t *testing.T) {
	r := fileRoute()
	h := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, loadFixture(t, "pay_v3")))
	for _, s := range append(uitest.AllSizes(), uitest.Tiny) {
		h.Resize(s.W, s.H)
		scr := h.Screen()
		checkEstimate(t, scr)
		for _, l := range strings.Split(scr, "\n") {
			// ("a 추…" in a cut key hint is not a cost)
			if strings.Contains(l, "추정…") || strings.Contains(l, "가격…") || strings.Contains(l, "가격표…") ||
				(strings.Contains(l, "$") && strings.Contains(l, "추…")) {
				t.Errorf("%dx%d: a cost note is cut:\n%s", s.W, s.H, scr)
			}
		}
	}
}

// Acceptance #13 end to end: Terraform written through the export dialog has
// no secret, AMI or account id; the variables that would hold them have no
// default.
func TestNoSecretsInExports(t *testing.T) {
	h, c, dir := exportHarness(t)
	exportKeys(h, c, 2, 0) // Terraform → a file
	tf, err := os.ReadFile(filepath.Join(dir, "결제 시스템.tf"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(tf)
	if strings.Contains(out, "ami-") || regexp.MustCompile(`[0-9]{12}`).MatchString(out) {
		t.Error("an AMI or an account-like number in the export")
	}
	// a value for a credential attribute (RDS's manage_master_user_password =
	// true keeps the password in the provider's secret store: no value)
	if m := regexp.MustCompile(`(?i)(password|secret|access_key|token)[a-z_]*\s*=\s*"`).FindString(out); m != "" || strings.Contains(out, "AKIA") {
		t.Errorf("a credential in the export: %q", m)
	}
	for _, v := range []string{`variable "ami_id" {`, `variable "account_id" {`, `variable "db_username" {`} {
		if i := strings.Index(out, v); i >= 0 {
			if block := out[i : i+strings.Index(out[i:], "\n}")]; strings.Contains(block, "default") {
				t.Errorf("%s has a default", v)
			}
		}
	}
	if !strings.Contains(out, "추정") {
		t.Error("the export lacks the estimate notice")
	}
}

// Acceptance #14: no Hangul string literal in the P3 packages (the i18n
// ratchet allows them none) — every user string is in archcanvas.toml.
func TestRatchetZero(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	pkgs := []string{"internal/ui/arch", "internal/archdoc", "internal/archsim", "internal/archsim/pricing", "internal/archsim/challenge", "internal/archdraw", "internal/archexport"}
	legacy, err := os.ReadFile(filepath.Join(root, "internal", "i18n", "legacy_literals.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		for _, l := range strings.Split(string(legacy), "\n") {
			if f := strings.Fields(l); len(f) == 2 && f[0] == p {
				t.Errorf("%s is in legacy_literals.txt", p)
			}
		}
		files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(p), "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no sources (%v)", p, err)
		}
		n := 0
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") || strings.HasSuffix(f, "_gen.go") {
				continue
			}
			n += hangulLiterals(t, f)
		}
		if n != 0 {
			t.Errorf("%s: %d Hangul string literals — use the catalog", p, n)
		}
	}
}

func hangulLiterals(t *testing.T, file string) int {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			s = lit.Value
		}
		for _, r := range s {
			if (r >= 0xAC00 && r <= 0xD7A3) || (r >= 0x1100 && r <= 0x11FF) || (r >= 0x3130 && r <= 0x318F) {
				n++
				break
			}
		}
		return true
	})
	return n
}
