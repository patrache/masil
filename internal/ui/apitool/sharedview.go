package apitool

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// Shared collections (C3.8, flow ③, P4 Task 18): a room document of type
// apicol, synced over CollabHub. In the tool a shared collection is the
// collection key "doc:<doc_id>" (a personal collection name never has ':'),
// shown at the top of the tree as "<name> (공유)". Every finished edit is
// Diff → Gate (the secret detector, C3.10 layer 2) → CheckSharedLimits →
// Session.Submit. My values, my secret marks and their allowed hosts stay
// in api/shared/<doc>.mine.json; the offline copy api/shared/<doc>.json is
// written a second after a change; a body a peer overwrote goes to
// .lost.json and offers "내 내용 복구".

// Dialog kinds of the shared collections.
const (
	dkUpload         dialogKind = "upload"
	dkUploadHeld     dialogKind = "upload_held"
	dkHeld           dialogKind = "held"
	dkLost           dialogKind = "lost"
	dkVersions       dialogKind = "versions"
	dkVersionPreview dialogKind = "version_preview"
)

// noEnv is a shared collection's "no environment" choice.
const noEnv = "\x00none"

// copyDelay batches the offline copy writes after changes.
const copyDelay = time.Second

// sharedDoc is one open shared collection.
type sharedDoc struct {
	id, title, space string
	sess             *collab.Session
	view             *collab.Doc         // the last view (rows -> requests)
	col              *apitool.Collection // the view as a collection, Mine applied, ID = doc id
	mine             *apitool.Mine
	loaded           bool
	copyDue          bool
	pendingReq       string               // the request to open once the document arrived
	lastAt           map[string]time.Time // request id -> when a peer changed it
	held             *heldEdit            // ops the detector held, waiting for the answer
	declined         map[string]bool      // held values the user kept local (heldKey)
}

// heldEdit is what Gate kept back: the held values as set ops.
type heldEdit struct {
	ops  []collab.Op
	held []apitool.HeldField
}

// lostItem is an overwritten body of mine waiting for the restore question.
type lostItem struct {
	Doc  string `json:"doc"`
	Req  string `json:"req"`
	Text string `json:"text"`
}

// collabEventMsg carries a session event of tool t's document doc.
type collabEventMsg struct {
	t   *Tool
	doc string
	e   collab.Event
}

// copyTickMsg: write the offline copy of doc now.
type copyTickMsg struct {
	t   *Tool
	doc string
}

// syncNowMsg syncs the edited request of tool t now.
type syncNowMsg struct{ t *Tool }

// sharedJobMsg applies a shared job's result on the UI goroutine.
type sharedJobMsg struct {
	t  *Tool
	fn func(ctx *ui.Context) tea.Cmd
}

func sharedKey(docID string) string { return "doc:" + docID }

func docOfKey(key string) (string, bool) { return strings.CutPrefix(key, "doc:") }

// curShared is the shared collection of the edited request (nil = personal).
func (t *Tool) curShared() *sharedDoc {
	if id, ok := docOfKey(t.colName); ok {
		return t.docs[id]
	}
	return nil
}

// colLabel is the shown name of the edited request's collection.
func (t *Tool) colLabel() string {
	if sd := t.curShared(); sd != nil {
		return sd.title
	}
	return t.colName
}

// myID is the logged-in user's id ("" = unknown).
func myID(ctx *ui.Context) string {
	if me := ctx.Me(); me != nil {
		return me.ID
	}
	return ""
}

// userName is a user's display name (handle when blank, the id when
// unknown).
func userName(ctx *ui.Context, id string) string {
	if ctx.Store != nil {
		if u := ctx.Store.User(id); u != nil {
			if !textwidth.Blank(u.DisplayName) {
				return u.DisplayName
			}
			return u.Handle
		}
	}
	return id
}

func (t *Tool) now(ctx *ui.Context) time.Time {
	if ctx.Now != nil {
		return ctx.Now()
	}
	return time.Now()
}

// openSharedAtStart opens the route's document, and in the main instance
// every shared collection with an offline copy.
func (t *Tool) openSharedAtStart(ctx *ui.Context, r ui.Route) {
	if r.ID == instanceID {
		ids, err := t.local.ListShared()
		if err != nil {
			slog.Debug("api: list shared", "err", err)
		}
		for _, id := range ids {
			t.openDoc(ctx, id, "")
		}
	}
	if d := r.Query[queryDoc]; d != "" {
		t.openDoc(ctx, d, r.Query[queryReq])
	}
}

