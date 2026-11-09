// Package collabtest is a fake CollabHub for tool tests (CA 10, P3·P4): the
// document socket (Dial, the collab.Options.Dial signature) sequenced with
// the same reducer as the client (collab.Doc.Apply, so the shared vectors
// hold — TestHubVectors), and the room-document REST (Register) on an
// apitest server. Import it from test files only.
package collabtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/realtime"
)

// Clock is the hub's fixed time for REST timestamps (the uitest start).
var Clock = time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)

var clientOpIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ErrOffline is what Dial returns for a user SetOnline(user, false) took off.
var ErrOffline = errors.New("collabtest: user is offline")

// Hub is the fake server. Safe for concurrent use.
type Hub struct {
	tb testing.TB

	mu      sync.Mutex
	docs    map[string]*doc
	offline map[string]bool
	tokens  map[string]string
	next    int
}

type entry struct {
	seq    int64
	author string
	ops    []collab.Op
}

type version struct {
	v    api.DocVersion
	snap collab.Snapshot
}

type reject struct{ code, detail string }

type doc struct {
	meta     api.Doc
	state    *collab.Doc
	log      []entry
	acks     map[string]int64
	conns    []*conn
	rejects  []reject
	versions []version
	comments []api.DocComment
	deleted  bool
}

// New is an empty hub.
func New(tb testing.TB) *Hub {
	return &Hub{tb: tb, docs: map[string]*doc{}, offline: map[string]bool{}, tokens: map[string]string{}}
}

func summary(user string) api.UserSummary {
	return api.UserSummary{ID: user, Handle: user, DisplayName: user}
}

// Seed creates document docID at snapshot s (seq = s.Seq), with REST metadata
// (type arch, title = docID, space "") for Register's routes.
func (h *Hub) Seed(docID string, s collab.Snapshot) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seed(api.Doc{ID: docID, Type: "arch", Title: docID, CreatedAt: Clock, UpdatedAt: Clock}, s)
}

func (h *Hub) seed(meta api.Doc, s collab.Snapshot) *doc {
	d := &doc{meta: meta, state: collab.NewDoc(s), acks: map[string]int64{}}
	d.meta.HeadSeq = s.Seq
	h.docs[meta.ID] = d
	return d
}

// MapToken makes Dial and the REST fakes read "Authorization: Bearer token"
// as user (default: the token itself is the user ID). uitest.WithCollab maps
// the harness credentials.
func (h *Hub) MapToken(token, user string) {
	h.mu.Lock()
	h.tokens[token] = user
	h.mu.Unlock()
}

func (h *Hub) userOf(hdr http.Header) string {
	tok := strings.TrimPrefix(hdr.Get("Authorization"), "Bearer ")
	if u, ok := h.tokens[tok]; ok {
		return u
	}
	return tok
}

// Ops are the document's sequenced ops, in order (all frames flattened).
func (h *Hub) Ops(docID string) []collab.Op {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.docs[docID]
	if d == nil {
		return nil
	}
	var out []collab.Op
	for _, e := range d.log {
		out = append(out, e.ops...)
	}
	return out
}

// Snapshot is the document's sequenced state.
func (h *Hub) Snapshot(docID string) collab.Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.docs[docID]; d != nil {
		return d.state.Snapshot()
	}
	return collab.Snapshot{}
}

// Inject sequences someone else's ops (reset allowed) and sends them to every
// connection.
func (h *Hub) Inject(docID, author string, ops []collab.Op) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.docs[docID]
	if d == nil {
		h.tb.Fatalf("collabtest: Inject: no document %s", docID)
		return
	}
	h.sequence(d, author, "", ops, nil)
}

// RejectNext makes the next op frame on docID get reject{code, detail}.
func (h *Hub) RejectNext(docID, code, detail string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.docs[docID]; d != nil {
		d.rejects = append(d.rejects, reject{code, detail})
	}
}

// Kick sends kicked{reason} to user's sockets on docID and closes them with
// 4100.
func (h *Hub) Kick(docID, user, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.docs[docID]
	if d == nil {
		return
	}
	for _, c := range slices.Clone(d.conns) {
		if c.user == user {
			c.push(mustJSON(map[string]any{"t": "kicked", "reason": reason}))
			h.drop(d, c, collab.CloseKicked)
		}
	}
}

