package apitool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/apitool/convert"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// The guard questions (registered overlays) and the import / export
// dialogs. Every answer comes back as a dialogMsg for the instance.

// Registered overlay ids (P4 Task 14).
const (
	overlaySecret    = "api.secret_detected"
	overlayHostBlock = "api.host_block"
	overlayPrivate   = "api.private_target"
)

// Dialog kinds.
const (
	dkSecret     dialogKind = "secret"
	dkHost       dialogKind = "host"
	dkPrivate    dialogKind = "private"
	dkImport     dialogKind = "import"
	dkExport     dialogKind = "export"
	dkExportPath dialogKind = "export_path"
	dkReport     dialogKind = "report"
	dkSaveBody   dialogKind = "save_body"
	dkFileExists dialogKind = "file_exists"
	dkInsecure   dialogKind = "insecure"
)

// pendingWrite is a save to a path that exists, waiting for the overwrite
// answer: kind is the save (dkSaveBody or dkExportPath), key the response.
type pendingWrite struct {
	kind      dialogKind
	key, path string
}

// promptArgs are a registered question's arguments (JSON).
type promptArgs struct {
	Inst string `json:"inst"`
	Var  string `json:"var,omitempty"`
	Host string `json:"host,omitempty"`
	By   string `json:"by,omitempty"`
	Name string `json:"name,omitempty"`
}

func (a promptArgs) encode() string {
	b, _ := json.Marshal(a)
	return string(b)
}

func decodeArgs(s string) promptArgs {
	var a promptArgs
	_ = json.Unmarshal([]byte(s), &a)
	return a
}

func registerOverlays() {
	ui.RegisterOverlay(overlaySecret, func(ctx *ui.Context, args string) ui.Overlay {
		p, a := decodeArgs(args), ctx.T.Apitest
		return &dialog{inst: p.Inst, kind: dkSecret, title: a.SecretTitle(), prefW: 60,
			body:    []string{a.SecretDetected(), a.SecretName()},
			input:   newLineInput(p.Name),
			choices: []string{a.SecretToVar(), a.SecretKeep()}}
	})
	ui.RegisterOverlay(overlayHostBlock, func(ctx *ui.Context, args string) ui.Overlay {
		p, a := decodeArgs(args), ctx.T.Apitest
		return &dialog{inst: p.Inst, kind: dkHost, title: a.HostBlockTitle(), prefW: 60,
			body:    []string{a.HostBlock(p.Var, p.Host)},
			choices: []string{a.HostAllow(), a.Cancel()}}
	})
	ui.RegisterOverlay(overlayPrivate, func(ctx *ui.Context, args string) ui.Overlay {
		p, a := decodeArgs(args), ctx.T.Apitest
		line := a.PrivateTargetUnknown()
		if p.By != "" {
			line = a.PrivateTarget(p.By)
		}
		return &dialog{inst: p.Inst, kind: dkPrivate, title: a.PrivateTitle(), prefW: 60,
			body:    []string{line},
			choices: []string{a.PrivateSend(), a.Cancel()}}
	})
}

// onDialog follows a closed dialog of this instance.
func (t *Tool) onDialog(ctx *ui.Context, m dialogMsg) tea.Cmd {
	switch m.Kind {
	case dkUpload, dkUploadHeld, dkHeld, dkLost, dkVersions, dkVersionPreview:
		return t.afterEdit(ctx, t.sharedDialog(ctx, m))
	case dkShare:
		return t.shareAnswer(ctx, m)
	case dkRunHosts:
		return t.runHostsAnswer(ctx, m)
	case dkEnvAdd, dkEnvValue, dkEnvHosts, dkEnvNew:
		return t.envDialog(ctx, m)
	case dkSecret:
		return t.secretAnswer(ctx, m)
	case dkHost:
		he := t.pendingHost
		t.pendingHost = nil
		if he != nil && m.Choice == 0 {
			return t.allowHost(ctx, he)
		}
	case dkPrivate:
		pk := t.pendingPrivate
		t.pendingPrivate = ""
		if pk != "" && m.Choice == 0 {
			t.privateOK[pk] = true
			return t.send(ctx)
		}
	case dkInsecure:
		pk := t.pendingInsecure
		t.pendingInsecure = ""
		if pk != "" && m.Choice == 0 {
			t.insecureOK[pk] = true
			return t.send(ctx)
		}
	case dkImport:
		if path := strings.TrimSpace(m.Input); m.Choice == 0 && path != "" {
			return t.importFile(ctx, path)
		}
	case dkExport:
		return t.exportChoice(ctx, m.Choice)
	case dkSaveBody:
		if path := strings.TrimSpace(m.Input); m.Choice == 0 && path != "" {
			return t.saveBody(ctx, m.Data, path, false)
		}
	case dkExportPath:
		if path := strings.TrimSpace(m.Input); m.Choice == 0 && path != "" {
			return t.exportPostman(ctx, path, false)
		}
	case dkFileExists:
		p := t.pendingWrite
		t.pendingWrite = nil
		if p == nil || m.Choice != 0 {
			return nil
		}
		if p.kind == dkExportPath {
			return t.exportPostman(ctx, p.path, true)
		}
		return t.saveBody(ctx, p.key, p.path, true)
	}
	return nil
}

