package apitool

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/secretsui"
	"github.com/patrache/masil/internal/ui/settings"
)

// The environments screen (e, C3.8 / C3.10): where secret values are kept
// (first line), the environment picker, the variables of the environment
// and my globals with their source, a secret's allowed hosts, and the
// collection's cookies (memory only). A secret value is typed into the
// secretsui prompt (a modal outside the panes, A9.1) and goes straight to
// the vault; the screen only ever holds "••••" + its last 4.

// Dialog kinds of the environments screen.
const (
	dkEnvAdd   dialogKind = "env_add"
	dkEnvValue dialogKind = "env_value"
	dkEnvHosts dialogKind = "env_hosts"
	dkEnvNew   dialogKind = "env_new"
)

// envRow is one variable row: of the environment, or global.
type envRow struct {
	v      apitool.Var
	global bool
}

// id names the row in dialog data ("e:<name>" / "g:<name>").
func (r envRow) id() string {
	if r.global {
		return "g:" + r.v.Name
	}
	return "e:" + r.v.Name
}

// masksMsg carries the masked secret values ("e:<name>" -> "••••1234").
type masksMsg struct {
	t     *Tool
	masks map[string]string
}

// envRows lists the environment's variables, then my globals.
func (t *Tool) envRows() []envRow {
	var out []envRow
	if t.env != nil {
		for _, v := range t.env.Vars {
			out = append(out, envRow{v: v})
		}
	}
	for _, v := range t.globals {
		out = append(out, envRow{v: v, global: true})
	}
	return out
}

// envSel is the selected variable row.
func (t *Tool) envSel() (envRow, bool) {
	rows := t.envRows()
	if t.envCur < 0 || t.envCur >= len(rows) {
		return envRow{}, false
	}
	return rows[t.envCur], true
}

// rowVars is the slice holding r (the environment's or my globals).
func (t *Tool) rowVars(global bool) *[]apitool.Var {
	if global || t.env == nil {
		return &t.globals
	}
	return &t.env.Vars
}

// slotOf is the vault key of the secret variable row r.
func (t *Tool) slotOf(r envRow) string {
	s := &apitool.Scope{Globals: t.globals}
	if !r.global {
		s = &apitool.Scope{Env: t.env}
	}
	key, _, _ := s.SecretSlot(r.v.Name)
	return key
}

// saveRowVars writes the file holding the row's variables.
func (t *Tool) saveRowVars(global bool) error {
	if global || t.env == nil {
		return t.local.SaveGlobals(t.globals)
	}
	return t.local.SaveEnv(t.env)
}

// openEnv opens the environments screen.
func (t *Tool) openEnv(ctx *ui.Context) tea.Cmd {
	t.envCur = 0
	t.envs, _ = t.local.ListEnvs()
	return tea.Batch(t.loadMasks(ctx), ui.OpenOverlay(&envScreen{t: t}))
}

// loadMasks reads the secret values off the UI goroutine and keeps only
// their masked form.
func (t *Tool) loadMasks(ctx *ui.Context) tea.Cmd {
	v := t.vault(ctx)
	if v == nil {
		return nil
	}
	keys := map[string]string{}
	for _, r := range t.envRows() {
		if r.v.Secret {
			if k := t.slotOf(r); k != "" {
				keys[r.id()] = k
			}
		}
	}
	return func() tea.Msg {
		out := map[string]string{}
		for id, k := range keys {
			if s, err := v.Get(k); err == nil && !s.IsZero() {
				out[id] = maskSecret(s.Reveal())
			}
		}
		return masksMsg{t: t, masks: out}
	}
}

