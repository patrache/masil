package apitool

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/patrache/masil/internal/secrets"
)

// Source is where a variable's value came from.
type Source int

// Sources, in resolution order (C3.8): temporary value in the request > my
// value > shared environment > collection variable > my globals. Dynamic
// variables ({{$uuid}} …) are computed.
const (
	SourceTemp Source = iota
	SourceMine
	SourceSharedEnv
	SourceCollection
	SourceGlobal
	SourceDynamic
)

// Value is a resolved variable.
type Value struct {
	Name, Text   string
	Secret       bool
	Source       Source
	AllowedHosts []string
}

// Mine is my local overlay on a shared collection (api/shared/<doc>.mine.json):
// my values for non-secret variables and the allowed hosts of my secrets,
// keyed "<envID>/<name>". A personal environment keeps hosts on the Var.
type Mine struct {
	Values map[string]string   `json:"values,omitempty"`
	Hosts  map[string][]string `json:"hosts,omitempty"`
}

// Scope is every variable layer of one send.
type Scope struct {
	Temp       map[string]string
	Mine       *Mine
	Env        *Env
	Collection []Var
	Globals    []Var
	Vault      Vault
	DocID      string // "" = personal collection

	// CollectionID scopes the vault slot of a secret collection variable
	// (envID "col:<CollectionID>").
	CollectionID string
}

// UndefinedError is a {{name}} without a value.
type UndefinedError struct{ Name string }

func (e *UndefinedError) Error() string { return "apitool: undefined variable {{" + e.Name + "}}" }

// envSlot is the env id used in Mine keys and vault slots for the env layer.
func envSlot(e *Env) string {
	if e.ID != "" {
		return e.ID
	}
	return e.Name
}

func findVar(vs []Var, name string) (Var, bool) {
	for _, v := range vs {
		if v.Name == name {
			return v, true
		}
	}
	return Var{}, false
}

// layer is one declaring layer of a variable.
type layer struct {
	src    Source
	slot   string // envID for SecretKey / Mine keys ("" = globals)
	shared bool   // part of a shared document (hosts come from Mine)
	v      Var
}

// declarations lists the layers declaring name: env, collection, globals.
func (s *Scope) declarations(name string) []layer {
	var out []layer
	shared := s.DocID != ""
	if s.Env != nil {
		if v, ok := findVar(s.Env.Vars, name); ok {
			out = append(out, layer{SourceSharedEnv, envSlot(s.Env), shared, v})
		}
	}
	if v, ok := findVar(s.Collection, name); ok {
		out = append(out, layer{SourceCollection, "col:" + s.CollectionID, shared, v})
	}
	if v, ok := findVar(s.Globals, name); ok {
		out = append(out, layer{SourceGlobal, "", false, v})
	}
	return out
}

// Lookup resolves name. A variable marked secret in any layer takes its
// value only from the Vault (never from a temporary, my or shared value);
// a missing vault value is *UndefinedError.
func (s *Scope) Lookup(name string) (Value, error) {
	decls := s.declarations(name)
	for _, d := range decls {
		if d.v.Secret {
			return s.lookupSecret(name, d)
		}
	}
	if v, ok := s.Temp[name]; ok {
		return Value{Name: name, Text: v, Source: SourceTemp}, nil
	}
	if s.Mine != nil {
		slot := ""
		if s.Env != nil {
			slot = envSlot(s.Env)
		}
		if v, ok := s.Mine.Values[slot+"/"+name]; ok {
			return Value{Name: name, Text: v, Source: SourceMine}, nil
		}
	}
	if len(decls) > 0 {
		d := decls[0]
		return Value{Name: name, Text: d.v.Value, Source: d.src}, nil
	}
	return Value{}, &UndefinedError{Name: name}
}

