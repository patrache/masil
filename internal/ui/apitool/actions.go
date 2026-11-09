package apitool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/apitool/convert"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/secretsui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// The request editor's actions: sending (a job; the guards' questions are
// overlays), pasting (curl fills the request, a secret-looking value offers
// a variable), saving to the personal collection.

// foreignInfo marks a request someone else changed last (a shared
// collection): By is their display name, Seq the version.
type foreignInfo struct {
	By  string
	Seq int
}

// pendingSecret is a pasted secret-looking span waiting for the
// secret_detected answer: the field's whole text and the span.
type pendingSecret struct {
	target string // "url", "table", "body"
	tb     *KVTable
	text   string
	f      apitool.Finding
}

// pendingPut is a secret value waiting to go into the vault; quiet ones
// (extracted from a response on every send) are stored without a toast.
type pendingPut struct {
	key, name string
	val       secrets.Secret
	quiet     bool
}

// secretPutMsg reports a vault write of tool t: consent = the OS store is
// missing and no fallback was chosen (rest are the unwritten values).
type secretPutMsg struct {
	t       *Tool
	done    []pendingPut
	rest    []pendingPut
	consent bool
	err     error
}

// fileCheckMsg reports whether collection name changed outside masil.
type fileCheckMsg struct {
	t       *Tool
	name    string
	changed bool
}

// saveConfirmedMsg: the overwrite question was answered yes.
type saveConfirmedMsg struct{ t *Tool }

// newID is a random request or collection id (16 hex digits).
func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// vault is the secret store slot of the logged-in account (nil when there is
// none: secret variables are then undefined).
func (t *Tool) vault(ctx *ui.Context) apitool.Vault {
	if ctx.Secrets == nil {
		return nil
	}
	server, user, ok := ctx.SecretScope()
	if !ok {
		return nil
	}
	return apitool.NewVault(context.Background(), ctx.Secrets, server, user)
}

// scope is the live variable scope of the edited request.
func (t *Tool) scope(ctx *ui.Context) *apitool.Scope {
	s := &apitool.Scope{Env: t.env, Globals: t.globals, Vault: t.vault(ctx)}
	if c := t.collection(t.colName); c != nil {
		s.Collection, s.CollectionID = c.Vars, c.ID
	}
	return s
}

// snapshot is a copy of the scope for a job (nothing shared with the screen).
func (t *Tool) snapshot(ctx *ui.Context) *apitool.Scope {
	s := t.scope(ctx)
	if s.Env != nil {
		e := *s.Env
		e.Vars = cloneVars(e.Vars)
		s.Env = &e
	}
	s.Globals = cloneVars(s.Globals)
	s.Collection = cloneVars(s.Collection)
	return s
}

func cloneVars(vs []apitool.Var) []apitool.Var {
	out := slices.Clone(vs)
	for i := range out {
		out[i].AllowedHosts = slices.Clone(out[i].AllowedHosts)
	}
	return out
}

// dyn feeds the dynamic variables from the app clock.
func dyn(ctx *ui.Context) apitool.Dyn { return apitool.Dyn{Now: ctx.Now} }

// privKey names one version of a request for the private-target consent.
func privKey(docID, reqID string, seq int) string {
	return docID + "/" + reqID + "/" + strconv.Itoa(seq)
}

// ---- send ----

// send sends the edited request in a job; ⏎ while one is in flight waits.
func (t *Tool) send(ctx *ui.Context) tea.Cmd {
	if t.req == nil || t.running(jobSend) {
		return nil
	}
	t.ed.store()
	if t.run != nil {
		t.run.tiny = false // the tiny view shows this send now
	}
	req := cloneRequest(t.req)
	key := t.respKey()
	sc := t.snapshot(ctx)
	colID := sc.CollectionID
	fi := t.foreign[req.ID]
	pk := privKey(sc.DocID, req.ID, fi.Seq)
	privOK := t.privateOK[pk]
	d := dyn(ctx)
	sender, hist, spillDir := t.sender, t.hist, t.spillDir
	t.rv.show(key)
	_, cmd := t.startJob(ctx, jobSend, func(c context.Context) func(*ui.Context) tea.Cmd {
		p, err := apitool.Prepare(req, colID, sc, d)
		if err != nil {
			return t.sendError(key, req, pk, err)
		}
		p.Foreign, p.LastBy, p.PrivateOK = fi.By != "", fi.By, privOK
		if err := os.MkdirAll(spillDir, 0o700); err != nil {
			slog.Warn("api: spill dir", "err", err)
		}
		resp, err := sender.Send(c, p)
		if err != nil {
			return t.sendError(key, req, pk, err)
		}
		st := newRespState(req, resp)
		vals, _ := apitool.ExtractAll(req.Extracts, resp)
		if err := hist.Add(req, p, resp); err != nil {
			slog.Warn("api: history add", "err", err)
		}
		return func(ctx *ui.Context) tea.Cmd { return t.sent(ctx, key, st, vals) }
	})
	return cmd
}