// SetOnline(user, false) drops the user's sockets (as a network loss) and
// refuses their dials until SetOnline(user, true).
func (h *Hub) SetOnline(user string, on bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.offline[user] = !on
	if on {
		return
	}
	for _, d := range h.docs {
		for _, c := range slices.Clone(d.conns) {
			if c.user == user {
				h.drop(d, c, int(websocket.StatusAbnormalClosure))
			}
		}
	}
}

// Conns is the number of open sockets on docID (by user "" = everyone).
func (h *Hub) Conns(docID, user string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	if d := h.docs[docID]; d != nil {
		for _, c := range d.conns {
			if user == "" || c.user == user {
				n++
			}
		}
	}
	return n
}

// Dial is collab.Options.Dial: the URL's doc id (…/v1/docs/{id}/ws) and the
// Authorization user. A missing or deleted document is a 404 api.Error.
func (h *Hub) Dial(ctx context.Context, rawURL string, hdr http.Header) (realtime.Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	p := strings.TrimSuffix(u.EscapedPath(), "/ws")
	i := strings.LastIndex(p, "/v1/docs/")
	if i < 0 || !strings.HasSuffix(u.EscapedPath(), "/ws") {
		return nil, &api.Error{Status: 404, Code: "not_found", Message: "collabtest: not a document socket", Details: json.RawMessage(`{}`)}
	}
	docID, err := url.PathUnescape(p[i+len("/v1/docs/"):])
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	user := h.userOf(hdr)
	if h.offline[user] {
		return nil, ErrOffline
	}
	d := h.docs[docID]
	if d == nil || d.deleted {
		return nil, &api.Error{Status: 404, Code: "not_found", Message: "doc not found", Details: json.RawMessage(`{}`)}
	}
	c := &conn{h: h, docID: docID, user: user, signal: make(chan struct{}, 1), done: make(chan struct{})}
	d.conns = append(d.conns, c)
	return c, nil
}

// drop closes c from the server side with code. h.mu held.
func (h *Hub) drop(d *doc, c *conn, code int) {
	d.conns = slices.DeleteFunc(d.conns, func(x *conn) bool { return x == c })
	c.shut(websocket.CloseError{Code: websocket.StatusCode(code)})
}

// sequence applies ops as the next frame and sends op to every helloed
// socket but from (which gets ack when clientOpID != ""). h.mu held.
func (h *Hub) sequence(d *doc, author, clientOpID string, ops []collab.Op, from *conn) int64 {
	seq := d.state.Seq() + 1
	d.state.Apply(seq, ops)
	d.log = append(d.log, entry{seq: seq, author: author, ops: ops})
	d.meta.HeadSeq = seq
	if clientOpID != "" {
		d.acks[clientOpID] = seq
	}
	frame := map[string]any{"t": "op", "seq": seq, "author": author, "ops": ops}
	if clientOpID != "" {
		frame["client_op_id"] = clientOpID
	}
	b := mustJSON(frame)
	for _, c := range d.conns {
		if c == from {
			c.push(mustJSON(map[string]any{"t": "ack", "client_op_id": clientOpID, "seq": seq}))
		} else if c.helloed {
			c.push(b)
		}
	}
	return seq
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("collabtest: " + err.Error())
	}
	return b
}

