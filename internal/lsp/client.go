package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/patrache/masil/internal/version"
)

// Defaults of Options.
const (
	// CompletionTimeout bounds one Complete; past it the answer is "nothing".
	CompletionTimeout = 1500 * time.Millisecond
	// ChangeDebounce is how long DidChange waits for more edits before it
	// sends the document (Complete sends a pending change at once).
	ChangeDebounce = 150 * time.Millisecond
	// ShutdownGrace is how long Shutdown waits for shutdown/exit before it
	// kills the process.
	ShutdownGrace = 2 * time.Second
	// InitTimeout bounds the initialize handshake when Start's ctx has no
	// earlier deadline.
	InitTimeout = 30 * time.Second
)

var (
	// ErrTimeout: the server did not answer within CompletionTimeout.
	ErrTimeout = errors.New("lsp: request timed out")
	// ErrClosed: the server process has exited (crashed or shut down).
	ErrClosed = errors.New("lsp: server is not running")
	// ErrUnavailable: there is no server for the document (none found, not
	// local, or given up after repeated crashes).
	ErrUnavailable = errors.New("lsp: no language server for this document")
)

// Options tune a Client. The zero value is the defaults above.
type Options struct {
	Clock             Clock
	Logger            *slog.Logger // default slog.Default() at Start
	CompletionTimeout time.Duration
	Debounce          time.Duration
	ShutdownGrace     time.Duration
}

