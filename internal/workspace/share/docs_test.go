package share_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/workspace/audit"
	"github.com/patrache/masil/internal/workspace/fsroot"
	"github.com/patrache/masil/internal/workspace/journal"
	"github.com/patrache/masil/internal/workspace/ot"
	"github.com/patrache/masil/internal/workspace/share"
	"github.com/patrache/masil/internal/workspace/wire"
)

const hubTS = "src/server/hub.ts"

// copyProj copies testdata/proj to a temp folder and adds what cannot live
// in the repository: node_modules (git-ignored here), a .git folder and a
// link to .env.
func copyProj(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	err := filepath.WalkDir("testdata/proj", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("testdata/proj", p)
		dst := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "node_modules", "x.js"), []byte("module.exports = 1\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("[core]\n"), 0o644))
	if err := os.Symlink(".env", filepath.Join(dir, "link_env")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type docRig struct {
	*rig
	dir   string
	root  *fsroot.Root
	docs  *share.DocManager
	j     *journal.Journal
	cache string
}

// newDocRig shares a copy of testdata/proj; seats join at level (editor
// and above verified).
func newDocRig(t *testing.T, level wire.Level, seats ...uint8) *docRig {
	t.Helper()
	d := &docRig{dir: copyProj(t), cache: t.TempDir()}
	root, err := fsroot.Open(d.dir)
	must(t, err)
	t.Cleanup(func() { _ = root.Close() })
	root.SetIgnorer(fsroot.NewFileIgnorer(root))
	d.root = root
	d.j, err = journal.Open(d.cache, root.Dir())
	must(t, err)
	d.rig = newRigWith(t, func(c *share.HostConfig) {
		d.docs = share.NewDocManager(root, d.j, c.Clock)
		c.Root, c.Docs = root, d.docs
	}, seats...)
	for _, s := range seats {
		d.join(s, level, level >= wire.LevelEditor)
	}
	return d
}

// open opens rel on the given guests and settles.
func (d *docRig) open(rel string, seats ...uint8) {
	d.t.Helper()
	for _, s := range seats {
		must(d.t, d.guests[s].OpenDoc(rel))
	}
	d.settle()
	for _, s := range seats {
		if _, ok := d.guests[s].DocText(rel); !ok {
			d.t.Fatalf("seat %d could not open %s: %#v", s, rel, d.gEv[s])
		}
	}
}

func (d *docRig) hostText(rel string) string {
	d.t.Helper()
	text, _, ok := d.docs.Text(rel)
	if !ok {
		d.t.Fatalf("%s not open on the host", rel)
	}
	return text
}

func (d *docRig) guestText(seat uint8, rel string) string {
	d.t.Helper()
	text, ok := d.guests[seat].DocText(rel)
	if !ok {
		d.t.Fatalf("%s not open on seat %d", rel, seat)
	}
	return text
}

func (d *docRig) diskText(rel string) string {
	d.t.Helper()
	b, err := os.ReadFile(filepath.Join(d.dir, filepath.FromSlash(rel)))
	must(d.t, err)
	return string(b)
}

// insertAt is the op inserting s at rune pos of text.
func insertAt(text string, pos int, s string) ot.Op {
	return ot.Insert(pos, s, len([]rune(text)))
}

// lineSpan is the rune range of line n (1-based) of text.
func lineSpan(text string, n int) (start, end int) {
	rs := []rune(text)
	line := 1
	for i, r := range rs {
		if line == n && start == 0 && (i == 0 || rs[i-1] == '\n') {
			start = i
		}
		if r == '\n' {
			if line == n {
				return start, i
			}
			line++
		}
	}
	return start, len(rs)
}

// TestThreeWayLiveEditing is spec B16 #3: the owner and two participants
// type into the same line at once through the relay, in a seeded random
// order with a reconnect; once the typing stops, all three copies are equal
// within a second and nobody's marker is lost or doubled.
func TestThreeWayLiveEditing(t *testing.T) {
	for seed := uint64(1); seed <= 6; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) { threeWay(t, seed) })
	}
}

