package archexport

import (
	"strings"

	"github.com/patrache/masil/internal/archdoc"
)

// d2Text escapes cleaned text for a D2 double-quoted string: backslash and
// quote are escaped and '$' becomes "\$" so "${…}" substitution cannot run.
var d2Text = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`)

func d2Quote(s string) string { return `"` + d2Text.Replace(cleanText(s)) + `"` }

// D2 writes boxes as nested maps holding their nodes ("b1: { label: …; n1: {
// label: …; shape: cylinder } }") and edges by full path ("b1.n1 -> n2:
// "HTTP/2 · 2ms""; async edges dashed). Labels are always quoted strings.
func D2(d *archdoc.Design, opt Options) []byte {
	t := newTree(d)
	var b strings.Builder
	for _, h := range headerLines(opt.Header) {
		// "$" is escaped here too so no "${" appears anywhere in the output.
		b.WriteString(strings.TrimRight("# "+strings.ReplaceAll(h, "$", `\$`), " ") + "\n")
	}
	b.WriteString("direction: right\n")
	var walk func(box string, depth int)
	walk = func(box string, depth int) {
		pad := strings.Repeat("  ", depth)
		for _, bx := range t.childBoxes[box] {
			b.WriteString(pad + t.ids[bx.ID] + ": {\n")
			b.WriteString(pad + "  label: " + d2Quote(boxLabel(bx)) + "\n")
			walk(bx.ID, depth+1)
			b.WriteString(pad + "}\n")
		}
		for _, n := range t.childNodes[box] {
			b.WriteString(pad + t.ids[n.ID] + ": {\n")
			b.WriteString(pad + "  label: " + d2Quote(nodeLabel(n)) + "\n")
			if s := d2Shape(n.Kind); s != "" {
				b.WriteString(pad + "  shape: " + s + "\n")
			}
			b.WriteString(pad + "}\n")
		}
	}
	walk("", 0)
	path := func(id string) string {
		n := t.node[id]
		var parts []string
		for _, bx := range t.boxPath(t.nodeBox(n)) {
			parts = append(parts, t.ids[bx])
		}
		return strings.Join(append(parts, t.ids[id]), ".")
	}
	for _, e := range t.edges {
		line := path(e.From) + " -> " + path(e.To)
		if l := edgeLabel(e); l != "" {
			line += ": " + d2Quote(l)
		} else if e.Protocol == archdoc.ProtoAsync {
			line += ":"
		}
		if e.Protocol == archdoc.ProtoAsync {
			line += " {style.stroke-dash: 3}"
		}
		b.WriteString(line + "\n")
	}
	return []byte(b.String())
}

func d2Shape(k archdoc.NodeKind) string {
	switch k {
	case archdoc.KindDBPrimary, archdoc.KindDBReplica:
		return "cylinder"
	case archdoc.KindQueue:
		return "queue"
	case archdoc.KindClient:
		return "person"
	case archdoc.KindCDN, archdoc.KindExternal:
		return "cloud"
	case archdoc.KindObjectStorage:
		return "stored_data"
	}
	return ""
}
