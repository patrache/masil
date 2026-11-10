package lsp

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Defaults of ManagerOptions.
const (
	// IdleTimeout: a server unused this long is shut down (started again on next use).
	IdleTimeout = 10 * time.Minute
	// MaxRestarts: after this many restarts following crashes, a server is
	// given up for the rest of the run (quietly: word completion stays).
	MaxRestarts = 3
)

// ManagerOptions tune a Manager. The zero value is the defaults.
type ManagerOptions struct {
	Client      Options // for every Client it starts
	IdleTimeout time.Duration
	MaxRestarts int // < 0 means none
	// Find picks the server for a languageId (default Find).
	Find func(lang string) (Server, bool)
	// Start runs one server (default Client.Options.Start).
	Start func(ctx context.Context, srv Server, root string) (*Client, error)
}

// Manager runs one Client per (language, root folder) for the editors. All
// its methods return quickly: servers start on goroutines, notifications are
// queued, and Complete is bounded by CompletionTimeout, so a tea.Cmd can call
// it directly.
type Manager struct {
	opts ManagerOptions
	log  *slog.Logger

	mu      sync.Mutex
	closed  bool
	servers map[serverKey]*entry
	docs    map[string]*document // by cleaned absolute path
	wg      sync.WaitGroup       // shutdowns in flight
}

type serverKey struct{ lang, root string }

type document struct {
	key  serverKey
	srv  Server // what Find picked at Open (an idle-stopped server restarts without a new PATH lookup)
	uri  string
	text string
}

type entry struct {
	key      serverKey
	srv      Server
	client   *Client       // nil while starting, after a crash or a failed start
	starting chan struct{} // non-nil while a start runs; closed when it ends
	started  bool          // a start was attempted before (a new one is a restart)
	restarts int
	gaveUp   bool
	lastUsed time.Time
	idle     Timer
}

// NewManager makes a Manager.
func NewManager(o ManagerOptions) *Manager {
	o.Client = o.Client.withDefaults()
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = IdleTimeout
	}
	if o.MaxRestarts == 0 {
		o.MaxRestarts = MaxRestarts
	} else if o.MaxRestarts < 0 {
		o.MaxRestarts = 0
	}
	if o.Find == nil {
		o.Find = Find
	}
	if o.Start == nil {
		co := o.Client
		o.Start = co.Start
	}
	return &Manager{
		opts:    o,
		log:     o.Client.Logger,
		servers: map[serverKey]*entry{},
		docs:    map[string]*document{},
	}
}

// ErrNotLocal: Open was given a root that is not an existing absolute local
// folder, or a file outside it.
var ErrNotLocal = errors.New("lsp: not a file in a local folder")

// Open starts serving file path (absolute, inside root) with text. root is
// the local user's own project folder (the workspace root, the LeetCode
// solution folder); never call Open on behalf of a share guest. It returns
// ErrUnavailable when no server is found for the file (keep word completion)
// and ErrNotLocal for a bad root/path. The server starts in the background;
// opening an open path updates its text.
func (m *Manager) Open(root, path, text string) error {
	root, path, err := localPaths(root, path)
	if err != nil {
		return err
	}
	lang := LangForPath(path)
	if lang == "" {
		return ErrUnavailable
	}
	key := serverKey{lang: lang, root: root}
	m.mu.Lock()
	e := m.servers[key]
	var srv Server
	if e != nil {
		srv = e.srv
	}
	m.mu.Unlock()
	if e == nil {
		// The PATH lookup runs outside the lock (it reads the file system).
		var ok bool
		if srv, ok = m.opts.Find(lang); !ok {
			return ErrUnavailable
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	if e = m.servers[key]; e == nil { // new, or idle-stopped meanwhile
		e = &entry{key: key, srv: srv}
		m.servers[key] = e
	}
	if e.gaveUp {
		return ErrUnavailable
	}
	d := m.docs[path]
	if d != nil && d.key != key {
		m.closeDocLocked(path, d)
		d = nil
	}
	if d == nil {
		d = &document{key: key, uri: URIFromPath(path)}
		m.docs[path] = d
	}
	d.srv = e.srv
	d.text = text
	m.touchLocked(e)
	if e.client != nil {
		e.client.DidOpen(d.uri, lang, text)
	} else {
		m.ensureLocked(e)
	}
	return nil
}

// localPaths checks Open's arguments: root an existing absolute folder, path
// absolute and inside it. Both come back cleaned.
func localPaths(root, path string) (string, string, error) {
	if root == "" || path == "" || !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return "", "", ErrNotLocal
	}
	root, path = filepath.Clean(root), filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", ErrNotLocal
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return "", "", ErrNotLocal
	}
	return root, path, nil
}