func (s *Scope) lookupSecret(name string, d layer) (Value, error) {
	if s.Vault == nil {
		return Value{}, &UndefinedError{Name: name}
	}
	docID := ""
	if d.shared {
		docID = s.DocID
	}
	sec, err := s.Vault.Get(SecretKey(docID, d.slot, name))
	if errors.Is(err, secrets.ErrNotFound) || (err == nil && sec.IsZero()) {
		return Value{}, &UndefinedError{Name: name}
	}
	if err != nil {
		return Value{}, fmt.Errorf("apitool: secret {{%s}}: %w", name, err)
	}
	hosts := d.v.AllowedHosts
	if d.shared {
		hosts = nil
		if s.Mine != nil {
			hosts = s.Mine.Hosts[d.slot+"/"+name]
		}
	}
	src := d.src
	if d.shared {
		src = SourceMine // a shared secret is always my value (C3.8)
	}
	return Value{Name: name, Text: sec.Reveal(), Secret: true, Source: src, AllowedHosts: append([]string(nil), hosts...)}, nil
}

// SecretSlot is the vault key and Mine host key ("<envID>/<name>") of the
// variable name as Lookup resolves it, and whether it is secret.
func (s *Scope) SecretSlot(name string) (vaultKey, mineKey string, secret bool) {
	for _, d := range s.declarations(name) {
		if d.v.Secret {
			docID := ""
			if d.shared {
				docID = s.DocID
			}
			return SecretKey(docID, d.slot, name), d.slot + "/" + name, true
		}
	}
	return "", "", false
}

// Dyn feeds the dynamic variables; nil fields mean time.Now and crypto/rand.
type Dyn struct {
	Now  func() time.Time
	Rand io.Reader
}

func (d Dyn) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Dyn) read(b []byte) error {
	r := d.Rand
	if r == nil {
		r = rand.Reader
	}
	_, err := io.ReadFull(r, b)
	return err
}

// placeholder is {{name}} with optional inner spaces.
var placeholder = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.$-]+)\s*\}\}`)

// HasPlaceholder reports whether s contains a {{…}} placeholder.
func HasPlaceholder(s string) bool { return placeholder.MatchString(s) }

func (d Dyn) dynamic(name string) (string, error) {
	switch name {
	case "$uuid":
		var b [16]byte
		if err := d.read(b[:]); err != nil {
			return "", err
		}
		b[6] = b[6]&0x0f | 0x40
		b[8] = b[8]&0x3f | 0x80
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
	case "$timestamp":
		return strconv.FormatInt(d.now().Unix(), 10), nil
	case "$isoTimestamp":
		return d.now().UTC().Format("2006-01-02T15:04:05.000Z"), nil
	case "$randomInt":
		var b [4]byte
		if err := d.read(b[:]); err != nil {
			return "", err
		}
		return strconv.Itoa(int(binary.BigEndian.Uint32(b[:]) % 1001)), nil
	}
	return "", &UndefinedError{Name: name}
}

// Expand replaces every {{name}} in tmpl in one pass (values are not
// expanded again). used lists each resolved variable once, in first-use
// order. The first undefined variable stops it with *UndefinedError.
func Expand(tmpl string, s *Scope, d Dyn) (text string, used []Value, err error) {
	if s == nil {
		s = &Scope{}
	}
	locs := placeholder.FindAllStringSubmatchIndex(tmpl, -1)
	if locs == nil {
		return tmpl, nil, nil
	}
	seen := map[string]int{}
	var b strings.Builder
	last := 0
	for _, l := range locs {
		b.WriteString(tmpl[last:l[0]])
		last = l[1]
		name := tmpl[l[2]:l[3]]
		if i, ok := seen[name]; ok && !strings.HasPrefix(name, "$") {
			b.WriteString(used[i].Text)
			continue
		}
		var v Value
		if strings.HasPrefix(name, "$") {
			t, err := d.dynamic(name)
			if err != nil {
				return "", used, err
			}
			v = Value{Name: name, Text: t, Source: SourceDynamic}
		} else {
			v, err = s.Lookup(name)
			if err != nil {
				return "", used, err
			}
		}
		if _, ok := seen[name]; !ok {
			seen[name] = len(used)
			used = append(used, v)
		}
		b.WriteString(v.Text)
	}
	b.WriteString(tmpl[last:])
	return b.String(), used, nil
}