// openDoc opens (or shows) shared collection id; req is the request to
// select once it is there. The offline copy shows at once; the session
// catches up in its goroutine.
func (t *Tool) openDoc(ctx *ui.Context, id, req string) *sharedDoc {
	if sd := t.docs[id]; sd != nil {
		if req != "" {
			sd.pendingReq = req
			t.selectPending(ctx, sd)
		}
		return sd
	}
	sd := &sharedDoc{id: id, pendingReq: req, lastAt: map[string]time.Time{}, declined: map[string]bool{}}
	sd.mine, _ = t.local.LoadMine(id)
	if sd.mine == nil {
		sd.mine = &apitool.Mine{}
	}
	if cp, err := t.local.LoadSharedCopy(id); err == nil {
		sd.title, sd.space = cp.Title, cp.SpaceID
		sd.view = collab.NewDoc(cp.Snapshot)
		if c, err := apitool.CollectionFromDoc(cp.Snapshot); err == nil {
			t.setCol(sd, c)
		}
	}
	if ctx.API != nil && collab.IsName(id) {
		post := ctx.Post
		sd.sess = collab.Open(context.Background(), collab.Options{
			API: ctx.API, DocID: id, UserID: myID(ctx), Dial: ctx.CollabDial,
			OnEvent: func(e collab.Event) {
				if post != nil {
					post.Post(collabEventMsg{t: t, doc: id, e: e})
				}
			},
		})
	}
	t.docs[id] = sd
	t.docOrder = append(t.docOrder, id)
	t.shared[sharedKey(id)] = true
	t.tree.expand(sharedKey(id), true)
	t.selectPending(ctx, sd)
	return sd
}

// setCol makes c (read from the document) the shared collection's state.
func (t *Tool) setCol(sd *sharedDoc, c *apitool.Collection) {
	c.ID = sd.id
	apitool.ApplyMine(c, sd.mine)
	sd.col, sd.loaded = c, true
	if c.Name != "" {
		sd.title = c.Name
	}
}

// applyMine applies my marks again after they changed.
func (t *Tool) applyMine(sd *sharedDoc) {
	if sd.col != nil {
		apitool.ApplyMine(sd.col, sd.mine)
	}
	if t.curShared() == sd {
		t.envDirty = false
		t.syncEnvMode()
	}
}

// selectPending opens the request a route asked for once it is there.
func (t *Tool) selectPending(ctx *ui.Context, sd *sharedDoc) {
	if sd.pendingReq == "" || sd.col == nil || sd.col.Requests[sd.pendingReq] == nil {
		return
	}
	t.selectRequest(sharedKey(sd.id), sd.pendingReq)
	sd.pendingReq = ""
	t.focus = paneRequest
	t.tree.follow(ctx, t)
}

// syncEnvMode keeps t.env right for the edited collection: a shared one
// uses a copy of its chosen environment, a personal one the personal file.
func (t *Tool) syncEnvMode() {
	sd := t.curShared()
	if sd == nil {
		if t.envShared {
			t.envShared, t.envDirty = false, false
			t.selectEnv(nil, t.personalEnv)
		}
		return
	}
	if !t.envShared {
		t.personalEnv = ""
		if t.env != nil {
			t.personalEnv = t.env.Name
		}
	}
	t.envShared = true
	if t.envDirty {
		return
	}
	t.env = nil
	if sd.col == nil {
		return
	}
	want := t.sharedEnvs[sd.id]
	for i, e := range sd.col.Envs {
		if e.Name == want || (want == "" && i == 0) {
			c := e
			c.Vars = cloneVars(e.Vars)
			t.env = &c
			return
		}
	}
}

// envSlotOf is the env id a variable's Mine key and vault slot use.
func envSlotOf(e *apitool.Env) string {
	if e.ID != "" {
		return e.ID
	}
	return e.Name
}

// saveEnv keeps the environment: a personal one in its file; a shared one
// marks my secrets (and their hosts) in .mine.json and syncs after the key.
func (t *Tool) saveEnv() error {
	if sd := t.curShared(); sd != nil && t.envShared {
		if t.env == nil {
			return nil
		}
		for _, v := range t.env.Vars {
			if v.Secret {
				sd.mine.MarkSecret(envSlotOf(t.env)+"/"+v.Name, v.AllowedHosts)
			}
		}
		t.envDirty = true
		return t.local.SaveMine(sd.id, sd.mine)
	}
	if t.env == nil {
		return nil
	}
	return t.local.SaveEnv(t.env)
}

// afterEdit adds the sync of a finished edit to cmd (shared collections).
func (t *Tool) afterEdit(ctx *ui.Context, cmd tea.Cmd) tea.Cmd {
	if t.curShared() == nil {
		return cmd
	}
	var s []tea.Cmd
	if t.envDirty {
		s = append(s, t.syncSharedEnv(ctx))
	}
	s = append(s, t.syncShared(ctx))
	return tea.Batch(append([]tea.Cmd{cmd}, s...)...)
}

// cloneCol deep-copies a collection.
func cloneCol(c *apitool.Collection) *apitool.Collection {
	out := *c
	out.Vars = cloneVars(c.Vars)
	out.Root = cloneTree(c.Root)
	out.Requests = make(map[string]*apitool.Request, len(c.Requests))
	for id, r := range c.Requests {
		out.Requests[id] = cloneRequest(r)
	}
	out.Envs = slices.Clone(c.Envs)
	for i := range out.Envs {
		out.Envs[i].Vars = cloneVars(c.Envs[i].Vars)
	}
	return &out
}

