package convert

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"path"
	"regexp"
	"strings"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/ids"
)

// postmanSchema is the Postman Collection v2.1 schema URL; imports also
// accept it on schema.postman.com.
const postmanSchema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

// ErrNotPostman: not a Postman v2.1 collection (or environment).
var ErrNotPostman = errors.New("convert: not a Postman v2.1 collection or environment")

// pmStr reads a JSON string, number, bool or null as text (Postman files
// are loose about value types); objects and arrays become compact JSON.
type pmStr string

func (s *pmStr) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = pmStr(v)
		return nil
	}
	if string(b) == "null" {
		*s = ""
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return err
	}
	*s = pmStr(buf.String())
	return nil
}

type pmKV struct {
	Key      pmStr           `json:"key"`
	Value    pmStr           `json:"value"`
	Disabled bool            `json:"disabled"`
	Type     string          `json:"type"`
	Src      json.RawMessage `json:"src"`
}

type pmEvent struct {
	Listen string `json:"listen"`
	Script struct {
		Exec json.RawMessage `json:"exec"`
	} `json:"script"`
}

type pmBehavior struct {
	FollowRedirects *bool `json:"followRedirects"`
	StrictSSL       *bool `json:"strictSSL"`
	MaxRedirects    *int  `json:"maxRedirects"`
}

type pmItem struct {
	Name     pmStr                      `json:"name"`
	Item     []pmItem                   `json:"item"`
	Request  json.RawMessage            `json:"request"`
	Event    []pmEvent                  `json:"event"`
	Auth     map[string]json.RawMessage `json:"auth"`
	Behavior *pmBehavior                `json:"protocolProfileBehavior"`
}

type pmCollection struct {
	Info struct {
		Name   pmStr `json:"name"`
		Schema pmStr `json:"schema"`
	} `json:"info"`
	Item     []pmItem                   `json:"item"`
	Auth     map[string]json.RawMessage `json:"auth"`
	Variable []pmKV                     `json:"variable"`
	Event    []pmEvent                  `json:"event"`
}

type pmRequest struct {
	Method pmStr                      `json:"method"`
	Header json.RawMessage            `json:"header"`
	URL    json.RawMessage            `json:"url"`
	Body   *pmBody                    `json:"body"`
	Auth   map[string]json.RawMessage `json:"auth"`
}

type pmBody struct {
	Mode       string `json:"mode"`
	Raw        pmStr  `json:"raw"`
	URLEncoded []pmKV `json:"urlencoded"`
	FormData   []pmKV `json:"formdata"`
	GraphQL    *struct {
		Query     pmStr `json:"query"`
		Variables pmStr `json:"variables"`
	} `json:"graphql"`
	Options struct {
		Raw struct {
			Language string `json:"language"`
		} `json:"raw"`
	} `json:"options"`
}

type pmURL struct {
	Raw      pmStr           `json:"raw"`
	Protocol pmStr           `json:"protocol"`
	Host     json.RawMessage `json:"host"`
	Port     pmStr           `json:"port"`
	Path     json.RawMessage `json:"path"`
	Query    []pmKV          `json:"query"`
	Variable []pmKV          `json:"variable"`
}

type pmImporter struct {
	col *apitool.Collection
	rep Report
}

