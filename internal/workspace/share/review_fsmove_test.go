package share_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

func writeProj(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	must(t, os.MkdirAll(filepath.Dir(p), 0o755))
	must(t, os.WriteFile(p, []byte(content), 0o644))
}

func (d *docRig) fsResults(seat uint8) []wire.FsResult {
	var res []wire.FsResult
	for _, e := range d.takeGuest(seat) {
		if r, ok := e.(share.EventFsResult); ok {
			res = append(res, r.FsResult)
		}
	}
	return res
}

func (d *docRig) exists(rel string) bool {
	_, err := os.Lstat(filepath.Join(d.dir, filepath.FromSlash(rel)))
	return err == nil
}

func (d *docRig) approveAll() {
	d.t.Helper()
	for _, p := range d.host.Proposals() {
		if _, err := d.host.HandleProposal(p.ID, share.ActApprove, ""); err != nil {
			d.t.Fatal(err)
		}
	}
	d.settle()
}

// v0.1.3 security (w): renaming a folder moves everything in it, entries
// hidden from the participant included; moving conf/ to pub/ would take
// conf/prod.yaml out of its ignore rule. A folder holding anything a
// participant cannot see is refused, at the request and again at approval.
func TestPeerFolderMoveKeepsHiddenEntries(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	g := d.guests[1]
	writeProj(t, d.dir, ".masilignore", "conf/prod.yaml\n")
	writeProj(t, d.dir, "conf/app.yaml", "a")
	writeProj(t, d.dir, "conf/prod.yaml", "SECRET")
	writeProj(t, d.dir, "deploy/sub/server.key", "KEY")
	writeProj(t, d.dir, "deploy/sub/readme.md", "r")
	writeProj(t, d.dir, "open/a.md", "a")
	writeProj(t, d.dir, "plain/a.md", "a")

	must(t, g.RequestFs(wire.FsRename, "conf", "pub"))
	must(t, g.RequestFs(wire.FsRename, "deploy", "d2"))
	must(t, g.RequestFs(wire.FsDelete, "conf", ""))
	d.settle()
	res := d.fsResults(1)
	if len(res) != 3 || res[0].Error != share.DocErrIgnored || res[1].Error != share.DocErrDenied || res[2].Error != share.DocErrIgnored {
		t.Fatalf("results = %#v", res)
	}
	if n := len(d.host.Proposals()); n != 0 {
		t.Fatalf("%d proposals queued for folders with hidden entries", n)
	}

	// Queued while clean; a hidden entry appears before the owner approves.
	must(t, g.RequestFs(wire.FsRename, "open", "open2"))
	must(t, g.RequestFs(wire.FsRename, "plain", "plain2"))
	d.settle()
	if n := len(d.host.Proposals()); n != 2 {
		t.Fatalf("clean folder renames not queued: %d", n)
	}
	writeProj(t, d.dir, "open/.env", "AWS_SECRET=x")
	d.approveAll()
	res = d.fsResults(1)
	if len(res) != 2 || res[0].Error != share.DocErrDenied || res[1].Error != "" {
		t.Fatalf("results after approval = %#v", res)
	}
	if !d.exists("open/.env") || d.exists("open2") || !d.exists("plain2/a.md") || !d.exists("conf/prod.yaml") {
		t.Fatal("wrong folders moved")
	}
}

// v0.1.3 security (x): a participant's rename never replaces an existing
// file (refused at the request and re-checked at approval).
func TestPeerRenameDoesNotOverwrite(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	g := d.guests[1]
	writeProj(t, d.dir, "a.md", "A")
	writeProj(t, d.dir, "b.md", "B")
	must(t, g.RequestFs(wire.FsRename, "a.md", "b.md"))
	must(t, g.RequestFs(wire.FsRename, "a.md", "src"))
	d.settle()
	res := d.fsResults(1)
	if len(res) != 2 || res[0].Error != share.FsErrExists || res[1].Error != share.FsErrExists {
		t.Fatalf("results = %#v", res)
	}
	if n := len(d.host.Proposals()); n != 0 {
		t.Fatalf("%d proposals queued onto existing paths", n)
	}
	must(t, g.RequestFs(wire.FsRename, "a.md", "c.md"))
	d.settle()
	writeProj(t, d.dir, "c.md", "C")
	d.approveAll()
	res = d.fsResults(1)
	if len(res) != 1 || res[0].Error != share.FsErrExists {
		t.Fatalf("results after approval = %#v", res)
	}
	if d.diskText("b.md") != "B" || d.diskText("c.md") != "C" || d.diskText("a.md") != "A" {
		t.Fatal("a rename replaced an existing file")
	}
}
