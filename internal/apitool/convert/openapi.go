package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/ids"
)

var (
	// ErrYAMLUnsupported: the document is not JSON (YAML waits for the
	// owner's approval of a YAML module, C3.3).
	ErrYAMLUnsupported = errors.New("convert: OpenAPI YAML is not supported, convert it to JSON")
	// ErrNotOpenAPI: the JSON is not an OpenAPI 3.0/3.1 document.
	ErrNotOpenAPI = errors.New("convert: not an OpenAPI 3.0/3.1 document")
)

// OpenAPIEnvName names the environment holding base_url and the auth
// placeholders an OpenAPI import creates.
const OpenAPIEnvName = "default"

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// headers OpenAPI says to ignore as parameters.
var ignoredHeaderParams = map[string]bool{"accept": true, "content-type": true, "authorization": true}

var pathParam = regexp.MustCompile(`\{([^{}/]+)\}`)

type oaImporter struct {
	root    *object
	rep     Report
	col     *apitool.Collection
	env     *apitool.Env
	folders map[string]*apitool.Folder
	seenRef map[string]bool
	colIdx  map[string]int // col.Vars index by name
	out     int            // text produced so far (MaxImportOutput)
	work    int            // steps taken so far (MaxImportWork)
}

// charge counts n produced bytes; over the budget reports true.
func (im *oaImporter) charge(n int) bool {
	im.out += n + rowCost
	return im.over()
}

func (im *oaImporter) over() bool { return im.out > MaxImportOutput || im.work > MaxImportWork }

// step counts n steps of work; over the budget reports true.
func (im *oaImporter) step(n int) bool {
	im.work += n
	return im.over()
}

// encode is encodeJSON within what is left of the output budget; past it
// the import is over budget (run stops with ErrTooLarge) and "" is returned.
func (im *oaImporter) encode(v any, indent bool) string {
	s, err := encodeCapped(v, indent, max(MaxImportOutput-im.out, 0))
	if err != nil {
		im.out = MaxImportOutput + 1
		return ""
	}
	return s
}

// validPath: a path key appended to {{base_url}} must stay a path ("@x" or
// "x.com/…" would change the host, "//x" is a host to a scheme-only base,
// '\\' is read as '/' by some clients). After a single leading "/" the
// authority has ended, so a '@' there is only path (/users/@me).
func validPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.Contains(p, "\\")
}

