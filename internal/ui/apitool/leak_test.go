package apitool

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/collab/collabtest"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui/uitest"
)

// Acceptance 7 and 11 (C3.10, C3.12): end-to-end sweeps over every path a
// value can take. A failure here goes back to the Task that owns the path —
// these tests are never relaxed.

const canary = "canary-Q9x7Z2"

// filesWith lists the files under dirs whose bytes contain s.
func filesWith(t *testing.T, s string, dirs ...string) []string {
	t.Helper()
	var hit []string
	for _, d := range dirs {
		if d == "" {
			continue
		}
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil
			}
			b, err := os.ReadFile(p)
			if err == nil && strings.Contains(string(b), s) {
				hit = append(hit, p)
			}
			return nil
		})
	}
	return hit
}

// apiRecords is every request the masil test server got, as one text.
func apiRecords(h *uitest.Harness) string {
	var b strings.Builder
	for _, r := range h.API.Requests() {
		b.WriteString(r.Method + " " + r.Path + "\n")
		for k, vs := range r.Header {
			b.WriteString(k + ": " + strings.Join(vs, ",") + "\n")
		}
		b.Write(r.Body)
		b.WriteString("\n")
	}
	return b.String()
}

// hubOps is every op of every document in hub, as one text.
func hubOps(hub *collabtest.Hub, ids []string) string {
	var b strings.Builder
	for _, id := range ids {
		j, _ := json.Marshal(hub.Ops(id))
		b.Write(j)
	}
	return b.String()
}

