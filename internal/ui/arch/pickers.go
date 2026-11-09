package arch

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim/pricing"
	"github.com/patrache/masil/internal/i18n"
)

// choice is one entry of a picker: the stored value (a JSON string) and
// what the list shows.
type choice struct {
	value string
	label string
}

// pickAction is what choosing in a picker does.
type pickAction int

const (
	pickField   pickAction = iota // set the field to the value
	pickConnect                   // connect the selected node to the value (a node id)
)

// picker is the open choice list of a panel row.
type picker struct {
	field  fieldDef
	obj    string
	action pickAction
	items  []choice
	cur    int
}

func (p *picker) value() string {
	if p == nil || p.cur < 0 || p.cur >= len(p.items) {
		return ""
	}
	return p.items[p.cur].value
}

// providerName is a provider's display name (company names are facts, C2.14).
func providerName(t *i18n.T, p string) string {
	switch pricing.Provider(p) {
	case pricing.AWS:
		return "AWS"
	case pricing.GCP:
		return "GCP"
	case pricing.Azure:
		return "Azure"
	case pricing.Custom:
		return t.Archcanvas.ChoiceCustom()
	}
	return cleanText(p)
}

func diskName(t *i18n.T, d archdoc.DiskType) string {
	switch d {
	case archdoc.DiskSSD:
		return t.Archcanvas.DiskSsd()
	case archdoc.DiskSSDFast:
		return t.Archcanvas.DiskSsdFast()
	case archdoc.DiskHDD:
		return t.Archcanvas.DiskHdd()
	}
	return cleanText(string(d))
}

func lbName(t *i18n.T, a archdoc.LBAlgo) string {
	switch a {
	case archdoc.LBRoundRobin:
		return t.Archcanvas.LbRoundRobin()
	case archdoc.LBLeastConn:
		return t.Archcanvas.LbLeastConn()
	}
	return t.Archcanvas.ChoiceNone()
}

func protoName(t *i18n.T, p archdoc.Protocol) string {
	switch p {
	case archdoc.ProtoHTTP1:
		return "HTTP/1.1"
	case archdoc.ProtoHTTP2:
		return "HTTP/2"
	case archdoc.ProtoGRPC:
		return "gRPC"
	case archdoc.ProtoTCP:
		return "TCP"
	case archdoc.ProtoAsync:
		return t.Archcanvas.ProtoAsync()
	}
	return cleanText(string(p))
}

// allRegions are every price-table region code (sorted, no duplicates).
func allRegions() []string {
	var out []string
	for _, p := range []pricing.Provider{pricing.AWS, pricing.GCP, pricing.Azure} {
		out = append(out, pricing.Regions(p)...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// choices are the picker entries of field f on sel.
func (m *canvasModel) choices(t *i18n.T, sel selection, f fieldDef) []choice {
	var out []choice
	switch f.Field {
	case "provider":
		for _, p := range []pricing.Provider{pricing.AWS, pricing.GCP, pricing.Azure, pricing.Custom} {
			out = append(out, choice{string(p), providerName(t, string(p))})
		}
	case "region":
		out = append(out, choice{"", t.Archcanvas.ChoiceRegionBox()})
		regions := allRegions()
		if sel.node != nil && priced(sel.node.Provider) {
			regions = pricing.Regions(pricing.Provider(sel.node.Provider))
		}
		for _, r := range regions {
			out = append(out, choice{r, r})
		}
	case "instance_type":
		if n := sel.node; n != nil && priced(n.Provider) {
			p := pricing.Provider(n.Provider)
			for _, it := range pricing.Instances(p, m.effRegion(n, p)) {
				out = append(out, choice{it.Name, it.Name})
			}
		}
	case "region_code":
		out = append(out, choice{"", t.Archcanvas.ChoiceNone()})
		for _, r := range allRegions() {
			out = append(out, choice{r, r})
		}
	case "disk.type":
		for _, d := range archdoc.DiskTypes() {
			out = append(out, choice{string(d), diskName(t, d)})
		}
	case "params.lb_algo":
		for _, a := range []archdoc.LBAlgo{archdoc.LBRoundRobin, archdoc.LBLeastConn} {
			out = append(out, choice{string(a), lbName(t, a)})
		}
	case "protocol":
		for _, p := range []archdoc.Protocol{archdoc.ProtoHTTP1, archdoc.ProtoHTTP2, archdoc.ProtoGRPC, archdoc.ProtoTCP, archdoc.ProtoAsync} {
			out = append(out, choice{string(p), protoName(t, p)})
		}
	case "traffic.curve":
		for _, c := range []archdoc.Curve{archdoc.CurveConstant, archdoc.CurveRamp, archdoc.CurveSpike, archdoc.CurveWave} {
			out = append(out, choice{string(c), curveNameT(t, c)})
		}
	case "params.failover_to":
		out = append(out, choice{"", t.Archcanvas.ChoiceNone()})
		for _, n := range m.design.Nodes {
			if n.Kind == archdoc.KindDBReplica && (sel.node == nil || n.ID != sel.node.ID) {
				out = append(out, choice{n.ID, m.label(n.ID)})
			}
		}
	}
	return out
}

// connectChoices are the nodes the selected node can connect to (the list
// view's "연결 대상 고르기"), in label order.
func (m *canvasModel) connectChoices(from string) []choice {
	var out []choice
	for _, n := range m.design.Nodes {
		if n.ID != from {
			out = append(out, choice{n.ID, m.label(n.ID)})
		}
	}
	slices.SortStableFunc(out, func(a, b choice) int {
		if c := strings.Compare(a.label, b.label); c != 0 {
			return c
		}
		return strings.Compare(a.value, b.value)
	})
	return out
}

// shown is field f's value of object obj as the panel shows it.
func (m *canvasModel) shown(t *i18n.T, sel selection, obj string, f fieldDef) string {
	v := m.raw(obj, f.Field)
	switch f.Input {
	case inputBool:
		if rawBool(v) {
			return t.Archcanvas.ChoiceOn()
		}
		return t.Archcanvas.ChoiceOff()
	case inputText:
		return cleanText(rawString(v))
	case inputRef:
		id := rawString(v)
		if id == "" {
			return t.Archcanvas.ChoiceNone()
		}
		return m.label(id)
	case inputChoice:
		s := rawString(v)
		switch f.Field {
		case "instance_type":
			if s == "" || s == string(pricing.Custom) || (sel.node != nil && pricing.Provider(sel.node.Provider) == pricing.Custom) {
				return t.Archcanvas.PanelCustomType()
			}
			return cleanText(s)
		case "region":
			if s == "" {
				return t.Archcanvas.ChoiceRegionBox()
			}
		case "region_code":
			if s == "" {
				return t.Archcanvas.ChoiceNone()
			}
		}
		for _, c := range m.choices(t, sel, f) {
			if c.value == s {
				return c.label
			}
		}
		return cleanText(s)
	}
	return typed(f, rawInt(v))
}

// jsonString is s as a JSON value.
func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func jsonInt(n int64) json.RawMessage {
	b, _ := json.Marshal(n)
	return b
}

func curveNameT(t *i18n.T, c archdoc.Curve) string {
	a := t.Archcanvas
	switch c {
	case archdoc.CurveRamp:
		return a.CurveRamp()
	case archdoc.CurveSpike:
		return a.CurveSpike()
	case archdoc.CurveWave:
		return a.CurveWave()
	case archdoc.CurveConstant:
		return a.CurveConstant()
	}
	return cleanText(string(c))
}
