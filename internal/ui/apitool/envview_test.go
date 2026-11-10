package apitool

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/secretsui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// envSetup is the first screen (staging: base_url, owner, secret token)
// with the environment screen open.
func envSetup(t *testing.T, opts ...uitest.Option) (*uitest.Harness, *Tool) {
	t.Helper()
	h := openAPI(t, opts...)
	seedFirstScreen(h)
	tl := toolOf(h)
	h.Press("e")
	if !strings.Contains(h.Screen(), ko.EnvTitle("staging")) {
		t.Fatalf("e did not open the environments:\n%s", h.Screen())
	}
	return h, tl
}

// selectVar moves the selection to the variable row name.
func selectVar(t *testing.T, h *uitest.Harness, tl *Tool, name string) {
	t.Helper()
	for range 20 {
		if r, ok := tl.envSel(); ok && r.v.Name == name {
			return
		}
		h.Press("j")
	}
	t.Fatalf("no variable row %q", name)
}

func TestEnvShowsStorageLocation(t *testing.T) {
	store := secrettest.NewStore(t, false)
	h, tl := envSetup(t, uitest.WithSecrets(store))
	if !hasWhole(h.Screen(), ko.EnvStorageUnset()) {
		t.Fatalf("no storage line before any value:\n%s", h.Screen())
	}
	selectVar(t, h, tl, "token")
	h.Press("enter")
	h.Type("ghp_abcdefghijklmnop1234")
	h.Press("enter")
	if boxes := h.Model.Frame().Overlays; len(boxes) == 0 || boxes[len(boxes)-1].ID != secretsui.OverlayConsent {
		t.Fatalf("no consent question:\n%s", h.Screen())
	}
	h.Press("enter")           // the default: this run only
	h.Advance(5 * time.Second) // the toast goes
	memory := secretsui.BackendLabel(i18n.KoT(), secrets.BackendMemory)
	line := ko.EnvStorage(memory)
	for _, sz := range firstSizes {
		if sz == uitest.Tiny {
			continue
		}
		h.Resize(sz.W, sz.H)
		if s := overlayText(h); !hasWhole(s, line) {
			t.Fatalf("%dx%d: no storage line %q:\n%s", sz.W, sz.H, line, h.Screen())
		}
	}
}

func TestEnvSecretMaskedLast4(t *testing.T) {
	h := openAPI(t)
	seedFirstScreen(h)
	tl := toolOf(h)
	putSecret(t, h, tl, "token", "ghp_abcdefghijklmnop1234")
	h.Press("e")
	s := h.Screen()
	if !strings.Contains(s, "••••1234") || strings.Contains(s, "ghp_abcdef") {
		t.Fatalf("the secret is not masked to its last 4:\n%s", s)
	}
}

func TestSwitchEnvironment(t *testing.T) {
	h := openAPI(t)
	seedFirstScreen(h)
	tl := toolOf(h)
	if err := apitool.OpenLocal(h.Context().Paths).SaveEnv(&apitool.Env{Name: "prod", Vars: []apitool.Var{{Name: "base_url", Value: "https://api.github.com"}}}); err != nil {
		t.Fatal(err)
	}
	h.Press("e")
	for range 4 {
		if tl.env != nil && tl.env.Name == "prod" {
			break
		}
		h.Press("l")
	}
	if tl.env == nil || tl.env.Name != "prod" {
		t.Fatalf("env = %+v, want prod", tl.env)
	}
	if !strings.Contains(h.Screen(), ko.EnvTitle("prod")) {
		t.Fatalf("the env screen title did not follow:\n%s", h.Screen())
	}
	h.Press("esc")
	if !strings.Contains(h.Screen(), "깃허브 API · prod") {
		t.Fatalf("the tool title did not follow:\n%s", h.Screen())
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteTools}})
	if !strings.Contains(h.Screen(), "깃허브 API (로컬) · prod") {
		t.Fatalf("the hub status did not follow:\n%s", h.Screen())
	}
}

func TestSecretFallbackPrompt(t *testing.T) {
	store := secrettest.NewStore(t, false)
	h, tl := envSetup(t, uitest.WithSecrets(store))
	selectVar(t, h, tl, "token")
	h.Press("enter")
	h.Type("s3cret-value-0001")
	h.Press("enter", "enter") // the value, then the consent default
	key, _, _ := tl.scope(h.Context()).SecretSlot("token")
	v, err := tl.vault(h.Context()).Get(key)
	if err != nil || v.Reveal() != "s3cret-value-0001" {
		t.Fatalf("the value was not kept: %v", err)
	}
	if w := tl.vault(h.Context()).Where(key); w != secrets.BackendMemory {
		t.Fatalf("kept in %q, want this run only (no fallback file)", w)
	}
	if !strings.Contains(h.Screen(), "••••0001") {
		t.Fatalf("the masked value is not shown:\n%s", h.Screen())
	}
}

