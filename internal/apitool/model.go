// Package apitool is the UI-free engine of the API test tool (v0.1.3 C3):
// the request model, personal collection files, variables, the sender and
// its guards, assertions, redaction, the secret detector, history, the runner
// and the shared-collection mapping. Requests go out only from this PC
// (Sender); nothing here talks to the masil server.
package apitool

import (
	"encoding/json"
	"time"
)

// Defaults of a request's settings (C3.7).
const (
	DefaultTimeout      = 30 * time.Second
	DefaultMaxRedirects = 10
)

// CollectionVersion is the on-disk format version of collection.json.
const CollectionVersion = 1

// Collection is a personal (local files) or shared (apicol) collection.
type Collection struct {
	Version  int                 `json:"version"`
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Vars     []Var               `json:"vars,omitempty"`
	Root     Folder              `json:"root"`
	Requests map[string]*Request `json:"-"`              // personal: requests/<id>.json
	Envs     []Env               `json:"envs,omitempty"` // shared collections only
}

// Folder is a node of the tree; Items keep the tree order.
type Folder struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Items []Node `json:"items,omitempty"`
}

// Node is either a sub-folder or a request id.
type Node struct {
	Folder  *Folder `json:"folder,omitempty"`
	Request string  `json:"request,omitempty"`
}

// Request is one request template ({{vars}} unexpanded).
type Request struct {
	ID       string      `json:"id"`
	Name     string      `json:"name,omitempty"`
	Method   string      `json:"method"`
	URL      string      `json:"url"`
	Query    []KV        `json:"query,omitempty"`
	Headers  []KV        `json:"headers,omitempty"`
	Body     Body        `json:"body,omitzero"`
	Auth     Auth        `json:"auth,omitzero"`
	Asserts  []Assertion `json:"asserts,omitempty"`
	Extracts []Extract   `json:"extracts,omitempty"`
	Settings Settings    `json:"settings,omitzero"`
}

// KV is one row of a query, header or form table. Off rows are kept but not sent.
type KV struct {
	ID    string `json:"id,omitempty"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
	Off   bool   `json:"off,omitempty"`
}

// BodyKind is the kind of a request body.
type BodyKind string

// Body kinds.
const (
	BodyNone       BodyKind = "none"
	BodyJSON       BodyKind = "json"
	BodyURLEncoded BodyKind = "urlencoded"
	BodyMultipart  BodyKind = "multipart"
	BodyRaw        BodyKind = "raw"
)

// Body is a request body. Text is used by json and raw; Form by urlencoded
// and multipart. ContentType overrides the default type (raw: the type).
type Body struct {
	Kind        BodyKind    `json:"kind,omitempty"`
	Text        string      `json:"text,omitempty"`
	ContentType string      `json:"content_type,omitempty"`
	Form        []FormField `json:"form,omitempty"`
}

// FormField is a form row; File is a path on this PC (multipart file field,
// never shared — a shared collection carries FileName only).
type FormField struct {
	KV
	File     string `json:"file,omitempty"`
	FileName string `json:"file_name,omitempty"`
}

// AuthKind is the auth helper of a request.
type AuthKind string

// Auth helpers (C3.7): each only builds a header or query row from variables.
const (
	AuthNone   AuthKind = "none"
	AuthBasic  AuthKind = "basic"
	AuthBearer AuthKind = "bearer"
	AuthAPIKey AuthKind = "apikey"
)

// Auth is the auth helper: basic uses User/Pass, bearer Token, apikey
// Name/Value placed In "header" (default) or "query".
type Auth struct {
	Kind  AuthKind `json:"kind,omitempty"`
	User  string   `json:"user,omitempty"`
	Pass  string   `json:"pass,omitempty"`
	Token string   `json:"token,omitempty"`
	Name  string   `json:"name,omitempty"`
	Value string   `json:"value,omitempty"`
	In    string   `json:"in,omitempty"`
}

// Settings are per-request options; the zero value means the defaults
// (30s timeout, follow redirects up to 10, verify TLS).
type Settings struct {
	TimeoutMS    int  `json:"timeout_ms,omitempty"`
	NoFollow     bool `json:"no_follow,omitempty"`
	MaxRedirects int  `json:"max_redirects,omitempty"`
	Insecure     bool `json:"insecure,omitempty"`
}

// Effective fills the defaults into zero fields.
func (s Settings) Effective() Settings {
	if s.TimeoutMS <= 0 {
		s.TimeoutMS = int(DefaultTimeout / time.Millisecond)
	}
	if s.MaxRedirects <= 0 {
		s.MaxRedirects = DefaultMaxRedirects
	}
	return s
}

// Timeout is the effective timeout.
func (s Settings) Timeout() time.Duration {
	return time.Duration(s.Effective().TimeoutMS) * time.Millisecond
}

// Assertion is one declarative check (C3.7): Target status|time|size|header|
// json|body, Path (header name or JSON path), Op and Value.
type Assertion struct {
	ID     string `json:"id,omitempty"`
	Target string `json:"target"`
	Path   string `json:"path,omitempty"`
	Op     string `json:"op"`
	Value  string `json:"value,omitempty"`
}

// Extract copies a response value From "json" (Path = JSON path) or "header"
// (Path = name) into the variable Var.
type Extract struct {
	ID   string `json:"id,omitempty"`
	From string `json:"from"`
	Path string `json:"path"`
	Var  string `json:"var"`
}

// Env is an environment (personal file or shared-collection environment).
type Env struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Vars []Var  `json:"vars,omitempty"`
}

// Var is a variable. A Secret variable's value lives only in the secret
// store: it is never marshaled, and a value read from JSON is dropped.
// AllowedHosts are the hosts the secret may be sent to (C3.10 layer 3).
type Var struct {
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name"`
	Value        string   `json:"value,omitempty"`
	Secret       bool     `json:"secret,omitempty"`
	AllowedHosts []string `json:"allowed_hosts,omitempty"`
}

type varJSON Var

// MarshalJSON writes the variable without the value of a secret.
func (v Var) MarshalJSON() ([]byte, error) {
	if v.Secret {
		v.Value = ""
	}
	return json.Marshal(varJSON(v))
}

// UnmarshalJSON reads a variable; a secret's value in the input is ignored.
func (v *Var) UnmarshalJSON(b []byte) error {
	var j varJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*v = Var(j)
	if v.Secret {
		v.Value = ""
	}
	return nil
}

// RequestIDs lists the request ids of the tree in tree order.
func (f *Folder) RequestIDs() []string {
	var out []string
	var walk func(f *Folder)
	walk = func(f *Folder) {
		for _, n := range f.Items {
			if n.Folder != nil {
				walk(n.Folder)
			} else if n.Request != "" {
				out = append(out, n.Request)
			}
		}
	}
	walk(f)
	return out
}

// removeRequest drops the request id from the tree; reports whether found.
func (f *Folder) removeRequest(id string) bool {
	found := false
	items := f.Items[:0]
	for _, n := range f.Items {
		if n.Folder == nil && n.Request == id {
			found = true
			continue
		}
		if n.Folder != nil && n.Folder.removeRequest(id) {
			found = true
		}
		items = append(items, n)
	}
	clear(f.Items[len(items):])
	f.Items = items
	return found
}
