package apitool

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/patrache/masil/internal/config"
)

// ErrBadName is returned for a collection, environment or request name that
// is not a single safe file name.
var ErrBadName = errors.New("apitool: bad name")

const (
	dirPerm  = 0o700
	filePerm = 0o600
	maxName  = 200
)

// LocalStore keeps personal collections, environments and globals under
// Dir (<config>/api, C3.8):
//
//	collections/<name>/collection.json, collections/<name>/requests/<id>.json
//	envs/<name>.json, globals.json
type LocalStore struct {
	Dir string

	mu   sync.Mutex
	seen map[string]string // collection name => fingerprint at the last read/write
}

// OpenLocal is the store at p.Dir/api (no I/O).
func OpenLocal(p config.Paths) *LocalStore {
	return &LocalStore{Dir: filepath.Join(p.Dir, "api")}
}

// validName accepts a single path element without separators, dots-only
// names, control characters or characters Windows forbids.
func validName(n string) bool {
	if n == "" || n == "." || n == ".." || len(n) > maxName || strings.TrimSpace(n) != n {
		return false
	}
	if strings.ContainsAny(n, `/\:*?"<>|`) || strings.ContainsFunc(n, unicode.IsControl) {
		return false
	}
	return !strings.HasSuffix(n, ".")
}

func checkName(n string) error {
	if !validName(n) {
		return fmt.Errorf("%w: %q", ErrBadName, n)
	}
	return nil
}

func (s *LocalStore) colDir(name string) string { return filepath.Join(s.Dir, "collections", name) }

func (s *LocalStore) writeJSON(path string, v any) error {
	b, err := canonicalJSON(v)
	if err != nil {
		return err
	}
	if err := mkdirs(filepath.Dir(path)); err != nil {
		return err
	}
	return config.WriteFileAtomic(path, b, filePerm)
}

// mkdirs creates dir and its parents with mode 0700.
func mkdirs(dir string) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("apitool: mkdir %s: %w", dir, err)
	}
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("apitool: %s: %w", path, err)
	}
	return nil
}

// listNames lists entries of dir (dirs or *.json files) as names, sorted.
func listNames(dir string, dirs bool) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if dirs {
			if e.IsDir() && validName(n) {
				out = append(out, n)
			}
			continue
		}
		if stem, ok := strings.CutSuffix(n, ".json"); ok && !e.IsDir() && validName(stem) && !strings.HasPrefix(n, ".") {
			out = append(out, stem)
		}
	}
	slices.Sort(out)
	return out, nil
}

// ListCollections lists the personal collection names, sorted.
func (s *LocalStore) ListCollections() ([]string, error) {
	return listNames(filepath.Join(s.Dir, "collections"), true)
}

// LoadCollection reads a collection and all its request files. Request files
// the tree does not list are appended at the root (added outside masil);
// tree entries without a file are dropped.
func (s *LocalStore) LoadCollection(name string) (*Collection, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	dir := s.colDir(name)
	var c Collection
	if err := readJSON(filepath.Join(dir, "collection.json"), &c); err != nil {
		return nil, err
	}
	c.Name = name
	c.Requests = map[string]*Request{}
	ids, err := listNames(filepath.Join(dir, "requests"), false)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		var r Request
		if err := readJSON(filepath.Join(dir, "requests", id+".json"), &r); err != nil {
			return nil, err
		}
		r.ID = id
		c.Requests[id] = &r
	}
	inTree := map[string]bool{}
	for _, id := range c.Root.RequestIDs() {
		if c.Requests[id] == nil {
			c.Root.removeRequest(id)
			continue
		}
		inTree[id] = true
	}
	for _, id := range ids {
		if !inTree[id] {
			c.Root.Items = append(c.Root.Items, Node{Request: id})
		}
	}
	fp, err := s.fingerprint(name)
	if err != nil {
		return nil, err
	}
	s.remember(name, fp)
	return &c, nil
}

