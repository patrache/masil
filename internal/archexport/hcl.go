package archexport

import (
	"strconv"
	"strings"
)

// hclString is a quoted HCL string literal of cleaned user text: '\' and '"'
// are escaped and "${" / "%{" are doubled to "$${" / "%%{" so the text can
// never start an interpolation or a template directive.
func hclString(s string) string {
	s = cleanText(s)
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			b.WriteString(`\\`)
		case c == '"':
			b.WriteString(`\"`)
		case (c == '$' || c == '%') && i+1 < len(s) && s[i+1] == '{':
			b.WriteByte(c)
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

const maxIdent = 48

// identPart lowercases ASCII letters and keeps digits; every other run of
// characters becomes one '_', trimmed at both ends.
func identPart(s string) string {
	var b strings.Builder
	under := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			under = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
			under = false
		default:
			if !under && b.Len() > 0 {
				b.WriteByte('_')
			}
			under = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// tfIdent is a Terraform name from a label: [a-z0-9_], "r_" in front of a
// leading digit, the fallback (the node kind) when nothing is left, and "_2",
// "_3", … when the name is taken. used records every name handed out.
func tfIdent(label, fallback string, used map[string]int) string {
	base := identPart(label)
	if base == "" {
		base = identPart(fallback)
	}
	if base == "" {
		base = "r"
	}
	if base[0] >= '0' && base[0] <= '9' {
		base = "r_" + base
	}
	if len(base) > maxIdent {
		base = strings.TrimRight(base[:maxIdent], "_")
	}
	if used[base] == 0 {
		used[base] = 1
		return base
	}
	for n := max(used[base], 1) + 1; ; n++ {
		if c := base + "_" + strconv.Itoa(n); used[c] == 0 {
			used[base], used[c] = n, 1
			return c
		}
	}
}

// hclWriter writes HCL the way `terraform fmt` does: two-space indent and the
// "=" of consecutive one-line attributes aligned.
type hclWriter struct {
	b     strings.Builder
	depth int
	run   [][2]string // pending attributes (name, expression)
}

func (w *hclWriter) pad() string { return strings.Repeat("  ", w.depth) }

func (w *hclWriter) flush() {
	width := 0
	for _, a := range w.run {
		width = max(width, len(a[0]))
	}
	for _, a := range w.run {
		w.b.WriteString(w.pad() + a[0] + strings.Repeat(" ", width-len(a[0])) + " = " + a[1] + "\n")
	}
	w.run = w.run[:0]
}

// attr adds "name = expr" (expr is HCL already: a quoted literal or a reference).
func (w *hclWriter) attr(name, expr string) { w.run = append(w.run, [2]string{name, expr}) }

func (w *hclWriter) line(s string) {
	w.flush()
	if s == "" {
		w.b.WriteString("\n")
		return
	}
	w.b.WriteString(w.pad() + s + "\n")
}

func (w *hclWriter) comment(s string) { w.line("# " + s) }

// open starts a block ("resource \"aws_vpc\" \"main\"") or an object
// attribute ("tags =").
func (w *hclWriter) open(head string) {
	w.line(head + " {")
	w.depth++
}

func (w *hclWriter) close() {
	w.flush()
	w.depth--
	w.line("}")
}

func (w *hclWriter) String() string {
	w.flush()
	return w.b.String()
}

func list(items []string) string { return "[" + strings.Join(items, ", ") + "]" }
