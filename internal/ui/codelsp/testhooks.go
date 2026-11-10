package codelsp

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/lsp"
)

// SetManagerForTest makes Get return m (nil: no server) until the returned
// func restores it. It panics outside a test binary.
func SetManagerForTest(m Manager) (restore func()) {
	if !testing.Testing() {
		panic("codelsp: SetManagerForTest outside a test binary")
	}
	mgrMu.Lock()
	savedSet, saved := mgrSet, mgr
	mgrSet, mgr = true, m
	mgrMu.Unlock()
	return func() {
		mgrMu.Lock()
		mgrSet, mgr = savedSet, saved
		mgrMu.Unlock()
	}
}

// Fake is a Manager for tests: it records what the editors tell it and
// answers Complete with Answer (nil: ErrUnavailable).
type Fake struct {
	mu      sync.Mutex
	root    map[string]string // open path -> root
	text    map[string]string // open path -> text
	changes []string          // paths, one per Change
	closed  []string
	asked   []FakeAsk
	// Answer answers a completion (nil: lsp.ErrUnavailable).
	Answer func(path string, line, utf16col int) (lsp.List, error)
	// Trigger is every document's trigger characters.
	Trigger []string
}

// FakeAsk is one Complete call.
type FakeAsk struct {
	Path           string
	Line, UTF16Col int
}

// NewFake makes a Fake that answers nothing.
func NewFake() *Fake { return &Fake{root: map[string]string{}, text: map[string]string{}} }

func (f *Fake) Open(root, path, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return lsp.ErrNotLocal
	}
	f.root[path], f.text[path] = root, text
	return nil
}

func (f *Fake) Change(path, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.text[path]; ok {
		f.text[path] = text
		f.changes = append(f.changes, path)
	}
}

func (f *Fake) Close(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.root, path)
	delete(f.text, path)
	f.closed = append(f.closed, path)
}

func (f *Fake) Complete(_ context.Context, path string, line, col int) (lsp.List, error) {
	f.mu.Lock()
	f.asked = append(f.asked, FakeAsk{path, line, col})
	answer := f.Answer
	_, open := f.text[path]
	f.mu.Unlock()
	if !open || answer == nil {
		return lsp.List{}, lsp.ErrUnavailable
	}
	return answer(path, line, col)
}

func (f *Fake) TriggerCharacters(string) []string { return slices.Clone(f.Trigger) }

// Opened reports the root and text path is open with (ok=false: not open).
func (f *Fake) Opened(path string) (root, text string, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	text, ok = f.text[path]
	return f.root[path], text, ok
}

// OpenPaths is every open path.
func (f *Fake) OpenPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for p := range f.text {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// Changes is how many Change calls reached open documents.
func (f *Fake) Changes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.changes)
}

// Closed is every path closed, in order.
func (f *Fake) Closed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.closed)
}

// Asked is every Complete call, in order.
func (f *Fake) Asked() []FakeAsk {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}