func TestCanarySecretNeverLeaves(t *testing.T) {
	var mu sync.Mutex
	var auths []string
	srv, _ := hitServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Echo", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "pay_1", "echo": r.Header.Get("Authorization")})
	})
	hub := collabtest.New(t)
	store := secrettest.NewStore(t, true)
	h := openAPI(t, uitest.WithCollab(hub), uitest.WithSecrets(store))
	acceptMessages(h)
	env := &apitool.Env{Name: "dev", Vars: []apitool.Var{
		{Name: "base_url", Value: srv.URL},
		{Name: "token", Secret: true, AllowedHosts: []string{"127.0.0.1"}},
	}}
	req := &apitool.Request{ID: "pay", Name: "결제 만들기", Method: "POST", URL: "{{base_url}}/v1/payments?sig={{token}}",
		Headers: []apitool.KV{{ID: "h1", Key: "Authorization", Value: "Bearer {{token}}"}, {ID: "h2", Key: "X-Trace", Value: "t-{{token}}"}},
		Body:    apitool.Body{Kind: apitool.BodyJSON, Text: `{"amount":12000,"sig":"{{token}}"}`},
		Asserts: []apitool.Assertion{{ID: "a1", Target: "status", Op: "eq", Value: "201"}}}
	tl := seedRequests(h, env, req)
	putSecret(t, h, tl, "token", canary)

	// send (history too)
	h.Press("enter")
	waitScreen(t, h, "201 Created")

	// a result card with every option on
	tl.focus = paneResponse
	h.Press("s")
	n := len(shareChannels(h.Context()))
	for i := range 3 {
		for range n + i {
			h.Press("down")
		}
		h.Press("enter")
		for range n + i {
			h.Press("up")
		}
	}
	if o := tl.share.opts; !o.ShowHost || !o.IncludeHeaders || !o.IncludeBody {
		t.Fatalf("card options = %+v", o)
	}
	h.Press("enter")
	waitFor(t, h, "the result card sent", func() bool { return len(sentCards(h)) == 1 })

	// curl (template), curl (values), Postman export
	tl.focus = paneRequest
	h.Press("x", "enter")
	h.Press("x", "down", "enter")
	out := filepath.Join(t.TempDir(), "out.postman.json")
	h.Press("x", "down", "down", "enter", "ctrl+u")
	h.Type(out)
	h.Press("enter")
	waitWhole(t, h, ko.ExportSaved(out))
	h.Press("esc") // the saved note

	// the runner, and its card
	h.Press("r", "enter")
	waitRun(t, h, tl)
	h.Press("s", "enter")
	waitFor(t, h, "the run card sent", func() bool { return len(sentCards(h)) == 2 })
	h.Press("esc")

	// upload to the room, then send and edit in the shared copy
	tl.focus = paneTree
	h.Press("U", "enter")
	waitFor(t, h, "the shared collection", func() bool { return len(tl.docs) == 1 })
	var docs []string
	for id := range tl.docs {
		docs = append(docs, id)
	}
	sd := tl.docs[docs[0]]
	waitFor(t, h, "the shared copy loaded", func() bool { return sd.col != nil && len(sd.col.Requests) == 1 })
	var sharedReq string
	for id := range sd.col.Requests {
		sharedReq = id
	}
	tl.selectRequest(sharedKey(sd.id), sharedReq)
	tl.focus = paneRequest
	before := len(auths)
	h.Press("enter")
	waitFor(t, h, "the shared send", func() bool { mu.Lock(); defer mu.Unlock(); return len(auths) > before })
	editHeaderValue(h, tl, 1, "edited")
	waitFor(t, h, "the edit on the hub", func() bool { return strings.Contains(hubOps(hub, docs), "edited") })
	h.Advance(2e9) // the offline copy is written
	h.Flush()

	paths := h.Context().Paths
	if hit := filesWith(t, canary, paths.Dir, paths.Cache, paths.LogDir); len(hit) > 0 {
		t.Fatalf("the canary is in files: %v", hit)
	}
	if s := apiRecords(h); strings.Contains(s, canary) {
		t.Fatalf("the canary reached the masil server:\n%s", s)
	}
	if s := hubOps(hub, docs); strings.Contains(s, canary) {
		t.Fatalf("the canary is in the hub ops: %s", s)
	}
	if b, err := json.Marshal(hub.Snapshot(docs[0])); err != nil || strings.Contains(string(b), canary) {
		t.Fatalf("the canary is in the hub snapshot (%v)", err)
	}
	if n := len(h.Clipboards()); n < 2 {
		t.Fatalf("the curl exports copied %d times", n)
	}
	for _, c := range h.Clipboards() {
		if strings.Contains(c, canary) {
			t.Fatalf("the canary was copied: %s", c)
		}
	}
	if b, err := os.ReadFile(out); err != nil || strings.Contains(string(b), canary) {
		t.Fatalf("export: %v / %s", err, b)
	}
	mu.Lock()
	defer mu.Unlock()
	got := 0
	for _, a := range auths {
		if a == "Bearer "+canary {
			got++
		}
	}
	if got < 3 { // send, run, shared send
		t.Fatalf("the target got the token %d times: %q", got, auths)
	}
}

func TestMasilServerNeverSeesTargets(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv, _ := hitServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path+" "+r.Header.Get("X-Canary"))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("canary-body"))
	})
	hub := collabtest.New(t)
	h := openAPI(t, uitest.WithCollab(hub))
	req := &apitool.Request{ID: "c", Name: "canary", Method: "GET", URL: srv.URL + "/canary-path",
		Headers: []apitool.KV{{ID: "h1", Key: "X-Canary", Value: "canary-header"}}}
	tl := seedRequests(h, nil, req)
	h.Press("enter")
	waitScreen(t, h, "canary-body")
	h.Press("r", "enter")
	waitRun(t, h, tl)
	h.Press("esc")
	mu.Lock()
	if len(seen) < 2 || seen[0] != "/canary-path canary-header" {
		t.Fatalf("the target saw %q", seen)
	}
	mu.Unlock()
	rec := apiRecords(h)
	ops := hubOps(hub, []string{sharedDocID})
	for _, v := range []string{"canary-path", "canary-header", "canary-body"} {
		if strings.Contains(rec, v) {
			t.Fatalf("%s reached the masil server:\n%s", v, rec)
		}
		if strings.Contains(ops, v) {
			t.Fatalf("%s is in the hub ops: %s", v, ops)
		}
	}
}