func cloneTree(f apitool.Folder) apitool.Folder {
	out := f
	out.Items = slices.Clone(f.Items)
	for i, it := range out.Items {
		if it.Folder != nil {
			sub := cloneTree(*it.Folder)
			out.Items[i].Folder = &sub
		}
	}
	return out
}

// rowIDs lists the row ids of r (to see whether AssignIDs gave new ones).
func rowIDs(r *apitool.Request) []string {
	var out []string
	for _, kv := range r.Query {
		out = append(out, kv.ID)
	}
	for _, kv := range r.Headers {
		out = append(out, kv.ID)
	}
	for _, f := range r.Body.Form {
		out = append(out, f.ID)
	}
	for _, a := range r.Asserts {
		out = append(out, a.ID)
	}
	for _, e := range r.Extracts {
		out = append(out, e.ID)
	}
	return out
}

// syncShared sends the edited request's changes to its shared collection.
func (t *Tool) syncShared(ctx *ui.Context) tea.Cmd {
	sd := t.curShared()
	if sd == nil || t.req == nil || sd.col == nil || t.ed.capturing() {
		return nil
	}
	t.ed.store()
	old := sd.col.Requests[t.req.ID]
	if old == nil || reflect.DeepEqual(old, t.req) {
		return nil
	}
	next := cloneCol(sd.col)
	next.Requests[t.req.ID] = cloneRequest(t.req)
	before := rowIDs(t.req)
	apitool.AssignIDs(next)
	if nr := next.Requests[t.req.ID]; nr != nil && !slices.Equal(before, rowIDs(nr)) {
		t.req = cloneRequest(nr)
		t.ed.reload(t.req)
	}
	return t.submit(ctx, sd, next)
}

// syncSharedEnv sends the edited environment copy's changes.
func (t *Tool) syncSharedEnv(ctx *ui.Context) tea.Cmd {
	sd := t.curShared()
	t.envDirty = false
	if sd == nil || sd.col == nil || t.env == nil {
		return nil
	}
	next := cloneCol(sd.col)
	i := slices.IndexFunc(next.Envs, func(e apitool.Env) bool { return e.ID == t.env.ID })
	if i < 0 {
		return nil
	}
	e := *t.env
	e.Vars = cloneVars(t.env.Vars)
	next.Envs[i] = e
	apitool.AssignIDs(next)
	t.env.Vars = cloneVars(next.Envs[i].Vars)
	return t.submit(ctx, sd, next)
}

// submit diffs the shared collection to next, holds what looks secret and
// sends the rest.
func (t *Tool) submit(ctx *ui.Context, sd *sharedDoc, next *apitool.Collection) tea.Cmd {
	a := ctx.T.Apitest
	if err := apitool.CheckSharedLimits(next); err != nil {
		return ui.Toast(a.SharedTooLarge(), ui.ToastError)
	}
	ops := apitool.Diff(sd.col, next)
	if len(ops) == 0 {
		return nil
	}
	send, held := apitool.GateIn(sd.col, ops)
	var ask []apitool.HeldField
	for _, h := range held {
		if !sd.declined[heldKey(ops, h)] {
			ask = append(ask, h)
		}
	}
	t.sendOps(sd, send)
	if len(ask) == 0 || sd.held != nil {
		return nil
	}
	sd.held = &heldEdit{ops: heldOps(ops, ask), held: ask}
	return t.heldDialog(ctx, sd)
}

// sendOps submits ops and moves the shared state past them at once (the
// session's view catches up through its events).
func (t *Tool) sendOps(sd *sharedDoc, ops []collab.Op) {
	if len(ops) == 0 {
		return
	}
	if sd.sess != nil {
		sd.sess.Submit(ops)
	}
	d := collab.NewDoc(apitool.DocFromCollection(sd.col))
	d.Apply(d.Seq()+1, ops)
	if c, err := apitool.CollectionFromDoc(d.Snapshot()); err == nil {
		t.setCol(sd, c)
	}
}

// heldOps are the held values as set ops.
func heldOps(ops []collab.Op, held []apitool.HeldField) []collab.Op {
	var out []collab.Op
	for _, h := range held {
		op := ops[h.OpIndex]
		v := op.Value
		if op.Op == collab.OpPut {
			v = op.Fields[h.F]
		}
		out = append(out, collab.Op{Op: collab.OpSet, ID: h.Obj, Field: h.F, Value: v})
	}
	return out
}

// heldKey names one held value (object, field, value) for "keep local".
func heldKey(ops []collab.Op, h apitool.HeldField) string {
	op := ops[h.OpIndex]
	v := op.Value
	if op.Op == collab.OpPut {
		v = op.Fields[h.F]
	}
	return h.Obj + "\x00" + h.F + "\x00" + string(v)
}

