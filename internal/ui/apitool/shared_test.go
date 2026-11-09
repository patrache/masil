package apitool

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/collab/collabtest"
	"github.com/patrache/masil/internal/secrets/secrettest"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

const sharedDocID = "01JD00000000000000000APIC1"

// sharedCollection is the shared fixture: environment staging (base_url =
// target, secret token), "Create payment" with two headers and "Get".
func sharedCollection(target string) *apitool.Collection {
	pay := &apitool.Request{ID: "r-pay", Name: "Create payment", Method: "POST", URL: "{{base_url}}/v1/payments",
		Headers: []apitool.KV{{ID: "h1", Key: "Authorization", Value: "Bearer {{token}}"}, {ID: "h2", Key: "X-Trace", Value: "a"}},
		Body:    apitool.Body{Kind: apitool.BodyJSON, Text: `{"amount": 1}`}}
	get := &apitool.Request{ID: "r-get", Name: "Get", Method: "GET", URL: "{{base_url}}/v1/payments/1"}
	return &apitool.Collection{Version: apitool.CollectionVersion, Name: "Payments team",
		Root:     apitool.Folder{Items: []apitool.Node{{Request: "r-pay"}, {Request: "r-get"}}},
		Requests: map[string]*apitool.Request{"r-pay": pay, "r-get": get},
		Envs: []apitool.Env{{ID: "e-stg", Name: "staging", Vars: []apitool.Var{
			{ID: "v1", Name: "base_url", Value: target}, {ID: "v2", Name: "token", Secret: true},
		}}},
	}
}

// seedShared puts the collection into the hub as document sharedDocID of
// the fixture room.
func seedShared(hub *collabtest.Hub, c *apitool.Collection) {
	hub.SeedDoc(api.Doc{ID: sharedDocID, SpaceID: fixtures.SpaceNova, Type: apitool.DocType, Title: c.Name}, apitool.DocFromCollection(c))
}

// docRoute is the shared collection's instance route (a card's enter).
func docRoute(req string) ui.Route {
	return ui.Route{Kind: ui.RouteTool, ID: ui.ToolAPI + ":doc:" + sharedDocID, Arg: ui.ToolAPI,
		Query: map[string]string{queryDoc: sharedDocID, queryReq: req}}
}