// Change replaces the text of an open path (sent debounced). A path that is
// not open is ignored.
func (m *Manager) Change(path, text string) {
	path = filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.docs[path]
	if d == nil || m.closed {
		return
	}
	d.text = text
	e := m.servers[d.key]
	if e == nil {
		e = m.entryLocked(d)
	}
	m.touchLocked(e)
	if e.client != nil {
		e.client.DidChange(d.uri, text)
	} else {
		m.ensureLocked(e)
	}
}

// Close stops serving path. The server keeps running until it is idle.
func (m *Manager) Close(path string) {
	path = filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.docs[path]; d != nil {
		m.closeDocLocked(path, d)
	}
}

func (m *Manager) closeDocLocked(path string, d *document) {
	delete(m.docs, path)
	if e := m.servers[d.key]; e != nil && e.client != nil {
		e.client.DidClose(d.uri)
	}
}

// Complete asks for completion at (line, utf16col) of open path; see
// UTF16Col for the column. It waits for a starting server within the same
// CompletionTimeout budget. Any error means "no items" (ErrUnavailable:
// no server for this document now; ErrTimeout: too slow).
func (m *Manager) Complete(ctx context.Context, path string, line, utf16col int) (List, error) {
	path = filepath.Clean(path)
	ctx, cancel := context.WithTimeout(ctx, m.opts.Client.CompletionTimeout)
	defer cancel()
	for range 2 { // a second round when the server was (re)started meanwhile
		m.mu.Lock()
		d := m.docs[path]
		if d == nil || m.closed {
			m.mu.Unlock()
			return List{}, ErrUnavailable
		}
		e := m.servers[d.key]
		if e == nil {
			e = m.entryLocked(d)
		}
		if e.gaveUp {
			m.mu.Unlock()
			return List{}, ErrUnavailable
		}
		m.touchLocked(e)
		if c := e.client; c != nil {
			uri := d.uri
			m.mu.Unlock()
			l, err := c.CompleteList(ctx, uri, line, utf16col)
			if errors.Is(err, ErrClosed) {
				return List{}, ErrUnavailable // crashed; the next call restarts it
			}
			return l, err
		}
		wait := m.ensureLocked(e)
		m.mu.Unlock()
		if wait == nil {
			return List{}, ErrUnavailable
		}
		select {
		case <-wait:
		case <-ctx.Done():
			return List{}, ErrTimeout
		}
	}
	return List{}, ErrUnavailable
}