func threeWay(t *testing.T, seed uint64) {
	d := newDocRig(t, wire.LevelEditor, 1, 2)
	_, err := d.docs.Open(hubTS, false)
	must(t, err)
	d.open(hubTS, 1, 2)
	rng := rand.New(rand.NewPCG(seed, 99))
	next := rune(0xE000)
	var markers []rune
	edit := func(text string, apply func(ot.Op) error) {
		s, e := lineSpan(text, 11)
		op := insertAt(text, s+rng.IntN(e-s+1), string(next))
		if err := apply(op); err == nil {
			markers = append(markers, next)
			next++
		}
	}
	const steps = 700
	for i := 0; i < steps; i++ {
		switch rng.IntN(11) {
		case 0, 1:
			text, _ := d.guests[1].DocText(hubTS)
			edit(text, func(op ot.Op) error { return d.guests[1].Edit(hubTS, op) })
		case 2, 3:
			text, _ := d.guests[2].DocText(hubTS)
			edit(text, func(op ot.Op) error { return d.guests[2].Edit(hubTS, op) })
		case 4:
			text, seq, _ := d.docs.Text(hubTS)
			edit(text, func(op ot.Op) error { _, _, err := d.docs.Submit(hubTS, 0, "", seq, op); return err })
		case 5, 6:
			d.relay.Pump()
		case 7:
			d.host.Tick(d.now)
		case 8:
			d.guests[1].Tick(d.now)
		case 9:
			d.guests[2].Tick(d.now)
		case 10:
			if i < steps-100 && rng.IntN(25) == 0 && d.relay.Connected(2) {
				d.relay.Drop(2)
			}
		}
		d.now = d.now.Add(10 * time.Millisecond)
	}
	last := d.now
	d.settle()
	for !d.relay.Connected(2) || d.now.Sub(last) < time.Second {
		d.advance(100 * time.Millisecond)
		if d.now.Sub(last) > 2*time.Second {
			t.Fatal("seat 2 did not come back")
		}
	}
	host := d.hostText(hubTS)
	for _, s := range []uint8{1, 2} {
		if got := d.guestText(s, hubTS); got != host {
			t.Fatalf("seat %d differs from the owner\n owner %q\n seat  %q", s, host, got)
		}
	}
	for _, m := range markers {
		if n := strings.Count(host, string(m)); n != 1 {
			t.Fatalf("marker %U appears %d times", m, n)
		}
	}
	if len(markers) < 100 {
		t.Fatalf("only %d edits made", len(markers))
	}
}

func TestDiskChangeMergedWithUnsaved(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	d.open(hubTS, 1)
	text := d.guestText(1, hubTS)
	must(t, d.guests[1].Edit(hubTS, insertAt(text, 0, "// edited by seat 1\n")))
	d.settle()
	disk := strings.Replace(d.diskText(hubTS), "import { DurableObject }", "import { DurableObject, env }", 1)
	must(t, os.WriteFile(filepath.Join(d.dir, hubTS), []byte(disk), 0o644))
	d.advance(share.DiskPoll)
	ev, ok := findEvent[share.EventDoc](d.takeHost())
	if _, merged := ev.DocEvent.(share.DocDiskMerged); !ok || !merged {
		t.Fatalf("host event = %#v", ev)
	}
	host := d.hostText(hubTS)
	if !strings.HasPrefix(host, "// edited by seat 1\n") || !strings.Contains(host, "DurableObject, env }") {
		t.Fatalf("merged text = %q", host)
	}
	if d.guestText(1, hubTS) != host {
		t.Fatal("guest did not converge")
	}
	if n, ok := findEvent[share.EventNotice](d.takeGuest(1)); !ok || n.Code != share.NoticeDiskMerged || n.Arg != hubTS {
		t.Fatalf("guest notice = %#v %v", n, ok)
	}
	if got := d.docs.Dirty(); len(got) != 1 || got[0] != hubTS {
		t.Fatalf("Dirty = %v", got)
	}
}

// TestDiskRenameWriteMergedOnce is Review Focus 4: an outside editor saves
// by writing a temp file and renaming it over, twice within 600 ms; each
// save merges exactly once.
func TestDiskRenameWriteMergedOnce(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	d.open(hubTS, 1)
	must(t, d.guests[1].Edit(hubTS, insertAt(d.guestText(1, hubTS), 0, "X")))
	d.settle()
	d.takeHost()
	write := func(content string) {
		tmp := filepath.Join(d.dir, "src", "server", ".hub.ts.tmp")
		must(t, os.WriteFile(tmp, []byte(content), 0o644))
		must(t, os.Rename(tmp, filepath.Join(d.dir, hubTS)))
	}
	base := d.diskText(hubTS)
	v1 := base + "// one\n"
	v2 := v1 + "// two two\n"
	write(v1)
	d.advance(share.DiskPoll)
	d.advance(100 * time.Millisecond)
	write(v2)
	d.advance(400 * time.Millisecond)
	d.advance(share.DiskPoll)
	merges := 0
	for _, e := range d.takeHost() {
		if ev, ok := e.(share.EventDoc); ok {
			if _, ok := ev.DocEvent.(share.DocDiskMerged); ok {
				merges++
			}
		}
	}
	if merges != 2 {
		t.Fatalf("merges = %d", merges)
	}
	if host := d.hostText(hubTS); host != "X"+v2 || d.guestText(1, hubTS) != host {
		t.Fatalf("host %q", host)
	}
}

