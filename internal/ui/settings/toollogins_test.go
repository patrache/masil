package settings_test

import (
	"context"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/settings"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// keychainOS is an OS store fake that reports the macOS keychain.
type keychainOS struct {
	mu   sync.Mutex
	vals map[string]string
}

func (k *keychainOS) Name() secrets.Backend { return secrets.BackendKeychain }
func (k *keychainOS) Set(_ context.Context, _, a, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.vals[a] = v
	return nil
}
func (k *keychainOS) Get(_ context.Context, _, a string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.vals[a]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (k *keychainOS) Delete(_ context.Context, _, a string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.vals[a]; !ok {
		return secrets.ErrNotFound
	}
	delete(k.vals, a)
	return nil
}

// toolLoginsSetup registers two test tools (leet "시험" without forget, api
// "API" with forget), turns their flags on and fills a keychain store.
func toolLoginsSetup(t *testing.T) (*keychainOS, *secrets.Store, *int) {
	t.Helper()
	t.Setenv("MASIL_FEATURES", "leet,api")
	t.Cleanup(ui.SnapshotRegistryForTest())
	t.Cleanup(settings.ResetToolLoginsForTest())
	ui.RegisterTool(ui.ToolSpec{ID: ui.ToolLeet, Order: 1, Name: func(*i18n.T) string { return "시험" }})
	ui.RegisterTool(ui.ToolSpec{ID: ui.ToolAPI, Order: 5, Name: func(*i18n.T) string { return "API" }})
	forgets := 0
	settings.RegisterToolLogin(ui.ToolLeet, func(*ui.Context) (string, bool) { return "sam_lc", true }, nil)
	settings.RegisterToolLogin(ui.ToolAPI, func(*ui.Context) (string, bool) { return "변수 1개", true }, func(*ui.Context) tea.Cmd {
		forgets++
		return nil
	})
	settings.RegisterToolLogin(ui.ToolGit, func(*ui.Context) (string, bool) { return "x", true }, nil) // flag off: no row
	kos := &keychainOS{vals: map[string]string{}}
	d := t.TempDir()
	st := secrets.Open(secrets.Options{OS: kos, Paths: config.Paths{Dir: d, SecretsDir: d + "/secrets"}})
	return kos, st, &forgets
}

func TestToolLoginsSection(t *testing.T) {
	kos, st, forgets := toolLoginsSetup(t)
	h, _ := newSettings(t, "", uitest.WithSecrets(st))
	u, err := url.Parse(h.API.URL)
	if err != nil {
		t.Fatal(err)
	}
	host := u.Host
	bg := context.Background()
	for _, k := range []secrets.Key{
		{Tool: ui.ToolLeet, Server: host, UserID: fixtures.UserSam, Name: "cookie.session"},
		{Tool: ui.ToolLeet, Server: host, UserID: fixtures.UserSam, Name: "cookie.csrf"},
		{Tool: ui.ToolAPI, Server: host, UserID: fixtures.UserSam, Name: "env/prod/TOKEN"},
		{Tool: ui.ToolLeet, Server: host, UserID: fixtures.UserHana, Name: "cookie.session"}, // another masil account
	} {
		if _, err := st.Put(bg, k, secrets.New("v")); err != nil {
			t.Fatal(err)
		}
	}
	open(h, "tools")
	h.Resize(200, 60)
	if !strings.Contains(h.Screen(), "도구 로그인") {
		t.Fatalf("the 도구 로그인 section is missing:\n%s", h.Screen())
	}
	if l := lineWith(h, "시험 · "); !strings.Contains(l, "시험 · sam_lc · 키체인 · [지우기]") {
		t.Fatalf("leet row = %q\n%s", l, h.Screen())
	}
	if l := lineWith(h, "API · "); !strings.Contains(l, "API · 변수 1개 · 키체인 · [지우기]") {
		t.Fatalf("api row = %q", l)
	}
	if strings.Contains(h.Screen(), " · x · ") {
		t.Fatal("a tool whose flag is off has a row")
	}
	for _, v := range []string{"cookie.session", "env/prod"} {
		if strings.Contains(h.Screen(), v) {
			t.Fatalf("value names leaked onto the screen (%s)", v)
		}
	}

	// [지우기] on leet (no forget): every leet name of this account goes, after a confirm.
	selectRow(t, h, "시험 · ")
	h.Press("enter")
	if !strings.Contains(h.Screen(), "시험 로그인을 지울까요?") {
		t.Fatalf("no confirm:\n%s", h.Screen())
	}
	h.Press("y")
	left := slices.Sorted(maps.Keys(kos.vals))
	want := []string{
		ui.ToolAPI + "/" + host + "/" + fixtures.UserSam + "/env/prod/TOKEN",
		ui.ToolLeet + "/" + host + "/" + fixtures.UserHana + "/cookie.session",
	}
	if !slices.Equal(left, want) {
		t.Fatalf("left = %v, want %v", left, want)
	}
	if *forgets != 0 {
		t.Fatal("forget called for another tool")
	}

	// [지우기] on api (forget given): only forget runs.
	selectRow(t, h, "API · ")
	h.Press("enter", "y")
	if *forgets != 1 {
		t.Fatalf("forget calls = %d", *forgets)
	}
	if len(kos.vals) != 2 {
		t.Fatalf("the store was changed directly: %v", kos.vals)
	}
}

func TestToolLoginsHiddenWithoutRows(t *testing.T) {
	t.Cleanup(settings.ResetToolLoginsForTest())
	h, _ := newSettings(t, "")
	h.Resize(200, 60)
	if strings.Contains(h.Screen(), "도구 로그인") {
		t.Fatalf("an empty 도구 로그인 section is drawn:\n%s", h.Screen())
	}
	// describe ok=false: no row either.
	t.Setenv("MASIL_FEATURES", "leet")
	t.Cleanup(ui.SnapshotRegistryForTest())
	ui.RegisterTool(ui.ToolSpec{ID: ui.ToolLeet, Order: 1, Name: func(*i18n.T) string { return "시험" }})
	settings.RegisterToolLogin(ui.ToolLeet, func(*ui.Context) (string, bool) { return "", false }, nil)
	h2, _ := newSettings(t, "")
	h2.Resize(200, 60)
	if strings.Contains(h2.Screen(), "도구 로그인") {
		t.Fatalf("a section with no logged-in tool is drawn:\n%s", h2.Screen())
	}
}

func TestRegisterToolLoginRejects(t *testing.T) {
	t.Cleanup(settings.ResetToolLoginsForTest())
	settings.RegisterToolLogin(ui.ToolLeet, func(*ui.Context) (string, bool) { return "", false }, nil)
	for name, f := range map[string]func(){
		"duplicate": func() {
			settings.RegisterToolLogin(ui.ToolLeet, func(*ui.Context) (string, bool) { return "", false }, nil)
		},
		"empty id":    func() { settings.RegisterToolLogin("", func(*ui.Context) (string, bool) { return "", false }, nil) },
		"no describe": func() { settings.RegisterToolLogin(ui.ToolAPI, nil, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			f()
		}()
	}
}