// handle is one client frame (collab.md 3.1, 3.4). h.mu held.
func (h *Hub) handle(c *conn, b []byte) {
	d := h.docs[c.docID]
	if d == nil {
		return
	}
	var f struct {
		T          string          `json:"t"`
		Since      *int64          `json:"since"`
		ClientOpID string          `json:"client_op_id"`
		BaseSeq    *int64          `json:"base_seq"`
		Ops        json.RawMessage `json:"ops"`
		Cursor     json.RawMessage `json:"cursor"`
		Selection  []string        `json:"selection"`
		Dragging   json.RawMessage `json:"dragging"`
	}
	if json.Unmarshal(b, &f) != nil {
		return
	}
	switch f.T {
	case "hello":
		if c.helloed {
			return
		}
		c.helloed = true
		seq := d.state.Seq()
		if f.Since == nil || *f.Since > seq || *f.Since < 0 {
			c.push(mustJSON(map[string]any{"t": "hello", "seq": seq, "snapshot": d.state.Snapshot()}))
			return
		}
		ops := []map[string]any{}
		for _, e := range d.log {
			if e.seq > *f.Since {
				ops = append(ops, map[string]any{"seq": e.seq, "author": e.author, "ops": e.ops})
			}
		}
		c.push(mustJSON(map[string]any{"t": "hello", "seq": seq, "ops": ops}))
	case "op":
		if !clientOpIDRe.MatchString(f.ClientOpID) {
			return
		}
		if seq, ok := d.acks[f.ClientOpID]; ok {
			c.push(mustJSON(map[string]any{"t": "ack", "client_op_id": f.ClientOpID, "seq": seq}))
			return
		}
		rej := func(code, detail string) {
			m := map[string]any{"t": "reject", "client_op_id": f.ClientOpID, "code": code}
			if detail != "" {
				m["detail"] = detail
			}
			c.push(mustJSON(m))
		}
		if len(d.rejects) > 0 {
			r := d.rejects[0]
			d.rejects = d.rejects[1:]
			rej(r.code, r.detail)
			return
		}
		ops, err := collab.ParseOps(f.Ops)
		if err != nil || f.BaseSeq == nil || *f.BaseSeq < 0 {
			rej("invalid", "")
			return
		}
		h.sequence(d, c.user, f.ClientOpID, ops, c)
	case "presence":
		if f.Cursor != nil {
			c.cursor = f.Cursor
		}
		if f.Selection != nil {
			c.selection = f.Selection
		}
		if f.Dragging != nil {
			c.dragging = f.Dragging
		}
		sel := c.selection
		if sel == nil {
			sel = []string{}
		}
		frame := mustJSON(map[string]any{"t": "presence", "user": c.user, "cursor": raw(c.cursor), "selection": sel, "dragging": raw(c.dragging)})
		for _, o := range d.conns {
			if o.user != c.user && o.helloed {
				o.push(frame)
			}
		}
	}
}

func raw(v json.RawMessage) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	return v
}

// conn is one fake document socket (realtime.Conn).
type conn struct {
	h            *Hub
	docID, user  string
	helloed      bool // h.mu
	cursor       json.RawMessage
	selection    []string
	dragging     json.RawMessage
	mu           sync.Mutex
	inbox        [][]byte
	signal, done chan struct{}
	closed       bool
	err          error
}

func (c *conn) push(b []byte) {
	c.mu.Lock()
	if !c.closed {
		c.inbox = append(c.inbox, b)
	}
	c.mu.Unlock()
	select {
	case c.signal <- struct{}{}:
	default:
	}
}

func (c *conn) shut(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed, c.err = true, err
	close(c.done)
}