// heldDialog asks about the first held field: make it a variable, share it
// anyway, or keep it here.
func (t *Tool) heldDialog(ctx *ui.Context, sd *sharedDoc) tea.Cmd {
	a := ctx.T.Apitest
	h := sd.held.held[0]
	name := suggestName(h.Finding.Rule, h.F)
	return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkHeld, title: a.HeldTitle(), data: sd.id, prefW: 64,
		body:    []string{a.SecretDetected(), a.HeldField(h.F), a.SecretName()},
		input:   newLineInput(name),
		choices: []string{a.SecretToVar(), a.UploadAnyway(), a.Cancel()}})
}

// heldAnswer follows the held question.
func (t *Tool) heldAnswer(ctx *ui.Context, m dialogMsg) tea.Cmd {
	sd := t.docs[m.Data]
	if sd == nil || sd.held == nil {
		return nil
	}
	he := sd.held
	sd.held = nil
	switch m.Choice {
	case 1: // share anyway
		t.sendOps(sd, he.ops)
		return nil
	case 0: // make it a variable
		name := strings.TrimSpace(m.Input)
		if !varName.MatchString(name) {
			for i := range he.ops {
				sd.declined[he.ops[i].ID+"\x00"+he.ops[i].Field+"\x00"+string(he.ops[i].Value)] = true
			}
			return ui.Toast(ctx.T.Apitest.SecretBadName(), ui.ToastError)
		}
		return t.heldToVar(ctx, sd, he, name)
	default: // keep it on this PC, unshared
		for i := range he.ops {
			sd.declined[he.ops[i].ID+"\x00"+he.ops[i].Field+"\x00"+string(he.ops[i].Value)] = true
		}
		return nil
	}
}

// heldToVar replaces the first held secret with {{name}} wherever it is
// held, declares name a secret collection variable (mine: its value in the
// vault, its host the request's) and syncs those fields; a field still
// holding another secret is asked about next (one variable per value).
func (t *Tool) heldToVar(ctx *ui.Context, sd *sharedDoc, he *heldEdit, name string) tea.Cmd {
	h0 := he.held[0]
	val := h0.Text[h0.Finding.Start:h0.Finding.End]
	var conv, rest []collab.Op
	var restHeld []apitool.HeldField
	for i, op := range he.ops {
		h := he.held[i]
		if h.Text[h.Finding.Start:h.Finding.End] != val {
			rest, restHeld = append(rest, op), append(restHeld, h)
			continue
		}
		op.Value = replaceInJSON(op.Value, val, "{{"+name+"}}")
		conv = append(conv, op)
	}
	next := cloneCol(sd.col)
	if !slices.ContainsFunc(next.Vars, func(v apitool.Var) bool { return v.Name == name }) {
		next.Vars = append(next.Vars, apitool.Var{Name: name, Secret: true})
	}
	hosts := []string{}
	if t.req != nil {
		if u, _, err := apitool.Expand(t.req.URL, t.scope(ctx), dyn(ctx)); err == nil {
			if h := hostOf(u); h != "" {
				hosts = append(hosts, h)
			}
		}
	}
	mk := "col:" + sd.id + "/" + name
	sd.mine.MarkSecret(mk, hosts)
	if err := t.local.SaveMine(sd.id, sd.mine); err != nil {
		slog.Warn("api: save mine", "err", err)
	}
	cmd := t.submit(ctx, sd, next)
	// A converted field may hold a second, different secret: it is gated
	// again and asked about with the rest.
	send, again := apitool.GateIn(sd.col, conv)
	t.sendOps(sd, send)
	rest = append(heldOps(conv, again), rest...)
	restHeld = append(again, restHeld...)
	if t.req != nil {
		if r := sd.col.Requests[t.req.ID]; r != nil {
			t.req = cloneRequest(r)
			t.ed.reload(t.req)
		}
	}
	t.pendingPuts = append(t.pendingPuts, pendingPut{key: apitool.SecretKey(sd.id, "col:"+sd.id, name), name: name, val: secrets.New(val)})
	cmds := []tea.Cmd{cmd, t.flushPuts(ctx)}
	if len(rest) > 0 && sd.held == nil {
		sd.held = &heldEdit{ops: rest, held: restHeld}
		cmds = append(cmds, t.heldDialog(ctx, sd))
	}
	return tea.Batch(cmds...)
}

// hostOf is the host name of an expanded URL.
func hostOf(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndex(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	if strings.HasPrefix(u, "[") {
		if i := strings.Index(u, "]"); i > 0 {
			return strings.ToLower(u[1:i])
		}
	}
	if i := strings.LastIndex(u, ":"); i >= 0 {
		u = u[:i]
	}
	return strings.ToLower(u)
}

// replaceInJSON replaces old with repl in every string of a JSON value.
func replaceInJSON(raw json.RawMessage, old, repl string) json.RawMessage {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			return strings.ReplaceAll(x, old, repl)
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		case map[string]any:
			for k := range x {
				x[k] = walk(x[k])
			}
		}
		return v
	}
	b, err := json.Marshal(walk(v))
	if err != nil {
		return raw
	}
	return b
}

