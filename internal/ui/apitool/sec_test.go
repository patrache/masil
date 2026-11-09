package apitool

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
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

// A shared request someone else turned the certificate check off for is
// not sent that way on their word: the first send asks (cancel sends
// nothing), and a run of the collection checks the certificate unless I
// allowed it. My own toggle counts as allowing it.
func TestPeerInsecureAsksBeforeSend(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(created))
	t.Cleanup(srv.Close)
	var hits atomic.Int32
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); created(w, r) })
	hub := collabtest.New(t)
	c := sharedCollection(srv.URL)
	c.Requests["r-get"].Settings.Insecure = true
	seedShared(hub, c)
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-get")
	tl.focus = paneRequest
	h.Press("enter")
	waitFor(t, h, "the certificate question", func() bool { return hasWhole(overlayText(h), ko.InsecureUnknown()) })
	settle(h)
	if hits.Load() != 0 {
		t.Fatalf("sent before the answer: %d", hits.Load())
	}
	// A run without my answer checks the certificate.
	pr, _ := tl.prepareRun(h.Context(), tl.colName, "", apitool.RunOptions{Iterations: 1})
	if pr == nil || pr.snap.Collection.Requests["r-get"].Settings.Insecure {
		t.Fatal("a run skips the certificate check on a peer's word")
	}
	h.Press("esc")
	settle(h)
	if hits.Load() != 0 {
		t.Fatalf("sent after cancel: %d", hits.Load())
	}
	h.Press("enter")
	waitFor(t, h, "the certificate question again", func() bool { return hasWhole(overlayText(h), ko.InsecureUnknown()) })
	settle(h)
	h.Press("enter") // 검사 없이 보내기
	waitFor(t, h, "the private-target question", func() bool { return hasWhole(overlayText(h), ko.PrivateTargetUnknown()) })
	settle(h)
	h.Press("enter")
	waitScreen(t, h, "201")
	if hits.Load() != 1 {
		t.Fatalf("target hits = %d", hits.Load())
	}
	if pr, _ := tl.prepareRun(h.Context(), tl.colName, "", apitool.RunOptions{Iterations: 1}); pr == nil || !pr.snap.Collection.Requests["r-get"].Settings.Insecure {
		t.Fatal("a run after my answer still checks the certificate")
	}
	// My own toggle on another request needs no question.
	tl2 := openShared(t, h, "r-pay")
	tl2.req.Settings.Insecure = true
	tl2.ed.load(tl2.req)
	h.Send(syncNowMsg{t: tl2})
	settle(h)
	if _, ask := tl2.peerInsecure(sharedDocID, tl2.req); ask {
		t.Fatal("my own certificate-check toggle asks me")
	}
}