// ImportPostman reads a Postman Collection v2.1 (C3.9): folders, requests,
// headers, query, bodies (raw, urlencoded, formdata; graphql as JSON),
// collection variables and auth (basic, bearer, apikey — inherited from a
// folder or the collection when a request has none). Scripts are dropped
// and listed in Report.Dropped ("prerequest: <name>", "test: <name>"); a
// secret collection variable — type "secret" or a credential-like name, as
// in an environment — keeps its name only ("variable value: <name>").
func ImportPostman(data []byte) (*apitool.Collection, Report, error) {
	if err := checkSize(len(data)); err != nil {
		return nil, Report{}, err
	}
	if _, err := decodeJSON(data); err != nil { // syntax and depth
		return nil, Report{}, err
	}
	var pc pmCollection
	if err := json.Unmarshal(data, &pc); err != nil {
		return nil, Report{}, ErrNotPostman
	}
	if !isPostmanSchema(string(pc.Info.Schema)) {
		return nil, Report{}, ErrNotPostman
	}
	name := strings.TrimSpace(string(pc.Info.Name))
	if name == "" {
		name = "postman"
	}
	im := &pmImporter{col: &apitool.Collection{Version: apitool.CollectionVersion, ID: ids.New(), Name: name, Requests: map[string]*apitool.Request{}}}
	for _, v := range pc.Variable {
		key := string(v.Key)
		if key == "" {
			continue
		}
		if v.Disabled {
			im.rep.Ignored = append(im.rep.Ignored, "variable: "+key+" (disabled)")
			continue
		}
		vr := apitool.Var{Name: key, Value: string(v.Value)}
		// as an environment: a credential-like name is a secret too
		if v.Type == "secret" || apitool.SensitiveHeader(key) {
			vr.Secret, vr.Value = true, ""
			if v.Value != "" {
				im.rep.Dropped = append(im.rep.Dropped, "variable value: "+key)
			}
		}
		im.col.Vars = append(im.col.Vars, vr)
	}
	im.events(pc.Event, name)
	auth := im.auth(pc.Auth, apitool.Auth{}, name)
	im.items(pc.Item, &im.col.Root, auth)
	im.rep.Suggestions = append(im.rep.Suggestions, suggestVars(im.col.Vars)...)
	return im.col, im.rep, nil
}

func isPostmanSchema(s string) bool {
	s = strings.TrimSuffix(strings.TrimSpace(s), "#")
	return s == postmanSchema || s == strings.Replace(postmanSchema, "schema.getpostman.com", "schema.postman.com", 1)
}

func (im *pmImporter) events(evs []pmEvent, name string) {
	for _, e := range evs {
		if scriptText(e.Script.Exec) != "" {
			im.rep.Dropped = append(im.rep.Dropped, e.Listen+": "+name)
		}
	}
}