func (o Options) withDefaults() Options {
	if o.Clock == nil {
		o.Clock = realClock{}
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.CompletionTimeout <= 0 {
		o.CompletionTimeout = CompletionTimeout
	}
	if o.Debounce <= 0 {
		o.Debounce = ChangeDebounce
	}
	if o.ShutdownGrace <= 0 {
		o.ShutdownGrace = ShutdownGrace
	}
	return o
}

// Client is one running language server. Its methods are safe for concurrent
// use. DidOpen, DidChange and DidClose never block (they queue a frame);
// Complete and Shutdown block for at most their timeouts.
type Client struct {
	srv  Server
	root string
	opts Options
	log  *slog.Logger
	cmd  *exec.Cmd
	tree *procTree // the server and what it started (nil: only the server itself)

	stdin io.WriteCloser

	// writer queue
	qmu    sync.Mutex
	queue  [][]byte
	wake   chan struct{}
	closed bool // no more frames are queued

	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan *message
	versions map[string]int        // open documents: uri -> version
	changes  map[string]*pendingCh // debounced full-text changes
	caps     serverCaps
	stopping bool // Shutdown was called

	done    chan struct{} // closed once the process has exited
	waitErr error
}

type pendingCh struct {
	text  string
	timer Timer
}

// serverCaps is the part of ServerCapabilities masil reads.
type serverCaps struct {
	Completion *struct {
		TriggerCharacters []string `json:"triggerCharacters"`
	} `json:"completionProvider"`
	TextDocumentSync json.RawMessage `json:"textDocumentSync"`
}

// Start runs srv in rootDir with the default Options.
func Start(ctx context.Context, srv Server, rootDir string) (*Client, error) {
	return Options{}.Start(ctx, srv, rootDir)
}

// Start runs srv in rootDir and does the initialize / initialized handshake.
// ctx bounds the handshake only (InitTimeout when it has no deadline); the
// process lives until Shutdown or until it exits. On a failed handshake the
// process is killed.
func (o Options) Start(ctx context.Context, srv Server, rootDir string) (*Client, error) {
	o = o.withDefaults()
	if len(srv.Command) == 0 {
		return nil, ErrUnavailable
	}
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, err
	}
	c := &Client{
		srv: srv, root: root, opts: o,
		log:      o.Logger.With("lsp", srv.Name, "lang", srv.Lang),
		wake:     make(chan struct{}, 1),
		pending:  map[int64]chan *message{},
		versions: map[string]int{},
		changes:  map[string]*pendingCh{},
		done:     make(chan struct{}),
	}
	cmd := exec.Command(srv.Command[0], srv.Command[1:]...)
	cmd.Dir = root
	cmd.Env = childEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = &stderrLog{log: c.log, left: maxStderrLines}
	cmd.WaitDelay = time.Second // a grandchild holding stdout open cannot hang Wait
	procAttr(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp: start %s: %w", srv.Name, err)
	}
	c.cmd, c.stdin = cmd, stdin
	if c.tree, err = newProcTree(cmd); err != nil {
		c.log.Warn("lsp: cannot group the server's processes; a kill reaches only the server itself", "err", err)
	}
	c.log.Info("lsp server started", "pid", cmd.Process.Pid, "root", root)

	readerDone := make(chan struct{})
	go func() {
		c.readLoop(bufio.NewReaderSize(pr, 64<<10))
		close(readerDone)
	}()
	go c.writeLoop()
	go func() {
		err := cmd.Wait()
		_ = pw.Close() // reader sees EOF
		<-readerDone
		c.finish(err)
	}()

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, InitTimeout)
		defer cancel()
	}
	raw, err := c.call(ctx, "initialize", c.initializeParams())
	if err != nil {
		c.kill()
		<-c.done
		return nil, fmt.Errorf("lsp: initialize %s: %w", srv.Name, err)
	}
	var res struct {
		Capabilities serverCaps `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		c.kill()
		<-c.done
		return nil, fmt.Errorf("lsp: initialize %s: %w", srv.Name, err)
	}
	c.mu.Lock()
	c.caps = res.Capabilities
	c.mu.Unlock()
	c.notify("initialized", struct{}{})
	return c, nil
}

func (c *Client) initializeParams() any {
	rootURI := URIFromPath(c.root)
	return map[string]any{
		"processId":  os.Getpid(),
		"clientInfo": map[string]any{"name": "masil", "version": version.Version},
		"rootUri":    rootURI,
		"rootPath":   c.root,
		"workspaceFolders": []map[string]any{
			{"uri": rootURI, "name": filepath.Base(c.root)},
		},
		"capabilities": map[string]any{
			"general": map[string]any{"positionEncodings": []string{"utf-16"}},
			"workspace": map[string]any{
				"workspaceFolders": true,
				"configuration":    false,
			},
			"textDocument": map[string]any{
				"synchronization": map[string]any{"dynamicRegistration": false, "didSave": false},
				"completion": map[string]any{
					"dynamicRegistration": false,
					"contextSupport":      true,
					"completionItem": map[string]any{
						"snippetSupport":          false,
						"insertReplaceSupport":    false,
						"commitCharactersSupport": false,
						"documentationFormat":     []string{"plaintext"},
						"deprecatedSupport":       true,
					},
				},
			},
			"window": map[string]any{"workDoneProgress": false},
		},
	}
}

// Server is the command this client runs.
func (c *Client) Server() Server { return c.srv }

// Root is the absolute folder the server was started in.
func (c *Client) Root() string { return c.root }

// Done is closed once the server process has exited (after Shutdown or a crash).
func (c *Client) Done() <-chan struct{} { return c.done }

// Err is the process's exit error once Done is closed (nil for a clean exit).
func (c *Client) Err() error {
	select {
	case <-c.done:
		return c.waitErr
	default:
		return nil
	}
}

// Stopping reports whether Shutdown was called (so an exit is not a crash).
func (c *Client) Stopping() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopping
}

// TriggerCharacters are the characters after which the server wants to be
// asked for completion without a word prefix ("." for most).
func (c *Client) TriggerCharacters() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.caps.Completion == nil {
		return nil
	}
	return append([]string(nil), c.caps.Completion.TriggerCharacters...)
}

// HasCompletion reports whether the server offers completion at all.
func (c *Client) HasCompletion() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caps.Completion != nil
}

type textDocumentID struct {
	URI string `json:"uri"`
}

type versionedDocID struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// DidOpen tells the server about document uri (languageId lang, e.g. from
// LangForPath) with its full text at version 1. Opening an open document
// re-sends it as a change.
func (c *Client) DidOpen(uri, lang, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, open := c.versions[uri]; open {
		c.queueChangeLocked(uri, text)
		return
	}
	c.versions[uri] = 1
	c.notifyLocked("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": lang, "version": 1, "text": text},
	})
}

// DidChange replaces document uri's text. It is sent ChangeDebounce after the
// last DidChange (or at once by Complete), with the version one higher than
// the last one sent. A document that is not open is ignored.
func (c *Client) DidChange(uri, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, open := c.versions[uri]; !open {
		return
	}
	c.queueChangeLocked(uri, text)
}

func (c *Client) queueChangeLocked(uri, text string) {
	pc := c.changes[uri]
	if pc == nil {
		pc = &pendingCh{}
		c.changes[uri] = pc
	} else if pc.timer != nil {
		pc.timer.Stop()
	}
	pc.text = text
	pc.timer = c.opts.Clock.AfterFunc(c.opts.Debounce, func() { c.flush(uri, pc) })
}

// flush sends uri's pending change if pc is still the pending one.
func (c *Client) flush(uri string, pc *pendingCh) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changes[uri] != pc {
		return
	}
	c.flushLocked(uri)
}

func (c *Client) flushLocked(uri string) {
	pc := c.changes[uri]
	if pc == nil {
		return
	}
	delete(c.changes, uri)
	if pc.timer != nil {
		pc.timer.Stop()
	}
	v, open := c.versions[uri]
	if !open {
		return
	}
	v++
	c.versions[uri] = v
	c.notifyLocked("textDocument/didChange", map[string]any{
		"textDocument":   versionedDocID{URI: uri, Version: v},
		"contentChanges": []map[string]string{{"text": pc.text}},
	})
}

// DidClose closes document uri (a pending change is dropped).
func (c *Client) DidClose(uri string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pc := c.changes[uri]; pc != nil {
		if pc.timer != nil {
			pc.timer.Stop()
		}
		delete(c.changes, uri)
	}
	if _, open := c.versions[uri]; !open {
		return
	}
	delete(c.versions, uri)
	c.notifyLocked("textDocument/didClose", map[string]any{"textDocument": textDocumentID{URI: uri}})
}

// Version is the last version sent for uri (0 when it is not open).
func (c *Client) Version(uri string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.versions[uri]
}

// Complete asks for completion at (line, utf16col) of uri; see CompleteList.
func (c *Client) Complete(ctx context.Context, uri string, line, utf16col int) ([]Item, error) {
	l, err := c.CompleteList(ctx, uri, line, utf16col)
	return l.Items, err
}

// CompleteList asks for completion at (line, utf16col) of open document uri,
// sending its pending change first. Past CompletionTimeout (or ctx) the
// request is cancelled and the answer is empty with ErrTimeout. A server
// without completion answers an empty list at once.
func (c *Client) CompleteList(ctx context.Context, uri string, line, utf16col int) (List, error) {
	c.mu.Lock()
	c.flushLocked(uri)
	hasCompletion := c.caps.Completion != nil
	c.mu.Unlock()
	if !hasCompletion {
		return List{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.CompletionTimeout)
	defer cancel()
	raw, err := c.call(ctx, "textDocument/completion", map[string]any{
		"textDocument": textDocumentID{URI: uri},
		"position":     Position{Line: line, Character: utf16col},
		"context":      map[string]any{"triggerKind": 1},
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return List{}, ErrTimeout
		}
		var re *rpcError
		if errors.As(err, &re) && re.Code == codeRequestCancelled {
			return List{}, ErrTimeout
		}
		return List{}, err
	}
	return parseCompletion(raw)
}

// Shutdown ends the server: shutdown request, exit notification, and a kill
// when it has not exited within ShutdownGrace (or ctx). It returns once the
// process is gone. Calling it again (or after a crash) is a no-op.
func (c *Client) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	if c.stopping {
		c.mu.Unlock()
		<-c.done
		return nil
	}
	c.stopping = true
	for uri, pc := range c.changes {
		if pc.timer != nil {
			pc.timer.Stop()
		}
		delete(c.changes, uri)
	}
	c.mu.Unlock()

	select {
	case <-c.done:
		return nil
	default:
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.ShutdownGrace)
	defer cancel()
	if _, err := c.call(ctx, "shutdown", nil); err == nil {
		c.notify("exit", nil)
	}
	c.closeQueue() // stdin closes after the queued frames: EOF ends most servers too
	select {
	case <-c.done:
	case <-ctx.Done():
		c.log.Warn("lsp server did not exit; killing it")
		c.kill()
		<-c.done
	}
	return nil
}

// kill ends the server and every process it started (its process group on
// Unix, its Job object on Windows).
func (c *Client) kill() {
	c.tree.kill()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// finish runs once the process has exited and all its output is read.
func (c *Client) finish(err error) {
	c.tree.close() // what the server left running ends with it
	c.mu.Lock()
	c.waitErr = err
	pending := c.pending
	c.pending = map[int64]chan *message{}
	stopping := c.stopping
	for _, pc := range c.changes {
		if pc.timer != nil {
			pc.timer.Stop()
		}
	}
	c.changes = map[string]*pendingCh{}
	c.mu.Unlock()
	c.closeQueue()
	close(c.done)
	for _, ch := range pending {
		close(ch)
	}
	if stopping {
		c.log.Info("lsp server stopped", "err", err)
	} else {
		c.log.Warn("lsp server exited", "err", err)
	}
}

// call sends request method and waits for its response.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	ch := make(chan *message, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, ErrClosed
	default:
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	frame, err := encodeFrame(outRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err == nil {
		err = c.enqueue(frame)
	}
	if err != nil {
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	c.mu.Unlock()

	select {
	case m, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		_, stillPending := c.pending[id]
		delete(c.pending, id)
		if stillPending {
			c.notifyLocked("$/cancelRequest", map[string]any{"id": id})
		}
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		// finish closed ch too; prefer an answer that raced in
		select {
		case m, ok := <-ch:
			if ok && m.Error == nil {
				return m.Result, nil
			}
		default:
		}
		return nil, ErrClosed
	}
}

func (c *Client) notify(method string, params any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifyLocked(method, params)
}

// notifyLocked queues a notification; c.mu is held so frames for one
// document keep their order.
func (c *Client) notifyLocked(method string, params any) {
	frame, err := encodeFrame(outNotification{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		c.log.Warn("lsp encode", "method", method, "err", err)
		return
	}
	_ = c.enqueue(frame)
}

// enqueue adds a frame for the writer goroutine; it never blocks.
func (c *Client) enqueue(frame []byte) error {
	c.qmu.Lock()
	if c.closed {
		c.qmu.Unlock()
		return ErrClosed
	}
	c.queue = append(c.queue, frame)
	c.qmu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return nil
}

// closeQueue lets the writer finish the queued frames and close stdin.
func (c *Client) closeQueue() {
	c.qmu.Lock()
	c.closed = true
	c.qmu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Client) writeLoop() {
	defer c.stdin.Close()
	broken := false
	for {
		c.qmu.Lock()
		q := c.queue
		c.queue = nil
		closed := c.closed
		c.qmu.Unlock()
		for _, f := range q {
			if broken {
				break
			}
			if _, err := c.stdin.Write(f); err != nil {
				broken = true // the process is gone; finish() follows
			}
		}
		if closed {
			c.qmu.Lock()
			rest := len(c.queue)
			c.qmu.Unlock()
			if rest == 0 {
				return
			}
			continue
		}
		select {
		case <-c.wake:
		case <-c.done:
			return
		}
	}
}

func (c *Client) readLoop(r *bufio.Reader) {
	for {
		m, err := readMessage(r)
		if err != nil {
			if errors.Is(err, errBody) {
				c.log.Warn("lsp read: message dropped", "err", err)
				continue // the body was consumed whole; framing is still in sync
			}
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				// A broken header (or stdout cut mid-frame): no later answer can
				// be read. Kill the server so it counts as a crash and the
				// Manager restarts it on next use.
				c.log.Warn("lsp read: framing lost; killing the server", "err", err)
				c.kill()
			}
			_, _ = io.Copy(io.Discard, r)
			return
		}
		switch {
		case m.Method != "" && !isNull(m.ID):
			c.answerServerRequest(m)
		case m.Method != "":
			c.serverNotification(m)
		default:
			c.deliver(m)
		}
	}
}

func (c *Client) deliver(m *message) {
	id, err := strconv.ParseInt(string(bytes.TrimSpace(m.ID)), 10, 64)
	if err != nil {
		return // not one of ours (masil only sends integer ids)
	}
	c.mu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ok {
		ch <- m
	}
}

// answerServerRequest replies to requests a server sends the client; servers
// stall when these go unanswered.
func (c *Client) answerServerRequest(m *message) {
	resp := outResponse{JSONRPC: "2.0", ID: m.ID}
	switch m.Method {
	case "workspace/configuration":
		var p struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(m.Params, &p)
		resp.Result = make([]any, len(p.Items)) // nulls: "no settings"
	case "window/workDoneProgress/create", "client/registerCapability", "client/unregisterCapability",
		"window/showMessageRequest", "workspace/semanticTokens/refresh", "workspace/inlayHint/refresh",
		"workspace/codeLens/refresh", "workspace/diagnostic/refresh":
		resp.Result = nil
	case "workspace/workspaceFolders":
		resp.Result = []map[string]any{{"uri": URIFromPath(c.root), "name": filepath.Base(c.root)}}
	case "workspace/applyEdit":
		resp.Result = map[string]any{"applied": false}
	default:
		resp.Error = &rpcError{Code: codeMethodNotFound, Message: "method not supported: " + m.Method}
	}
	frame, err := encodeFrame(resp)
	if err != nil {
		return
	}
	_ = c.enqueue(frame)
}

func (c *Client) serverNotification(m *message) {
	switch m.Method {
	case "window/logMessage", "window/showMessage":
		var p struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(m.Params, &p) == nil {
			level := slog.LevelDebug // info/log lines: servers send many
			if p.Type == 1 {         // MessageType.Error
				level = slog.LevelWarn
			}
			c.log.Log(context.Background(), level, "lsp message", "type", p.Type, "msg", truncate(p.Message, 2000))
		}
	}
	// publishDiagnostics, $/progress, telemetry …: not used yet.
}

// maxStderrLines caps how much of one server's stderr reaches masil's log
// (a chatty server must not fill it; the log rotates at 5 MB).
const maxStderrLines = 200

// stderrLog sends a server's stderr to the log line by line (never the screen).
type stderrLog struct {
	log  *slog.Logger
	buf  []byte
	left int // lines still logged
}

func (s *stderrLog) Write(p []byte) (int, error) {
	s.buf = append(s.buf, p...)
	for {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			break
		}
		s.emit(s.buf[:i])
		s.buf = s.buf[i+1:]
	}
	if len(s.buf) > 8<<10 { // a huge line without newline
		s.emit(s.buf)
		s.buf = nil
	}
	return len(p), nil
}

func (s *stderrLog) emit(line []byte) {
	line = bytes.TrimRight(line, "\r")
	if len(line) == 0 || s.left <= 0 {
		return
	}
	s.left--
	s.log.Info("lsp stderr", "line", truncate(string(line), 2000))
	if s.left == 0 {
		s.log.Info("lsp stderr: further lines dropped")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