// waitFor delivers posts and advances the clock until cond holds.
func waitFor(t testing.TB, h *uitest.Harness, what string, cond func() bool) {
	t.Helper()
	for range 600 {
		h.Flush()
		if cond() {
			return
		}
		h.Advance(50 * time.Millisecond)
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("never: %s\n%s", what, h.Screen())
}

// settle lets the harness run a few clock steps (an overlay opened by a
// command is ready for keys).
func settle(h *uitest.Harness) {
	for range 10 {
		h.Flush()
		h.Advance(50 * time.Millisecond)
		time.Sleep(3 * time.Millisecond)
	}
}

// openShared opens the shared collection on request req in h and waits
// until it is loaded.
func openShared(t testing.TB, h *uitest.Harness, req string) *Tool {
	t.Helper()
	r := docRoute(req)
	h.Send(ui.NavigateMsg{Route: r})
	tl, _ := h.Instance(r.Key()).(*Tool)
	if tl == nil {
		t.Fatalf("the shared instance did not open:\n%s", h.Screen())
	}
	waitFor(t, h, "shared request "+req+" open", func() bool {
		return tl.req != nil && tl.req.ID == req && tl.colName == sharedKey(sharedDocID)
	})
	return tl
}

// hanaCreds logs a harness in as hana.
func hanaCreds() uitest.Option {
	return uitest.WithCredentials(&auth.Credentials{Version: 1, Server: "", Token: "msl_hana", UserID: fixtures.UserHana, Handle: "hana"})
}

func opsText(hub *collabtest.Hub) string {
	b, _ := json.Marshal(hub.Ops(sharedDocID))
	return string(b)
}

// editHeaderValue edits the value of header row i of the shown request.
func editHeaderValue(h *uitest.Harness, tl *Tool, i int, value string) {
	tl.focus = paneRequest
	h.Press("2", "j")
	for range i {
		h.Press("j")
	}
	h.Press("right", "enter", "ctrl+u")
	h.Type(value)
	h.Press("enter")
}

func TestUploadToSpaceFlow(t *testing.T) {
	hub := collabtest.New(t)
	h := openAPI(t, uitest.WithCollab(hub))
	srv, _ := hitServer(t, created)
	seedRequests(h, nil, paymentRequest(srv))
	h.Press("U")
	waitWhole(t, h, ko.UploadPlaintextNote())
	if !strings.Contains(overlayText(h), "노바 개발방") {
		t.Fatalf("no room to pick:\n%s", overlayText(h))
	}
	h.Press("enter")
	waitFor(t, h, "the shared row in the tree", func() bool { return strings.Contains(h.Screen(), ko.TreeShared("시험")) })
	n := 0
	for _, r := range h.API.Requests() {
		if r.Method == http.MethodPost && r.Path == "/v1/spaces/"+fixtures.SpaceNova+"/docs" {
			n++
			var body struct{ Type string }
			_ = json.Unmarshal(r.Body, &body)
			if body.Type != apitool.DocType {
				t.Fatalf("doc type = %q", body.Type)
			}
		}
	}
	if n != 1 {
		t.Fatalf("CreateDoc calls = %d", n)
	}
}

// createDocBody is the snapshot JSON of the CreateDoc calls h made.
func createDocBodies(h *uitest.Harness) []string {
	var out []string
	for _, r := range h.API.Requests() {
		if r.Method == http.MethodPost && r.Path == "/v1/spaces/"+fixtures.SpaceNova+"/docs" {
			out = append(out, string(r.Body))
		}
	}
	return out
}

// Upload (flow ③) asks about every held field with the edit path's
// choices: 변수로 바꾸기 turns that secret into a secret collection
// variable (its value in my vault for the new document), 그래도 올리기
// shares that field; nothing held goes up unasked.
func TestUploadHeldEachFieldAsked(t *testing.T) {
	const key2 = "q8Zr3Kp9Lm2Xw7Tb5Nc4Vh6Jd1Fs0Gy"
	hub := collabtest.New(t)
	store := secrettest.NewStore(t, true)
	h := openAPI(t, uitest.WithCollab(hub), uitest.WithSecrets(store))
	a := &apitool.Request{ID: "ra", Name: "A", Method: "GET", URL: "https://api.example.com/a",
		Headers: []apitool.KV{{ID: "h1", Key: "Authorization", Value: "Bearer " + testJWT}}}
	b := &apitool.Request{ID: "rb", Name: "B", Method: "GET", URL: "https://api.example.com/b",
		Query: []apitool.KV{{ID: "q1", Key: "api_key", Value: key2}}}
	tl := seedRequests(h, nil, a, b)
	h.Press("U")
	waitWhole(t, h, ko.UploadPlaintextNote())
	h.Press("enter")
	waitFor(t, h, "the first held question", func() bool {
		o := overlayText(h)
		return hasWhole(o, ko.SecretDetected()) && strings.Contains(o, ko.SecretToVar()) && strings.Contains(o, ko.UploadAnyway())
	})
	settle(h)
	first := overlayText(h)
	h.Press("ctrl+u")
	h.Type("tok1")
	h.Press("enter") // 변수로 바꾸기
	waitFor(t, h, "the second held question", func() bool {
		o := overlayText(h)
		return hasWhole(o, ko.SecretDetected()) && o != first
	})
	if n := len(createDocBodies(h)); n != 0 {
		t.Fatalf("uploaded before every held field was answered (%d)", n)
	}
	settle(h)
	h.Press("down", "enter") // 그래도 올리기
	waitFor(t, h, "the upload", func() bool { return len(createDocBodies(h)) == 1 })
	body := createDocBodies(h)[0]
	conv, kept := testJWT, key2
	if !strings.Contains(body, "Bearer {{tok1}}") {
		conv, kept = key2, testJWT
	}
	if strings.Contains(body, conv) || !strings.Contains(body, "{{tok1}}") || !strings.Contains(body, kept) {
		t.Fatalf("snapshot (converted %q, kept %q):\n%s", conv[:8], kept[:8], body)
	}
	if !strings.Contains(body, `\"name\":\"tok1\",\"secret\":true`) && !strings.Contains(body, `"name":"tok1","secret":true`) {
		t.Fatalf("no secret variable tok1 in the snapshot:\n%s", body)
	}
	waitFor(t, h, "the shared copy open", func() bool { return len(tl.docs) == 1 })
	var docID string
	for id := range tl.docs {
		docID = id
	}
	waitFor(t, h, "the converted value in my vault", func() bool {
		s, err := tl.vault(h.Context()).Get(apitool.SecretKey(docID, "col:"+docID, "tok1"))
		return err == nil && s.Reveal() == conv
	})
	if m, _ := apitool.OpenLocal(h.Context().Paths).LoadMine(docID); !m.IsSecret("col:" + docID + "/tok1") {
		t.Fatalf("tok1 is not marked mine: %+v", m)
	}
}

func TestUploadHeldCancelUploadsNothing(t *testing.T) {
	hub := collabtest.New(t)
	h := openAPI(t, uitest.WithCollab(hub))
	a := &apitool.Request{ID: "ra", Name: "A", Method: "GET", URL: "https://api.example.com/a",
		Headers: []apitool.KV{{ID: "h1", Key: "Authorization", Value: "Bearer " + testJWT}}}
	seedRequests(h, nil, a)
	h.Press("U")
	waitWhole(t, h, ko.UploadPlaintextNote())
	h.Press("enter")
	waitFor(t, h, "the held question", func() bool { return hasWhole(overlayText(h), ko.SecretDetected()) })
	settle(h)
	h.Press("down", "down", "enter") // 취소
	settle(h)
	if n := len(createDocBodies(h)); n != 0 {
		t.Fatalf("CreateDoc calls = %d", n)
	}
}

func TestPeerSeesNoValueForSecret(t *testing.T) {
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	h := openAPI(t, uitest.WithCollab(hub), hanaCreds())
	tl := openShared(t, h, "r-pay")
	h.Press("e")
	waitFor(t, h, "the env screen", func() bool { return strings.Contains(h.Screen(), ko.NoValue()) })
	line := ""
	for _, l := range strings.Split(h.Screen(), "\n") {
		if strings.Contains(l, "token") && strings.Contains(l, ko.NoValue()) {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no 'token … %s' row:\n%s", ko.NoValue(), h.Screen())
	}
	if tl.env == nil || tl.env.Name != "staging" {
		t.Fatalf("the shared environment is not selected: %+v", tl.env)
	}
}

func TestConcurrentHeaderEditsBothKept(t *testing.T) {
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	a := openAPI(t, uitest.WithCollab(hub))
	ta := openShared(t, a, "r-pay")
	b := openAPI(t, uitest.WithCollab(hub), hanaCreds())
	tb := openShared(t, b, "r-pay")
	editHeaderValue(a, ta, 0, "Bearer {{token2}}")
	editHeaderValue(b, tb, 1, "b")
	both := func(tl *Tool) func() bool {
		return func() bool {
			r := tl.req
			return r != nil && len(r.Headers) == 2 && r.Headers[0].Value == "Bearer {{token2}}" && r.Headers[1].Value == "b"
		}
	}
	waitFor(t, a, "both edits on a", both(ta))
	waitFor(t, b, "both edits on b", both(tb))
	got, err := apitool.CollectionFromDoc(hub.Snapshot(sharedDocID))
	if err != nil {
		t.Fatal(err)
	}
	hs := got.Requests["r-pay"].Headers
	if hs[0].Value != "Bearer {{token2}}" || hs[1].Value != "b" {
		t.Fatalf("hub headers = %+v", hs)
	}
}

func TestConcurrentBodyEditRecovery(t *testing.T) {
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	a := openAPI(t, uitest.WithCollab(hub))
	ta := openShared(t, a, "r-pay")
	b := openAPI(t, uitest.WithCollab(hub), hanaCreds())
	tb := openShared(t, b, "r-pay")

	ta.req.Body.Text = `{"amount": 2}`
	ta.ed.load(ta.req)
	a.Send(syncNowMsg{t: ta})
	waitFor(t, a, "a's body on the hub", func() bool { return strings.Contains(opsText(hub), `amount\": 2`) })
	waitFor(t, b, "b sees a's body", func() bool { return tb.req.Body.Text == `{"amount": 2}` })
	tb.req.Body.Text = `{"amount": 3}`
	tb.ed.load(tb.req)
	b.Send(syncNowMsg{t: tb})

	waitFor(t, a, "the restore question on a", func() bool { return hasWhole(overlayText(a), ko.BodyOverwritten("하나")) })
	if ta.req.Body.Text != `{"amount": 3}` || tb.req.Body.Text != `{"amount": 3}` {
		t.Fatalf("final bodies differ: a=%s b=%s", ta.req.Body.Text, tb.req.Body.Text)
	}
	lost, err := apitool.OpenLocal(a.Context().Paths).LoadLost(sharedDocID)
	if err != nil || len(lost) != 1 || lost[0].Text != `{"amount": 2}` {
		t.Fatalf(".lost.json = %+v, %v", lost, err)
	}
	a.Press("enter") // 내 내용 복구
	waitFor(t, b, "my body restored as a new edit", func() bool { return tb.req.Body.Text == `{"amount": 2}` })
	if ta.req.Body.Text != `{"amount": 2}` {
		t.Fatalf("a's body = %s", ta.req.Body.Text)
	}
}

func TestHeldFieldAsksBeforeSync(t *testing.T) {
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-pay")
	editHeaderValue(h, tl, 1, "Bearer "+testJWT)
	waitFor(t, h, "the held question", func() bool {
		o := overlayText(h)
		return hasWhole(o, ko.SecretDetected()) && strings.Contains(o, ko.UploadAnyway()) && strings.Contains(o, ko.SecretToVar())
	})
	for range 10 { // the value never leaves before the answer
		h.Flush()
		h.Advance(50 * time.Millisecond)
		time.Sleep(3 * time.Millisecond)
	}
	if strings.Contains(opsText(hub), testJWT) {
		t.Fatalf("the detected value reached the hub before the answer: %s", opsText(hub))
	}
	h.Press("down", "enter") // 그래도 올리기
	waitFor(t, h, "the confirmed value on the hub", func() bool { return strings.Contains(opsText(hub), testJWT) })
}

// A literal typed into an existing header row's value (a set with no name
// in the op) is held under the row's name.
func TestHeldSetOfExistingRowValue(t *testing.T) {
	const key = "q8Zr3Kp9Lm2Xw7Tb5Nc4Vh6Jd1Fs0Gy"
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-pay")
	editHeaderValue(h, tl, 0, key) // Authorization
	waitFor(t, h, "the held question", func() bool { return hasWhole(overlayText(h), ko.SecretDetected()) })
	for range 10 {
		h.Flush()
		h.Advance(50 * time.Millisecond)
		time.Sleep(3 * time.Millisecond)
	}
	if strings.Contains(opsText(hub), key) {
		t.Fatalf("the literal reached the hub: %s", opsText(hub))
	}
}

// Two different secrets held in one edit: 변수로 바꾸기 turns only the
// first into {{name}} (its value in the vault) and asks again about the
// other, which never takes the first one's name.
func TestHeldToVarDistinctSecrets(t *testing.T) {
	const key2 = "q8Zr3Kp9Lm2Xw7Tb5Nc4Vh6Jd1Fs0Gy"
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	store := secrettest.NewStore(t, true)
	h := openAPI(t, uitest.WithCollab(hub), uitest.WithSecrets(store))
	tl := openShared(t, h, "r-get")
	tl.req.Headers = []apitool.KV{{Key: "Authorization", Value: "Bearer " + testJWT}}
	tl.req.Query = []apitool.KV{{Key: "api_key", Value: key2}}
	tl.ed.load(tl.req)
	h.Send(syncNowMsg{t: tl})
	waitFor(t, h, "the held question", func() bool { return hasWhole(overlayText(h), ko.SecretDetected()) })
	settle(h)
	h.Press("ctrl+u")
	h.Type("token")
	h.Press("enter") // 변수로 바꾸기
	// The detector's first held field (either one) becomes {{token}}.
	waitFor(t, h, "one field as {{token}} on the hub", func() bool { return strings.Contains(opsText(hub), "{{token}}\"") })
	waitFor(t, h, "the question about the other secret", func() bool { return hasWhole(overlayText(h), ko.SecretDetected()) })
	if strings.Contains(opsText(hub), key2) || strings.Contains(opsText(hub), testJWT) {
		t.Fatalf("a held value reached the hub: %s", opsText(hub))
	}
	r := tl.docs[sharedDocID].col.Requests["r-get"]
	first, other := testJWT, "Bearer {{token}}"
	if len(r.Query) == 1 && r.Query[0].Value == "{{token}}" {
		first, other = key2, ""
	} else if len(r.Headers) != 1 || r.Headers[0].Value != "Bearer {{token}}" {
		t.Fatalf("neither field became {{token}}: %+v %+v", r.Headers, r.Query)
	}
	if other != "" && len(r.Query) == 1 && r.Query[0].Value == "{{token}}" {
		t.Fatalf("both fields took the first secret's name: %+v %+v", r.Headers, r.Query)
	}
	if other == "" && strings.Contains(opsText(hub), "Bearer {{token}}") {
		t.Fatalf("both fields took the first secret's name: %s", opsText(hub))
	}
	waitFor(t, h, "the first secret in the vault", func() bool {
		s, err := tl.vault(h.Context()).Get(apitool.SecretKey(sharedDocID, "col:"+sharedDocID, "token"))
		return err == nil && s.Reveal() == first
	})
}

func TestOfflineSendWorks(t *testing.T) {
	srv, hits := hitServer(t, created)
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection(srv.URL))
	store := secrettest.NewStore(t, true)
	h := openAPI(t, uitest.WithCollab(hub), uitest.WithSecrets(store))
	ls := apitool.OpenLocal(h.Context().Paths)
	mine := &apitool.Mine{}
	mine.MarkSecret("e-stg/token", []string{"127.0.0.1"})
	if err := ls.SaveMine(sharedDocID, mine); err != nil {
		t.Fatal(err)
	}
	tl := openShared(t, h, "r-pay")
	putSecret(t, h, tl, "token", "canary-Q9x7Z2")
	h.Advance(2 * time.Second) // the offline copy is written
	h.Flush()
	hub.SetOnline(fixtures.UserSam, false)
	tl.focus = paneRequest
	h.Press("enter")
	// 127.0.0.1 from a document whose author I cannot know asks once
	waitFor(t, h, "the private-target question", func() bool { return hasWhole(overlayText(h), ko.PrivateTargetUnknown()) })
	h.Press("enter") // 그래도 보내기
	waitScreen(t, h, "201")
	if hits.Load() != 1 {
		t.Fatalf("target hits = %d", hits.Load())
	}
	b, err := os.ReadFile(filepath.Join(ls.Dir, "shared", sharedDocID+".json"))
	if err != nil {
		t.Fatalf("no offline copy: %v", err)
	}
	if strings.Contains(string(b), "canary") || !strings.Contains(string(b), "Payments team") {
		t.Fatalf("offline copy:\n%s", b)
	}
}

func TestLastChangedShown(t *testing.T) {
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-pay")
	hub.Inject(sharedDocID, fixtures.UserHana, []collab.Op{{Op: collab.OpSet, ID: "req:r-pay", Field: "url", Value: json.RawMessage(`"{{base_url}}/v2/payments"`)}})
	waitFor(t, h, "the last-changed line", func() bool { return strings.Contains(h.Screen(), ko.LastChanged("하나", "방금")) })
	if tl.req.URL != "{{base_url}}/v2/payments" {
		t.Fatalf("url = %s", tl.req.URL)
	}
	if fi := tl.foreign["r-pay"]; fi.By != "하나" {
		t.Fatalf("foreign = %+v", fi)
	}
}

// A peer changing a variable the URLs read (base_url) makes every request
// of the document theirs for the private-target check (C3.10).
func TestPeerVarChangeMarksRequestsForeign(t *testing.T) {
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-pay")
	hub.Inject(sharedDocID, fixtures.UserHana, []collab.Op{{Op: collab.OpSet, ID: "envvar:v1", Field: "value", Value: json.RawMessage(`"http://169.254.169.254"`)}})
	waitFor(t, h, "both requests hana's", func() bool {
		return tl.foreign["r-pay"].By == "하나" && tl.foreign["r-get"].By == "하나"
	})
}

// A request whose last author I cannot know (the document's first
// snapshot, the offline copy, a document opened from a card) is not mine:
// a private target asks once before it is sent.
func TestUnknownAuthorAsksForPrivateTarget(t *testing.T) {
	srv, hits := hitServer(t, created)
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection(srv.URL))
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-get")
	tl.focus = paneRequest
	h.Press("enter")
	waitFor(t, h, "the private-target question", func() bool { return hasWhole(overlayText(h), ko.PrivateTargetUnknown()) })
	if hits.Load() != 0 {
		t.Fatalf("sent before the answer: %d", hits.Load())
	}
	h.Press("enter") // 그래도 보내기
	waitScreen(t, h, "201")
	if hits.Load() != 1 {
		t.Fatalf("target hits = %d", hits.Load())
	}
}

