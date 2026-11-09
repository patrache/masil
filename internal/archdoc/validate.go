package archdoc

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// IssueCode names one validation problem (arch.md 1.3).
type IssueCode string

const (
	IssueTooLarge      IssueCode = "too_large"
	IssueSyntax        IssueCode = "syntax" // not one JSON object
	IssueSchemaVersion IssueCode = "schema_version"
	IssueTooManyNodes  IssueCode = "too_many_nodes"
	IssueTooManyEdges  IssueCode = "too_many_edges"
	IssueTooManyBoxes  IssueCode = "too_many_boxes"
	IssueTooManyFaults IssueCode = "too_many_faults"
	IssueDepth         IssueCode = "depth"
	IssueUnknownKind   IssueCode = "unknown_kind"
	IssueDanglingRef   IssueCode = "dangling_ref"
	IssueDuplicateID   IssueCode = "duplicate_id"
	IssueRange         IssueCode = "range"
	IssueLabelTooLong  IssueCode = "label_too_long"
	IssueParentKind    IssueCode = "parent_kind"
)

// Issue is one problem at a path such as "nodes[0].instances" (design JSON) or
// "n:<id>.instances" (collab objects).
type Issue struct {
	Path string
	Code IssueCode
}

func (i Issue) String() string { return i.Path + ": " + string(i.Code) }

// ErrInvalid is matched by every *InvalidError.
var ErrInvalid = errors.New("archdoc: invalid design")

// InvalidError carries the issues of a rejected design.
type InvalidError struct{ Issues []Issue }

func (e *InvalidError) Error() string {
	parts := make([]string, 0, min(len(e.Issues), 5))
	for i, is := range e.Issues {
		if i == 5 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, is.String())
	}
	return ErrInvalid.Error() + ": " + strings.Join(parts, ", ")
}

func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }

func invalid(is ...Issue) error { return &InvalidError{Issues: is} }

// graphemes counts user-perceived characters.
func graphemes(s string) int {
	n := 0
	for s != "" {
		g, _ := ansi.FirstGraphemeCluster(s, ansi.GraphemeWidth)
		if g == "" {
			_, w := utf8.DecodeRuneInString(s)
			g = s[:w]
		}
		s = s[len(g):]
		n++
	}
	return n
}