// ---- import ----

// openImport asks for the file to import.
func (t *Tool) openImport(ctx *ui.Context) tea.Cmd {
	a := ctx.T.Apitest
	return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkImport, title: a.ImportTitle(), prefW: 72,
		body: []string{a.ImportPrompt()}, input: newLineInput("")})
}

// imported is an import job's result.
type imported struct {
	col     *apitool.Collection
	first   string   // the request to open ("" = the collection's first)
	envs    []string // environment names saved
	secrets []pendingPut
	rep     convert.Report
}

// importFile reads and converts path in a job, saves the result as personal
// collection / environment files (new names, nothing is overwritten) and
// shows the report.
func (t *Tool) importFile(ctx *ui.Context, path string) tea.Cmd {
	local := t.local
	defaultCol := ctx.T.Apitest.DefaultCollection()
	_, cmd := t.startJob(ctx, jobFile, func(context.Context) func(*ui.Context) tea.Cmd {
		res, err := importAt(local, path, defaultCol)
		return func(ctx *ui.Context) tea.Cmd { return t.importDone(ctx, res, err) }
	})
	return cmd
}

// importAt does the file work of an import (off the UI goroutine).
func importAt(local *apitool.LocalStore, path, defaultCol string) (*imported, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, convert.MaxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > convert.MaxImportBytes {
		return nil, convert.ErrTooLarge
	}
	text := bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	res := &imported{}
	switch {
	case bytes.HasPrefix(text, []byte("curl")):
		r, rep, err := convert.ParseCurl(string(text))
		if err != nil {
			return nil, err
		}
		c, err := local.LoadCollection(defaultCol)
		if err != nil {
			c = &apitool.Collection{Version: apitool.CollectionVersion, ID: newID(), Name: defaultCol, Requests: map[string]*apitool.Request{}}
		}
		r.ID = newID()
		if r.Name == "" {
			r.Name = shortPath(r.URL)
		}
		c.Requests[r.ID] = r
		c.Root.Items = append(c.Root.Items, apitool.Node{Request: r.ID})
		if err := local.SaveCollection(c); err != nil {
			return nil, err
		}
		res.first = r.ID
		res.col, res.rep = &apitool.Collection{Name: c.Name, Requests: map[string]*apitool.Request{r.ID: r}}, rep
		return res, nil
	case len(text) == 0 || (text[0] != '{' && text[0] != '['):
		_, _, err := convert.ImportOpenAPI(text) // YAML: ErrYAMLUnsupported
		if err == nil {
			err = errUnknownFormat
		}
		return nil, err
	}
	var c *apitool.Collection
	switch {
	case bytes.Contains(text, []byte(`"openapi"`)):
		c, res.rep, err = convert.ImportOpenAPI(text)
	default:
		c, res.rep, err = convert.ImportPostman(text)
		if errors.Is(err, convert.ErrNotPostman) {
			return importEnv(local, text, res)
		}
	}
	if err != nil {
		return nil, err
	}
	for _, e := range c.Envs {
		e.Name = freeName(e.Name, mustList(local.ListEnvs()))
		if err := local.SaveEnv(&e); err != nil {
			return nil, err
		}
		res.envs = append(res.envs, e.Name)
	}
	c.Envs = nil
	c.Name = freeName(c.Name, mustList(local.ListCollections()))
	if err := local.SaveCollection(c); err != nil {
		return nil, err
	}
	res.col = c
	return res, nil
}

// errUnknownFormat: not a format the import reads.
var errUnknownFormat = errors.New("unknown format")

