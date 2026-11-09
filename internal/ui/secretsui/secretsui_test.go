package secretsui_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/secretsui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

// probeKind hosts the probe: a tool stand-in that records the secret dialogs'
// broadcasts and retries its Put on ConsentMsg (as a tool does).
const probeKind = ui.RouteTrends

type probe struct {
	consents []secretsui.ConsentMsg
	prompts  []secretsui.PromptMsg
	retryErr error
	retried  bool
	size     ui.Size
}

var current *probe

func (p *probe) Init(*ui.Context) tea.Cmd { return nil }
func (p *probe) Update(ctx *ui.Context, msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case secretsui.ConsentMsg:
		p.consents = append(p.consents, m)
		if m.Choice != secrets.FallbackUnset {
			_, p.retryErr = ctx.Secrets.Put(context.Background(), probeKey, secrets.New("retried"))
			p.retried = true
		}
	case secretsui.PromptMsg:
		p.prompts = append(p.prompts, m)
	}
	return nil
}
func (p *probe) Resize(_ *ui.Context, s ui.Size) { p.size = s }
func (p *probe) View(*ui.Context, bool) string {
	lines := make([]string, max(p.size.H, 0))
	for i := range lines {
		lines[i] = strings.Repeat(" ", max(p.size.W, 0))
	}
	return strings.Join(lines, "\n")
}

var probeKey = secrets.Key{Tool: "leet", Server: "masil.example", UserID: "U1", Name: "cookie"}

func TestMain(m *testing.M) {
	stub.Install()
	ui.RegisterRoute(probeKind, func(*ui.Context, ui.Route) ui.Component {
		current = &probe{}
		return current
	})
	os.Exit(m.Run())
}

func newHarness(t *testing.T, opts ...uitest.Option) *uitest.Harness {
	t.Helper()
	current = nil
	h := uitest.New(t, append([]uitest.Option{uitest.WithRoute(ui.Route{Kind: probeKind})}, opts...)...)
	if current == nil {
		t.Fatal("probe not built")
	}
	return h
}

// overlayText is the top overlay's inner area, rows joined by "\n".
func overlayText(h *uitest.Harness) string {
	boxes := h.Model.Frame().Overlays
	if len(boxes) == 0 {
		return ""
	}
	b := boxes[len(boxes)-1]
	r := b.Rect
	if b.Bordered {
		r = r.Inner()
	}
	rows := strings.Split(h.Screen(), "\n")
	var out []string
	for y := r.Y; y < r.Y+r.H && y < len(rows); y++ {
		out = append(out, ansi.Cut(rows[y], r.X, r.X+r.W))
	}
	return strings.Join(out, "\n")
}

func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func openConsent(h *uitest.Harness) {
	h.Send(ui.OpenOverlayMsg{ID: secretsui.OverlayConsent, Args: "leet"})
}

func TestConsentDefaultSession(t *testing.T) {
	h := newHarness(t, uitest.WithSecrets(secrettest.NewStore(t, false)))
	openConsent(h)
	if len(h.Model.Frame().Overlays) != 1 {
		t.Fatalf("consent overlay not open:\n%s", h.Screen())
	}
	h.Press("enter")
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("enter must close the dialog")
	}
	if len(current.consents) != 1 || current.consents[0] != (secretsui.ConsentMsg{Tool: "leet", Choice: secrets.FallbackSession}) {
		t.Fatalf("consents = %+v", current.consents)
	}
}

func TestConsentChoices(t *testing.T) {
	h := newHarness(t, uitest.WithSecrets(secrettest.NewStore(t, false)))
	openConsent(h)
	h.Press("1", "enter")
	if len(current.consents) != 1 || current.consents[0].Choice != secrets.FallbackFile {
		t.Fatalf("1 enter: consents = %+v", current.consents)
	}
	if h.Context().Secrets.Fallback() != secrets.FallbackFile {
		t.Fatal("fallback not set")
	}
	openConsent(h)
	h.Press("up", "down", "enter") // ↑ = ①, ↓ = ②
	if len(current.consents) != 2 || current.consents[1].Choice != secrets.FallbackSession {
		t.Fatalf("consents = %+v", current.consents)
	}
	openConsent(h)
	h.Press("esc")
	if len(current.consents) != 3 || current.consents[2] != (secretsui.ConsentMsg{Tool: "leet", Choice: secrets.FallbackUnset}) {
		t.Fatalf("esc: consents = %+v", current.consents)
	}
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("esc must close the dialog")
	}
}