// validID reports whether id fits ^[A-Za-z0-9_-]{1,62}$ so "n:"+id is a valid
// collab object id (collab.md 1.2).
func validID(id string) bool {
	if len(id) < 1 || len(id) > 62 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func checkAll[T any](v *T, defs []fieldDef[T], path string, is *[]Issue) {
	for _, def := range defs {
		ptr := def.at(v, false)
		if ptr == nil {
			continue
		}
		if code := checkField(def, ptr); code != "" {
			*is = append(*is, Issue{Path: path + def.name, Code: code})
		}
	}
}

func at(list string, i int) string { return list + "[" + strconv.Itoa(i) + "]." }

// Validate returns every problem with d, in a fixed order; nil = valid.
func (d *Design) Validate() []Issue {
	var is []Issue
	add := func(path string, code IssueCode) { is = append(is, Issue{Path: path, Code: code}) }
	if d.SchemaVersion != SchemaVersion {
		add("schema_version", IssueSchemaVersion)
	}
	checkAll(d, metaFields, "", &is)
	for _, c := range []struct {
		list   string
		n, max int
		code   IssueCode
	}{
		{"nodes", len(d.Nodes), MaxNodes, IssueTooManyNodes},
		{"edges", len(d.Edges), MaxEdges, IssueTooManyEdges},
		{"boxes", len(d.Boxes), MaxBoxes, IssueTooManyBoxes},
		{"faults", len(d.Faults), MaxFaults, IssueTooManyFaults},
	} {
		if c.n > c.max {
			add(c.list, c.code)
		}
	}

	seen := map[string]bool{}
	id := func(path, id string) {
		switch {
		case !validID(id):
			add(path+"id", IssueRange)
		case seen[id]:
			add(path+"id", IssueDuplicateID)
		}
		seen[id] = true
	}
	nodes := map[string]*Node{}
	boxes := map[string]*Box{}
	edges := map[string]bool{}
	for i := range d.Nodes {
		nodes[d.Nodes[i].ID] = &d.Nodes[i]
	}
	for i := range d.Boxes {
		boxes[d.Boxes[i].ID] = &d.Boxes[i]
	}
	for i := range d.Edges {
		edges[d.Edges[i].ID] = true
	}

	for i := range d.Nodes {
		n := &d.Nodes[i]
		p := at("nodes", i)
		id(p, n.ID)
		checkAll(n, nodeFields, p, &is)
		if n.Parent != "" && boxes[n.Parent] == nil {
			add(p+"parent", IssueDanglingRef)
		}
		if f := n.Params.FailoverTo; f != "" && (nodes[f] == nil || f == n.ID) {
			add(p+"params.failover_to", IssueDanglingRef)
		}
		if n.Autoscale.Enabled && n.Autoscale.Min > n.Autoscale.Max {
			add(p+"autoscale.max", IssueRange)
		}
	}
	for i := range d.Boxes {
		b := &d.Boxes[i]
		p := at("boxes", i)
		id(p, b.ID)
		checkAll(b, boxFields, p, &is)
		if b.Parent == "" {
			continue
		}
		parent := boxes[b.Parent]
		if parent == nil {
			add(p+"parent", IssueDanglingRef)
			continue
		}
		if !parentKindOK(b.Kind, parent.Kind) {
			add(p+"parent", IssueParentKind)
		}
	}
	for i := range d.Boxes {
		if boxDepth(boxes, &d.Boxes[i]) > MaxDepth {
			add(at("boxes", i)+"parent", IssueDepth)
		}
	}
	for i := range d.Edges {
		e := &d.Edges[i]
		p := at("edges", i)
		id(p, e.ID)
		checkAll(e, edgeFields, p, &is)
		if nodes[e.From] == nil {
			add(p+"from", IssueDanglingRef)
		}
		if nodes[e.To] == nil {
			add(p+"to", IssueDanglingRef)
		}
	}
	for i := range d.Faults {
		f := &d.Faults[i]
		p := at("faults", i)
		id(p, f.ID)
		checkAll(f, faultFields, p, &is)
		if !faultTargetOK(f, nodes, boxes, edges) {
			add(p+"target", IssueDanglingRef)
		}
	}
	for i := range is {
		is[i].Path = strings.TrimSuffix(is[i].Path, ".")
	}
	return is
}

// parentKindOK is the box nesting rule AZ ⊂ VPC ⊂ region; a VPC or AZ may also
// sit at the top level (Check warns about that).
func parentKindOK(child, parent BoxKind) bool {
	switch child {
	case BoxVPC:
		return parent == BoxRegion
	case BoxAZ:
		return parent == BoxVPC
	}
	return false
}

// boxDepth is 1 for a top-level box; a parent cycle counts as too deep.
func boxDepth(boxes map[string]*Box, b *Box) int {
	depth := 1
	for b.Parent != "" {
		p := boxes[b.Parent]
		if p == nil {
			break
		}
		depth++
		if depth > MaxDepth+len(boxes) { // only a cycle gets here
			return depth
		}
		b = p
	}
	return depth
}

func faultTargetOK(f *Fault, nodes map[string]*Node, boxes map[string]*Box, edges map[string]bool) bool {
	switch f.Kind {
	case FaultNodeOff, FaultInstancesOff:
		return nodes[f.Target] != nil
	case FaultLatency:
		return nodes[f.Target] != nil || edges[f.Target]
	case FaultAZOff:
		return boxes[f.Target] != nil && boxes[f.Target].Kind == BoxAZ
	case FaultRegionOff:
		return boxes[f.Target] != nil && boxes[f.Target].Kind == BoxRegion
	}
	return true // unknown kind is already reported on .kind
}
