package share_test

import (
	"testing"

	"github.com/patrache/masil/internal/workspace/share"
)

// A keyboard participant's input reaches the owner's screen as an event
// (the owner's input band), right after it goes to the terminal.
func TestKeyboardInputReported(t *testing.T) {
	r, ft := keyboardRig(t, nil, share.DefaultKeyboard)
	r.takeHost()
	r.host.OwnerActivity(r.now)
	if err := r.guests[1].Input(0, []byte("ls -la\r")); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := ft.typed(); got != "ls -la\r" {
		t.Fatalf("typed = %q", got)
	}
	var got []share.EventInput
	for _, ev := range r.takeHost() {
		if e, ok := ev.(share.EventInput); ok {
			got = append(got, e)
		}
	}
	if len(got) != 1 || got[0].Seat != 1 || got[0].Term != 0 || string(got[0].Data) != "ls -la\r" {
		t.Fatalf("input events = %+v", got)
	}
}