// sent applies a response on the UI goroutine: the pane, the extracted
// variables (saved), the history rows.
func (t *Tool) sent(ctx *ui.Context, key string, st *respState, vals map[string]string) tea.Cmd {
	t.applyResponse(key, st)
	cmds := []tea.Cmd{t.loadHistory()}
	if rb := st.resp.RedirectBlocked; rb != nil {
		cmds = append(cmds, ui.Toast(ctx.T.Apitest.RedirectBlocked(textwidth.Sanitize(rb.Var), textwidth.Sanitize(rb.To)), ui.ToastInfo))
	}
	if len(vals) > 0 {
		// A secret variable's value goes to the vault off the UI goroutine
		// (the OS store may block or need consent); plain ones are stored here.
		sc := t.scope(ctx)
		plain := map[string]string{}
		for name, v := range vals {
			if key, _, secret := sc.SecretSlot(name); secret {
				t.pendingPuts = append(t.pendingPuts, pendingPut{key: key, name: name, val: secrets.New(v), quiet: true})
			} else {
				plain[name] = v
			}
		}
		changed, err := apitool.ApplyExtracts(plain, sc)
		if err != nil {
			slog.Warn("api: extracts", "err", err)
		}
		if len(changed) > 0 {
			t.globals = sc.Globals
			t.saveVars()
		}
		cmds = append(cmds, t.flushPuts(ctx))
	}
	return tea.Batch(cmds...)
}

// saveVars writes the environment and my globals.
func (t *Tool) saveVars() {
	if t.env != nil {
		if err := t.local.SaveEnv(t.env); err != nil {
			slog.Warn("api: save env", "err", err)
		}
	}
	if err := t.local.SaveGlobals(t.globals); err != nil {
		slog.Warn("api: save globals", "err", err)
	}
}

// sendError is what a failed send shows: an undefined variable a toast, a
// guard its question, else the response pane's error line.
func (t *Tool) sendError(key string, req *apitool.Request, pk string, err error) func(*ui.Context) tea.Cmd {
	return func(ctx *ui.Context) tea.Cmd {
		a := ctx.T.Apitest
		var ue *apitool.UndefinedError
		var he *apitool.HostNotAllowedError
		var pe *apitool.PrivateTargetError
		st := &respState{err: err, method: methodOf(req), url: req.URL}
		switch {
		case errors.As(err, &ue):
			return ui.Toast(a.UndefinedVar(textwidth.Sanitize(ue.Name)), ui.ToastError)
		case errors.As(err, &he):
			t.pendingHost = he
			return ui.OpenRegisteredOverlay(overlayHostBlock, promptArgs{Inst: t.route.ID, Var: he.Var, Host: he.Host}.encode())
		case errors.As(err, &pe):
			t.pendingPrivate = pk
			return ui.OpenRegisteredOverlay(overlayPrivate, promptArgs{Inst: t.route.ID, By: pe.LastBy}.encode())
		case errors.Is(err, context.Canceled):
			st.canceled, st.errText = true, a.SendCanceled()
		case errors.Is(err, context.DeadlineExceeded):
			st.errText = a.SendTimeout()
		default:
			st.errText = a.SendFailed(errText(err))
		}
		t.applyResponse(key, st)
		return nil
	}
}

// errText is an engine error without its package prefixes (errors never
// carry the URL).
func errText(err error) string {
	s := err.Error()
	for _, p := range []string{"apitool: send: ", "apitool: ", "convert: "} {
		s = strings.TrimPrefix(s, p)
	}
	return textwidth.Sanitize(s)
}