// ImportOpenAPI reads an OpenAPI 3.0/3.1 JSON document (C3.9): every path
// and method becomes a request in the folder of its first tag, named by
// summary, operationId or "METHOD path"; path parameters become {{vars}}
// (collection variables), query and header parameters rows (optional ones
// off), the request body example the body, servers[0] the base_url of the
// "default" environment, and the security scheme an auth helper with a
// {{token}} (or {{username}}/{{password}}) placeholder. Only same-document
// $ref ("#/…") are followed; remote ones are listed in Report.Ignored and
// never fetched.
func ImportOpenAPI(data []byte) (*apitool.Collection, Report, error) {
	if err := checkSize(len(data)); err != nil {
		return nil, Report{}, err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	switch {
	case len(trimmed) == 0:
		return nil, Report{}, ErrNotOpenAPI
	case trimmed[0] == '[':
		return nil, Report{}, ErrNotOpenAPI
	case trimmed[0] != '{':
		return nil, Report{}, ErrYAMLUnsupported
	}
	v, err := decodeJSON(trimmed)
	if err != nil {
		return nil, Report{}, err
	}
	root, _ := v.(*object)
	ver := str(root.get("openapi"))
	if !strings.HasPrefix(ver, "3.0") && !strings.HasPrefix(ver, "3.1") {
		return nil, Report{}, ErrNotOpenAPI
	}
	im := &oaImporter{root: root, folders: map[string]*apitool.Folder{}, seenRef: map[string]bool{}, colIdx: map[string]int{}}
	return im.run()
}

func (im *oaImporter) run() (*apitool.Collection, Report, error) {
	name := strings.TrimSpace(str(obj(im.root.get("info")).get("title")))
	if name == "" {
		name = "openapi"
	}
	im.col = &apitool.Collection{Version: apitool.CollectionVersion, ID: ids.New(), Name: name, Requests: map[string]*apitool.Request{}}
	im.env = &apitool.Env{ID: ids.New(), Name: OpenAPIEnvName, Vars: []apitool.Var{{Name: "base_url", Value: im.baseURL()}}}

	tooLarge := func() (*apitool.Collection, Report, error) {
		if im.work > MaxImportWork {
			return nil, Report{}, fmt.Errorf("%w (an import takes more than %d steps)", ErrTooLarge, MaxImportWork)
		}
		return nil, Report{}, fmt.Errorf("%w (an import makes more than %d bytes)", ErrTooLarge, MaxImportOutput)
	}
	paths := obj(im.root.get("paths"))
	if paths != nil {
		for _, p := range paths.keys {
			if im.step(1) {
				return tooLarge()
			}
			if !validPath(p) {
				im.rep.Ignored = append(im.rep.Ignored, "path: "+p)
				continue
			}
			item := obj(im.resolve(paths.vals[p]))
			if item == nil {
				continue
			}
			for _, m := range item.keys {
				if im.step(1) {
					return tooLarge()
				}
				if !isMethod(m) {
					continue
				}
				if op := obj(im.resolve(item.vals[m])); op != nil {
					im.operation(p, strings.ToUpper(m), item, op)
				}
				if im.over() {
					return tooLarge()
				}
			}
		}
	}
	if im.over() {
		return tooLarge()
	}
	if hooks := obj(im.root.get("webhooks")); hooks != nil {
		for _, k := range hooks.keys {
			im.rep.Ignored = append(im.rep.Ignored, "webhooks: "+k)
		}
	}
	im.col.Envs = []apitool.Env{*im.env}
	im.rep.Suggestions = append(im.rep.Suggestions, suggestVars(im.col.Vars)...)
	im.rep.Suggestions = append(im.rep.Suggestions, suggestVars(im.env.Vars)...)
	return im.col, im.rep, nil
}

func isMethod(m string) bool {
	for _, h := range httpMethods {
		if m == h {
			return true
		}
	}
	return false
}

// baseURL is servers[0].url with its variables' defaults filled in and no
// trailing slash.
func (im *oaImporter) baseURL() string {
	servers, _ := im.root.get("servers").([]any)
	if len(servers) == 0 {
		return ""
	}
	s := obj(servers[0])
	u := str(s.get("url"))
	vars := obj(s.get("variables"))
	u = pathParam.ReplaceAllStringFunc(u, func(m string) string {
		if v := obj(vars.get(m[1 : len(m)-1])); v != nil {
			if d, ok := v.get("default").(string); ok {
				return d
			}
		}
		return m
	})
	return strings.TrimRight(u, "/")
}

// resolve follows same-document $ref chains (at most MaxDepth hops). A
// remote, broken or cyclic $ref is reported and resolves to nil.
func (im *oaImporter) resolve(v any) any {
	first := ""
	for range MaxDepth {
		o := obj(v)
		if o == nil {
			return v
		}
		ref, ok := o.get("$ref").(string)
		if !ok {
			return v
		}
		if first == "" {
			first = ref
		}
		if im.step(1 + len(ref)/rowCost) {
			return nil // run stops with ErrTooLarge
		}
		if !strings.HasPrefix(ref, "#/") {
			im.ignoreRef("$ref " + ref)
			return nil
		}
		v = im.pointer(ref[2:])
		if v == nil {
			im.ignoreRef("$ref " + ref)
			return nil
		}
	}
	im.ignoreRef("$ref " + first + " (cycle)")
	return nil
}

func (im *oaImporter) ignoreRef(s string) {
	if !im.seenRef[s] {
		im.seenRef[s] = true
		im.rep.Ignored = append(im.rep.Ignored, s)
	}
}

// pointer walks a JSON pointer (after "#/") from the document root.
func (im *oaImporter) pointer(p string) any {
	var cur any = im.root
	for more := true; more; {
		var seg string
		seg, p, more = strings.Cut(p, "/")
		if s, err := url.PathUnescape(seg); err == nil {
			seg = s
		}
		seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
		switch t := cur.(type) {
		case *object:
			v, ok := t.vals[seg]
			if !ok {
				return nil
			}
			cur = v
		case []any:
			i := -1
			if _, err := fmt.Sscanf(seg, "%d", &i); err != nil || i < 0 || i >= len(t) {
				return nil
			}
			cur = t[i]
		default:
			return nil
		}
	}
	return cur
}

func (im *oaImporter) operation(path, method string, item, op *object) {
	label := method + " " + path
	r := &apitool.Request{ID: ids.New(), Method: method}
	r.Name = strings.TrimSpace(str(op.get("summary")))
	if r.Name == "" {
		r.Name = strings.TrimSpace(str(op.get("operationId")))
	}
	if r.Name == "" {
		r.Name = label
	}
	r.URL = "{{base_url}}" + pathParam.ReplaceAllStringFunc(path, func(m string) string {
		name := m[1 : len(m)-1]
		im.colVar(name, "")
		return "{{" + name + "}}"
	})
	if im.charge(len(r.Name) + len(r.URL)) {
		return
	}

	for _, p := range im.params(item, op) {
		name := str(p.get("name"))
		if name == "" {
			continue
		}
		required, _ := p.get("required").(bool)
		val := im.example(p)
		if im.charge(len(name) + len(val)) {
			return
		}
		switch str(p.get("in")) {
		case "query":
			r.Query = append(r.Query, apitool.KV{Key: name, Value: val, Off: !required})
		case "header":
			if ignoredHeaderParams[strings.ToLower(name)] {
				continue
			}
			r.Headers = append(r.Headers, apitool.KV{Key: name, Value: val, Off: !required})
		case "path":
			im.colVar(name, "") // empty (plan Task 10): an example id is not a value to send
		case "cookie":
			im.rep.Ignored = append(im.rep.Ignored, "cookie parameter: "+name+" ("+label+")")
		}
	}
	if rb := obj(im.resolve(op.get("requestBody"))); rb != nil {
		r.Body = im.body(obj(rb.get("content")))
	}
	if im.charge(len(r.Body.Text)) {
		return
	}
	im.auth(r, op, label)

	im.col.Requests[r.ID] = r
	folder := &im.col.Root
	if tags, _ := op.get("tags").([]any); len(tags) > 0 {
		if tag := strings.TrimSpace(str(tags[0])); tag != "" {
			folder = im.folder(tag)
		}
	}
	folder.Items = append(folder.Items, apitool.Node{Request: r.ID})
	im.rep.Suggestions = append(im.rep.Suggestions, suggest(r)...)
}

func (im *oaImporter) folder(tag string) *apitool.Folder {
	if f := im.folders[tag]; f != nil {
		return f
	}
	f := &apitool.Folder{ID: ids.New(), Name: tag}
	im.col.Root.Items = append(im.col.Root.Items, apitool.Node{Folder: f})
	im.folders[tag] = f
	return f
}

// params merges path-level and operation parameters (the operation wins
// for the same name and location), resolved.
func (im *oaImporter) params(item, op *object) []*object {
	var out []*object
	idx := map[string]int{}
	for _, list := range []any{item.get("parameters"), op.get("parameters")} {
		arr, _ := list.([]any)
		for _, raw := range arr {
			if im.step(1) {
				return out // run stops with ErrTooLarge
			}
			p := obj(im.resolve(raw))
			if p == nil {
				continue
			}
			key := str(p.get("in")) + "\x00" + str(p.get("name"))
			if i, ok := idx[key]; ok {
				out[i] = p
				continue
			}
			idx[key] = len(out)
			out = append(out, p)
		}
	}
	return out
}

func (im *oaImporter) colVar(name, value string) {
	if i, ok := im.colIdx[name]; ok {
		if im.col.Vars[i].Value == "" {
			im.col.Vars[i].Value = value
		}
		return
	}
	im.colIdx[name] = len(im.col.Vars)
	im.col.Vars = append(im.col.Vars, apitool.Var{Name: name, Value: value})
}

func (im *oaImporter) envVar(name string, secret bool) {
	for _, v := range im.env.Vars {
		if v.Name == name {
			return
		}
	}
	im.env.Vars = append(im.env.Vars, apitool.Var{Name: name, Secret: secret})
}

// exampleOf finds an example for a parameter, media type or schema:
// example, examples (map of {value} or 3.1 array), the schema's example,
// examples, default or first enum value.
func (im *oaImporter) exampleOf(o *object) (any, bool) { return im.exampleAt(o, 0) }

func (im *oaImporter) exampleAt(o *object, depth int) (any, bool) {
	if o == nil || depth > 8 {
		return nil, false
	}
	if v, ok := o.vals["example"]; ok {
		return v, true
	}
	switch ex := o.get("examples").(type) {
	case *object:
		for _, k := range ex.keys {
			if im.step(1) {
				return nil, false // run stops with ErrTooLarge
			}
			if e := obj(im.resolve(ex.vals[k])); e != nil {
				if v, ok := e.vals["value"]; ok {
					return v, true
				}
			}
		}
	case []any:
		if len(ex) > 0 {
			return ex[0], true
		}
	}
	if s := obj(im.resolve(o.get("schema"))); s != nil && s != o {
		if v, ok := im.exampleAt(s, depth+1); ok {
			return v, true
		}
	}
	if v, ok := o.vals["default"]; ok {
		return v, true
	}
	if enum, _ := o.get("enum").([]any); len(enum) > 0 {
		return enum[0], true
	}
	return nil, false
}

// example is a parameter's example as row text.
func (im *oaImporter) example(o *object) string {
	v, ok := im.exampleOf(o)
	if !ok || v == nil {
		return ""
	}
	return im.scalarText(v)
}

func (im *oaImporter) scalarText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	}
	return im.encode(v, false)
}

