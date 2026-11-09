package apitool

import (
	"strings"
	"testing"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// hostile is a string with terminal escapes (title, clipboard, clear), a
// bell and a bidi override, as a shared or imported collection may carry.
const hostile = "\u202ex\x1b]0;PWN\a\x1b]52;c;ZXZpbA==\a\x1b[2J"

// noControls fails when s carries any of hostile's sequences (the screen's
// own styling is SGR: only those count).
func noControls(t *testing.T, where, s string) {
	t.Helper()
	for _, bad := range []string{"\x1b]", "\x1b[2J", "\u202e", "\a"} {
		if strings.Contains(s, bad) {
			t.Fatalf("%s carries %q:\n%q", where, bad, s)
		}
	}
}

// Every string a shared or imported collection holds is sanitized wherever
// the tool draws it: the URL line, each editor tab (the body kind label
// included), the tree and the tiny view.
func TestHostileCollectionSanitized(t *testing.T) {
	h := openAPI(t)
	r := &apitool.Request{
		ID: "h", Name: "n" + hostile, Method: "GET" + hostile, URL: "https://example.com/" + hostile,
		Query:   []apitool.KV{{ID: "q", Key: "k" + hostile, Value: "v" + hostile}},
		Headers: []apitool.KV{{ID: "hd", Key: "H" + hostile, Value: "v" + hostile}},
		Body: apitool.Body{Kind: apitool.BodyKind("kind" + hostile), Text: "b" + hostile, ContentType: "c" + hostile,
			Form: []apitool.FormField{{KV: apitool.KV{ID: "f", Key: "f" + hostile, Value: "v" + hostile}}}},
		Asserts:  []apitool.Assertion{{ID: "a", Target: "status", Op: "eq" + hostile, Value: "v" + hostile, Path: "p" + hostile}},
		Extracts: []apitool.Extract{{ID: "x", Var: "x" + hostile, From: "json", Path: "$." + hostile}},
	}
	r.Settings.Insecure = true
	tl := seedRequests(h, nil, r)
	ctx := h.Context()
	for tab := tabQuery; tab <= tabSettings; tab++ {
		tl.ed.tab = tab
		noControls(t, "editor tab "+string(rune('1'+tab)), strings.Join(tl.ed.view(ctx, tl, 90, 8, true), "\n"))
		noControls(t, "narrow editor tab "+string(rune('1'+tab)), strings.Join(tl.ed.view(ctx, tl, 20, 8, true), "\n"))
	}
	noControls(t, "the tree", strings.Join(tl.tree.view(ctx, tl, 40, 10, true), "\n"))
	noControls(t, "the tiny view", strings.Join(tl.viewTiny(ctx), "\n"))
	noControls(t, "the screen", h.Styled())
}

// The environments screen sanitizes variable names, values and the masked
// tail of a secret value (an extracted value is the server's text).
func TestHostileEnvScreenSanitized(t *testing.T) {
	store := secrettest.NewStore(t, true)
	h, tl := envSetup(t, uitest.WithSecrets(store))
	h.Press("esc")
	tl.env.Vars = append(tl.env.Vars, apitool.Var{Name: "v" + hostile, Value: "val" + hostile})
	putSecret(t, h, tl, "token", "tok-0123456789\x1b]0;PWN\a")
	h.Press("e")
	h.Send(tl.loadMasks(h.Context())())
	s := &envScreen{t: tl}
	s.Resize(h.Context(), ui.Size{W: 80, H: 20})
	noControls(t, "the environments screen", s.View(h.Context(), true))
}
