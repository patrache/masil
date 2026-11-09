package apitool

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/collab/collabtest"
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

// Replacing a saved file (after the overwrite question) replaces the path
// itself: a symlink there is not followed to the file it points at.
func TestSaveOverwriteDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "rc")
	if err := os.WriteFile(target, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "body.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := writePrivate(link, []byte("server"), false); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a new save onto a symlink = %v, want exists", err)
	}
	if err := writePrivate(link, []byte("server"), true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "mine" {
		t.Fatalf("the symlink's target was written: %q", b)
	}
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&fs.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		t.Fatalf("the saved path is not a plain file: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(link); string(b) != "server" {
		t.Fatalf("saved = %q", b)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
}

// "Share anyway" on the held question shares only the field it shows; a
// second held field is asked about next and stays here until answered.
func TestHeldShareAnywayOnlyShown(t *testing.T) {
	const key2 = "q8Zr3Kp9Lm2Xw7Tb5Nc4Vh6Jd1Fs0Gy"
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-get")
	tl.req.Headers = []apitool.KV{{Key: "Authorization", Value: "Bearer " + testJWT}}
	tl.req.Query = []apitool.KV{{Key: "api_key", Value: key2}}
	tl.ed.load(tl.req)
	h.Send(syncNowMsg{t: tl})
	waitFor(t, h, "the held question", func() bool { return hasWhole(overlayText(h), ko.SecretDetected()) })
	settle(h)
	h.Press("down", "enter") // 그래도 올리기
	waitFor(t, h, "one secret on the hub", func() bool {
		return strings.Contains(opsText(hub), testJWT) || strings.Contains(opsText(hub), key2)
	})
	waitFor(t, h, "the question about the other field", func() bool { return hasWhole(overlayText(h), ko.SecretDetected()) })
	settle(h)
	if strings.Contains(opsText(hub), testJWT) && strings.Contains(opsText(hub), key2) {
		t.Fatalf("share anyway sent a field it never showed: %s", opsText(hub))
	}
	h.Press("down", "enter")
	waitFor(t, h, "both secrets on the hub", func() bool {
		return strings.Contains(opsText(hub), testJWT) && strings.Contains(opsText(hub), key2)
	})
}