// body picks the request body: a JSON media type (example or "{}"), a
// form (schema properties as rows) or the first media type as raw text.
func (im *oaImporter) body(content *object) apitool.Body {
	if content == nil || len(content.keys) == 0 {
		return apitool.Body{}
	}
	pick := func(match func(string) bool) (string, *object) {
		for _, k := range content.keys {
			if im.step(1) {
				return "", nil // run stops with ErrTooLarge
			}
			if match(strings.ToLower(k)) {
				return k, obj(im.resolve(content.vals[k]))
			}
		}
		return "", nil
	}
	isJSON := func(k string) bool { return k == "application/json" || strings.Contains(k, "json") }
	if k, m := pick(isJSON); k != "" {
		text := "{}"
		if v, ok := im.exampleOf(m); ok {
			text = im.encode(v, true)
		}
		b := apitool.Body{Kind: apitool.BodyJSON, Text: text}
		if !strings.EqualFold(k, "application/json") {
			b.ContentType = k
		}
		return b
	}
	if _, m := pick(func(k string) bool { return k == formContentType }); m != nil || (!im.step(len(content.keys)) && hasKey(content, formContentType)) {
		return apitool.Body{Kind: apitool.BodyURLEncoded, Form: im.formFields(m, false)}
	}
	if _, m := pick(func(k string) bool { return strings.HasPrefix(k, "multipart/") }); m != nil {
		return apitool.Body{Kind: apitool.BodyMultipart, Form: im.formFields(m, true)}
	}
	k := content.keys[0]
	m := obj(im.resolve(content.vals[k]))
	text := ""
	if v, ok := im.exampleOf(m); ok {
		text = im.scalarText(v)
	}
	return apitool.Body{Kind: apitool.BodyRaw, Text: text, ContentType: k}
}