// allowHost adds the blocked host to the secret variable's allowed hosts
// (the layer that declares it) and sends again.
func (t *Tool) allowHost(ctx *ui.Context, he *apitool.HostNotAllowedError) tea.Cmd {
	add := func(vs []apitool.Var) bool {
		for i := range vs {
			if vs[i].Name == he.Var && vs[i].Secret {
				if !slices.Contains(vs[i].AllowedHosts, he.Host) {
					vs[i].AllowedHosts = append(vs[i].AllowedHosts, he.Host)
				}
				return true
			}
		}
		return false
	}
	switch c := t.collection(t.colName); {
	case t.env != nil && add(t.env.Vars):
		if err := t.local.SaveEnv(t.env); err != nil {
			return ui.Toast(ctx.T.Apitest.SaveFailed(errText(err)), ui.ToastError)
		}
	case c != nil && add(c.Vars):
		if err := t.local.SaveCollection(c); err != nil {
			return ui.Toast(ctx.T.Apitest.SaveFailed(errText(err)), ui.ToastError)
		}
	case add(t.globals):
		if err := t.local.SaveGlobals(t.globals); err != nil {
			return ui.Toast(ctx.T.Apitest.SaveFailed(errText(err)), ui.ToastError)
		}
	default:
		return nil
	}
	return t.send(ctx)
}

// ---- paste ----

// onPaste: a curl command fills the request (on the URL line or with no
// field edited); into a field, the sanitized text goes in and a
// secret-looking span offers a variable (C3.10 layer 2).
func (t *Tool) onPaste(ctx *ui.Context, m tea.PasteMsg) tea.Cmd {
	if mode := t.mode(ctx); mode == vmNone || mode == vmTiny {
		return nil
	}
	if t.rv.find != nil {
		t.rv.find.paste(m.Content)
		return nil
	}
	e := t.ed
	text := strings.TrimSpace(m.Content)
	isCurl := text == "curl" || strings.HasPrefix(text, "curl ") || strings.HasPrefix(text, "curl\t")
	if isCurl && (e.url != nil || !e.capturing()) {
		return t.pasteCurl(ctx, text)
	}
	switch tb := e.table(); {
	case e.req == nil:
		return nil
	case e.url != nil:
		e.url.paste(m.Content)
		return t.offerSecret(ctx, &pendingSecret{target: "url", text: e.url.text()}, "")
	case tb != nil && !e.onURL && tb.CapturesInput():
		tb.HandleMsg(m)
		k, v, _ := tb.EditCell()
		return t.offerSecret(ctx, &pendingSecret{target: "table", tb: tb, text: v}, k)
	case e.body.editing:
		e.body.insert(textwidth.SanitizeInput(m.Content))
		e.req.Body.Text = e.body.text()
		return t.offerSecret(ctx, &pendingSecret{target: "body", text: e.req.Body.Text}, "")
	}
	return nil
}

// pasteCurl replaces the edited request's content with a curl command's.
func (t *Tool) pasteCurl(ctx *ui.Context, cmd string) tea.Cmd {
	r, rep, err := convert.ParseCurl(cmd)
	if err != nil {
		return ui.Toast(ctx.T.Apitest.ImportFailed(errText(err)), ui.ToastError)
	}
	if t.req == nil {
		t.newRequest(ctx)
	}
	r.ID, r.Name = t.req.ID, t.req.Name
	*t.req = *r
	t.ed.load(t.req)
	t.focus = paneRequest
	if len(rep.Ignored)+len(rep.Dropped)+len(rep.Suggestions) == 0 {
		return nil
	}
	return ui.OpenOverlay(t.reportDialog(ctx, nil, rep, nil))
}

// varName is a variable name the placeholder syntax accepts.
var varName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// offerSecret asks about the first secret-looking span of p.text (the field
// named name).
func (t *Tool) offerSecret(_ *ui.Context, p *pendingSecret, name string) tea.Cmd {
	fs := apitool.Detect(name, p.text)
	if len(fs) == 0 {
		return nil
	}
	p.f = fs[0]
	t.pendingSecret = p
	return ui.OpenRegisteredOverlay(overlaySecret, promptArgs{Inst: t.route.ID, Name: suggestName(p.f.Rule, name)}.encode())
}

