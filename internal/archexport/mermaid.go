package archexport

import (
	"strings"

	"github.com/patrache/masil/internal/archdoc"
)

// mermaidText escapes cleaned text for a Mermaid quoted label: the quote,
// angle brackets, '#', '%', '&' and '`' become entity codes, so a label can
// neither end its string nor form a "%%{…}%%" directive or a markdown string.
var mermaidText = strings.NewReplacer(
	"#", "#35;", `"`, "#quot;", "<", "#lt;", ">", "#gt;", "%", "#37;", "&", "#38;", "`", "#96;",
)

func mermaidQuote(s string) string { return `"` + mermaidText.Replace(cleanText(s)) + `"` }

// Mermaid writes a "flowchart LR": boxes as nested subgraphs, nodes with a
// fixed shape per kind (databases cylinders, queues parallelograms), edges
// with "protocol · latency" labels (async edges dotted). No user text appears
// outside a quoted label, and the output never holds "click" or "%%{"
// directives from user text.
func Mermaid(d *archdoc.Design, opt Options) []byte {
	t := newTree(d)
	var b strings.Builder
	for _, h := range headerLines(opt.Header) {
		// "{" gets a space in front so a header can never form "%%{".
		b.WriteString(strings.TrimRight("%% "+strings.ReplaceAll(h, "{", " {"), " ") + "\n")
	}
	b.WriteString("flowchart LR\n")
	var walk func(box string, depth int)
	walk = func(box string, depth int) {
		pad := strings.Repeat("    ", depth)
		for _, bx := range t.childBoxes[box] {
			b.WriteString(pad + "subgraph " + t.ids[bx.ID] + "[" + mermaidQuote(boxLabel(bx)) + "]\n")
			walk(bx.ID, depth+1)
			b.WriteString(pad + "end\n")
		}
		for _, n := range t.childNodes[box] {
			b.WriteString(pad + t.ids[n.ID] + mermaidShape(n.Kind, mermaidQuote(nodeLabel(n))) + "\n")
		}
	}
	walk("", 1)
	for _, e := range t.edges {
		arrow := " -->"
		if e.Protocol == archdoc.ProtoAsync {
			arrow = " -.->"
		}
		label := ""
		if l := edgeLabel(e); l != "" {
			label = "|" + mermaidQuote(l) + "|"
		}
		b.WriteString("    " + t.ids[e.From] + arrow + label + " " + t.ids[e.To] + "\n")
	}
	return []byte(b.String())
}

func mermaidShape(k archdoc.NodeKind, q string) string {
	switch k {
	case archdoc.KindDBPrimary, archdoc.KindDBReplica:
		return "[(" + q + ")]"
	case archdoc.KindQueue:
		return "[/" + q + "/]"
	}
	return "[" + q + "]"
}
