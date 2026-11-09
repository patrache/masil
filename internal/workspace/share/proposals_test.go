package share_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"ls -la\n\tok":                    "ls -la\n\tok",
		"a\x1b[201~b":                     "ab",
		"a\x1b]52;c;ZXZpbA==\x07b":        "ab",
		"a\x1b]0;title\x1b\\b":            "ab",
		"a\x1bPq#0;1\x1b\\b":              "ab",
		"a\x1bcb":                         "ab",
		"a\x1b(Bb":                        "ab",
		"a\rb\x00c\x7fd\u0085e\u009b31mf": "abcde31mf",
		"한글 👩‍💻 é":                        "한글 👩‍💻 é",
		"cut\x1b[":                        "cut",
		"line\x1b[2\nnext":                "line\nnext",
		"\x1b":                            "",
	}
	for in, want := range cases {
		if got := share.Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func proposeAndWait(t *testing.T, r *rig, seat uint8, text string) (string, share.Proposal) {
	t.Helper()
	id, err := r.guests[seat].Propose(text)
	if err != nil {
		t.Fatal(err)
	}
	r.settle()
	ps := r.host.Proposals()
	if len(ps) == 0 || ps[len(ps)-1].Text != text || ps[len(ps)-1].Seat != seat {
		t.Fatalf("Proposals = %#v", ps)
	}
	return id, ps[len(ps)-1]
}

func propResults(evs []share.Event) []wire.PropResult {
	var out []wire.PropResult
	for _, e := range evs {
		if p, ok := e.(share.EventPropResult); ok {
			out = append(out, p.PropResult)
		}
	}
	return out
}

func TestProposalSanitizedAndBracketed(t *testing.T) {
	r, ft := termRig(t, wire.LevelPrompter, 1)
	ft.bracketed = true
	text := "ls\x1b[201~; rm -rf ~\x07\nnext\u009b"
	id, p := proposeAndWait(t, r, 1, text)
	if !hasEvent[share.EventProposal](r.takeHost()) {
		t.Fatal("no EventProposal")
	}
	need, err := r.host.HandleProposal(p.ID, share.ActInsert, "")
	if err != nil || need {
		t.Fatalf("HandleProposal = %v %v", need, err)
	}
	if got, want := ft.typed(), "\x1b[200~ls; rm -rf ~\nnext\x1b[201~"; got != want {
		t.Fatalf("typed %q, want %q", got, want)
	}
	r.settle()
	res := propResults(r.takeGuest(1))
	if len(res) != 2 || res[0].Action != share.PropQueued || res[1].ID != id || res[1].Action != "insert" {
		t.Fatalf("results = %#v", res)
	}
	if !r.auditHas(audit.EvProposal, dataIs("text", text)) || !r.auditHas(audit.EvProposalAction, dataIs("action", "insert")) {
		t.Fatal("proposal not recorded")
	}

	// No paste mode, two lines: ask first, type only after the confirmation.
	ft.bracketed = false
	ft.injected = nil
	_, p = proposeAndWait(t, r, 1, "echo a\necho b")
	need, err = r.host.HandleProposal(p.ID, share.ActInsertSend, "")
	if err != nil || !need || ft.typed() != "" {
		t.Fatalf("multi-line without paste mode: need %v err %v typed %q", need, err, ft.typed())
	}
	if need, err = r.host.HandleProposal(p.ID, share.ActConfirmMultiline, ""); err != nil || need {
		t.Fatalf("confirm = %v %v", need, err)
	}
	if ft.typed() != "echo a\necho b\r" {
		t.Fatalf("typed %q", ft.typed())
	}
	if _, err := r.host.HandleProposal(p.ID, share.ActConfirmMultiline, ""); !errors.Is(err, share.ErrNoProposal) {
		t.Fatalf("handled twice: %v", err)
	}

	// One line goes straight in; edit replaces the text; reject types nothing.
	ft.injected = nil
	_, p = proposeAndWait(t, r, 1, "pwd")
	if _, err := r.host.HandleProposal(p.ID, share.ActInsertSend, ""); err != nil || ft.typed() != "pwd\r" {
		t.Fatalf("insert_send: %v %q", err, ft.typed())
	}
	ft.injected = nil
	_, p = proposeAndWait(t, r, 1, "rm -rf /")
	if _, err := r.host.HandleProposal(p.ID, share.ActEdit, "ls /\x1b[2J"); err != nil || ft.typed() != "ls /" {
		t.Fatalf("edit: %v %q", err, ft.typed())
	}
	ft.injected = nil
	_, p = proposeAndWait(t, r, 1, "whoami")
	if _, err := r.host.HandleProposal(p.ID, share.ActReject, ""); err != nil || ft.typed() != "" {
		t.Fatalf("reject: %v %q", err, ft.typed())
	}
	r.settle()
	res = propResults(r.takeGuest(1))
	if last := res[len(res)-1]; last.Action != share.PropRejected {
		t.Fatalf("last result = %#v", last)
	}
	if len(r.host.Proposals()) != 0 {
		t.Fatal("queue not empty")
	}
}

func TestProposalLimits(t *testing.T) {
	r, _ := termRig(t, wire.LevelPrompter, 1)
	g := r.guests[1]
	if _, err := g.Propose(strings.Repeat("가", share.ProposalMax+1)); !errors.Is(err, share.ErrTooLong) {
		t.Fatalf("4001 runes = %v", err)
	}
	if _, err := g.Propose(strings.Repeat("가", share.ProposalMax)); err != nil {
		t.Fatalf("4000 runes = %v", err)
	}
	// A modified client that skips the check: the owner refuses too.
	long := `{"id":"zz","text":"` + strings.Repeat("a", share.ProposalMax+1) + `"}`
	r.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(g, wire.KindPropSubmit, []byte(long)))
	r.settle()
	if n := len(r.host.Proposals()); n != 1 {
		t.Fatalf("queue = %d", n)
	}
	for i := 0; i < 9; i++ {
		if _, err := g.Propose("echo ok"); err != nil {
			t.Fatal(err)
		}
	}
	r.settle()
	// The 11th in a minute: the hub drops it, and an 11th sent past the hub
	// is refused by the owner as well.
	if _, err := g.Propose("echo 11th"); err != nil {
		t.Fatal(err)
	}
	r.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(g, wire.KindPropSubmit, []byte(`{"id":"p11","text":"echo 11th"}`)))
	r.settle()
	if n := len(r.host.Proposals()); n != 10 {
		t.Fatalf("queue after 11 in a minute = %d", n)
	}
	res := propResults(r.takeGuest(1))
	if last := res[len(res)-1]; last.ID != "p11" || last.Action != share.PropRateHit {
		t.Fatalf("11th result = %#v", last)
	}
	r.advance(time.Minute)
	for i := 0; i < 10; i++ {
		_, _ = g.Propose("echo more")
	}
	r.settle()
	r.advance(time.Minute)
	if n := len(r.host.Proposals()); n != share.ProposalQueueMax {
		t.Fatalf("queue = %d", n)
	}
	_, _ = g.Propose("echo 21st")
	r.settle()
	if n := len(r.host.Proposals()); n != share.ProposalQueueMax {
		t.Fatalf("queue = %d", n)
	}
	res = propResults(r.takeGuest(1))
	if res[len(res)-1].Action != share.PropFull {
		t.Fatalf("21st result = %#v", res[len(res)-1])
	}
}

