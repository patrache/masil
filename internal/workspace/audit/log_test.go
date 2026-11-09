package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAuditAppendOnly0600(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workspace-audit")
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

	l, err := Open(dir, "sh_123")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Write(Entry{At: at, Ev: EvShareStart}); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(dir, "sh_123")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Write(Entry{At: at, Ev: EvSAS, Seat: 2, User: "u_1", Data: map[string]any{"result": "match"}}); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()

	p := filepath.Join(dir, "sh_123.jsonl")
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.Contains(lines[0], `"ev":"share_start"`) || strings.Contains(lines[0], "seat") || strings.Contains(lines[0], "data") {
		t.Errorf("line 0 = %s", lines[0])
	}
	var e Entry
	if err := json.Unmarshal([]byte(lines[1]), &e); err != nil {
		t.Fatal(err)
	}
	if e.Ev != EvSAS || e.Seat != 2 || e.User != "u_1" || e.Data["result"] != "match" || !e.At.Equal(at) {
		t.Errorf("entry = %+v", e)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
			t.Errorf("file mode = %v", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
			t.Errorf("dir mode = %v", fi.Mode().Perm())
		}
	}
}

func TestAuditRejectsBadShareID(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"", "../x", "a/b", `a\b`, "..", "a.b"} {
		if l, err := Open(dir, id); err == nil {
			_ = l.Close()
			t.Errorf("Open(%q) accepted", id)
		}
	}
}

func TestAuditPurge(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := mk("old.jsonl", 31*24*time.Hour)
	recent := mk("recent.jsonl", 29*24*time.Hour)
	other := mk("old.txt", 40*24*time.Hour)
	n, err := Purge(dir, now)
	if err != nil || n != 1 {
		t.Errorf("Purge = %d, %v; want 1", n, err)
	}
	for p, want := range map[string]bool{old: false, recent: true, other: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", filepath.Base(p), err == nil, want)
		}
	}
	if n, err := Purge(filepath.Join(dir, "missing"), now); n != 0 || err != nil {
		t.Errorf("Purge(missing) = %d, %v", n, err)
	}
}