// Client is the running client serving path, nil when none (yet). For
// TriggerCharacters and such.
func (m *Manager) Client(path string) *Client {
	path = filepath.Clean(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.docs[path]
	if d == nil {
		return nil
	}
	if e := m.servers[d.key]; e != nil {
		return e.client
	}
	return nil
}

// entryLocked recreates the entry for d's server after an idle stop.
func (m *Manager) entryLocked(d *document) *entry {
	e := &entry{key: d.key, srv: d.srv}
	m.servers[d.key] = e
	return e
}

func (m *Manager) touchLocked(e *entry) { e.lastUsed = m.opts.Client.Clock.Now() }

// ensureLocked starts e's server unless it runs, is starting or was given
// up. It returns the channel closed when the start in flight ends, nil when
// no start runs.
func (m *Manager) ensureLocked(e *entry) chan struct{} {
	if e.client != nil || e.gaveUp || m.closed {
		return nil
	}
	if e.starting != nil {
		return e.starting
	}
	if e.started {
		if e.restarts >= m.opts.MaxRestarts {
			e.gaveUp = true
			m.log.Warn("lsp server keeps crashing; giving up for this run", "lsp", e.srv.Name, "root", e.key.root)
			return nil
		}
		e.restarts++
		m.log.Info("lsp server restarting", "lsp", e.srv.Name, "restart", e.restarts)
	}
	e.started = true
	ch := make(chan struct{})
	e.starting = ch
	go m.start(e, ch)
	return ch
}

func (m *Manager) start(e *entry, ch chan struct{}) {
	defer close(ch)
	c, err := m.opts.Start(context.Background(), e.srv, e.key.root)
	m.mu.Lock()
	defer m.mu.Unlock()
	e.starting = nil
	if err != nil {
		m.log.Warn("lsp server failed to start", "lsp", e.srv.Name, "err", err)
		return
	}
	if m.closed || m.servers[e.key] != e {
		m.shutdownAsyncLocked(c)
		return
	}
	e.client = c
	for _, d := range m.docs {
		if d.key == e.key {
			c.DidOpen(d.uri, e.key.lang, d.text)
		}
	}
	m.scheduleIdleLocked(e, m.opts.IdleTimeout)
	go m.watch(e, c)
}

// watch notices a crash: the entry forgets the client and the next use restarts it.
func (m *Manager) watch(e *entry, c *Client) {
	<-c.Done()
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.client == c {
		e.client = nil
		if e.idle != nil {
			e.idle.Stop()
			e.idle = nil
		}
	}
}

func (m *Manager) scheduleIdleLocked(e *entry, after time.Duration) {
	if e.idle != nil {
		e.idle.Stop()
	}
	e.idle = m.opts.Client.Clock.AfterFunc(after, func() { m.idleCheck(e) })
}

func (m *Manager) idleCheck(e *entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || e.client == nil || m.servers[e.key] != e {
		return
	}
	unused := m.opts.Client.Clock.Now().Sub(e.lastUsed)
	if unused < m.opts.IdleTimeout {
		m.scheduleIdleLocked(e, m.opts.IdleTimeout-unused)
		return
	}
	m.log.Info("lsp server idle; stopping it", "lsp", e.srv.Name, "root", e.key.root)
	e.idle = nil
	delete(m.servers, e.key) // next use starts a fresh entry (not a restart)
	m.shutdownAsyncLocked(e.client)
	e.client = nil
}

func (m *Manager) shutdownAsyncLocked(c *Client) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		_ = c.Shutdown(context.Background())
	}()
}

// StopAll shuts every server down (in parallel, each killed after
// ShutdownGrace) and waits for them; the Manager serves nothing afterwards.
func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	m.closed = true
	var starting []chan struct{}
	for _, e := range m.servers {
		if e.idle != nil {
			e.idle.Stop()
			e.idle = nil
		}
		if e.client != nil {
			m.shutdownAsyncLocked(e.client)
			e.client = nil
		}
		if e.starting != nil {
			starting = append(starting, e.starting)
		}
	}
	m.mu.Unlock()
	// A start in flight shuts its client down itself (m.closed) once it ends.
	for _, ch := range starting {
		select {
		case <-ch:
		case <-ctx.Done():
		}
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

var (
	sharedOnce sync.Once
	shared     *Manager
	sharedMu   sync.Mutex
	sharedUsed bool
)

// Shared is the process-wide Manager the editors use; StopAll stops it.
func Shared() *Manager {
	sharedOnce.Do(func() {
		shared = NewManager(ManagerOptions{})
	})
	sharedMu.Lock()
	sharedUsed = true
	sharedMu.Unlock()
	return shared
}

// StopAll stops the Shared manager's servers (masil's exit path calls it;
// a no-op when no editor ever used it).
func StopAll(ctx context.Context) {
	sharedMu.Lock()
	used := sharedUsed
	sharedMu.Unlock()
	if used {
		shared.StopAll(ctx)
	}
}