// suggestName is the offered variable name for a finding.
func suggestName(rule, field string) string {
	switch rule {
	case "jwt", "bearer":
		return "token"
	case "basic":
		return "basic_auth"
	case "named_high_entropy":
		n := strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
				return r
			case r >= 'A' && r <= 'Z':
				return r + 'a' - 'A'
			}
			return '_'
		}, field)
		if n = strings.Trim(n, "_"); n != "" {
			return n
		}
		return "secret"
	case "":
		return "secret"
	}
	return rule
}

// secretAnswer replaces the span with {{name}}, declares the secret
// variable (environment, else my globals; allowed hosts = the request's
// and base_url's) and puts the value in the vault.
func (t *Tool) secretAnswer(ctx *ui.Context, m dialogMsg) tea.Cmd {
	p := t.pendingSecret
	t.pendingSecret = nil
	if p == nil || m.Choice != 0 || t.req == nil {
		return nil
	}
	name := strings.TrimSpace(m.Input)
	if !varName.MatchString(name) {
		return ui.Toast(ctx.T.Apitest.SecretBadName(), ui.ToastError)
	}
	val := secrets.New(p.text[p.f.Start:p.f.End])
	replaced := p.text[:p.f.Start] + "{{" + name + "}}" + p.text[p.f.End:]
	e := t.ed
	reqURL := t.req.URL // the URL whose host the secret may go to
	switch p.target {
	case "table":
		if p.tb.CapturesInput() {
			p.tb.ReplaceEdit(replaced)
			e.store()
		}
	case "url":
		if e.url != nil {
			e.url = newLineInput(replaced)
		}
		// The pasted URL is not the request's yet (enter confirms it): its
		// host is the one meant. The text with the value still in it
		// expands (the new variable is not declared yet).
		reqURL = p.text
	case "body":
		e.body.set(replaced)
		e.req.Body.Text = replaced
	}
	return t.declareSecret(ctx, name, val, reqURL)
}

// declareSecret makes name a secret variable and stores val for it; its
// default allowed hosts are reqURL's and base_url's.
func (t *Tool) declareSecret(ctx *ui.Context, name string, val secrets.Secret, reqURL string) tea.Cmd {
	sc := t.scope(ctx)
	d := dyn(ctx)
	var p *apitool.Prepared
	if u, _, err := apitool.Expand(reqURL, sc, d); err == nil {
		if !strings.Contains(u, "://") {
			u = "http://" + u
		}
		p = &apitool.Prepared{URL: u}
	}
	hosts := apitool.DefaultHosts(p, sc, d)
	declare := func(vs []apitool.Var) []apitool.Var {
		for i := range vs {
			if vs[i].Name == name {
				vs[i].Secret, vs[i].Value = true, ""
				for _, h := range hosts {
					if !slices.Contains(vs[i].AllowedHosts, h) {
						vs[i].AllowedHosts = append(vs[i].AllowedHosts, h)
					}
				}
				return vs
			}
		}
		return append(vs, apitool.Var{Name: name, Secret: true, AllowedHosts: hosts})
	}
	var err error
	if t.env != nil {
		t.env.Vars = declare(t.env.Vars)
		err = t.local.SaveEnv(t.env)
	} else {
		t.globals = declare(t.globals)
		err = t.local.SaveGlobals(t.globals)
	}
	if err != nil {
		return ui.Toast(ctx.T.Apitest.SaveFailed(errText(err)), ui.ToastError)
	}
	key, _, ok := t.scope(ctx).SecretSlot(name)
	if !ok {
		return nil
	}
	t.pendingPuts = append(t.pendingPuts, pendingPut{key: key, name: name, val: val})
	return t.flushPuts(ctx)
}

// flushPuts writes the waiting secret values off the UI goroutine (the OS
// store may block); a missing OS store asks for consent first.
func (t *Tool) flushPuts(ctx *ui.Context) tea.Cmd {
	v := t.vault(ctx)
	puts := t.pendingPuts
	t.pendingPuts = nil
	if v == nil || len(puts) == 0 {
		return nil
	}
	return func() tea.Msg {
		for i, p := range puts {
			if err := v.Put(p.key, p.val); err != nil {
				if errors.Is(err, secrets.ErrNeedConsent) {
					return secretPutMsg{t: t, done: puts[:i], rest: puts[i:], consent: true}
				}
				return secretPutMsg{t: t, done: puts[:i], err: err}
			}
		}
		return secretPutMsg{t: t, done: puts}
	}
}

