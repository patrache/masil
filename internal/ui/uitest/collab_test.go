package uitest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/patrache/masil/internal/collab"
	"github.com/patrache/masil/internal/collab/collabtest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// TestWithCollab: ctx.CollabDial is the hub's socket and the hub serves the
// doc REST on the harness server, both as the logged-in user.
func TestWithCollab(t *testing.T) {
	hub := collabtest.New(t)
	hub.Seed("D1", collab.Snapshot{Seq: 1, Objects: []collab.Object{{ID: "n1", Type: "node", Fields: map[string]json.RawMessage{}}}})
	h := New(t, WithCollab(hub))
	ctx := h.Context()
	if ctx.CollabDial == nil {
		t.Fatal("CollabDial not set")
	}
	conn, err := ctx.CollabDial(context.Background(), ctx.API.DocSocketURL("D1"), ctx.API.AuthHeader())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(1000, "")
	if n := hub.Conns("D1", fixtures.UserSam); n != 1 {
		t.Fatalf("hub sees %d sockets for %s", n, fixtures.UserSam)
	}
	d, err := ctx.API.GetDoc(context.Background(), "D1")
	if err != nil || d.ID != "D1" || d.HeadSeq != 1 {
		t.Fatalf("GetDoc through the harness server: %+v %v", d, err)
	}
}

// Without WithCollab the field stays nil (the real dial).
func TestCollabDialDefaultsToReal(t *testing.T) {
	if New(t).Context().CollabDial != nil {
		t.Fatal("CollabDial set without WithCollab")
	}
}