func hasKey(o *object, k string) bool {
	for _, key := range o.keys {
		if strings.EqualFold(key, k) {
			return true
		}
	}
	return false
}

// formFields lists the schema's properties as form rows; in multipart a
// binary string is a file field (FileName = the field name, no path yet).
func (im *oaImporter) formFields(media *object, multipart bool) []apitool.FormField {
	s := obj(im.resolve(media.get("schema")))
	props := obj(im.resolve(s.get("properties")))
	if props == nil {
		return nil
	}
	var out []apitool.FormField
	for _, name := range props.keys {
		if im.step(1) {
			break // run stops with ErrTooLarge
		}
		p := obj(im.resolve(props.vals[name]))
		f := apitool.FormField{KV: apitool.KV{Key: name}}
		format := str(p.get("format"))
		if multipart && (format == "binary" || format == "base64") {
			f.FileName = name
		} else {
			f.Value = im.example(p)
		}
		out = append(out, f)
		if im.charge(len(name) + len(f.Value)) {
			break
		}
	}
	return out
}

// auth applies the first security requirement (operation, else document).
func (im *oaImporter) auth(r *apitool.Request, op *object, label string) {
	sec, ok := op.vals["security"]
	if !ok {
		sec = im.root.get("security")
	}
	reqs, _ := sec.([]any)
	if len(reqs) == 0 {
		return
	}
	first := obj(reqs[0])
	if first == nil || len(first.keys) == 0 {
		return // {} = auth optional
	}
	name := first.keys[0]
	schemes := obj(obj(im.root.get("components")).get("securitySchemes"))
	s := obj(im.resolve(schemes.get(name)))
	ignore := func() { im.rep.Ignored = append(im.rep.Ignored, "security: "+name+" ("+label+")") }
	if s == nil {
		ignore()
		return
	}
	switch str(s.get("type")) {
	case "http":
		switch strings.ToLower(str(s.get("scheme"))) {
		case "bearer":
			r.Auth = apitool.Auth{Kind: apitool.AuthBearer, Token: "{{token}}"}
			im.envVar("token", true)
		case "basic":
			r.Auth = apitool.Auth{Kind: apitool.AuthBasic, User: "{{username}}", Pass: "{{password}}"}
			im.envVar("username", false)
			im.envVar("password", true)
		default:
			ignore()
		}
	case "apiKey":
		in := str(s.get("in"))
		if in != "header" && in != "query" {
			ignore()
			return
		}
		r.Auth = apitool.Auth{Kind: apitool.AuthAPIKey, Name: str(s.get("name")), Value: "{{token}}", In: in}
		im.envVar("token", true)
	case "oauth2", "openIdConnect":
		r.Auth = apitool.Auth{Kind: apitool.AuthBearer, Token: "{{token}}"}
		im.envVar("token", true)
	default:
		ignore()
	}
}

func obj(v any) *object {
	o, _ := v.(*object)
	return o
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	}
	return ""
}