// onSecretPut follows a vault write.
func (t *Tool) onSecretPut(ctx *ui.Context, m secretPutMsg) tea.Cmd {
	a := ctx.T.Apitest
	switch {
	case m.consent:
		t.pendingPuts = append(m.rest, t.pendingPuts...)
		return ui.OpenRegisteredOverlay(secretsui.OverlayConsent, ui.ToolAPI)
	case m.err != nil:
		return ui.Toast(a.SaveFailed(errText(m.err)), ui.ToastError)
	case len(m.done) > 0:
		i := len(m.done) - 1
		for i >= 0 && m.done[i].quiet {
			i--
		}
		if i < 0 {
			return t.loadMasks(ctx)
		}
		last := m.done[i]
		where := ""
		if v := t.vault(ctx); v != nil {
			where = secretsui.BackendLabel(ctx.T, v.Where(last.key))
		}
		return tea.Batch(ui.Toast(a.SecretSaved(textwidth.Sanitize(last.name), where), ui.ToastSuccess), t.loadMasks(ctx))
	}
	return nil
}

// ---- save ----

// saveRequest writes the edited request into its personal collection (a
// new request into "내 요청"). A collection changed outside masil asks
// before it is overwritten.
func (t *Tool) saveRequest(ctx *ui.Context) tea.Cmd {
	if t.req == nil {
		return nil
	}
	t.ed.store()
	a := ctx.T.Apitest
	if t.colName != "" && t.collection(t.colName) != nil {
		if changed, _ := t.local.Changed(t.colName); changed {
			return ui.OpenOverlay(widgets.Confirm(a.OverwriteTitle(), a.OverwriteBody(), a.OverwriteYes(), a.Cancel(),
				func() tea.Msg { return saveConfirmedMsg{t: t} }))
		}
	}
	return t.writeRequest(ctx)
}

// writeRequest saves without asking.
func (t *Tool) writeRequest(ctx *ui.Context) tea.Cmd {
	a := ctx.T.Apitest
	name := t.colName
	if name == "" {
		name = a.DefaultCollection()
	}
	c := t.collection(name)
	if c == nil {
		c = &apitool.Collection{Version: apitool.CollectionVersion, ID: newID(), Name: name, Requests: map[string]*apitool.Request{}}
	}
	if t.req.ID == "" {
		t.req.ID = newID()
	}
	if c.Requests[t.req.ID] == nil {
		c.Root.Items = append(c.Root.Items, apitool.Node{Request: t.req.ID})
	}
	c.Requests[t.req.ID] = cloneRequest(t.req)
	if err := t.local.SaveCollection(c); err != nil {
		return ui.Toast(a.SaveFailed(errText(err)), ui.ToastError)
	}
	t.cols[name] = c
	oldKey := t.respKey()
	t.colName = name
	if st := t.rv.states[oldKey]; st != nil && oldKey != t.respKey() {
		t.rv.states[t.respKey()] = st
		delete(t.rv.states, oldKey)
		t.rv.order = slices.DeleteFunc(t.rv.order, func(k string) bool { return k == oldKey })
		t.rv.order = append(t.rv.order, t.respKey())
	}
	t.rv.show(t.respKey())
	t.fileChanged = false
	t.reload(ctx)
	t.tree.expand(name, true)
	t.tree.follow(ctx, t)
	t.save(ctx)
	return ui.Toast(a.Saved(textwidth.Sanitize(name)), ui.ToastSuccess)
}

// checkFile looks (off the UI goroutine) whether the shown collection
// changed outside masil.
func (t *Tool) checkFile() tea.Cmd {
	name := t.colName
	if name == "" {
		return nil
	}
	local := t.local
	return func() tea.Msg {
		changed, err := local.Changed(name)
		if err != nil {
			slog.Warn("api: collection changed", "err", err)
		}
		return fileCheckMsg{t: t, name: name, changed: changed}
	}
}

// reloadFile reads the changed collection again (R on the band).
func (t *Tool) reloadFile(ctx *ui.Context) tea.Cmd {
	name, id := t.colName, ""
	if t.req != nil {
		id = t.req.ID
	}
	delete(t.cols, name)
	t.fileChanged = false
	t.reload(ctx)
	t.openFirst(name, id)
	t.tree.follow(ctx, t)
	return nil
}