func TestOpenSharedFromRoute(t *testing.T) {
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-get")
	if tl.req.Name != "Get" || !strings.Contains(h.Screen(), ko.TreeShared("Payments team")) {
		t.Fatalf("screen:\n%s", h.Screen())
	}
	if tl.route.ID != ui.ToolAPI+":doc:"+sharedDocID {
		t.Fatalf("instance = %s", tl.route.ID)
	}
}

func TestPeerUnsetsSecretFlagKeepsMyValueLocal(t *testing.T) {
	srv, _ := hitServer(t, created)
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection(srv.URL))
	store := secrettest.NewStore(t, true)
	h := openAPI(t, uitest.WithCollab(hub), uitest.WithSecrets(store))
	mine := &apitool.Mine{}
	mine.MarkSecret("e-stg/token", []string{"127.0.0.1"})
	if err := apitool.OpenLocal(h.Context().Paths).SaveMine(sharedDocID, mine); err != nil {
		t.Fatal(err)
	}
	tl := openShared(t, h, "r-pay")
	putSecret(t, h, tl, "token", "canary-Q9x7Z2")
	hub.Inject(sharedDocID, fixtures.UserHana, []collab.Op{
		{Op: collab.OpSet, ID: "envvar:v2", Field: "secret", Value: json.RawMessage(`false`)},
		{Op: collab.OpSet, ID: "envvar:v2", Field: "value", Value: json.RawMessage(`"x"`)},
	})
	waitFor(t, h, "hana's change", func() bool { return tl.foreign["r-pay"].Seq > 0 || len(hub.Ops(sharedDocID)) >= 1 })
	h.Flush()
	editHeaderValue(h, tl, 1, "c") // an edit of mine after it
	waitFor(t, h, "my edit on the hub", func() bool { return strings.Contains(opsText(hub), `"c"`) })
	if strings.Contains(opsText(hub), "canary") {
		t.Fatalf("my vault value reached the hub: %s", opsText(hub))
	}
	v, err := tl.scope(h.Context()).Lookup("token")
	if err != nil || v.Text != "canary-Q9x7Z2" || !v.Secret {
		t.Fatalf("Lookup = %+v, %v", v, err)
	}
	// The host binding stays: the secret goes only to the allowed host.
	sc := tl.scope(h.Context())
	sc.Env = &apitool.Env{ID: "e-stg", Vars: []apitool.Var{{Name: "base_url", Value: "https://evil.example"}, {Name: "token", Secret: true}}}
	p, err := apitool.Prepare(tl.req, sharedDocID, sc, apitool.Dyn{})
	if err != nil {
		t.Fatal(err)
	}
	if err := apitool.CheckSecretHosts(p); err == nil {
		t.Fatal("the secret may go to evil.example")
	}
}