func TestDiskDeleteRecreate(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	d.open(hubTS, 1)
	want := d.hostText(hubTS)
	must(t, os.Remove(filepath.Join(d.dir, hubTS)))
	d.advance(share.DiskPoll)
	ev, ok := findEvent[share.EventDoc](d.takeHost())
	if _, del := ev.DocEvent.(share.DocDeletedOnDisk); !ok || !del {
		t.Fatalf("host event = %#v", ev)
	}
	if n, ok := findEvent[share.EventNotice](d.takeGuest(1)); !ok || n.Code != share.NoticeDeleted {
		t.Fatalf("guest notice = %#v", n)
	}
	must(t, d.guests[1].Save(hubTS))
	d.settle()
	if got := d.diskText(hubTS); got != want {
		t.Fatalf("recreated file = %q", got)
	}
	recreated := false
	for _, e := range d.takeHost() {
		if ev, ok := e.(share.EventDoc); ok {
			if _, ok := ev.DocEvent.(share.DocRecreated); ok {
				recreated = true
			}
		}
	}
	if !recreated {
		t.Fatal("no DocRecreated")
	}
	// Deleted then put back from outside: the state clears by itself.
	must(t, os.Remove(filepath.Join(d.dir, hubTS)))
	d.advance(share.DiskPoll)
	must(t, os.WriteFile(filepath.Join(d.dir, hubTS), []byte(want+"// back\n"), 0o644))
	d.advance(share.DiskPoll)
	if len(d.docs.Dirty()) != 0 || d.hostText(hubTS) != want+"// back\n" {
		t.Fatalf("after recreate outside: dirty %v text %q", d.docs.Dirty(), d.hostText(hubTS))
	}
}

func TestSaveAtomicAndAudited(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	must(t, os.Chmod(filepath.Join(d.dir, hubTS), 0o640))
	d.open(hubTS, 1)
	secret := "const marker = 'Q7ZX-content-not-for-audit';\n"
	must(t, d.guests[1].Edit(hubTS, insertAt(d.guestText(1, hubTS), 0, secret)))
	d.settle()
	if got := d.host.EndCheck(); len(got) != 1 || got[0] != hubTS {
		t.Fatalf("EndCheck = %v", got)
	}
	must(t, d.guests[1].Save(hubTS))
	d.settle()
	if disk := d.diskText(hubTS); disk != d.hostText(hubTS) || !strings.HasPrefix(disk, secret) {
		t.Fatalf("disk = %q", disk)
	}
	fi, err := os.Stat(filepath.Join(d.dir, hubTS))
	must(t, err)
	if fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", fi.Mode())
	}
	size := float64(len(d.diskText(hubTS)))
	if !d.auditHas(audit.EvPeerSave, func(e audit.Entry) bool {
		return e.Data["rel"] == hubTS && e.Data["size"] == size && e.Data["seat"] == float64(1)
	}) {
		t.Fatalf("peer_save not recorded: %#v", d.auditEntries())
	}
	raw, err := os.ReadFile(d.audit)
	must(t, err)
	if bytes.Contains(raw, []byte("Q7ZX")) {
		t.Fatal("file content in the audit record")
	}
	if n, ok := findEvent[share.EventNotice](d.takeGuest(1)); !ok || n.Code != share.NoticeSaved || n.Seat != 1 {
		t.Fatalf("notice = %#v", n)
	}
	if len(d.host.EndCheck()) != 0 || len(d.docs.Dirty()) != 0 {
		t.Fatal("still dirty after save")
	}
}

func docErrors(evs []share.Event) map[string]string {
	out := map[string]string{}
	for _, e := range evs {
		if s, ok := e.(share.EventDocState); ok && s.Error != "" {
			out[s.Doc] = s.Error
		}
	}
	return out
}