// storageLine is where secret values are kept: the backend of the first
// value this account keeps, else the chosen fallback (never the OS store).
func (t *Tool) storageLine(ctx *ui.Context) string {
	a := ctx.T.Apitest
	server, user, ok := ctx.SecretScope()
	if !ok || ctx.Secrets == nil {
		return a.EnvStorageUnset()
	}
	var where secrets.Backend
	if names, err := ctx.Secrets.List(context.Background(), ui.ToolAPI, server, user); err == nil && len(names) > 0 {
		where = ctx.Secrets.Where(secrets.Key{Tool: ui.ToolAPI, Server: server, UserID: user, Name: names[0]})
	}
	if where == "" {
		switch ctx.Secrets.Fallback() {
		case secrets.FallbackSession:
			where = secrets.BackendMemory
		case secrets.FallbackFile:
			where = secrets.BackendFile
		}
	}
	if l := secretsui.BackendLabel(ctx.T, where); l != "" {
		return a.EnvStorage(l)
	}
	return a.EnvStorageUnset()
}

// sourceLabel names where a value comes from; a personal environment is my
// own value.
func sourceLabel(ctx *ui.Context, s apitool.Source, shared bool) string {
	a := ctx.T.Apitest
	switch s {
	case apitool.SourceTemp:
		return a.SrcTemp()
	case apitool.SourceMine:
		return a.SrcMine()
	case apitool.SourceSharedEnv:
		if shared {
			return a.SrcShared()
		}
		return a.SrcMine()
	case apitool.SourceCollection:
		return a.SrcCollection()
	case apitool.SourceGlobal:
		return a.SrcGlobal()
	}
	return a.SrcDynamic()
}

// cookies are the collection's cookies for the edited request's host.
func (t *Tool) cookies(ctx *ui.Context) []*shownCookie {
	if t.req == nil {
		return nil
	}
	sc := t.scope(ctx)
	sc.Vault = nil // never read a secret to draw
	u, _, err := apitool.Expand(t.req.URL, sc, dyn(ctx))
	if err != nil {
		return nil
	}
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	pu, err := url.Parse(u)
	if err != nil || pu.Host == "" {
		return nil
	}
	var out []*shownCookie
	for _, c := range t.sender.Jars.List(sc.CollectionID, pu) {
		out = append(out, &shownCookie{name: c.Name, value: c.Value})
	}
	return out
}

// shownCookie is a cookie as shown (name and value only).
type shownCookie struct{ name, value string }

// switchEnv picks the next (d 1) or previous environment; "none" is last.
func (t *Tool) switchEnv(ctx *ui.Context, d int) tea.Cmd {
	t.envs, _ = t.local.ListEnvs()
	names := append(slices.Clone(t.envs), "")
	cur := ""
	if t.env != nil {
		cur = t.env.Name
	}
	i := slices.Index(names, cur)
	t.selectEnv(ctx, names[(i+d+len(names))%len(names)])
	t.envCur = 0
	t.save(ctx)
	return t.loadMasks(ctx)
}

// envKey is the environments screen's keys.
func (t *Tool) envKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	a := ctx.T.Apitest
	rows := t.envRows()
	r, ok := t.envSel()
	switch msg.String() {
	case "esc", "q":
		return true, func() tea.Msg { return ui.CloseOverlayMsg{} }
	case "j", "down":
		t.envCur = min(t.envCur+1, max(len(rows)-1, 0))
	case "k", "up":
		t.envCur = max(t.envCur-1, 0)
	case "h", "left":
		return true, t.switchEnv(ctx, -1)
	case "l", "right":
		return true, t.switchEnv(ctx, 1)
	case "N":
		return true, ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkEnvNew, title: a.EnvNewTitle(), prefW: 56,
			body: []string{a.EnvNewPrompt()}, input: newLineInput("")})
	case "a":
		return true, ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkEnvAdd, title: a.EnvAddTitle(), prefW: 56,
			body: []string{a.EnvAddPrompt()}, input: newLineInput("")})
	case "enter":
		if !ok {
			return true, nil
		}
		if r.v.Secret {
			args := `{"tool":"` + ui.ToolAPI + `","name":` + quoteJSON(r.id()) + `,"label":` + quoteJSON(a.SecretLabel(r.v.Name)) + `}`
			return true, ui.OpenRegisteredOverlay(secretsui.OverlayPrompt, args)
		}
		return true, ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkEnvValue, data: r.id(), title: a.EnvValueTitle(r.v.Name), prefW: 64,
			input: newLineInput(r.v.Value)})
	case "H":
		if ok && r.v.Secret {
			return true, ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkEnvHosts, data: r.id(), title: a.EnvHostsTitle(), prefW: 64,
				body: []string{a.EnvHostsPrompt()}, input: newLineInput(strings.Join(r.v.AllowedHosts, ", "))})
		}
	case "s":
		if ok && !r.v.Secret {
			return true, t.makeSecret(ctx, r)
		}
	case "d":
		if ok {
			return true, t.deleteVar(ctx, r)
		}
	case "C":
		t.sender.Jars.Clear(t.scope(ctx).CollectionID)
		return true, ui.Toast(a.EnvCookiesCleared(), ui.ToastSuccess)
	}
	return true, nil
}