// ---- session events ----

// rebuild reads the shared collection from the session's view.
func (t *Tool) rebuild(sd *sharedDoc) {
	if sd.sess == nil {
		return
	}
	sd.view = sd.sess.View()
	if c, err := apitool.CollectionFromDoc(sd.view.Snapshot()); err == nil {
		t.setCol(sd, c)
	}
}

// reqOfObject is the request an object belongs to ("" = none).
func reqOfObject(d *collab.Doc, id string) string {
	if r, ok := strings.CutPrefix(id, "req:"); ok {
		return r
	}
	if strings.HasPrefix(id, "row:") && d != nil {
		var r string
		_ = json.Unmarshal(d.Field(id, "req"), &r)
		return r
	}
	return ""
}

// onCollabEvent follows a shared collection's session.
func (t *Tool) onCollabEvent(ctx *ui.Context, m collabEventMsg) tea.Cmd {
	sd := t.docs[m.doc]
	if sd == nil {
		return nil
	}
	a := ctx.T.Apitest
	switch e := m.e.(type) {
	case collab.EventChanged:
		t.rebuild(sd)
		if e.Remote && e.Author != "" && e.Author != myID(ctx) {
			by := userName(ctx, e.Author)
			for _, c := range e.Changes {
				if r := reqOfObject(sd.view, c.ID); r != "" {
					t.foreign[r] = foreignInfo{By: by, Seq: int(sd.view.Seq())}
					sd.lastAt[r] = t.now(ctx)
				}
			}
		}
		return t.afterChange(ctx, sd, e.Remote)
	case collab.EventOverwritten:
		if e.Field != "body" {
			return nil // other fields follow the newer value quietly
		}
		var mine string
		if json.Unmarshal(e.Mine, &mine) != nil {
			return nil
		}
		req := reqOfObject(sd.view, e.ID)
		by := userName(ctx, e.By)
		if err := t.local.AddLost(sd.id, apitool.LostEntry{Request: req, Field: e.Field, Text: mine, By: by, At: t.now(ctx)}); err != nil {
			slog.Warn("api: lost copy", "err", err)
		}
		t.lostQ = append(t.lostQ, lostItem{Doc: sd.id, Req: req, Text: mine})
		b, _ := json.Marshal(lostItem{Doc: sd.id, Req: req, Text: mine})
		return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkLost, data: string(b), title: a.HeldTitle(), prefW: 60,
			body:    []string{a.BodyOverwritten(textwidth.Sanitize(by))},
			choices: []string{a.RestoreMine(), a.Cancel()}})
	case collab.EventRejected:
		t.rebuild(sd)
		cmd := t.afterChange(ctx, sd, true)
		return tea.Batch(cmd, ui.Toast(rejectText(ctx, e.Code, e.Detail), ui.ToastError))
	case collab.EventKicked:
		title := sd.title
		t.closeDoc(ctx, sd)
		return ui.Toast(a.SharedKicked(textwidth.Sanitize(title)), ui.ToastInfo)
	}
	return nil
}

// rejectText is a refused edit in the shown language (collab.md 7.2).
func rejectText(ctx *ui.Context, code, detail string) string {
	a := ctx.T.Apitest
	switch detail {
	case "invalid_op":
		return a.RejectInvalidOp()
	case "body_too_large":
		return a.RejectBodyTooLarge()
	case "limit_requests":
		return a.RejectLimitRequests()
	case "secret_value":
		return a.RejectSecretValue()
	}
	switch code {
	case "too_large", "limit_exceeded":
		return a.RejectTooLarge()
	case "locked", "forbidden":
		return a.RejectLocked()
	case "invalid":
		return a.RejectInvalidOp()
	}
	return a.RejectOther(textwidth.Sanitize(code))
}

// afterChange follows a new state: the asked request, a peer's change of
// the edited one, the environment copy and the offline copy.
func (t *Tool) afterChange(ctx *ui.Context, sd *sharedDoc, remote bool) tea.Cmd {
	t.selectPending(ctx, sd)
	if t.curShared() == sd && t.req != nil && remote && !t.ed.capturing() {
		if nr := sd.col.Requests[t.req.ID]; nr != nil {
			t.ed.store()
			if !reflect.DeepEqual(nr, t.req) {
				t.req = cloneRequest(nr)
				t.ed.reload(t.req)
			}
		}
	}
	if t.curShared() == sd && remote {
		t.syncEnvMode()
	}
	if sd.copyDue {
		return nil
	}
	sd.copyDue = true
	return ui.After(ctx, copyDelay, copyTickMsg{t: t, doc: sd.id})
}