func TestPeerCannotOpenSecrets(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	for _, rel := range []string{".env", "../x", "link_env", ".git/config", "/etc/passwd", "node_modules/x.js"} {
		must(t, d.guests[1].OpenDoc(rel))
	}
	d.settle()
	got := docErrors(d.takeGuest(1))
	want := map[string]string{".env": "denied", "../x": "escape", "link_env": "denied", ".git/config": "denied",
		"/etc/passwd": "absolute", "node_modules/x.js": "ignored"}
	for rel, code := range want {
		if got[rel] != code {
			t.Errorf("open %s: %q, want %q", rel, got[rel], code)
		}
	}
	n := 0
	for _, e := range d.auditEntries() {
		if e.Ev == audit.EvPathViolation {
			n++
		}
	}
	if n != len(want) {
		t.Fatalf("path_violation records = %d", n)
	}
	// The owner can still open their own .env (only peers are refused).
	if _, err := d.docs.Open(".env", false); err != nil {
		t.Fatalf("owner open .env: %v", err)
	}
}

func TestSizeRules(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	must(t, os.WriteFile(filepath.Join(d.dir, "big.log"), bytes.Repeat([]byte("a\n"), (3<<20)/2), 0o644))
	must(t, os.WriteFile(filepath.Join(d.dir, "logo.png"), []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), 0o644))
	must(t, os.WriteFile(filepath.Join(d.dir, "mid.txt"), bytes.Repeat([]byte("abcdefghi\n"), (1<<20)/10), 0o644))
	for _, rel := range []string{"big.log", "logo.png", "mid.txt"} {
		must(t, d.guests[1].OpenDoc(rel))
	}
	d.settle()
	evs := d.takeGuest(1)
	errs := docErrors(evs)
	if errs["big.log"] != share.DocErrTooLarge || errs["logo.png"] != share.DocErrBinary {
		t.Fatalf("errors = %v", errs)
	}
	var mid wire.DocState
	for _, e := range evs {
		if s, ok := e.(share.EventDocState); ok && s.Doc == "mid.txt" {
			mid = s.DocState
		}
	}
	if !mid.ReadOnly || mid.Reason != share.DocReasonLarge || len(mid.Text) != 1<<20-(1<<20)%10 {
		t.Fatalf("mid.txt state: read_only %v reason %q len %d", mid.ReadOnly, mid.Reason, len(mid.Text))
	}
	if err := d.guests[1].Edit("mid.txt", insertAt("a", 0, "x")); !errors.Is(err, share.ErrReadOnly) {
		t.Fatalf("guest edit of a read-only doc = %v", err)
	}
	// A modified client sends the edit anyway: the owner refuses it.
	op := ot.Insert(0, "x", len([]rune(mid.Text)))
	body := fmt.Sprintf(`{"doc":"mid.txt","client_op_id":"c1","base_seq":0,"ops":%s}`, mustMarshal(t, op))
	d.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(d.guests[1], wire.KindDocOp, []byte(body)))
	d.settle()
	rej, ok := findEvent[share.EventDocRejected](d.takeGuest(1))
	if !ok || rej.Code != share.RejectReadOnly {
		t.Fatalf("reject = %#v %v", rej, ok)
	}
	if d.diskText("mid.txt")[0] != 'a' || !strings.HasPrefix(d.hostText("mid.txt"), "abc") {
		t.Fatal("read-only doc changed")
	}
}

func mustMarshal(t *testing.T, op ot.Op) string {
	t.Helper()
	b, err := op.MarshalJSON()
	must(t, err)
	return string(b)
}

func TestOpenDocCap(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor)
	for i := 0; i <= share.MaxOpenDocs; i++ {
		must(t, os.WriteFile(filepath.Join(d.dir, fmt.Sprintf("f%02d.txt", i)), []byte("x\n"), 0o644))
	}
	for i := 0; i < share.MaxOpenDocs; i++ {
		if _, err := d.docs.Open(fmt.Sprintf("f%02d.txt", i), false); err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
	}
	_, err := d.docs.Open(fmt.Sprintf("f%02d.txt", share.MaxOpenDocs), false)
	if !errors.Is(err, share.ErrTooManyDocs) || share.DocErrorCode(err) != share.DocErrTooMany {
		t.Fatalf("21st open = %v", err)
	}
	// Closing one (nothing unsaved) makes room.
	d.docs.Close("f00.txt")
	if _, err := d.docs.Open(fmt.Sprintf("f%02d.txt", share.MaxOpenDocs), false); err != nil {
		t.Fatalf("open after close: %v", err)
	}
}