// importEnv imports a Postman environment; its secret values go to the vault
// (by the continuation).
func importEnv(local *apitool.LocalStore, text []byte, res *imported) (*imported, error) {
	env, vals, rep, err := convert.ImportPostmanEnv(text)
	if errors.Is(err, convert.ErrNotPostman) {
		return nil, errUnknownFormat
	}
	if err != nil {
		return nil, err
	}
	env.Name = freeName(env.Name, mustList(local.ListEnvs()))
	if err := local.SaveEnv(env); err != nil {
		return nil, err
	}
	sc := &apitool.Scope{Env: env}
	for name, v := range vals {
		if key, _, ok := sc.SecretSlot(name); ok {
			res.secrets = append(res.secrets, pendingPut{key: key, name: name, val: secrets.New(v)})
		}
	}
	slices.SortFunc(res.secrets, func(a, b pendingPut) int { return strings.Compare(a.name, b.name) })
	res.envs, res.rep = []string{env.Name}, rep
	return res, nil
}

func mustList(names []string, _ error) []string { return names }

// freeName is name, or "name 2", "name 3" … when taken (an import never
// overwrites a file).
func freeName(name string, taken []string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "import"
	}
	if !slices.Contains(taken, name) {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s %d", name, i); !slices.Contains(taken, n) {
			return n
		}
	}
}

// importDone shows the imported collection and the report.
func (t *Tool) importDone(ctx *ui.Context, res *imported, err error) tea.Cmd {
	a := ctx.T.Apitest
	if err != nil {
		var msg string
		switch {
		case errors.Is(err, convert.ErrYAMLUnsupported):
			msg = a.OpenapiYamlUnsupported()
		case errors.Is(err, convert.ErrTooLarge):
			msg = a.ImportTooLarge()
		case errors.Is(err, errUnknownFormat), errors.Is(err, convert.ErrNotOpenAPI), errors.Is(err, convert.ErrNotCurl):
			msg = a.ImportUnknown()
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
			msg = a.ImportFailed(describeErr(ctx, err))
		default:
			msg = a.ImportFailed(errText(err))
		}
		return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkReport, title: a.ImportTitle(), prefW: 72, body: []string{msg, a.ReportClose()}})
	}
	t.reload(ctx)
	if res.col != nil {
		delete(t.cols, res.col.Name)
		t.openFirst(res.col.Name, res.first)
		t.focus = paneTree
		t.tree.follow(ctx, t)
		t.save(ctx)
	}
	t.pendingPuts = append(t.pendingPuts, res.secrets...)
	return tea.Batch(t.flushPuts(ctx), ui.OpenOverlay(t.reportDialog(ctx, res.col, res.rep, res.envs)))
}

// reportDialog is the import report: what came in, the dropped scripts,
// what was not read and the secret-looking values (C3.9).
func (t *Tool) reportDialog(ctx *ui.Context, c *apitool.Collection, rep convert.Report, envs []string) *dialog {
	a := ctx.T.Apitest
	var body []string
	if c != nil {
		body = append(body, a.ImportDone(c.Name, len(c.Requests)))
	}
	for _, e := range envs {
		body = append(body, a.ImportEnvDone(e))
	}
	scripts := 0
	var dropped []string
	for _, d := range rep.Dropped {
		if strings.HasPrefix(d, "prerequest: ") || strings.HasPrefix(d, "test: ") {
			scripts++
			continue
		}
		dropped = append(dropped, d)
	}
	if scripts > 0 {
		body = append(body, a.ScriptsDropped(scripts))
	}
	if len(dropped) > 0 {
		body = append(body, a.ImportDropped(strings.Join(dropped, ", ")))
	}
	if len(rep.Ignored) > 0 {
		body = append(body, a.ImportIgnored(strings.Join(rep.Ignored, ", ")))
	}
	if len(rep.Suggestions) > 0 {
		var where []string
		for _, s := range rep.Suggestions {
			w := s.Where
			if c != nil && c.Requests[s.RequestID] != nil {
				w = c.Requests[s.RequestID].Name + " · " + w
			}
			if !slices.Contains(where, w) {
				where = append(where, w)
			}
		}
		body = append(body, a.SecretDetected(), a.ImportSecretWhere(strings.Join(where, ", ")))
	}
	body = append(body, a.ReportClose())
	return &dialog{inst: t.route.ID, kind: dkReport, title: a.ImportTitle(), prefW: 72, body: body}
}

// ---- export ----

// openExport offers curl (two ways) and the Postman file.
func (t *Tool) openExport(ctx *ui.Context) tea.Cmd {
	if t.req == nil {
		return nil
	}
	a := ctx.T.Apitest
	d := &dialog{inst: t.route.ID, kind: dkExport, title: a.ExportTitle(), prefW: 56,
		choices: []string{a.ExportCurl(), a.ExportCurlPlain(), a.ExportPostman()}}
	if t.collection(t.colName) != nil {
		d.chain = func(choice int) *dialog {
			if choice != 2 {
				return nil
			}
			dir, err := os.UserHomeDir()
			if err != nil {
				dir = "."
			}
			def := filepath.Join(dir, safeFileName(t.colName)+".postman_collection.json")
			return &dialog{inst: t.route.ID, kind: dkExportPath, title: a.ExportTitle(), prefW: 72,
				body: []string{a.ExportPath()}, input: newLineInput(def)}
		}
	}
	return ui.OpenOverlay(d)
}