// writeCopy writes the offline copy (no secrets: the document has none).
func (t *Tool) writeCopy(id string) tea.Cmd {
	sd := t.docs[id]
	if sd == nil || sd.view == nil {
		return nil
	}
	sd.copyDue = false
	cp := apitool.SharedCopy{Title: sd.title, SpaceID: sd.space, Snapshot: sd.view.Snapshot()}
	ls := t.local
	return func() tea.Msg {
		if err := ls.SaveSharedCopy(id, cp); err != nil {
			slog.Warn("api: offline copy", "err", err)
		}
		return nil
	}
}

// closeDoc closes a shared collection (kicked, left, deleted).
func (t *Tool) closeDoc(ctx *ui.Context, sd *sharedDoc) {
	if s := sd.sess; s != nil {
		go s.Close() // Close waits for a running OnEvent, which may be posting to this goroutine
	}
	delete(t.docs, sd.id)
	t.docOrder = slices.DeleteFunc(t.docOrder, func(id string) bool { return id == sd.id })
	delete(t.shared, sharedKey(sd.id))
	if t.colName == sharedKey(sd.id) {
		t.colName, t.req = "", nil
		t.ed.load(&apitool.Request{})
		t.ed.req = nil
		t.syncEnvMode()
		t.openFirst("", "")
		t.tree.follow(ctx, t)
	}
}

// closeDocs closes every session (the tool closes).
func (t *Tool) closeDocs() {
	for _, sd := range t.docs {
		if s := sd.sess; s != nil {
			go s.Close()
		}
	}
}

// lastChangedLine is "마지막 변경: hana · 방금" for a request a peer changed.
func (t *Tool) lastChangedLine(ctx *ui.Context) string {
	sd := t.curShared()
	if sd == nil || t.req == nil {
		return ""
	}
	fi, ok := t.foreign[t.req.ID]
	at, seen := sd.lastAt[t.req.ID]
	if !ok || !seen || fi.By == "" {
		return ""
	}
	ago := humanize.For(ctx.T.Locale()).Relative(t.now(ctx), at)
	return ctx.T.Apitest.LastChanged(textwidth.Sanitize(fi.By), ago)
}

// ---- upload (flow ③) ----

// uploadTarget is the personal collection U shares: the tree's selected
// one, else the edited request's.
func (t *Tool) uploadTarget(ctx *ui.Context) string {
	if t.focus == paneTree {
		rows := t.tree.rows(ctx, t)
		if t.tree.cur < len(rows) {
			if c := rows[t.tree.cur].col; c != "" && slices.Contains(t.names, c) {
				return c
			}
		}
	}
	if slices.Contains(t.names, t.colName) {
		return t.colName
	}
	return ""
}

// openUpload asks which room to share the personal collection to; the
// question shows the plain-text note in full (C3.10).
func (t *Tool) openUpload(ctx *ui.Context) tea.Cmd {
	name := t.uploadTarget(ctx)
	if name == "" || ctx.API == nil {
		return nil
	}
	a := ctx.T.Apitest
	t.spaces = nil
	var names []string
	if ctx.Store != nil {
		for _, id := range ctx.Store.SpaceOrder {
			if sp := ctx.Store.Spaces[id]; sp != nil {
				t.spaces = append(t.spaces, id)
				names = append(names, sp.Name)
			}
		}
	}
	if len(t.spaces) == 0 {
		return ui.Toast(a.UploadNoSpace(), ui.ToastInfo)
	}
	return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkUpload, data: name, title: a.UploadTitle(), prefW: 64,
		body: []string{a.UploadPlaintextNote(), a.UploadPick()}, choices: names})
}

// uploadPlan is a personal collection ready to share.
type uploadPlan struct {
	name, space string
	col         *apitool.Collection
	snap        collab.Snapshot
	envFrom     string // the personal environment attached (its secrets are copied)
}

// prepareUpload copies the collection (with the selected personal
// environment attached as its shared one) and checks the limits and the
// detector.
func (t *Tool) prepareUpload(ctx *ui.Context, name, space string) (*uploadPlan, []apitool.HeldField, tea.Cmd) {
	a := ctx.T.Apitest
	c := t.collection(name)
	if c == nil {
		return nil, nil, nil
	}
	cp := cloneCol(c)
	cp.Envs = nil
	envFrom := ""
	if t.env != nil && !t.envShared {
		e := *t.env
		e.ID = ""
		e.Vars = cloneVars(t.env.Vars)
		cp.Envs = []apitool.Env{e}
		envFrom = t.env.Name
	}
	apitool.AssignIDs(cp)
	if err := apitool.CheckSharedLimits(cp); err != nil {
		return nil, nil, ui.Toast(a.SharedTooLarge(), ui.ToastError)
	}
	snap := apitool.DocFromCollection(cp)
	var puts []collab.Op
	for _, o := range snap.Objects {
		puts = append(puts, collab.Op{Op: collab.OpPut, ID: o.ID, Type: o.Type, Fields: o.Fields})
	}
	_, held := apitool.Gate(puts)
	return &uploadPlan{name: name, space: space, col: cp, snap: snap, envFrom: envFrom}, held, nil
}

