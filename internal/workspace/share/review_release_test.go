package share_test

import (
	"context"
	"testing"

	"github.com/patrache/masil/internal/workspace/wire"
)

// Review 9: the documents participants opened are released when the share
// ends and when a participant is lowered to none.
func TestShareEndReleasesPeerDocs(t *testing.T) {
	d := newDocRig(t, wire.LevelViewer, 1, 2)
	d.open(hubTS, 1)
	d.open("README.md", 2)
	must(t, d.host.SetLevel(2, wire.LevelNone))
	d.settle()
	if _, ok := d.docs.View("README.md"); ok {
		t.Fatal("README.md stays loaded after seat 2 was lowered to none")
	}
	must(t, d.host.End(context.Background()))
	d.settle()
	if _, ok := d.docs.View(hubTS); ok {
		t.Fatal("hub.ts stays loaded after the share ended")
	}
	// Later solo edits do not reach the ended host.
	v, err := d.docs.Open(hubTS, false)
	must(t, err)
	_, _, err = d.docs.Submit(v.Rel, 0, "", v.Seq, insertAt(v.Text, 0, "x"))
	must(t, err)
	d.docs.Close(v.Rel)
}