// exportChoice runs the chosen export.
func (t *Tool) exportChoice(ctx *ui.Context, choice int) tea.Cmd {
	a := ctx.T.Apitest
	switch choice {
	case 0, 1:
		if t.req == nil {
			return nil
		}
		t.ed.store()
		mode := convert.CurlTemplate
		if choice == 1 {
			mode = convert.CurlNonSecret
		}
		req, sc, d, ok := cloneRequest(t.req), t.snapshot(ctx), dyn(ctx), a.ExportCopied()
		// ToCurl reads the vault (to hide secret values): off the UI goroutine.
		return func() tea.Msg {
			s, err := convert.ToCurl(req, sc, mode, d)
			if err != nil {
				return ui.ToastMsg{Text: a.ExportFailed(describeErr(ctx, err)), Kind: ui.ToastError}
			}
			return ui.Copy(ctx, s, ok)()
		}
	case 2: // a saved collection chains into the path dialog (openExport)
		return ui.Toast(a.SaveFirst(), ui.ToastInfo)
	}
	return nil
}

// safeFileName keeps a collection name usable as a file name.
func safeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, textwidth.Sanitize(s))
	if strings.TrimSpace(s) == "" {
		return "collection"
	}
	return s
}

// exportPostman writes the collection as a Postman v2.1 file (0600) in a
// job; an existing file is replaced only after the overwrite question.
func (t *Tool) exportPostman(ctx *ui.Context, path string, overwrite bool) tea.Cmd {
	c := t.collection(t.colName)
	if c == nil {
		return nil
	}
	data, _, err := convert.ExportPostman(c)
	if err != nil {
		return ui.Toast(ctx.T.Apitest.ExportFailed(describeErr(ctx, err)), ui.ToastError)
	}
	_, cmd := t.startJob(ctx, jobFile, func(context.Context) func(*ui.Context) tea.Cmd {
		err := writePrivate(path, data, overwrite)
		return func(ctx *ui.Context) tea.Cmd {
			if errors.Is(err, fs.ErrExist) && !overwrite {
				return t.askOverwrite(ctx, pendingWrite{kind: dkExportPath, path: path})
			}
			a := ctx.T.Apitest
			msg := a.ExportSaved(path)
			if err != nil {
				msg = a.ExportFailed(describeErr(ctx, err))
			}
			return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkReport, title: a.ExportTitle(), prefW: 72, body: []string{msg, a.ReportClose()}})
		}
	})
	return cmd
}

// askOverwrite asks before a save replaces an existing file.
func (t *Tool) askOverwrite(ctx *ui.Context, p pendingWrite) tea.Cmd {
	a := ctx.T.Apitest
	t.pendingWrite = &p
	return ui.OpenOverlay(&dialog{inst: t.route.ID, kind: dkFileExists, title: a.FileExistsTitle(), prefW: 72,
		body: []string{a.FileExistsBody(p.path)}, choices: []string{a.OverwriteYes(), a.Cancel()}})
}

// fillPrivate writes path readable by me only, its content from fill: a
// new file (an existing path fails with fs.ErrExist — the screen asks
// first), or (overwrite) a temporary file in the same folder renamed over
// path, so a symlink there is replaced, never followed to the file it
// points at.
func fillPrivate(path string, overwrite bool, fill func(io.Writer) error) error {
	if !overwrite {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if err := writeClose(f, fill); err != nil {
			_ = os.Remove(path)
			return err
		}
		return nil
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".masil-save-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if err := writeClose(f, fill); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// writeClose makes f mine only, fills and closes it. On Windows, which has
// no POSIX modes, Chmod(0o600) only clears the read-only attribute: the file
// keeps the ACL it inherits from its folder (under %USERPROFILE%, the user,
// SYSTEM and Administrators only).
func writeClose(f *os.File, fill func(io.Writer) error) error {
	err := f.Chmod(0o600)
	if err == nil {
		err = fill(f)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// writePrivate writes data to path readable by me only (see fillPrivate).
func writePrivate(path string, data []byte, overwrite bool) error {
	return fillPrivate(path, overwrite, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}