func TestKickedClosesDoc(t *testing.T) {
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-pay")
	hub.Kick(sharedDocID, fixtures.UserSam, "kicked")
	// The toast box wraps the name, so the sentence is matched up to it.
	waitFor(t, h, "the kicked toast", func() bool { return hasWhole(h.Screen(), ko.SharedKicked("")+"Payments") })
	if strings.Contains(h.Screen(), ko.TreeShared("Payments team")) || tl.docs[sharedDocID] != nil {
		t.Fatalf("the shared tree stayed:\n%s", h.Screen())
	}
	if tl.colName == sharedKey(sharedDocID) {
		t.Fatal("still editing the closed collection")
	}
}

func TestVersionsRestore(t *testing.T) {
	hub := collabtest.New(t)
	seedShared(hub, sharedCollection("https://api.example.com"))
	h := openAPI(t, uitest.WithCollab(hub))
	tl := openShared(t, h, "r-pay")
	ctx := h.Context()
	if _, err := ctx.API.CreateDocVersion(context.Background(), sharedDocID, "v1"); err != nil {
		t.Fatal(err)
	}
	tl.focus = paneTree
	h.Press("v")
	waitFor(t, h, "the version list", func() bool { return strings.Contains(overlayText(h), "#1") })
	h.Press("enter")
	waitFor(t, h, "the version preview", func() bool { return strings.Contains(overlayText(h), ko.VersionPreview(1, 2)) })
	h.Press("enter") // 이 버전으로 되돌리기
	waitFor(t, h, "the restored version on top", func() bool {
		return len(tl.versions) >= 2 && tl.versions[0].RestoredFrom != nil && *tl.versions[0].RestoredFrom == 1
	})
	n := 0
	for _, r := range h.API.Requests() {
		if r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/versions/1/restore") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("RestoreDocVersion calls = %d", n)
	}
	waitFor(t, h, "the list again, new version first", func() bool {
		o := overlayText(h)
		i, j := strings.Index(o, "#2"), strings.Index(o, "#1 ")
		return i >= 0 && j >= 0 && i < j
	})
}