func scriptText(raw json.RawMessage) string {
	var lines []string
	if err := json.Unmarshal(raw, &lines); err != nil {
		var one string
		_ = json.Unmarshal(raw, &one)
		lines = []string{one}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// auth turns a Postman auth object into ours: absent or "inherit" keeps
// parent, "noauth" clears it, an unsupported type is dropped (no auth).
func (im *pmImporter) auth(raw map[string]json.RawMessage, parent apitool.Auth, name string) apitool.Auth {
	if raw == nil {
		return parent
	}
	var typ string
	_ = json.Unmarshal(raw["type"], &typ)
	params := map[string]string{}
	var list []pmKV
	if err := json.Unmarshal(raw[typ], &list); err == nil {
		for _, kv := range list {
			params[string(kv.Key)] = string(kv.Value)
		}
	} else {
		var m map[string]pmStr
		if json.Unmarshal(raw[typ], &m) == nil {
			for k, v := range m {
				params[k] = string(v)
			}
		}
	}
	switch typ {
	case "", "inherit":
		return parent
	case "noauth":
		return apitool.Auth{}
	case "basic":
		return apitool.Auth{Kind: apitool.AuthBasic, User: params["username"], Pass: params["password"]}
	case "bearer":
		return apitool.Auth{Kind: apitool.AuthBearer, Token: params["token"]}
	case "apikey":
		in := params["in"]
		if in != "query" {
			in = "header"
		}
		return apitool.Auth{Kind: apitool.AuthAPIKey, Name: params["key"], Value: params["value"], In: in}
	}
	im.rep.Dropped = append(im.rep.Dropped, "auth: "+typ+" ("+name+")")
	return apitool.Auth{}
}

func (im *pmImporter) items(items []pmItem, into *apitool.Folder, parent apitool.Auth) {
	for _, it := range items {
		name := strings.TrimSpace(string(it.Name))
		im.events(it.Event, name)
		switch {
		case len(it.Request) > 0 && string(it.Request) != "null":
			r := im.request(it, name, parent)
			if r == nil {
				continue
			}
			im.col.Requests[r.ID] = r
			into.Items = append(into.Items, apitool.Node{Request: r.ID})
			im.rep.Suggestions = append(im.rep.Suggestions, suggest(r)...)
		case it.Item != nil:
			f := &apitool.Folder{ID: ids.New(), Name: name}
			im.items(it.Item, f, im.auth(it.Auth, parent, name))
			into.Items = append(into.Items, apitool.Node{Folder: f})
		}
	}
}

var postmanPathVar = regexp.MustCompile(`/:([A-Za-z_][A-Za-z0-9_.-]*)`)

func (im *pmImporter) request(it pmItem, name string, parent apitool.Auth) *apitool.Request {
	var pr pmRequest
	var rawURL string
	if err := json.Unmarshal(it.Request, &rawURL); err != nil {
		if err := json.Unmarshal(it.Request, &pr); err != nil {
			im.rep.Ignored = append(im.rep.Ignored, "request: "+name)
			return nil
		}
	} else {
		pr.URL, _ = json.Marshal(rawURL)
	}
	r := &apitool.Request{ID: ids.New(), Name: name, Method: strings.ToUpper(strings.TrimSpace(string(pr.Method)))}
	if r.Method == "" {
		r.Method = "GET"
	}
	if r.Name == "" {
		r.Name = r.Method
	}
	r.URL, r.Query = im.url(pr.URL)
	r.Headers = headers(pr.Header)
	if pr.Body != nil {
		r.Body = im.body(pr.Body, name)
	}
	r.Auth = im.auth(pr.Auth, parent, name)
	if b := it.Behavior; b != nil {
		if b.FollowRedirects != nil && !*b.FollowRedirects {
			r.Settings.NoFollow = true
		}
		if b.StrictSSL != nil && !*b.StrictSSL {
			r.Settings.Insecure = true
		}
		if b.MaxRedirects != nil && *b.MaxRedirects > 0 && *b.MaxRedirects <= 1000 {
			r.Settings.MaxRedirects = *b.MaxRedirects
		}
	}
	return r
}

func (im *pmImporter) colVar(name, value string) {
	for i, v := range im.col.Vars {
		if v.Name == name {
			if v.Value == "" && !v.Secret {
				im.col.Vars[i].Value = value
			}
			return
		}
	}
	im.col.Vars = append(im.col.Vars, apitool.Var{Name: name, Value: value})
}

// url reads a string or object URL: the raw text without its query is the
// URL (":name" path variables become {{name}}), the query array (or the
// raw query) the rows, kept literal as Postman does.
func (im *pmImporter) url(raw json.RawMessage) (string, []apitool.KV) {
	var u pmURL
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		u.Raw = pmStr(s)
	} else {
		_ = json.Unmarshal(raw, &u)
	}
	full := string(u.Raw)
	if full == "" {
		full = buildURL(u)
	}
	base, q, _ := strings.Cut(full, "?")
	vals := map[string]string{}
	for _, v := range u.Variable {
		vals[string(v.Key)] = string(v.Value)
	}
	base = postmanPathVar.ReplaceAllStringFunc(base, func(m string) string {
		n := m[2:]
		im.colVar(n, vals[n])
		return "/{{" + n + "}}"
	})
	var rows []apitool.KV
	if u.Query != nil {
		for _, kv := range u.Query {
			rows = append(rows, apitool.KV{Key: string(kv.Key), Value: string(kv.Value), Off: kv.Disabled})
		}
	} else if q != "" {
		for _, p := range strings.Split(q, "&") {
			if p == "" {
				continue
			}
			k, v, _ := strings.Cut(p, "=")
			rows = append(rows, apitool.KV{Key: k, Value: v})
		}
	}
	return base, rows
}

// buildURL joins the parts of a URL object without raw text.
func buildURL(u pmURL) string {
	parts := func(raw json.RawMessage, sep string) string {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
		var arr []json.RawMessage
		_ = json.Unmarshal(raw, &arr)
		var out []string
		for _, e := range arr {
			var v pmStr
			if json.Unmarshal(e, &v) == nil && !strings.HasPrefix(string(v), "{\"") {
				out = append(out, string(v))
				continue
			}
			var o struct {
				Value pmStr `json:"value"`
			}
			_ = json.Unmarshal(e, &o)
			out = append(out, string(o.Value))
		}
		return strings.Join(out, sep)
	}
	var b strings.Builder
	if u.Protocol != "" {
		b.WriteString(string(u.Protocol) + "://")
	}
	b.WriteString(parts(u.Host, "."))
	if u.Port != "" {
		b.WriteString(":" + string(u.Port))
	}
	if p := parts(u.Path, "/"); p != "" {
		b.WriteString("/" + p)
	}
	return b.String()
}

func headers(raw json.RawMessage) []apitool.KV {
	var out []apitool.KV
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		for _, line := range strings.Split(s, "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) != "" {
				out = append(out, apitool.KV{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
			}
		}
		return out
	}
	var list []pmKV
	_ = json.Unmarshal(raw, &list)
	for _, kv := range list {
		if strings.TrimSpace(string(kv.Key)) == "" {
			continue
		}
		out = append(out, apitool.KV{Key: string(kv.Key), Value: string(kv.Value), Off: kv.Disabled})
	}
	return out
}

// langType maps a raw body language to our content type and back.
var langType = map[string]string{
	"javascript": "application/javascript",
	"html":       "text/html",
	"xml":        "application/xml",
}

func (im *pmImporter) body(b *pmBody, name string) apitool.Body {
	switch b.Mode {
	case "raw":
		lang := b.Options.Raw.Language
		if lang == "json" {
			return apitool.Body{Kind: apitool.BodyJSON, Text: string(b.Raw)}
		}
		return apitool.Body{Kind: apitool.BodyRaw, Text: string(b.Raw), ContentType: langType[lang]}
	case "urlencoded":
		out := apitool.Body{Kind: apitool.BodyURLEncoded}
		for _, kv := range b.URLEncoded {
			out.Form = append(out.Form, apitool.FormField{KV: apitool.KV{Key: string(kv.Key), Value: string(kv.Value), Off: kv.Disabled}})
		}
		return out
	case "formdata":
		out := apitool.Body{Kind: apitool.BodyMultipart}
		for _, kv := range b.FormData {
			f := apitool.FormField{KV: apitool.KV{Key: string(kv.Key), Off: kv.Disabled}}
			if kv.Type == "file" {
				var src string
				if json.Unmarshal(kv.Src, &src) != nil {
					var srcs []string
					if json.Unmarshal(kv.Src, &srcs) == nil && len(srcs) > 0 {
						src = srcs[0]
					}
				}
				// The path is never kept: a collection from someone else
				// could name any file on this PC. The user picks it again.
				f.FileName = string(kv.Key)
				if src != "" {
					f.FileName = path.Base(strings.ReplaceAll(src, `\`, "/"))
					im.rep.Dropped = append(im.rep.Dropped, "file: "+string(kv.Key)+" ("+src+")")
				}
			} else {
				f.Value = string(kv.Value)
			}
			out.Form = append(out.Form, f)
		}
		return out
	case "graphql":
		if b.GraphQL == nil {
			return apitool.Body{}
		}
		o := &object{vals: map[string]any{}}
		o.keys = append(o.keys, "query")
		o.vals["query"] = string(b.GraphQL.Query)
		if vs := strings.TrimSpace(string(b.GraphQL.Variables)); vs != "" {
			o.keys = append(o.keys, "variables")
			if v, err := decodeJSON([]byte(vs)); err == nil {
				o.vals["variables"] = v
			} else {
				o.vals["variables"] = vs
			}
		}
		return apitool.Body{Kind: apitool.BodyJSON, Text: encodeJSON(o, true)}
	case "":
		return apitool.Body{}
	}
	im.rep.Dropped = append(im.rep.Dropped, "body: "+b.Mode+" ("+name+")")
	return apitool.Body{}
}

// --- environments ---

type pmEnv struct {
	Name   pmStr `json:"name"`
	Values *[]struct {
		Key     pmStr  `json:"key"`
		Value   pmStr  `json:"value"`
		Type    string `json:"type"`
		Enabled *bool  `json:"enabled"`
	} `json:"values"`
	Scope string `json:"_postman_variable_scope"`
}

// ImportPostmanEnv reads a Postman v2.1 environment. A "secret" variable or
// one whose name looks secret (token, secret, key, auth, session, password)
// becomes a secret variable: its value is returned in secretValues for the
// caller to put in the Vault and never in env.
func ImportPostmanEnv(data []byte) (env *apitool.Env, secretValues map[string]string, r Report, err error) {
	if err := checkSize(len(data)); err != nil {
		return nil, nil, Report{}, err
	}
	if _, err := decodeJSON(data); err != nil {
		return nil, nil, Report{}, err
	}
	var pe pmEnv
	if err := json.Unmarshal(data, &pe); err != nil || pe.Values == nil || (pe.Scope != "" && pe.Scope != "environment") {
		return nil, nil, Report{}, ErrNotPostman
	}
	name := strings.TrimSpace(string(pe.Name))
	if name == "" {
		name = "postman"
	}
	env = &apitool.Env{ID: ids.New(), Name: name}
	secretValues = map[string]string{}
	for _, v := range *pe.Values {
		key := string(v.Key)
		if key == "" {
			continue
		}
		if v.Enabled != nil && !*v.Enabled {
			r.Ignored = append(r.Ignored, "variable: "+key+" (disabled)")
			continue
		}
		if v.Type == "secret" || apitool.SensitiveHeader(key) {
			env.Vars = append(env.Vars, apitool.Var{Name: key, Secret: true})
			if v.Value != "" {
				secretValues[key] = string(v.Value)
			}
			continue
		}
		env.Vars = append(env.Vars, apitool.Var{Name: key, Value: string(v.Value)})
	}
	return env, secretValues, r, nil
}

// --- export ---

type pxKV struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Type     string `json:"type,omitempty"`
	Src      string `json:"src,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type pxURL struct {
	Raw      string   `json:"raw"`
	Protocol string   `json:"protocol,omitempty"`
	Host     []string `json:"host,omitempty"`
	Port     string   `json:"port,omitempty"`
	Path     []string `json:"path,omitempty"`
	Query    []pxKV   `json:"query,omitempty"`
}

type pxBody struct {
	Mode       string `json:"mode"`
	Raw        string `json:"raw,omitempty"`
	URLEncoded []pxKV `json:"urlencoded,omitempty"`
	FormData   []pxKV `json:"formdata,omitempty"`
	Options    *struct {
		Raw struct {
			Language string `json:"language"`
		} `json:"raw"`
	} `json:"options,omitempty"`
}

type pxAuth struct {
	Type   string `json:"type"`
	Basic  []pxKV `json:"basic,omitempty"`
	Bearer []pxKV `json:"bearer,omitempty"`
	APIKey []pxKV `json:"apikey,omitempty"`
}

type pxRequest struct {
	Method string  `json:"method"`
	Header []pxKV  `json:"header"`
	URL    pxURL   `json:"url"`
	Body   *pxBody `json:"body,omitempty"`
	Auth   *pxAuth `json:"auth,omitempty"`
}

type pxBehavior struct {
	FollowRedirects *bool `json:"followRedirects,omitempty"`
	StrictSSL       *bool `json:"strictSSL,omitempty"`
	MaxRedirects    int   `json:"maxRedirects,omitempty"`
}

type pxItem struct {
	Name     string      `json:"name"`
	Item     *[]pxItem   `json:"item,omitempty"`
	Request  *pxRequest  `json:"request,omitempty"`
	Behavior *pxBehavior `json:"protocolProfileBehavior,omitempty"`
}

type pxCollection struct {
	Info struct {
		ID     string `json:"_postman_id,omitempty"`
		Name   string `json:"name"`
		Schema string `json:"schema"`
	} `json:"info"`
	Item     []pxItem `json:"item"`
	Variable []pxKV   `json:"variable,omitempty"`
}

type pmExporter struct {
	c                 *apitool.Collection
	rep               Report
	asserts, extracts bool
}

// ExportPostman writes c as a Postman v2.1 collection. Checks, extracts,
// timeouts and environments have no Postman form and are listed in
// Report.Dropped; typed-in credentials are hidden (apitool.RedactRequest)
// and secret variables carry no value.
func ExportPostman(c *apitool.Collection) ([]byte, Report, error) {
	ex := &pmExporter{c: c}
	var out pxCollection
	out.Info.ID = c.ID
	out.Info.Name = c.Name
	out.Info.Schema = postmanSchema
	out.Item = ex.folder(&c.Root)
	if out.Item == nil {
		out.Item = []pxItem{}
	}
	for _, v := range c.Vars {
		kv := pxKV{Key: v.Name, Value: v.Value}
		if v.Secret {
			kv.Type, kv.Value = "secret", ""
		}
		out.Variable = append(out.Variable, kv)
	}
	if ex.asserts {
		ex.rep.Dropped = append(ex.rep.Dropped, "asserts")
	}
	if ex.extracts {
		ex.rep.Dropped = append(ex.rep.Dropped, "extracts")
	}
	if len(c.Envs) > 0 {
		ex.rep.Dropped = append(ex.rep.Dropped, "environments")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, ex.rep, err
	}
	return buf.Bytes(), ex.rep, nil
}

func (ex *pmExporter) folder(f *apitool.Folder) []pxItem {
	var out []pxItem
	for _, n := range f.Items {
		if n.Folder != nil {
			items := ex.folder(n.Folder)
			if items == nil {
				items = []pxItem{}
			}
			out = append(out, pxItem{Name: n.Folder.Name, Item: &items})
			continue
		}
		if r := ex.c.Requests[n.Request]; r != nil {
			out = append(out, ex.request(r))
		}
	}
	return out
}

func kvs(rows []apitool.KV) []pxKV {
	out := []pxKV{}
	for _, kv := range rows {
		out = append(out, pxKV{Key: kv.Key, Value: kv.Value, Disabled: kv.Off})
	}
	return out
}

func (ex *pmExporter) request(orig *apitool.Request) pxItem {
	r := apitool.RedactRequest(orig, nil)
	ex.asserts = ex.asserts || len(r.Asserts) > 0
	ex.extracts = ex.extracts || len(r.Extracts) > 0
	if r.Settings.TimeoutMS > 0 {
		ex.rep.Dropped = append(ex.rep.Dropped, "timeout: "+r.Name)
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = "GET"
	}
	pr := &pxRequest{Method: method, Header: kvs(r.Headers), URL: exportURL(r)}

	switch r.Body.Kind {
	case apitool.BodyJSON, apitool.BodyRaw:
		b := &pxBody{Mode: "raw", Raw: r.Body.Text}
		b.Options = &struct {
			Raw struct {
				Language string `json:"language"`
			} `json:"raw"`
		}{}
		lang := "text"
		if r.Body.Kind == apitool.BodyJSON {
			lang = "json"
		} else if ct := r.Body.ContentType; ct != "" && !strings.HasPrefix(strings.ToLower(ct), "text/plain") {
			lang = ""
			for l, t := range langType {
				if strings.EqualFold(t, ct) {
					lang = l
				}
			}
			if lang == "" {
				lang = "text"
				if !hasHeader(r.Headers, "Content-Type") {
					pr.Header = append(pr.Header, pxKV{Key: "Content-Type", Value: ct})
				}
			}
		}
		b.Options.Raw.Language = lang
		pr.Body = b
	case apitool.BodyURLEncoded:
		b := &pxBody{Mode: "urlencoded", URLEncoded: []pxKV{}}
		for _, f := range r.Body.Form {
			b.URLEncoded = append(b.URLEncoded, pxKV{Key: f.Key, Value: f.Value, Disabled: f.Off})
		}
		pr.Body = b
	case apitool.BodyMultipart:
		b := &pxBody{Mode: "formdata", FormData: []pxKV{}}
		for _, f := range r.Body.Form {
			if f.File != "" || f.FileName != "" {
				// the file name only: a local path would tell my user name
				// and folders to whoever gets the export
				src := f.FileName
				if src == "" {
					src = path.Base(strings.ReplaceAll(f.File, `\`, "/"))
				}
				b.FormData = append(b.FormData, pxKV{Key: f.Key, Type: "file", Src: src, Disabled: f.Off})
				continue
			}
			b.FormData = append(b.FormData, pxKV{Key: f.Key, Value: f.Value, Type: "text", Disabled: f.Off})
		}
		pr.Body = b
	}

	switch r.Auth.Kind {
	case apitool.AuthBasic:
		pr.Auth = &pxAuth{Type: "basic", Basic: []pxKV{{Key: "username", Value: r.Auth.User, Type: "string"}, {Key: "password", Value: r.Auth.Pass, Type: "string"}}}
	case apitool.AuthBearer:
		pr.Auth = &pxAuth{Type: "bearer", Bearer: []pxKV{{Key: "token", Value: r.Auth.Token, Type: "string"}}}
	case apitool.AuthAPIKey:
		in := r.Auth.In
		if in != "query" {
			in = "header"
		}
		pr.Auth = &pxAuth{Type: "apikey", APIKey: []pxKV{{Key: "key", Value: r.Auth.Name, Type: "string"}, {Key: "value", Value: r.Auth.Value, Type: "string"}, {Key: "in", Value: in, Type: "string"}}}
	}

	item := pxItem{Name: r.Name, Request: pr}
	if s := r.Settings; s.NoFollow || s.Insecure || s.MaxRedirects > 0 {
		b := &pxBehavior{MaxRedirects: s.MaxRedirects}
		f := false
		if s.NoFollow {
			b.FollowRedirects = &f
		}
		if s.Insecure {
			b.StrictSSL = &f
		}
		item.Behavior = b
	}
	return item
}

func hasHeader(rows []apitool.KV, name string) bool {
	for _, kv := range rows {
		if !kv.Off && strings.EqualFold(kv.Key, name) {
			return true
		}
	}
	return false
}

// exportURL writes the URL object: raw with the enabled query rows, the
// parts Postman shows, and every query row (disabled ones flagged).
func exportURL(r apitool.Request) pxURL {
	u := pxURL{Raw: r.URL}
	var qs []string
	for _, kv := range r.Query {
		if !kv.Off {
			qs = append(qs, kv.Key+"="+kv.Value)
		}
	}
	if len(qs) > 0 {
		sep := "?"
		if strings.Contains(r.URL, "?") {
			sep = "&"
		}
		u.Raw += sep + strings.Join(qs, "&")
	}
	if len(r.Query) > 0 {
		u.Query = kvs(r.Query)
	}
	rest := r.URL
	if i := strings.Index(rest, "://"); i >= 0 {
		u.Protocol, rest = rest[:i], rest[i+3:]
	}
	rest, _, _ = strings.Cut(rest, "?")
	hostPart, pathPart, hasPath := strings.Cut(rest, "/")
	switch {
	case strings.Contains(hostPart, "{{"):
		u.Host = []string{hostPart}
	case hostPart != "":
		if h, p, err := net.SplitHostPort(hostPart); err == nil {
			hostPart, u.Port = h, p
		}
		u.Host = strings.Split(hostPart, ".")
	}
	if hasPath {
		u.Path = strings.Split(pathPart, "/")
	}
	return u
}