func TestValueSourceShown(t *testing.T) {
	h := openAPI(t)
	seedFirstScreen(h)
	tl := toolOf(h)
	tl.globals = []apitool.Var{{Name: "user_agent", Value: "masil"}}
	if err := tl.local.SaveGlobals(tl.globals); err != nil {
		t.Fatal(err)
	}
	h.Press("e")
	selectVar(t, h, tl, "base_url")
	if s := h.Screen(); !strings.Contains(s, ko.EnvSource(ko.SrcMine())) {
		t.Fatalf("base_url source:\n%s", s)
	}
	selectVar(t, h, tl, "user_agent")
	if s := h.Screen(); !strings.Contains(s, ko.EnvSource(ko.SrcGlobal())) {
		t.Fatalf("user_agent source:\n%s", s)
	}
}

func TestAllowedHostsEdit(t *testing.T) {
	h, tl := envSetup(t)
	selectVar(t, h, tl, "token")
	h.Press("H")
	if !strings.Contains(overlayText(h), "api.github.com") {
		t.Fatalf("the hosts field is not prefilled:\n%s", h.Screen())
	}
	h.Press("ctrl+u")
	h.Type("api.github.com, uploads.github.com")
	h.Press("enter")
	env, err := apitool.OpenLocal(h.Context().Paths).LoadEnv("staging")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(env.Vars, func(v apitool.Var) bool { return v.Name == "token" })
	if want := []string{"api.github.com", "uploads.github.com"}; !slices.Equal(env.Vars[i].AllowedHosts, want) {
		t.Fatalf("hosts = %v, want %v", env.Vars[i].AllowedHosts, want)
	}
	if !strings.Contains(h.Screen(), "uploads.github.com") {
		t.Fatalf("the hosts are not shown:\n%s", h.Screen())
	}
}

func TestToolLoginRow(t *testing.T) {
	store := secrettest.NewStore(t, true)
	h := openAPI(t, uitest.WithSecrets(store))
	seedFirstScreen(h)
	tl := toolOf(h)
	putSecret(t, h, tl, "token", "ghp_abcdefghijklmnop1234")
	if err := tl.vault(h.Context()).Put(apitool.SecretKey("", "", "other"), secrets.New("x-other-secret-value")); err != nil {
		t.Fatal(err)
	}
	h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteSettings, Anchor: "tools"}})
	h.Resize(200, 60)
	where := secretsui.BackendLabel(i18n.KoT(), secrets.BackendMemory)
	want := ko.ToolName() + " · " + ko.ToolLoginAccount(2) + " · " + where
	row := ""
	for _, l := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(l, ko.ToolLoginAccount(2)) {
			row = l
		}
	}
	if !strings.Contains(row, want) {
		t.Fatalf("tool login row = %q, want %q\n%s", row, want, h.Screen())
	}
	for range 80 {
		if strings.Contains(selectedRow(h), ko.ToolLoginAccount(2)) {
			break
		}
		h.Press("j")
	}
	h.Press("enter", "y")
	h.Flush()
	for k := range secrettest.Values(store) {
		if strings.HasPrefix(k, ui.ToolAPI+"/") {
			t.Fatalf("an api value is left: %s", k)
		}
	}
}

// selectedRow is the screen line with the settings selection marker.
func selectedRow(h *uitest.Harness) string {
	for _, l := range strings.Split(h.Screen(), "\n") {
		for _, part := range strings.Split(l, "│") {
			if strings.HasPrefix(strings.TrimLeft(part, " ║╔╚═|─╭╰"), "› ") {
				return l
			}
		}
	}
	return ""
}

func TestCookiesViewAndClear(t *testing.T) {
	srv, _ := hitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/"})
		w.WriteHeader(http.StatusNoContent)
	})
	h := openAPI(t)
	tl := seedRequests(h, nil, &apitool.Request{ID: "login", Method: "POST", URL: srv.URL + "/login"})
	h.Press("enter")
	waitScreen(t, h, "204")
	u, _ := url.Parse(srv.URL)
	if len(tl.sender.Jars.List(tl.scope(h.Context()).CollectionID, u)) != 1 {
		t.Fatalf("no cookie in the jar")
	}
	h.Press("e")
	if s := h.Screen(); !strings.Contains(s, ko.EnvCookies(1)) || !strings.Contains(s, "sid=abc123") {
		t.Fatalf("the cookies are not shown:\n%s", s)
	}
	h.Press("C")
	if n := len(tl.sender.Jars.List(tl.scope(h.Context()).CollectionID, u)); n != 0 {
		t.Fatalf("cookies after C = %d", n)
	}
	if strings.Contains(h.Screen(), "sid=abc123") {
		t.Fatalf("the cleared cookie is still shown:\n%s", h.Screen())
	}
	for _, dir := range []string{h.Context().Paths.Dir, h.Context().Paths.Cache} {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.Contains(strings.ToLower(d.Name()), "cookie") {
				t.Errorf("a cookie file on disk: %s", p)
			}
			return nil
		})
	}
}