// quoteJSON is s as a JSON string.
func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20:
			b.WriteString(" ")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// findRow finds the row named by id ("e:<name>" / "g:<name>").
func (t *Tool) findRow(id string) (envRow, bool) {
	for _, r := range t.envRows() {
		if r.id() == id {
			return r, true
		}
	}
	return envRow{}, false
}

// setVarField changes the row's variable in place and saves its file.
func (t *Tool) setVarField(ctx *ui.Context, id string, f func(v *apitool.Var)) tea.Cmd {
	r, ok := t.findRow(id)
	if !ok {
		return nil
	}
	vs := t.rowVars(r.global)
	i := slices.IndexFunc(*vs, func(v apitool.Var) bool { return v.Name == r.v.Name })
	if i < 0 {
		return nil
	}
	f(&(*vs)[i])
	if err := t.saveRowVars(r.global); err != nil {
		return ui.Toast(ctx.T.Apitest.SaveFailed(describeErr(ctx, err)), ui.ToastError)
	}
	return nil
}

// makeSecret marks a variable secret: its value moves into the vault and
// its allowed hosts start as the request's and base_url's hosts.
func (t *Tool) makeSecret(ctx *ui.Context, r envRow) tea.Cmd {
	val := r.v.Value
	var hosts []string
	if t.req != nil {
		sc, d := t.scope(ctx), dyn(ctx)
		var p *apitool.Prepared
		if u, _, err := apitool.Expand(t.req.URL, sc, d); err == nil {
			if !strings.Contains(u, "://") {
				u = "http://" + u
			}
			p = &apitool.Prepared{URL: u}
		}
		hosts = apitool.DefaultHosts(p, sc, d)
	}
	if cmd := t.setVarField(ctx, r.id(), func(v *apitool.Var) {
		v.Secret, v.Value = true, ""
		if len(v.AllowedHosts) == 0 {
			v.AllowedHosts = hosts
		}
	}); cmd != nil {
		return cmd
	}
	r.v.Secret = true
	if val == "" {
		return nil
	}
	t.pendingPuts = append(t.pendingPuts, pendingPut{key: t.slotOf(r), name: r.v.Name, val: secrets.New(val)})
	return tea.Batch(t.flushPuts(ctx))
}

// deleteVar removes a variable (and a secret's value from the vault).
func (t *Tool) deleteVar(ctx *ui.Context, r envRow) tea.Cmd {
	key := ""
	if r.v.Secret {
		key = t.slotOf(r)
	}
	vs := t.rowVars(r.global)
	*vs = slices.DeleteFunc(*vs, func(v apitool.Var) bool { return v.Name == r.v.Name })
	t.envCur = max(min(t.envCur, len(t.envRows())-1), 0)
	if err := t.saveRowVars(r.global); err != nil {
		return ui.Toast(ctx.T.Apitest.SaveFailed(describeErr(ctx, err)), ui.ToastError)
	}
	v := t.vault(ctx)
	if key == "" || v == nil {
		return nil
	}
	return func() tea.Msg {
		if err := v.Delete(key); err != nil && !isNotFound(err) {
			slog.Warn("api: delete secret", "err", err)
		}
		return nil
	}
}

func isNotFound(err error) bool { return errors.Is(err, secrets.ErrNotFound) }

