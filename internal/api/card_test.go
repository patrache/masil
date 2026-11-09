package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
)

// A card send carries kind "card" and card {type, v, data}; a text send has
// no "card" key (0.1.2 wire unchanged).
func TestSendMessageCard(t *testing.T) {
	s := apitest.New(t)
	reply := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"message":{"id":"M1","kind":"card","body":"x","card":{"type":"arch.design","v":1,"data":{"n":14}}}}`))
	}
	s.Handle("POST /v1/channels/{id}/messages", reply)
	s.Handle("POST /v1/messages/{id}/thread", reply)
	card := &api.Card{Type: "arch.design", V: 1, Data: json.RawMessage(`{}`)}
	m, _, err := s.Client().SendMessage(ctx, "C", api.SendMessageRequest{Kind: api.MessageKindCard, Body: "x", Card: card})
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != api.MessageKindCard || m.Card == nil || m.Card.Type != "arch.design" || m.Card.V != 1 || string(m.Card.Data) != `{"n":14}` {
		t.Fatalf("decoded card = %+v / %+v", m, m.Card)
	}
	if _, _, err := s.Client().ReplyThread(ctx, "R", api.ThreadReplyRequest{Kind: api.MessageKindCard, Body: "x", Card: card}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Client().SendMessage(ctx, "C", api.SendMessageRequest{Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	reqs := s.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d", len(reqs))
	}
	for i, want := range []string{`"kind":"card"`, `"card":{"type":"arch.design","v":1,"data":{}}`} {
		for _, r := range reqs[:2] {
			if !bytes.Contains(r.Body, []byte(want)) {
				t.Errorf("%d: body %s lacks %s", i, r.Body, want)
			}
		}
	}
	if bytes.Contains(reqs[2].Body, []byte(`"card"`)) {
		t.Errorf("text send carries a card key: %s", reqs[2].Body)
	}
}
