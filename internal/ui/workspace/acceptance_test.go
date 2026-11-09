package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/workspace/ot"
	"github.com/patrache/masil/internal/workspace/share"
)

// Spec B16 acceptance criteria that no single Task's tests covered (P2
// Task 25). The table of B16 items to test names is in the P2 exec log.

// recConn records every frame a connection sends.
type recConn struct {
	share.Conn
	rec func([]byte)
}

func (c recConn) Send(f []byte) error {
	c.rec(f)
	return c.Conn.Send(f)
}

// B16 #1: a whole round (share start, join, open, edit, save) sends the
// masil server no folder path, no file name and no file content: not in
// any REST request, not in any frame the relay sees (plaintext headers,
// server frames, the owner's control frames — and the payloads, which must
// be ciphertext).
func TestNoFolderDataInAnyRequest(t *testing.T) {
	fake := setup(t)
	h := uitest.New(t)
	r := newShareRig(t, h, fake, map[uint8]api.UserSummary{1: minjiUser})

	var mu sync.Mutex
	var frames [][]byte
	rec := func(f []byte) {
		mu.Lock()
		frames = append(frames, append([]byte(nil), f...))
		mu.Unlock()
	}
	r.relay.Tamper(func(_, _ uint8, f []byte) []byte { rec(f); return f })
	ownerDial := r.relay.Dial(fixtures.UserSam)
	t.Cleanup(SetDialForTest(func(ctx context.Context, id string) (share.Conn, error) {
		c, err := ownerDial(ctx, id)
		if err != nil {
			return nil, err
		}
		return recConn{Conn: c, rec: rec}, nil
	}))

	r.shareFriends("minji")
	r.join(1, "편집", true)
	g := r.guests[1]
	if err := g.OpenDoc(hubTS); err != nil {
		t.Fatal(err)
	}
	r.settle()
	text, ok := g.DocText(hubTS)
	if !ok {
		t.Fatalf("guest could not open %s", hubTS)
	}
	const marker = "Q7ZX-folder-data-marker"
	if err := g.Edit(hubTS, ot.Insert(0, "// "+marker+"\n", len([]rune(text)))); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if err := g.Save(hubTS); err != nil {
		t.Fatal(err)
	}
	r.settle()
	disk, err := os.ReadFile(filepath.Join(r.s.dir, filepath.FromSlash(hubTS)))
	if err != nil || !bytes.Contains(disk, []byte(marker)) {
		t.Fatalf("the round did not save (err %v)", err)
	}

	forbidden := []string{
		r.s.dir, filepath.ToSlash(r.s.dir), // the folder
		"hub.ts", "src/server", "index.ts", "README", "id_rsa", ".env", // file names
		marker, "DurableObject", "cloudflare:workers", "Test fixture for the share engine", // contents
	}
	mu.Lock()
	defer mu.Unlock()
	if len(frames) < 10 {
		t.Fatalf("only %d frames recorded", len(frames))
	}
	for i, f := range frames {
		for _, s := range forbidden {
			if bytes.Contains(f, []byte(s)) {
				t.Errorf("frame %d carries %q in the clear", i, s)
			}
		}
	}
	reqs := h.API.Requests()
	if len(reqs) == 0 {
		t.Fatal("no REST requests recorded")
	}
	for _, q := range reqs {
		all := q.Method + " " + q.Path + "?" + q.Query + " " + string(q.Body)
		for _, s := range forbidden {
			if strings.Contains(all, s) {
				t.Errorf("%s %s carries %q", q.Method, q.Path, s)
			}
		}
	}
}