func TestConsentSetsFallbackThenBroadcasts(t *testing.T) {
	st := secrettest.NewStore(t, false)
	h := newHarness(t, uitest.WithSecrets(st))
	if _, err := st.Put(context.Background(), probeKey, secrets.New("v")); err != secrets.ErrNeedConsent {
		t.Fatalf("Put = %v, want ErrNeedConsent", err)
	}
	openConsent(h)
	h.Press("enter")
	if !current.retried || current.retryErr != nil {
		t.Fatalf("the tool's retried Put on ConsentMsg: retried=%v err=%v", current.retried, current.retryErr)
	}
	if st.Fallback() != secrets.FallbackSession {
		t.Fatalf("Fallback = %v", st.Fallback())
	}
	if v := secrettest.Values(st)[probeKey.Account()]; v != "retried" {
		t.Fatalf("stored %q", v)
	}
}

func TestConsentNeverTruncated(t *testing.T) {
	sizes := append(append([]uitest.Size{}, uitest.Required...), uitest.Size{W: 40, H: 14})
	for _, sz := range sizes {
		h := newHarness(t, uitest.WithSecrets(secrettest.NewStore(t, false)))
		h.Resize(sz.W, sz.H)
		openConsent(h)
		h.Check()
		q := squash(h.Context().T.Tools.ConsentQuestion())
		if got := squash(overlayText(h)); !strings.Contains(got, q) {
			t.Errorf("%dx%d: the consent question is not shown in full:\n%s", sz.W, sz.H, overlayText(h))
		}
		if !strings.Contains(overlayText(h), "›") {
			t.Errorf("%dx%d: no selected choice visible:\n%s", sz.W, sz.H, overlayText(h))
		}
	}
}

func promptArgs(t *testing.T) string {
	b, err := json.Marshal(map[string]string{"tool": "leet", "name": "cookie.session", "label": "LEETCODE_SESSION"})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPromptMasksValue(t *testing.T) {
	for _, sz := range []uitest.Size{{W: 120, H: 40}, {W: 40, H: 14}} {
		h := newHarness(t)
		h.Resize(sz.W, sz.H)
		h.Send(ui.OpenOverlayMsg{ID: secretsui.OverlayPrompt, Args: promptArgs(t)})
		if !strings.Contains(overlayText(h), "LEETCODE_SESSION") {
			t.Fatalf("%v: label missing:\n%s", sz, overlayText(h))
		}
		h.Type("abcd")
		h.Paste("efgh")
		h.Check()
		if s := h.Screen(); strings.Contains(s, "abcd") || strings.Contains(s, "efgh") {
			t.Fatalf("%v: the value is visible:\n%s", sz, s)
		}
		if !strings.Contains(overlayText(h), "••••••••") {
			t.Fatalf("%v: no mask:\n%s", sz, overlayText(h))
		}
		if h.Cursor() == nil {
			t.Errorf("%v: no cursor in the field", sz)
		}
		h.Press("backspace")
		h.Type("h")
		h.Press("enter")
		if len(current.prompts) != 1 {
			t.Fatalf("%v: prompts = %d", sz, len(current.prompts))
		}
		p := current.prompts[0]
		if p.Tool != "leet" || p.Name != "cookie.session" || p.Canceled || p.Value.Reveal() != "abcdefgh" {
			t.Fatalf("%v: PromptMsg = %+v %q", sz, p, p.Value.Reveal())
		}
		if len(h.Model.Frame().Overlays) != 0 {
			t.Fatal("enter must close the prompt")
		}
	}
}

func TestPromptCancel(t *testing.T) {
	h := newHarness(t)
	h.Send(ui.OpenOverlayMsg{ID: secretsui.OverlayPrompt, Args: promptArgs(t)})
	h.Type("secret")
	h.Press("esc")
	if len(current.prompts) != 1 || !current.prompts[0].Canceled || !current.prompts[0].Value.IsZero() {
		t.Fatalf("prompts = %+v", current.prompts)
	}
	// Broken args open nothing.
	h.Send(ui.OpenOverlayMsg{ID: secretsui.OverlayPrompt, Args: "{"})
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("broken args opened a prompt")
	}
}

func TestBackendLabel(t *testing.T) {
	ko := i18n.For(i18n.Ko)
	for b, want := range map[secrets.Backend]string{
		secrets.BackendKeychain:      "키체인",
		secrets.BackendCredMan:       "자격 증명 관리자",
		secrets.BackendSecretService: "Secret Service",
		secrets.BackendFile:          "일반 파일(권한 600) — 이 PC 에 접근할 수 있는 사람은 볼 수 있어요",
		secrets.BackendMemory:        "이번 실행만",
		"":                           "",
	} {
		if got := secretsui.BackendLabel(ko, b); got != want {
			t.Errorf("BackendLabel(%q) = %q, want %q", b, got, want)
		}
	}
	if secretsui.BackendLabel(nil, secrets.BackendKeychain) != "키체인" {
		t.Error("nil T must read ko")
	}
}