// startUpload creates the document in a job; my secret values of the
// attached environment are copied to the shared slots (still only mine).
func (t *Tool) startUpload(ctx *ui.Context, p *uploadPlan) tea.Cmd {
	if ctx.API == nil {
		return nil
	}
	cl, v, local := ctx.API, t.vault(ctx), t.local
	snapRaw, err := json.Marshal(p.snap)
	if err != nil {
		return nil
	}
	_, cmd := t.startJob(ctx, jobShare, func(c context.Context) func(*ui.Context) tea.Cmd {
		d, err := cl.CreateDoc(c, p.space, api.CreateDocRequest{Type: apitool.DocType, Title: p.name, Snapshot: snapRaw})
		if err != nil {
			return func(ctx *ui.Context) tea.Cmd {
				return ui.Toast(ctx.T.Apitest.UploadFailed(describeErr(ctx, err)), ui.ToastError)
			}
		}
		mine := &apitool.Mine{}
		for _, e := range p.col.Envs {
			for _, vr := range e.Vars {
				if !vr.Secret {
					continue
				}
				mine.MarkSecret(e.ID+"/"+vr.Name, vr.AllowedHosts)
				if v == nil {
					continue
				}
				if s, err := v.Get(apitool.SecretKey("", p.envFrom, vr.Name)); err == nil && !s.IsZero() {
					if err := v.Put(apitool.SecretKey(d.ID, e.ID, vr.Name), s); err != nil {
						slog.Warn("api: copy secret", "err", err)
					}
				}
			}
		}
		if err := local.SaveMine(d.ID, mine); err != nil {
			slog.Warn("api: save mine", "err", err)
		}
		if err := local.SaveSharedCopy(d.ID, apitool.SharedCopy{Title: d.Title, SpaceID: d.SpaceID, Snapshot: p.snap}); err != nil {
			slog.Warn("api: offline copy", "err", err)
		}
		return func(ctx *ui.Context) tea.Cmd {
			sd := t.openDoc(ctx, d.ID, "")
			if sd.title == "" {
				sd.title = d.Title
			}
			return ui.Toast(ctx.T.Apitest.UploadDone(textwidth.Sanitize(d.Title)), ui.ToastSuccess)
		}
	})
	return cmd
}

// ---- versions ----

// openVersions lists the shared collection's versions (a job).
func (t *Tool) openVersions(ctx *ui.Context, id string) tea.Cmd {
	if ctx.API == nil {
		return nil
	}
	cl := ctx.API
	_, cmd := t.startJob(ctx, jobShare, func(c context.Context) func(*ui.Context) tea.Cmd {
		list, err := cl.ListDocVersions(c, id)
		return func(ctx *ui.Context) tea.Cmd {
			if err != nil {
				return ui.Toast(ctx.T.Apitest.VersionsFailed(describeErr(ctx, err)), ui.ToastError)
			}
			t.versions, t.verDoc = list, id
			return ui.OpenOverlay(t.versionsDialog(ctx))
		}
	})
	return cmd
}

// versionRow is one version line.
func versionRow(ctx *ui.Context, v api.DocVersion) string {
	a := ctx.T.Apitest
	at := v.CreatedAt
	if ctx.Loc != nil {
		at = at.In(ctx.Loc)
	}
	when := at.Format("01-02 15:04")
	who := a.VersionAuto()
	if v.CreatedBy != nil {
		who = textwidth.Sanitize(v.CreatedBy.Handle)
		if !textwidth.Blank(v.CreatedBy.DisplayName) {
			who = textwidth.Sanitize(v.CreatedBy.DisplayName)
		}
	}
	row := a.VersionRow(v.No, when, who)
	if v.RestoredFrom != nil {
		row = a.VersionRestoredRow(v.No, *v.RestoredFrom, when, who)
	}
	if v.Name != nil && *v.Name != "" {
		row += " · " + textwidth.Sanitize(*v.Name)
	}
	return row
}

// versionsDialog is the version list (newest first).
func (t *Tool) versionsDialog(ctx *ui.Context) *dialog {
	a := ctx.T.Apitest
	title := t.verDoc
	if sd := t.docs[t.verDoc]; sd != nil {
		title = sd.title
	}
	d := &dialog{inst: t.route.ID, kind: dkVersions, data: t.verDoc, title: a.VersionsTitle(textwidth.Sanitize(title)), prefW: 64}
	if len(t.versions) == 0 {
		d.body = []string{a.VersionsEmpty()}
		return d
	}
	for _, v := range t.versions {
		d.choices = append(d.choices, versionRow(ctx, v))
	}
	return d
}