func TestViewerCannotPropose(t *testing.T) {
	r, _ := termRig(t, wire.LevelViewer, 1)
	g := r.guests[1]
	if _, err := g.Propose("ls"); !errors.Is(err, share.ErrLevel) {
		t.Fatalf("viewer Propose = %v", err)
	}
	r.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(g, wire.KindPropSubmit, []byte(`{"id":"x","text":"rm -rf ~"}`)))
	r.settle()
	if len(r.host.Proposals()) != 0 {
		t.Fatal("forged proposal queued")
	}
	if !r.auditHas(audit.EvFrameRejected, func(e audit.Entry) bool {
		return e.Data["reason"] == "level" && e.Data["kind"] == wire.KindPropSubmit.String() && e.Seat == 1
	}) {
		t.Fatal("forged proposal not recorded")
	}
}

func TestOwnInputNotAudited(t *testing.T) {
	r, ft := keyboardRig(t, nil, 30*time.Minute)
	r.host.OwnerActivity(r.now)
	// The owner's own typing goes to the pane directly, never through Host.
	_ = ft.Inject([]byte("my-password\r"))
	if err := r.guests[1].Input(0, []byte("ls\r")); err != nil {
		t.Fatal(err)
	}
	r.settle()
	n := 0
	for _, e := range r.auditEntries() {
		if e.Ev == audit.EvKeyboardInput {
			n++
			if e.Seat != 1 {
				t.Fatalf("keyboard input recorded for seat %d", e.Seat)
			}
		}
		if strings.Contains(e.Ev, "password") || strings.Contains(stringData(e), "my-password") {
			t.Fatal("owner input in the record")
		}
	}
	if n != 1 {
		t.Fatalf("keyboard_input records = %d", n)
	}
}

func stringData(e audit.Entry) string {
	var b strings.Builder
	for k, v := range e.Data {
		b.WriteString(k)
		if s, ok := v.(string); ok {
			b.WriteString(s)
		}
	}
	return b.String()
}
