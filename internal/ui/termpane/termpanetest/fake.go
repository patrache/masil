// Package termpanetest is a stand-in for termpane.Launch (P0 Task 16a): tool
// tests (P1, P2) open programs without a PTY, see the requests exactly as the
// tool built them (Env included — for the secret-leak tests) and drive the
// program's end, working folder and attention signals by hand.
package termpanetest

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/termpane"
)

// Fake replaces Launch while installed.
type Fake struct {
	mu      sync.Mutex
	started []termhost.Request
	env     *termhost.Env
	limit   int
	live    map[string]*handle       // live fake panes by instance id
	modes   map[string]termhost.Mode // the mode each instance opened in
	handles map[string]*handle       // every fake pane by instance id
}

// Install makes termpane.Launch use a new Fake until tb's cleanup.
func Install(tb testing.TB) *Fake {
	tb.Helper()
	f := &Fake{limit: termhost.MaxPanes, live: map[string]*handle{}, modes: map[string]termhost.Mode{}, handles: map[string]*handle{}}
	tb.Cleanup(termpane.SetLaunchForTest(f.launch))
	return f
}

// SetEnv is the environment Choose decides in (Windows, tmux, tiny…). Without
// it: macOS, embedded panes on, outside tmux, tiny from the layout, the
// tool's configured open mode.
func (f *Fake) SetEnv(e termhost.Env) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.env = &e
}

// SetLimit makes a pane launch fail with termhost.ErrPaneLimit while n fake
// panes are alive (default termhost.MaxPanes).
func (f *Fake) SetLimit(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limit = n
}

// Started returns every request Launch got, as given (copies).
func (f *Fake) Started() []termhost.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]termhost.Request, len(f.started))
	for i, r := range f.started {
		out[i] = cloneReq(r)
	}
	return out
}

// Keys returns the keys instance instanceID's fake pane got.
func (f *Fake) Keys(instanceID string) []string {
	f.mu.Lock()
	h := f.handles[instanceID]
	f.mu.Unlock()
	if h == nil {
		return nil
	}
	return h.gotKeys()
}

// IDs returns the instance ids of every fake pane launched, sorted.
func (f *Fake) IDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Sorted(maps.Keys(f.handles))
}

// Exit ends instance instanceID's program with code: the msg to Send (the
// real Launch posts it by itself; neither needs the view to be Init'd). A
// pane stops counting toward the limit.
func (f *Fake) Exit(instanceID string, code int) tea.Msg {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.live, instanceID)
	mode := f.modes[instanceID]
	if mode == "" {
		mode = termhost.ModePane
	}
	return termpane.ExitedMsg{InstanceID: instanceID, Info: termhost.ExitInfo{Mode: mode, Code: code}}
}

// Cwd is the CwdMsg instanceID's program would post (OSC 7).
func (f *Fake) Cwd(instanceID, dir string) tea.Msg {
	return termpane.CwdMsg{InstanceID: instanceID, Dir: dir}
}

// Notify is the NotifyMsg instanceID's program would post ("bel", "osc9", "osc777").
func (f *Fake) Notify(instanceID, kind string) tea.Msg {
	return termpane.NotifyMsg{InstanceID: instanceID, Kind: kind}
}

func (f *Fake) launch(ctx *ui.Context, req termhost.Request, opts termpane.Options) (termpane.Launched, tea.Cmd, error) {
	if req.ToolID == "" {
		req.ToolID = opts.ToolID
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, cloneReq(req))
	var env termhost.Env
	if f.env != nil {
		env = *f.env
	} else {
		env = termhost.Env{GOOS: "darwin", TERM: "xterm-256color", Embedded: true, Tiny: ctx.Mode == layout.ModeTiny}
		if ctx.Config != nil {
			env.ToolOpen = termhost.Mode(ctx.Config.ToolOpen(req.ToolID))
		}
	}
	mode, downgraded := termhost.Choose(req, env)
	var toast tea.Cmd
	if downgraded {
		toast = termpane.DowngradedToast(ctx.T, req, env)
	}
	id := opts.InstanceID
	if mode != termhost.ModePane {
		f.modes[id] = mode
		return termpane.Launched{Mode: mode}, toast, nil
	}
	if len(f.live) >= f.limit {
		return termpane.Launched{}, tea.Batch(toast, termpane.PaneLimitToast(ctx.T, f.limit)), termhost.ErrPaneLimit
	}
	h := &handle{program: req.Program, onClose: func() { f.closed(id) }}
	f.live[id], f.handles[id], f.modes[id] = h, h, mode
	return termpane.Launched{Mode: mode, View: termpane.New(h, opts)}, toast, nil
}

// closed is a fake pane's Close: it no longer counts toward the limit.
func (f *Fake) closed(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.live, id)
}

func cloneReq(r termhost.Request) termhost.Request {
	r.Args, r.Env = slices.Clone(r.Args), slices.Clone(r.Env)
	return r
}

// handle is a fake pane: it draws "<program> pane WxH" and records keys.
type handle struct {
	mu      sync.Mutex
	program string
	size    termhost.Size
	keys    []string
	onClose func()
}

func (h *handle) Program() string { return h.program }
func (h *handle) Resize(s termhost.Size) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.size = s
	return nil
}
func (h *handle) SendKey(k tea.KeyPressMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.keys = append(h.keys, k.String())
}
func (h *handle) SendPaste(string)                 {}
func (h *handle) SendMouse(tea.MouseMsg, int, int) {}
func (h *handle) SetFocus(bool)                    {}

// Render draws "<base name of the program> pane WxH". Only the base name,
// so a frame with a pane in it does not depend on where the tool found the
// program (on Windows a drive-qualified C:\... path): one golden serves
// every OS. The full program is in Program() and in Started().
func (h *handle) Render() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return fmt.Sprintf("%s pane %dx%d", filepath.Base(h.program), h.size.W, h.size.H)
}
func (h *handle) Cursor() (int, int, bool)       { return 0, 0, false }
func (h *handle) Title() string                  { return "" }
func (h *handle) MouseMode() bool                { return false }
func (h *handle) Scrollback() []string           { return nil }
func (h *handle) Done() <-chan termhost.ExitInfo { return nil } // ends only through Fake.Exit
func (h *handle) Shared() bool                   { return false }
func (h *handle) Close() error {
	h.onClose()
	return nil
}

func (h *handle) gotKeys() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.keys)
}