// Read returns the next server frame; frames queued before a close come first.
func (c *conn) Read(ctx context.Context) ([]byte, error) {
	for {
		c.mu.Lock()
		if len(c.inbox) > 0 {
			b := c.inbox[0]
			c.inbox = c.inbox[1:]
			c.mu.Unlock()
			return b, nil
		}
		if c.closed {
			err := c.err
			c.mu.Unlock()
			return nil, err
		}
		c.mu.Unlock()
		select {
		case <-c.signal:
		case <-c.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Write hands one client frame to the hub; a frame over 65,536 bytes closes
// the socket with 1009 like the server.
func (c *conn) Write(ctx context.Context, b []byte) error {
	c.mu.Lock()
	closed, err := c.closed, c.err
	c.mu.Unlock()
	if closed {
		return err
	}
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	d := c.h.docs[c.docID]
	if len(b) > collab.MaxClientFrame {
		if d != nil {
			c.h.drop(d, c, int(websocket.StatusMessageTooBig))
		}
		return nil
	}
	c.h.handle(c, b)
	return nil
}

// Close is the client closing its socket.
func (c *conn) Close(code int, reason string) error {
	c.h.mu.Lock()
	if d := c.h.docs[c.docID]; d != nil {
		d.conns = slices.DeleteFunc(d.conns, func(x *conn) bool { return x == c })
	}
	c.h.mu.Unlock()
	c.shut(websocket.CloseError{Code: websocket.StatusCode(code), Reason: reason})
	return nil
}

// ---- REST (api.md 5.24) ----

// Register serves the room-document REST on srv from this hub: list / create
// / get / patch / delete docs, versions (list, create, get, restore — a
// restore sequences a reset by the caller) and comments. Callers are read
// from Authorization (MapToken).
func (h *Hub) Register(srv *apitest.Server) {
	j := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	notFound := func(w http.ResponseWriter, what string) {
		j(w, 404, map[string]any{"error": map[string]any{"code": "not_found", "message": what + " not found", "details": map[string]any{}}})
	}
	invalid := func(w http.ResponseWriter, field, why string) {
		j(w, 400, map[string]any{"error": map[string]any{"code": "invalid_request", "message": "invalid request", "details": map[string]any{"fields": map[string]string{field: why}}}})
	}
	// live is the doc of the {id} wildcard, or nil after a 404. h.mu held.
	live := func(w http.ResponseWriter, r *http.Request) *doc {
		d := h.docs[r.PathValue("id")]
		if d == nil || d.deleted {
			notFound(w, "doc")
			return nil
		}
		return d
	}
	lock := func(f func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			h.mu.Lock()
			defer h.mu.Unlock()
			f(w, r)
		}
	}

	srv.Handle("GET /v1/spaces/{id}/docs", lock(func(w http.ResponseWriter, r *http.Request) {
		typ := r.URL.Query().Get("type")
		items := []api.Doc{}
		for _, d := range h.docs {
			if !d.deleted && d.meta.SpaceID == r.PathValue("id") && (typ == "" || d.meta.Type == typ) {
				items = append(items, d.meta)
			}
		}
		slices.SortFunc(items, func(a, b api.Doc) int { return strings.Compare(b.ID, a.ID) })
		j(w, 200, map[string]any{"items": items, "next_cursor": nil})
	}))
	srv.Handle("POST /v1/spaces/{id}/docs", lock(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Type     string          `json:"type"`
			Title    string          `json:"title"`
			Snapshot json.RawMessage `json:"snapshot"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || (body.Type != "arch" && body.Type != "apicol") {
			invalid(w, "type", "invalid")
			return
		}
		if body.Title == "" {
			invalid(w, "title", "required")
			return
		}
		snap := collab.Snapshot{}
		if len(body.Snapshot) > 0 && string(body.Snapshot) != "null" {
			s, err := collab.DecodeSnapshot(body.Snapshot)
			if err != nil {
				invalid(w, "snapshot", "invalid")
				return
			}
			snap = s
			snap.Seq = 0
			if len(s.Objects) > 0 {
				snap.Seq = 1
			}
		}
		h.next++
		id := fmt.Sprintf("D%04d", h.next)
		d := h.seed(api.Doc{ID: id, SpaceID: r.PathValue("id"), Type: body.Type, Title: body.Title,
			CreatedBy: summary(h.userOf(r.Header)), CreatedAt: Clock, UpdatedAt: Clock}, snap)
		j(w, 201, map[string]any{"doc": d.meta})
	}))
	srv.Handle("GET /v1/docs/{id}", lock(func(w http.ResponseWriter, r *http.Request) {
		if d := live(w, r); d != nil {
			j(w, 200, map[string]any{"doc": d.meta})
		}
	}))
	srv.Handle("PATCH /v1/docs/{id}", lock(func(w http.ResponseWriter, r *http.Request) {
		d := live(w, r)
		if d == nil {
			return
		}
		var body struct {
			Title  *string `json:"title"`
			Locked *bool   `json:"locked"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Title != nil {
			d.meta.Title = *body.Title
		}
		if body.Locked != nil {
			d.meta.Locked = *body.Locked
		}
		j(w, 200, map[string]any{"doc": d.meta})
	}))
	srv.Handle("DELETE /v1/docs/{id}", lock(func(w http.ResponseWriter, r *http.Request) {
		d := live(w, r)
		if d == nil {
			return
		}
		d.deleted = true
		for _, c := range slices.Clone(d.conns) {
			c.push(mustJSON(map[string]any{"t": "kicked", "reason": "doc_deleted"}))
			h.drop(d, c, collab.CloseKicked)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	srv.Handle("GET /v1/docs/{id}/versions", lock(func(w http.ResponseWriter, r *http.Request) {
		d := live(w, r)
		if d == nil {
			return
		}
		items := []api.DocVersion{}
		for i := len(d.versions) - 1; i >= 0; i-- {
			items = append(items, d.versions[i].v)
		}
		j(w, 200, map[string]any{"items": items})
	}))
	addVersion := func(d *doc, name *string, by string, from *int) api.DocVersion {
		u := summary(by)
		v := api.DocVersion{No: len(d.versions) + 1, Seq: d.state.Seq(), Name: name, RestoredFrom: from, CreatedBy: &u, CreatedAt: Clock}
		d.versions = append(d.versions, version{v: v, snap: d.state.Snapshot()})
		return v
	}
	srv.Handle("POST /v1/docs/{id}/versions", lock(func(w http.ResponseWriter, r *http.Request) {
		d := live(w, r)
		if d == nil {
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Name == "" {
			invalid(w, "name", "required")
			return
		}
		j(w, 201, map[string]any{"version": addVersion(d, &body.Name, h.userOf(r.Header), nil)})
	}))
	versionOf := func(w http.ResponseWriter, r *http.Request, d *doc) *version {
		no, err := strconv.Atoi(r.PathValue("no"))
		if err != nil || no < 1 || no > len(d.versions) {
			notFound(w, "version")
			return nil
		}
		return &d.versions[no-1]
	}
	srv.Handle("GET /v1/docs/{id}/versions/{no}", lock(func(w http.ResponseWriter, r *http.Request) {
		d := live(w, r)
		if d == nil {
			return
		}
		if v := versionOf(w, r, d); v != nil {
			j(w, 200, map[string]any{"version": v.v, "snapshot": v.snap})
		}
	}))
	srv.Handle("POST /v1/docs/{id}/versions/{no}/restore", lock(func(w http.ResponseWriter, r *http.Request) {
		d := live(w, r)
		if d == nil {
			return
		}
		v := versionOf(w, r, d)
		if v == nil {
			return
		}
		snap := v.snap
		by := h.userOf(r.Header)
		h.sequence(d, by, "", []collab.Op{{Op: collab.OpReset, Snapshot: &snap}}, nil)
		from := v.v.No
		nv := addVersion(d, nil, by, &from)
		j(w, 200, map[string]any{"doc": d.meta, "version": nv})
	}))

	srv.Handle("GET /v1/docs/{id}/comments", lock(func(w http.ResponseWriter, r *http.Request) {
		if d := live(w, r); d != nil {
			j(w, 200, map[string]any{"items": append([]api.DocComment{}, d.comments...), "next_cursor": nil})
		}
	}))
	srv.Handle("POST /v1/docs/{id}/comments", lock(func(w http.ResponseWriter, r *http.Request) {
		d := live(w, r)
		if d == nil {
			return
		}
		var body struct {
			Anchor string  `json:"anchor"`
			Body   string  `json:"body"`
			RootID *string `json:"root_id"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Body == "" {
			invalid(w, "body", "required")
			return
		}
		if body.Anchor == "" {
			invalid(w, "anchor", "required")
			return
		}
		h.next++
		c := api.DocComment{ID: fmt.Sprintf("K%04d", h.next), DocID: d.meta.ID, Anchor: body.Anchor, Author: summary(h.userOf(r.Header)),
			Body: body.Body, RootID: body.RootID, CreatedAt: Clock}
		d.comments = append(d.comments, c)
		j(w, 201, map[string]any{"comment": c})
	}))
	comment := func(w http.ResponseWriter, r *http.Request, d *doc) *api.DocComment {
		for i := range d.comments {
			if d.comments[i].ID == r.PathValue("cid") && !d.comments[i].Deleted {
				return &d.comments[i]
			}
		}
		notFound(w, "comment")
		return nil
	}
	srv.Handle("PATCH /v1/docs/{id}/comments/{cid}", lock(func(w http.ResponseWriter, r *http.Request) {
		d := live(w, r)
		if d == nil {
			return
		}
		c := comment(w, r, d)
		if c == nil {
			return
		}
		var body struct {
			Body     *string `json:"body"`
			Resolved *bool   `json:"resolved"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Body != nil {
			if c.Author.ID != h.userOf(r.Header) {
				j(w, 403, map[string]any{"error": map[string]any{"code": "forbidden", "message": "only the author", "details": map[string]any{}}})
				return
			}
			c.Body = *body.Body
		}
		if body.Resolved != nil {
			if *body.Resolved {
				t := Clock
				c.ResolvedAt = &t
			} else {
				c.ResolvedAt = nil
			}
		}
		j(w, 200, map[string]any{"comment": *c})
	}))
	srv.Handle("DELETE /v1/docs/{id}/comments/{cid}", lock(func(w http.ResponseWriter, r *http.Request) {
		d := live(w, r)
		if d == nil {
			return
		}
		if c := comment(w, r, d); c != nil {
			c.Deleted, c.Body = true, ""
			w.WriteHeader(http.StatusNoContent)
		}
	}))
}