// versionPreview fetches version no and shows what it holds.
func (t *Tool) versionPreview(ctx *ui.Context, id string, no int) tea.Cmd {
	cl := ctx.API
	_, cmd := t.startJob(ctx, jobShare, func(c context.Context) func(*ui.Context) tea.Cmd {
		vs, err := cl.GetDocVersion(c, id, no)
		n := 0
		if err == nil {
			var s collab.Snapshot
			if s, err = collab.DecodeSnapshot(vs.Snapshot); err == nil {
				if col, cerr := apitool.CollectionFromDoc(s); cerr == nil {
					n = len(col.Root.RequestIDs())
				}
			}
		}
		return func(ctx *ui.Context) tea.Cmd {
			a := ctx.T.Apitest
			if err != nil {
				return ui.Toast(a.VersionsFailed(describeErr(ctx, err)), ui.ToastError)
			}
			return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkVersionPreview, data: id + "\x00" + strconv.Itoa(no),
				title: a.VersionsTitle(textwidth.Sanitize(t.docTitle(id))), prefW: 56,
				body: []string{a.VersionPreview(no, n)}, choices: []string{a.VersionRestore(), a.Cancel()}})
		}
	})
	return cmd
}

func (t *Tool) docTitle(id string) string {
	if sd := t.docs[id]; sd != nil {
		return sd.title
	}
	return id
}

// restoreVersion restores version no; the version it made goes on top.
func (t *Tool) restoreVersion(ctx *ui.Context, id string, no int) tea.Cmd {
	cl := ctx.API
	_, cmd := t.startJob(ctx, jobShare, func(c context.Context) func(*ui.Context) tea.Cmd {
		v, err := cl.RestoreDocVersion(c, id, no)
		return func(ctx *ui.Context) tea.Cmd {
			a := ctx.T.Apitest
			if err != nil {
				return ui.Toast(a.VersionsFailed(describeErr(ctx, err)), ui.ToastError)
			}
			if t.verDoc == id {
				t.versions = append([]api.DocVersion{*v}, t.versions...)
			}
			return tea.Batch(ui.Toast(a.VersionRestored(v.No), ui.ToastSuccess), ui.OpenOverlay(t.versionsDialog(ctx)))
		}
	})
	return cmd
}

// ---- dialogs ----

// sharedDialog follows the shared collections' dialogs.
func (t *Tool) sharedDialog(ctx *ui.Context, m dialogMsg) tea.Cmd {
	switch m.Kind {
	case dkUpload:
		if m.Choice < 0 || m.Choice >= len(t.spaces) {
			return nil
		}
		p, held, cmd := t.prepareUpload(ctx, m.Data, t.spaces[m.Choice])
		if p == nil {
			return cmd
		}
		if len(held) == 0 {
			return t.startUpload(ctx, p)
		}
		a := ctx.T.Apitest
		t.upPlan = p
		return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkUploadHeld, title: a.HeldTitle(), prefW: 64,
			body:    []string{a.SecretDetected(), a.HeldField(held[0].Obj + "." + held[0].F)},
			choices: []string{a.UploadAnyway(), a.Cancel()}})
	case dkUploadHeld:
		p := t.upPlan
		t.upPlan = nil
		if p != nil && m.Choice == 0 {
			return t.startUpload(ctx, p)
		}
	case dkHeld:
		return t.heldAnswer(ctx, m)
	case dkLost:
		var it lostItem
		if json.Unmarshal([]byte(m.Data), &it) != nil {
			return nil
		}
		t.lostQ = slices.DeleteFunc(t.lostQ, func(x lostItem) bool { return x == it })
		if m.Choice == 0 {
			return t.restoreMine(ctx, it)
		}
	case dkVersions:
		if m.Choice >= 0 && m.Choice < len(t.versions) {
			return t.versionPreview(ctx, m.Data, t.versions[m.Choice].No)
		}
	case dkVersionPreview:
		id, no, _ := strings.Cut(m.Data, "\x00")
		n, err := strconv.Atoi(no)
		if err == nil && m.Choice == 0 {
			return t.restoreVersion(ctx, id, n)
		}
	}
	return nil
}

// restoreMine writes my overwritten body back as a new edit.
func (t *Tool) restoreMine(ctx *ui.Context, it lostItem) tea.Cmd {
	sd := t.docs[it.Doc]
	if sd == nil || sd.col == nil || sd.col.Requests[it.Req] == nil {
		return nil
	}
	next := cloneCol(sd.col)
	next.Requests[it.Req].Body.Text = it.Text
	cmd := t.submit(ctx, sd, next)
	if t.curShared() == sd && t.req != nil && t.req.ID == it.Req {
		t.req = cloneRequest(sd.col.Requests[it.Req])
		t.ed.reload(t.req)
	}
	return tea.Batch(cmd, ui.Toast(ctx.T.Apitest.RestoredMine(), ui.ToastSuccess))
}
