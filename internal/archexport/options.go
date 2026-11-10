// Package archexport turns an architecture design into text other tools read
// (spec C2.11): Mermaid and D2 diagrams and an AWS Terraform skeleton. Output is
// deterministic and never lets user text become code: every label is a quoted,
// escaped string and identifiers are generated (n1, b1, … or label-derived
// [a-z0-9_] names). It accepts unvalidated designs (dangling references and box
// parent cycles are tolerated) and has no UI, network or file access.
package archexport

import (
	"strconv"
	"strings"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/textwidth"
)

// Options are the caller's choices for an export.
type Options struct {
	// Header lines go first as comments. The caller takes them from the catalog
	// (e.g. the "generated, review before use" notice).
	Header []string
}

// cleanText makes user text one safe line: line breaks and tabs become spaces,
// then textwidth.Sanitize drops escapes, control, bidi and other invisible
// characters.
func cleanText(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ", " ", " ", " ", " ").Replace(s)
	return strings.TrimSpace(textwidth.Sanitize(s))
}

func knownKind(k archdoc.NodeKind) bool {
	for _, x := range archdoc.NodeKinds() {
		if x == k {
			return true
		}
	}
	return false
}

// nodeLabel is the label to show: the cleaned label, else the kind.
func nodeLabel(n *archdoc.Node) string {
	if s := cleanText(n.Label); s != "" {
		return s
	}
	if knownKind(n.Kind) {
		return string(n.Kind)
	}
	return "node"
}

func boxLabel(b *archdoc.Box) string {
	if s := cleanText(b.Label); s != "" {
		return s
	}
	switch b.Kind {
	case archdoc.BoxRegion, archdoc.BoxVPC, archdoc.BoxAZ:
		return string(b.Kind)
	}
	return "box"
}

var protocolNames = map[archdoc.Protocol]string{
	archdoc.ProtoHTTP1: "HTTP/1.1", archdoc.ProtoHTTP2: "HTTP/2", archdoc.ProtoGRPC: "gRPC",
	archdoc.ProtoTCP: "TCP", archdoc.ProtoAsync: "async",
}

// edgeLabel is "protocol · latency" ("HTTP/2 · 2ms"); an unknown protocol is
// shown cleaned, the topology-default latency (-1) is left out.
func edgeLabel(e *archdoc.Edge) string {
	p, ok := protocolNames[e.Protocol]
	if !ok {
		p = cleanText(string(e.Protocol))
	}
	if e.LatencyMicros < 0 {
		return p
	}
	tenths := (int64(e.LatencyMicros) + 50) / 100
	ms := strconv.FormatInt(tenths/10, 10)
	if tenths%10 != 0 {
		ms += "." + strconv.FormatInt(tenths%10, 10)
	}
	if p == "" {
		return ms + "ms"
	}
	return p + " · " + ms + "ms"
}

func headerLines(h []string) []string {
	var out []string
	for _, l := range h {
		out = append(out, cleanText(l))
	}
	return out
}
