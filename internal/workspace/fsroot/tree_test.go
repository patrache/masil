package fsroot

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func names(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}

func listFixture(t *testing.T) *Root {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, ".env", "K=V")
	writeFile(t, dir, "src/a.go", "package a")
	writeFile(t, dir, "node_modules/m/i.js", "m")
	writeFile(t, dir, "README.md", "readme")
	writeFile(t, dir, ".gitignore", "node_modules/\n")
	writeFile(t, dir, ".masilignore", "notes.txt\n")
	writeFile(t, dir, "notes.txt", "n")
	r := openRoot(t, dir)
	r.SetIgnorer(NewFileIgnorer(r))
	return r
}

func TestListForPeerHidesDeniedAndIgnored(t *testing.T) {
	r := listFixture(t)

	owner, more, err := r.List(".", false)
	if err != nil || more != 0 {
		t.Fatalf("owner List = %v, %d, %v", owner, more, err)
	}
	byName := map[string]Entry{}
	for _, e := range owner {
		byName[e.Name] = e
	}
	if e, ok := byName[".env"]; !ok || !e.Denied {
		t.Errorf("owner list .env = %+v, %v; want Denied", e, ok)
	}
	if e, ok := byName["node_modules"]; !ok || !e.Ignored || !e.Dir {
		t.Errorf("owner list node_modules = %+v, %v; want Dir+Ignored", e, ok)
	}
	for _, n := range []string{".gitignore", ".masilignore", "notes.txt", "README.md", "src"} {
		if _, ok := byName[n]; !ok {
			t.Errorf("owner list misses %q", n)
		}
	}
	if !owner[0].Dir || byName["README.md"].Size != int64(len("readme")) {
		t.Errorf("owner list order/size wrong: %+v", owner)
	}

	peer, _, err := r.List(".", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(peer); !reflect.DeepEqual(got, []string{"src", "README.md"}) {
		t.Errorf("peer List = %v, want [src README.md]", got)
	}
	if _, _, err := r.List("node_modules", true); err == nil {
		t.Errorf("peer can list an ignored folder")
	}
	if _, _, err := r.List("node_modules", false); err != nil {
		t.Errorf("owner cannot list an ignored folder: %v", err)
	}
	for _, rel := range []string{".gitignore", "notes.txt", "node_modules/m/i.js"} {
		if _, err := r.Check(rel, true); err == nil {
			t.Errorf("peer Check(%q) accepted", rel)
		} else if reason, _ := reasonOf(err); reason != ReasonIgnored {
			t.Errorf("peer Check(%q) reason %v, want Ignored", rel, reason)
		}
		if _, err := r.Check(rel, false); err != nil {
			t.Errorf("owner Check(%q) = %v", rel, err)
		}
	}
}

func TestListSortsDirsFirstByteOrder(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b.txt", "B.txt", "a.txt"} {
		writeFile(t, dir, n, "x")
	}
	for _, d := range []string{"zdir", "Adir"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	r := openRoot(t, dir)
	es, _, err := r.List(".", false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Adir", "zdir", "B.txt", "a.txt", "b.txt"}
	if got := names(es); !reflect.DeepEqual(got, want) {
		// a case-insensitive file system folds B.txt and b.txt into one
		if len(got) != 4 {
			t.Errorf("List order = %v, want %v", got, want)
		}
	}
}

func TestListHidesEscapingLinkFromPeer(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	writeFile(t, base, "outside/f", "secret")
	writeFile(t, proj, "a.go", "a")
	symlink(t, proj, "../outside", "out")
	r := openRoot(t, proj)
	peer, _, err := r.List(".", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(peer); !reflect.DeepEqual(got, []string{"a.go"}) {
		t.Errorf("peer list = %v, want [a.go]", got)
	}
	owner, _, _ := r.List(".", false)
	for _, e := range owner {
		if e.Name == "out" && !e.Denied {
			t.Errorf("escaping link not marked Denied for owner: %+v", e)
		}
	}
}

func TestListCapsAt5000(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < MaxDirEntries+3; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := openRoot(t, dir)
	es, more, err := r.List(".", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != MaxDirEntries || more != 3 {
		t.Errorf("List = %d entries, more %d; want %d, 3", len(es), more, MaxDirEntries)
	}
}