// envDialog follows the environments screen's dialogs.
func (t *Tool) envDialog(ctx *ui.Context, m dialogMsg) tea.Cmd {
	if m.Choice != 0 {
		return nil
	}
	a := ctx.T.Apitest
	in := strings.TrimSpace(m.Input)
	switch m.Kind {
	case dkEnvNew:
		if in == "" || slices.Contains(t.envs, in) {
			return nil
		}
		if err := t.local.SaveEnv(&apitool.Env{Name: in}); err != nil {
			return ui.Toast(a.SaveFailed(describeErr(ctx, err)), ui.ToastError)
		}
		t.reload(ctx)
		t.selectEnv(ctx, in)
		t.envCur = 0
		t.save(ctx)
	case dkEnvAdd:
		if !varName.MatchString(in) {
			return ui.Toast(a.SecretBadName(), ui.ToastError)
		}
		vs := t.rowVars(false)
		if !slices.ContainsFunc(*vs, func(v apitool.Var) bool { return v.Name == in }) {
			*vs = append(*vs, apitool.Var{Name: in})
		}
		if err := t.saveRowVars(false); err != nil {
			return ui.Toast(a.SaveFailed(describeErr(ctx, err)), ui.ToastError)
		}
		rows := t.envRows()
		t.envCur = max(slices.IndexFunc(rows, func(r envRow) bool { return r.v.Name == in && r.global == (t.env == nil) }), 0)
	case dkEnvValue:
		return t.setVarField(ctx, m.Data, func(v *apitool.Var) { v.Value = m.Input })
	case dkEnvHosts:
		var hosts []string
		for _, h := range strings.Split(in, ",") {
			if h = strings.TrimSpace(h); h != "" && !slices.Contains(hosts, h) {
				hosts = append(hosts, h)
			}
		}
		return t.setVarField(ctx, m.Data, func(v *apitool.Var) { v.AllowedHosts = hosts })
	}
	return nil
}

// onPrompt stores a secret value typed into the secretsui prompt (Name is
// the row id).
func (t *Tool) onPrompt(ctx *ui.Context, m secretsui.PromptMsg) tea.Cmd {
	if m.Canceled || m.Value.IsZero() {
		return nil
	}
	r, ok := t.findRow(m.Name)
	if !ok || !r.v.Secret {
		return nil
	}
	t.pendingPuts = append(t.pendingPuts, pendingPut{key: t.slotOf(r), name: r.v.Name, val: m.Value})
	return t.flushPuts(ctx)
}

// registerToolLogin adds the settings "도구 로그인" row: the number of
// secret variables this account keeps. [지우기] uses the settings default
// (every api value of this account is deleted from ctx.Secrets).
func registerToolLogin() {
	settings.RegisterToolLogin(ui.ToolAPI, describeLogin, nil)
}

// describeLogin is "비밀 변수 {n}개" (no row without a value). It lists names
// only (never the OS store).
func describeLogin(ctx *ui.Context) (string, bool) {
	server, user, ok := ctx.SecretScope()
	if !ok || ctx.Secrets == nil {
		return "", false
	}
	names, err := ctx.Secrets.List(context.Background(), ui.ToolAPI, server, user)
	if err != nil || len(names) == 0 {
		return "", false
	}
	return ctx.T.Apitest.ToolLoginAccount(len(names)), true
}

// ---- the overlay ----

// envScreen draws the environments screen of tool t (state lives in t).
type envScreen struct {
	t    *Tool
	size ui.Size
	top  int
}

var (
	_ ui.Overlay    = (*envScreen)(nil)
	_ ui.KeyHandler = (*envScreen)(nil)
	_ ui.Titled     = (*envScreen)(nil)
)

func (s *envScreen) Init(*ui.Context) tea.Cmd            { return nil }
func (s *envScreen) Update(*ui.Context, tea.Msg) tea.Cmd { return nil }
func (s *envScreen) Resize(_ *ui.Context, sz ui.Size)    { s.size = sz }
func (s *envScreen) Modal() bool                         { return true }

// Bounds is most of the body (the app drops the border when it does not fit).
func (s *envScreen) Bounds(body layout.Rect) layout.Rect {
	w := min(body.W, 96)
	h := min(body.H, 30)
	return layout.Rect{X: body.X + (body.W-w)/2, Y: body.Y + (body.H-h)/2, W: w, H: h}
}