// SaveCollection writes collection.json and one file per request, and
// removes request files no longer in c.Requests.
func (s *LocalStore) SaveCollection(c *Collection) error {
	if err := checkName(c.Name); err != nil {
		return err
	}
	for id, r := range c.Requests {
		if err := checkName(id); err != nil {
			return err
		}
		if r == nil {
			return fmt.Errorf("apitool: nil request %q", id)
		}
	}
	dir := s.colDir(c.Name)
	head := *c
	if head.Version == 0 {
		head.Version = CollectionVersion
	}
	if err := s.writeJSON(filepath.Join(dir, "collection.json"), &head); err != nil {
		return err
	}
	if err := mkdirs(filepath.Join(dir, "requests")); err != nil {
		return err
	}
	for id, r := range c.Requests {
		cp := *r
		cp.ID = id
		if err := s.writeJSON(filepath.Join(dir, "requests", id+".json"), &cp); err != nil {
			return err
		}
	}
	existing, err := listNames(filepath.Join(dir, "requests"), false)
	if err != nil {
		return err
	}
	for _, id := range existing {
		if _, ok := c.Requests[id]; !ok {
			if err := os.Remove(filepath.Join(dir, "requests", id+".json")); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	fp, err := s.fingerprint(c.Name)
	if err != nil {
		return err
	}
	s.remember(c.Name, fp)
	return nil
}

// DeleteRequest removes one request from the collection (tree and file).
func (s *LocalStore) DeleteRequest(col, id string) error {
	if err := checkName(id); err != nil {
		return err
	}
	c, err := s.LoadCollection(col)
	if err != nil {
		return err
	}
	delete(c.Requests, id)
	c.Root.removeRequest(id)
	return s.SaveCollection(c)
}

// LoadEnv reads envs/<name>.json; the name is the file name.
func (s *LocalStore) LoadEnv(name string) (*Env, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	var e Env
	if err := readJSON(filepath.Join(s.Dir, "envs", name+".json"), &e); err != nil {
		return nil, err
	}
	e.Name = name
	return &e, nil
}

// SaveEnv writes envs/<e.Name>.json (secret values never written).
func (s *LocalStore) SaveEnv(e *Env) error {
	if err := checkName(e.Name); err != nil {
		return err
	}
	return s.writeJSON(filepath.Join(s.Dir, "envs", e.Name+".json"), e)
}

// ListEnvs lists the personal environment names, sorted.
func (s *LocalStore) ListEnvs() ([]string, error) {
	return listNames(filepath.Join(s.Dir, "envs"), false)
}

type globalsFile struct {
	Version int   `json:"version"`
	Vars    []Var `json:"vars"`
}

// LoadGlobals reads globals.json (missing file = none).
func (s *LocalStore) LoadGlobals() ([]Var, error) {
	var g globalsFile
	err := readJSON(filepath.Join(s.Dir, "globals.json"), &g)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return g.Vars, err
}

// SaveGlobals writes globals.json.
func (s *LocalStore) SaveGlobals(vs []Var) error {
	if vs == nil {
		vs = []Var{}
	}
	return s.writeJSON(filepath.Join(s.Dir, "globals.json"), globalsFile{Version: 1, Vars: vs})
}

// Changed reports whether the collection's files changed on disk since this
// store last read or wrote them (an edit outside masil). A collection never
// read here is not changed.
func (s *LocalStore) Changed(name string) (bool, error) {
	if err := checkName(name); err != nil {
		return false, err
	}
	s.mu.Lock()
	prev, ok := s.seen[name]
	s.mu.Unlock()
	if !ok {
		return false, nil
	}
	fp, err := s.fingerprint(name)
	if err != nil {
		return false, err
	}
	return fp != prev, nil
}

func (s *LocalStore) remember(name, fp string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]string{}
	}
	s.seen[name] = fp
}

// fingerprint hashes the names and contents of the collection's files, so
// any edit (even one keeping size and mtime) is seen and a touch is not.
func (s *LocalStore) fingerprint(name string) (string, error) {
	dir := s.colDir(name)
	h := sha256.New()
	add := func(rel string) error {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		fmt.Fprintf(h, "%s\x00%x\x00", rel, sum)
		return nil
	}
	if err := add("collection.json"); err != nil {
		return "", err
	}
	ids, err := listNames(filepath.Join(dir, "requests"), false)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		if err := add(filepath.Join("requests", id+".json")); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
