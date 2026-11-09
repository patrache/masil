package apitool

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// sentCard is one card message the test server received.
type sentCard struct {
	Path string
	Kind string `json:"kind"`
	Body string `json:"body"`
	Card struct {
		Type string          `json:"type"`
		V    int             `json:"v"`
		Data json.RawMessage `json:"data"`
	} `json:"card"`
}

// acceptMessages makes the test server answer sends with 201.
func acceptMessages(h *uitest.Harness) {
	h.API.Handle("POST /v1/channels/{channel_id}/messages", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Kind string    `json:"kind"`
			Body string    `json:"body"`
			Card *api.Card `json:"card"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		m := api.Message{ID: "01JDCARD000000000000000001", ChannelID: r.PathValue("channel_id"), Kind: in.Kind, Body: in.Body, Card: in.Card,
			Mentions: []api.MentionRef{}, Reactions: []api.ReactionSummary{}, CreatedAt: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": m})
	})
}

// sentCards are the card messages the test server got.
func sentCards(h *uitest.Harness) []sentCard {
	var out []sentCard
	for _, r := range h.API.Requests() {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.Path, "/messages") {
			continue
		}
		var c sentCard
		if json.Unmarshal(r.Body, &c) == nil && c.Kind == api.MessageKindCard {
			c.Path = r.Path
			out = append(out, c)
		}
	}
	return out
}

func TestSharePreviewIsExactPayload(t *testing.T) {
	srv, _ := hitServer(t, created)
	h := openAPI(t)
	acceptMessages(h)
	tl := seedRequests(h, nil, paymentRequest(srv))
	h.Press("enter")
	waitScreen(t, h, "201 Created")
	tl.focus = paneResponse
	h.Press("s")
	if tl.share == nil {
		t.Fatalf("s opened no preview:\n%s", h.Screen())
	}
	o := overlayText(h)
	if !strings.Contains(o, ko.ShareTitle()) || !strings.Contains(o, "🧪 POST "+srv.URL) {
		t.Fatalf("preview:\n%s", o)
	}
	// the preview is the renderer drawing the exact bytes that will go
	r, _ := ui.CardFor(ui.CardAPIResult)
	for _, l := range r.Lines(h.Context(), shareCard(h.Context(), tl.share), sharePreviewW) {
		if !hasWhole(o, ansi.Strip(l)) {
			t.Fatalf("preview lacks %q:\n%s", ansi.Strip(l), o)
		}
	}
	data := bytes.Clone(tl.share.data)
	st := tl.rv.current()
	want := ko.CardResultBody("POST", "/v1/payments", 201, ms(st.resp.Timing.Total), 4, 4)
	h.Press("enter") // the first channel
	waitFor(t, h, "the card sent", func() bool { return len(sentCards(h)) == 1 })
	c := sentCards(h)[0]
	if c.Path != "/v1/channels/"+shareChannels(h.Context())[0]+"/messages" {
		t.Fatalf("sent to %s", c.Path)
	}
	if c.Card.Type != ui.CardAPIResult || c.Card.V != 1 || !bytes.Equal(c.Card.Data, data) {
		t.Fatalf("card = %+v\nwant data %s", c.Card, data)
	}
	if c.Body != want || !strings.HasPrefix(c.Body, "🧪 POST /v1/payments · 201 · ") || !strings.HasSuffix(c.Body, "ms · ✓4/4") {
		t.Fatalf("body = %q, want %q", c.Body, want)
	}
}

func TestShareOptionsRebuildPreview(t *testing.T) {
	srv, _ := hitServer(t, created)
	h := openAPI(t)
	acceptMessages(h)
	tl := seedRequests(h, nil, paymentRequest(srv))
	h.Press("enter")
	waitScreen(t, h, "201 Created")
	tl.focus = paneResponse
	h.Press("s")
	n := len(shareChannels(h.Context()))
	for range n + 2 { // down to "응답 본문 앞 20줄 넣기"
		h.Press("down")
	}
	h.Press("enter")
	if !tl.share.opts.IncludeBody || !strings.Contains(string(tl.share.data), `"body_head"`) {
		t.Fatalf("body option not applied: %s", tl.share.data)
	}
	if !strings.Contains(overlayText(h), `"id": "pay_1"`) && !strings.Contains(overlayText(h), `"id":"pay_1"`) {
		t.Fatalf("preview has no body:\n%s", overlayText(h))
	}
	data := bytes.Clone(tl.share.data)
	for range n + 2 {
		h.Press("up")
	}
	h.Press("enter")
	waitFor(t, h, "the card sent", func() bool { return len(sentCards(h)) == 1 })
	if !bytes.Equal(sentCards(h)[0].Card.Data, data) {
		t.Fatalf("sent %s\nwant %s", sentCards(h)[0].Card.Data, data)
	}
}

// cardMessage is a card message in channel ch.
func cardMessage(typ string, v int, data, body, ch string) *api.Message {
	return &api.Message{ID: "01JDCARD000000000000000009", ChannelID: ch, Kind: api.MessageKindCard, Body: body,
		Card: &api.Card{Type: typ, V: v, Data: json.RawMessage(data)}}
}

const resultData1 = `{"method":"POST","url":"{{base_url}}/v1/payments","status":201,"time_ms":142,"size":98,"asserts":{"passed":4,"total":4},"env":"staging","ref":{"collection":"Payments team","request_id":"r-pay","doc_id":"01JD00000000000000000APIC1"}}`

func TestCardRendersAndFallsBack(t *testing.T) {
	h := openAPI(t)
	ctx := h.Context()
	r, ok := ui.CardFor(ui.CardAPIResult)
	if !ok {
		t.Fatal("api.result has no renderer")
	}
	m := cardMessage(ui.CardAPIResult, 1, resultData1, "🧪 POST /v1/payments · 201 · 142ms · ✓4/4", fixtures.ChGeneral)
	got := ansi.Strip(strings.Join(r.Lines(ctx, m, 60), "\n"))
	if !strings.Contains(got, "🧪 POST {{base_url}}/v1/payments") || !strings.Contains(got, ko.CardResultMeta(201, 142, "98B", 4, 4)+" · staging") ||
		!strings.Contains(got, ko.CardOpenHint()) {
		t.Fatalf("lines:\n%s", got)
	}
	for _, l := range r.Lines(ctx, m, 20) {
		if w := ansi.StringWidth(l); w > 20 {
			t.Fatalf("a line of %d cells at width 20: %q", w, l)
		}
	}
	// another v -> the body, as an older client draws it
	m2 := cardMessage(ui.CardAPIResult, 2, resultData1, "🧪 POST /v1/payments · 201", fixtures.ChGeneral)
	if got := ansi.Strip(strings.Join(r.Lines(ctx, m2, 60), "\n")); got != "🧪 POST /v1/payments · 201" {
		t.Fatalf("v2 lines = %q", got)
	}
	// broken data -> the body
	m3 := cardMessage(ui.CardAPIResult, 1, `{"x":1}`, "body \x1b[31mred", fixtures.ChGeneral)
	if got := strings.Join(r.Lines(ctx, m3, 60), "\n"); strings.Contains(got, "\x1b[31m") || !strings.Contains(got, "body") {
		t.Fatalf("broken lines = %q", got)
	}
	// hostile strings in the data are sanitized
	m4 := cardMessage(ui.CardAPIResult, 1, `{"method":"GET","url":"/x\u001b]0;pwn\u0007","status":200,"time_ms":1,"size":1,"asserts":{"passed":0,"total":0},"ref":{"collection":"c","request_id":"r"}}`, "b", fixtures.ChGeneral)
	if got := strings.Join(r.Lines(ctx, m4, 60), "\n"); strings.Contains(got, "\x1b]0;pwn") {
		t.Fatalf("hostile lines = %q", got)
	}
	run, _ := ui.CardFor(ui.CardAPIRun)
	rm := cardMessage(ui.CardAPIRun, 1, `{"collection":"Payments","requests":6,"passed":2,"failed":4,"time_ms":1500,"failures":[{"name":"a","method":"GET","status":500,"reason":"status"}],"ref":{}}`, "run body", fixtures.ChGeneral)
	got = ansi.Strip(strings.Join(run.Lines(ctx, rm, 60), "\n"))
	if !strings.Contains(got, ko.CardRunTitle("Payments", 6)) || !strings.Contains(got, ko.CardRunMeta(2, 4, 1500)) || !strings.Contains(got, "✕ GET a · 500") {
		t.Fatalf("run lines:\n%s", got)
	}
	// the flag off -> the body
	t.Setenv("MASIL_FEATURES", "")
	if got := ansi.Strip(strings.Join(r.Lines(ctx, m, 60), "\n")); got != m.Body {
		t.Fatalf("flag off lines = %q", got)
	}
}

func TestCardEnterOpensSharedRequest(t *testing.T) {
	h := openAPI(t)
	ctx := h.Context()
	r, _ := ui.CardFor(ui.CardAPIResult)
	m := cardMessage(ui.CardAPIResult, 1, resultData1, "b", fixtures.ChGeneral)
	cmd := r.Open(ctx, m)
	if cmd == nil {
		t.Fatal("a room member's enter did nothing")
	}
	nav, ok := cmd().(ui.NavigateMsg)
	if !ok {
		t.Fatalf("Open = %T", cmd())
	}
	rt := nav.Route
	if rt.Kind != ui.RouteTool || rt.Arg != ui.ToolAPI || rt.ID != ui.ToolAPI+":doc:"+sharedDocID ||
		rt.Query[queryDoc] != sharedDocID || rt.Query[queryReq] != "r-pay" {
		t.Fatalf("route = %+v", rt)
	}
	// a personal collection's card opens nothing
	personal := cardMessage(ui.CardAPIResult, 1, strings.Replace(resultData1, `,"doc_id":"01JD00000000000000000APIC1"`, "", 1), "b", fixtures.ChGeneral)
	if r.Open(ctx, personal) != nil {
		t.Fatal("a personal card opened something")
	}
	// the flag off -> enter falls back (no tool)
	t.Setenv("MASIL_FEATURES", "")
	if r.Open(ctx, m) != nil {
		t.Fatal("Open with the flag off")
	}
}

func TestCardNonMemberShowsOnly(t *testing.T) {
	h := openAPI(t)
	ctx := h.Context()
	r, _ := ui.CardFor(ui.CardAPIResult)
	for _, ch := range []string{fixtures.DMMinji, "01JB0000000000000000000ZZ9"} {
		m := cardMessage(ui.CardAPIResult, 1, resultData1, "b", ch)
		if r.Open(ctx, m) != nil {
			t.Fatalf("enter opened a card of %s", ch)
		}
		if got := ansi.Strip(strings.Join(r.Lines(ctx, m, 60), "\n")); strings.Contains(got, ko.CardOpenHint()) || !strings.Contains(got, "{{base_url}}/v1/payments") {
			t.Fatalf("lines in %s:\n%s", ch, got)
		}
	}
}

func TestRunnerShareCard(t *testing.T) {
	srv, _ := hitServer(t, created)
	h := openAPI(t)
	acceptMessages(h)
	tl := seedRequests(h, nil, runRequests(srv)...)
	h.Press("r", "enter")
	h.Press("s") // while it runs (or right as it ends): nothing until it is done
	if tl.share != nil && !tl.run.done {
		t.Fatal("s during the run opened a preview")
	}
	waitRun(t, h, tl)
	if tl.share != nil {
		h.Press("esc")
		tl.share = nil
	}
	h.Press("s")
	if tl.share == nil || tl.share.typ != ui.CardAPIRun {
		t.Fatalf("s after the run opened no preview:\n%s", h.Screen())
	}
	want, err := apitool.RunCard(*tl.run.sum, "")
	if err != nil || !bytes.Equal(tl.share.data, want) {
		t.Fatalf("preview data %s, want %s (%v)", tl.share.data, want, err)
	}
	if !hasWhole(overlayText(h), ko.CardRunTitle("시험", 3)) {
		t.Fatalf("preview:\n%s", overlayText(h))
	}
	h.Press("enter")
	waitFor(t, h, "the run card sent", func() bool { return len(sentCards(h)) == 1 })
	c := sentCards(h)[0]
	if c.Card.Type != ui.CardAPIRun || !bytes.Equal(c.Card.Data, want) || c.Body != ko.CardRunBody("시험", 3, 2, 1, ms(tl.run.sum.Total)) {
		t.Fatalf("sent %+v", c)
	}
}