func (s *envScreen) Title(ctx *ui.Context) (string, ui.Badge) {
	name := ctx.T.Apitest.EnvNone()
	if s.t.env != nil {
		name = textwidth.Sanitize(s.t.env.Name)
	}
	return ctx.T.Apitest.EnvTitle(name), ui.Badge{}
}

func (s *envScreen) HandleKey(ctx *ui.Context, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return false, nil
	}
	return s.t.envKey(ctx, msg)
}

// View: storage line, picker, variables, the selected one's source and
// hosts, cookies, the key hint.
func (s *envScreen) View(ctx *ui.Context, _ bool) string {
	t, a := s.t, ctx.T.Apitest
	w, h := s.size.W, s.size.H
	pad := ""
	if w >= 20 {
		pad = " "
	}
	cw := max(w-2*len(pad), 1)
	line := func(x string) string { return pad + textwidth.Truncate(x, cw, "…") }
	head := []string{}
	for _, l := range textwidth.Wrap(t.storageLine(ctx), cw) {
		head = append(head, pad+l)
	}
	name := a.EnvNone()
	if t.env != nil {
		name = textwidth.Sanitize(t.env.Name)
	}
	head = append(head, line(a.EnvPicker(name)), "")

	var foot []string
	rows := t.envRows()
	if r, ok := t.envSel(); ok {
		sc := t.scope(ctx)
		sc.Vault = nil
		src := apitool.SourceGlobal
		if !r.global {
			src = apitool.SourceSharedEnv
		}
		if v, err := sc.Lookup(r.v.Name); err == nil || r.v.Secret {
			if err == nil {
				src = v.Source
			}
		}
		info := a.EnvSource(sourceLabel(ctx, src, sc.DocID != ""))
		if r.v.Secret {
			info += " · " + a.EnvHosts(textwidth.Sanitize(strings.Join(r.v.AllowedHosts, ", ")))
		}
		foot = append(foot, "")
		for _, l := range textwidth.Wrap(info, cw) {
			foot = append(foot, pad+l)
		}
	}
	cookies := t.cookies(ctx)
	if len(cookies) > 0 {
		foot = append(foot, "", line(a.EnvCookies(len(cookies))))
		for _, c := range cookies {
			foot = append(foot, line("  "+textwidth.Sanitize(c.name+"="+c.value)))
		}
	}
	foot = append(foot, line(dim(ctx.Theme, a.EnvHint())))

	bodyH := max(h-len(head)-len(foot), 1)
	var body []string
	if len(rows) == 0 {
		body = []string{line(dim(ctx.Theme, a.EnvEmpty()))}
	} else {
		if t.envCur < s.top {
			s.top = t.envCur
		}
		if t.envCur >= s.top+bodyH {
			s.top = t.envCur - bodyH + 1
		}
		nameW := min(20, max(cw/3, 6))
		for i := s.top; i < len(rows) && len(body) < bodyH; i++ {
			r := rows[i]
			val := textwidth.Sanitize(r.v.Value)
			if r.v.Secret {
				val = t.masks[r.id()]
				if val == "" {
					val = a.NoValue()
				}
			}
			mark := ""
			if r.global {
				mark = "  " + a.EnvGlobalMark()
			}
			l := textwidth.PadRight(textwidth.Truncate(textwidth.Sanitize(r.v.Name), nameW, "…"), nameW) + "  " + val
			l = textwidth.Truncate(l, max(cw-textwidth.Width(mark), 1), "…") + dim(ctx.Theme, mark)
			if i == t.envCur {
				l = selected(ctx.Theme, textwidth.PadRight(l, cw))
			}
			body = append(body, pad+l)
		}
	}
	out := append(head, textwidth.Fit(body, w, bodyH)...)
	out = append(out, foot...)
	if len(out) > h {
		// Too short: the storage line and the selection win over the rest.
		out = append(out[:max(h-1, 1)], foot[len(foot)-1])[:h]
	}
	return strings.Join(textwidth.Fit(out, w, h), "\n")
}