func TestRenameDeleteNeedOwnerApproval(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	g := d.guests[1]
	must(t, g.RequestFs(wire.FsCreate, "notes.md", ""))
	must(t, g.RequestFs(wire.FsDelete, "README.md", ""))
	must(t, g.RequestFs(wire.FsRename, "src/index.ts", "src/main.ts"))
	must(t, g.RequestFs(wire.FsDelete, ".env", ""))
	d.settle()
	if _, err := os.Stat(filepath.Join(d.dir, "notes.md")); err != nil {
		t.Fatalf("create did not happen at once: %v", err)
	}
	ps := d.host.Proposals()
	if len(ps) != 2 || ps[0].Kind != share.KindDelete || ps[0].From != "README.md" || ps[1].Kind != share.KindRename || ps[1].To != "src/main.ts" {
		t.Fatalf("Proposals = %#v", ps)
	}
	if _, err := os.Stat(filepath.Join(d.dir, "README.md")); err != nil {
		t.Fatal("deleted before approval")
	}
	if _, err := d.host.HandleProposal(ps[0].ID, share.ActApprove, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.host.HandleProposal(ps[1].ID, share.ActReject, ""); err != nil {
		t.Fatal(err)
	}
	d.settle()
	if _, err := os.Stat(filepath.Join(d.dir, "README.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("approved delete did not happen")
	}
	if _, err := os.Stat(filepath.Join(d.dir, "src/index.ts")); err != nil {
		t.Fatal("rejected rename happened")
	}
	var res []wire.FsResult
	for _, e := range d.takeGuest(1) {
		if r, ok := e.(share.EventFsResult); ok {
			res = append(res, r.FsResult)
		}
	}
	if len(res) != 4 || res[0].Error != "" || res[1].Error != "denied" || res[2].Error != "" || res[3].Error != share.FsErrRejected {
		t.Fatalf("results = %#v", res)
	}
	if !d.auditHas(audit.EvFsRequest, nil) || !d.auditHas(audit.EvFsAction, dataIs("action", "approve")) {
		t.Fatal("file requests not recorded")
	}
}

func TestJournalRecovery(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	d.open(hubTS, 1)
	must(t, d.guests[1].Edit(hubTS, insertAt(d.guestText(1, hubTS), 0, "// unsaved\n")))
	d.settle()
	if p, _ := d.j.Pending(); len(p) != 0 {
		t.Fatal("journal written before the batch delay")
	}
	d.advance(time.Second)
	p, err := d.j.Pending()
	must(t, err)
	if len(p) != 1 || p[0].Rel != hubTS || !strings.HasPrefix(p[0].Text, "// unsaved\n") {
		t.Fatalf("journal = %#v", p)
	}
	// masil restarts: a new manager on the same folder sees it.
	m2 := share.NewDocManager(d.root, d.j, fixedClock{&d.now})
	rec, err := m2.Recoverable()
	must(t, err)
	if len(rec) != 1 {
		t.Fatalf("Recoverable = %#v", rec)
	}
	v, err := m2.Recover(hubTS)
	must(t, err)
	if !strings.HasPrefix(v.Text, "// unsaved\n") {
		t.Fatalf("recovered = %q", v.Text)
	}
	must(t, m2.Save(hubTS, 0))
	if p, _ := d.j.Pending(); len(p) != 0 {
		t.Fatalf("journal after save = %#v", p)
	}
	if !strings.HasPrefix(d.diskText(hubTS), "// unsaved\n") {
		t.Fatal("recovered text not saved")
	}
}

func TestAutosave(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1)
	d.open(hubTS, 1)
	d.docs.SetAutosave(true)
	must(t, d.guests[1].Edit(hubTS, insertAt(d.guestText(1, hubTS), 0, "A")))
	d.settle()
	d.advance(share.AutosaveDelay - 100*time.Millisecond)
	if strings.HasPrefix(d.diskText(hubTS), "A") {
		t.Fatal("saved early")
	}
	d.advance(200 * time.Millisecond)
	if !strings.HasPrefix(d.diskText(hubTS), "A") {
		t.Fatal("autosave did not happen")
	}
}

// TestLostAckNotDuplicated: the owner sequences an edit but its doc_ack is
// lost with the socket; after the reconnect the edit is in the text once.
func TestLostAckNotDuplicated(t *testing.T) {
	d := newDocRig(t, wire.LevelEditor, 1, 2)
	d.open(hubTS, 1, 2)
	g := d.guests[1]
	d.relay.Tamper(func(from, to uint8, f []byte) []byte {
		if h, _, err := wire.Parse(f); err == nil && to == 1 && h.Kind == wire.KindDocAck {
			return nil
		}
		return f
	})
	must(t, g.Edit(hubTS, insertAt(d.guestText(1, hubTS), 0, "\uE901")))
	d.relay.Pump()
	d.host.Tick(d.now) // sequenced; the ack is dropped
	d.relay.Drop(1)
	d.relay.Tamper(nil)
	d.settle()
	d.advance(time.Second) // redial, resume, resync
	host := d.hostText(hubTS)
	if strings.Count(host, "\uE901") != 1 {
		t.Fatalf("marker appears %d times", strings.Count(host, "\uE901"))
	}
	if d.guestText(1, hubTS) != host || d.guestText(2, hubTS) != host {
		t.Fatal("copies differ after the resync")
	}
	// Further typing still works.
	must(t, g.Edit(hubTS, insertAt(host, 1, "\uE902")))
	d.settle()
	if got := d.hostText(hubTS); !strings.HasPrefix(got, "\uE901\uE902") || d.guestText(2, hubTS) != got {
		t.Fatalf("after resync: %q", got)
	}

	// The same doc_op retransmitted: one doc_ack with the first seq, no
	// second doc_bcast, text unchanged. Seat 2 sending seat 1's id is a
	// different edit.
	acks, bcasts := 0, 0
	d.relay.Tamper(func(from, to uint8, f []byte) []byte {
		if h, _, err := wire.Parse(f); err == nil && from == wire.SeatOwner {
			if to == 1 && h.Kind == wire.KindDocAck {
				acks++
			}
			if to == 2 && h.Kind == wire.KindDocBcast {
				bcasts++
			}
		}
		return f
	})
	_, seq, _ := d.docs.Text(hubTS)
	text := d.hostText(hubTS)
	body := fmt.Sprintf(`{"doc":%q,"client_op_id":"dup-1","base_seq":%d,"ops":%s}`, hubTS, seq, mustMarshal(t, insertAt(text, 0, "\uE900")))
	d.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(g, wire.KindDocOp, []byte(body)))
	d.settle()
	d.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(g, wire.KindDocOp, []byte(body)))
	d.settle()
	if acks != 2 || bcasts != 1 || strings.Count(d.hostText(hubTS), "\uE900") != 1 {
		t.Fatalf("acks %d bcasts %d text %q", acks, bcasts, d.hostText(hubTS))
	}
	d.relay.Inject(wire.SeatOwner, share.GuestSealToOwner(d.guests[2], wire.KindDocOp, []byte(body)))
	d.settle()
	if strings.Count(d.hostText(hubTS), "\uE900") != 2 {
		t.Fatal("seat 2's edit with seat 1's id was not sequenced")
	}
	// The injected edits look like the seats' own, so their copies miss
	// them until the next edit shows a gap in seq and they re-sync.
	text, seq, _ = d.docs.Text(hubTS)
	_, _, err := d.docs.Submit(hubTS, 0, "", seq, insertAt(text, 0, "\uE903"))
	must(t, err)
	d.settle()
	d.advance(time.Second)
	if d.guestText(1, hubTS) != d.hostText(hubTS) || d.guestText(2, hubTS) != d.hostText(hubTS) {
		t.Fatal("copies differ at the end")
	}
}

func TestPresenceRelayed(t *testing.T) {
	d := newDocRig(t, wire.LevelViewer, 1, 2)
	d.open(hubTS, 1, 2)
	d.guests[1].Presence(hubTS, 5, 2, 7)
	d.settle()
	p, ok := findEvent[share.EventPresence](d.takeGuest(2))
	if !ok || p.Seat != 1 || p.Cursor != 5 || p.SelTo != 7 {
		t.Fatalf("presence = %#v %v", p, ok)
	}
	if hasEvent[share.EventPresence](d.takeGuest(1)) {
		t.Fatal("own presence echoed")
	}
	if err := d.guests[1].Edit(hubTS, insertAt("a", 0, "x")); !errors.Is(err, share.ErrLevel) {
		t.Fatalf("viewer Edit = %v", err)
	}
}
