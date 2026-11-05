package notify_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/notify"
)

func pathsIn(t *testing.T, dir string) config.Paths {
	t.Helper()
	t.Setenv("MASIL_CONFIG_DIR", dir)
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newPaths(t *testing.T) config.Paths {
	t.Helper()
	return pathsIn(t, filepath.Join(t.TempDir(), "masil"))
}

type ledgerFile struct {
	Version int      `json:"version"`
	IDs     []string `json:"ids"`
}

func readLedgerFile(t *testing.T, p config.Paths) ledgerFile {
	t.Helper()
	b, err := os.ReadFile(p.AlertsShown)
	if err != nil {
		t.Fatal(err)
	}
	var f ledgerFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("ledger file is not valid JSON: %v (%q)", err, b)
	}
	if f.Version != 1 {
		t.Fatalf("version = %d", f.Version)
	}
	return f
}

func TestLedgerDedup(t *testing.T) {
	p := newPaths(t)
	l := notify.OpenLedger(p)
	if seen, err := l.Seen("a"); err != nil || seen {
		t.Fatalf("Seen before = %v, %v", seen, err)
	}
	if isNew, err := l.MarkIfNew("a"); err != nil || !isNew {
		t.Fatalf("first MarkIfNew = %v, %v", isNew, err)
	}
	if isNew, err := l.MarkIfNew("a"); err != nil || isNew {
		t.Fatalf("second MarkIfNew = %v, %v", isNew, err)
	}
	if seen, err := l.Seen("a"); err != nil || !seen {
		t.Fatalf("Seen after = %v, %v", seen, err)
	}
	// A second ledger over the same paths shares the record.
	if isNew, err := notify.OpenLedger(p).MarkIfNew("a"); err != nil || isNew {
		t.Fatalf("other ledger MarkIfNew = %v, %v", isNew, err)
	}
	if st, err := os.Stat(p.AlertsShown); err != nil {
		t.Fatal(err)
	} else if st.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v", st.Mode().Perm())
	}
	if f := readLedgerFile(t, p); len(f.IDs) != 1 || f.IDs[0] != "a" {
		t.Errorf("ids = %v", f.IDs)
	}
}

func TestLedgerCap500(t *testing.T) {
	p := newPaths(t)
	l := notify.OpenLedger(p)
	for i := 0; i < 501; i++ {
		if isNew, err := l.MarkIfNew(strconv.Itoa(i)); err != nil || !isNew {
			t.Fatalf("MarkIfNew(%d) = %v, %v", i, isNew, err)
		}
	}
	f := readLedgerFile(t, p)
	if len(f.IDs) != 500 {
		t.Fatalf("len = %d", len(f.IDs))
	}
	if f.IDs[0] != "1" || f.IDs[499] != "500" {
		t.Fatalf("order: first %q last %q", f.IDs[0], f.IDs[499])
	}
	if seen, _ := l.Seen("0"); seen {
		t.Error("oldest id should be dropped")
	}
	if isNew, _ := l.MarkIfNew("0"); !isNew {
		t.Error("dropped id is new again")
	}
}

func TestLedgerRecoversCorruptFile(t *testing.T) {
	for _, garbage := range []string{"garbage", "", `{"version":1,"ids":"x"}`, `{"version":2,"ids":["a"]}`, "\x00\xff{"} {
		p := newPaths(t)
		if err := config.EnsureDir(p); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.AlertsShown, []byte(garbage), 0o600); err != nil {
			t.Fatal(err)
		}
		l := notify.OpenLedger(p)
		if seen, err := l.Seen("a"); err != nil || seen {
			t.Errorf("%q: Seen = %v, %v", garbage, seen, err)
		}
		if isNew, err := l.MarkIfNew("a"); err != nil || !isNew {
			t.Errorf("%q: MarkIfNew = %v, %v", garbage, isNew, err)
		}
		if f := readLedgerFile(t, p); len(f.IDs) != 1 || f.IDs[0] != "a" {
			t.Errorf("%q: rewritten ids = %v", garbage, f.IDs)
		}
	}
}

func TestLedgerConcurrentGoroutines(t *testing.T) {
	p := newPaths(t)
	const n = 200
	var mu sync.Mutex
	counts := map[string]int{}
	var wg sync.WaitGroup
	for g := 0; g < 2; g++ {
		l := notify.OpenLedger(p) // separate ledgers, same files
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				id := strconv.Itoa(i)
				if g == 1 {
					id = strconv.Itoa(n - 1 - i) // opposite order: real contention, not leader-follower
				}
				isNew, err := l.MarkIfNew(id)
				if err != nil {
					t.Errorf("MarkIfNew(%s): %v", id, err)
				}
				if isNew {
					mu.Lock()
					counts[id]++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if c := counts[strconv.Itoa(i)]; c != 1 {
			t.Errorf("id %d new %d times", i, c)
		}
	}
	if f := readLedgerFile(t, p); len(f.IDs) != n {
		t.Errorf("file has %d ids", len(f.IDs))
	}
}

// pause between marks in TestLedgerTwoProcesses: a polling TryLock never
// wins against a peer that relocks immediately, so without gaps one process
// would simply run after the other.
const pause = 5 * time.Millisecond

func TestLedgerTwoProcesses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "masil")
	p := pathsIn(t, dir)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperMarkLedger$", "-test.count=1")
	cmd.Env = append(os.Environ(), "MASIL_HELPER=1", "MASIL_CONFIG_DIR="+dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	childNew := make(chan []string, 1)
	ready := make(chan struct{})
	go func() {
		var ids []string
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			switch {
			case line == "ready":
				close(ready)
			case strings.HasPrefix(line, "new "):
				ids = append(ids, strings.TrimPrefix(line, "new "))
			case line == "done":
				childNew <- ids
				return
			}
		}
		close(childNew)
	}()
	select {
	case <-ready:
	case <-time.After(30 * time.Second):
		t.Fatal("helper not ready")
	}
	if _, err := io.WriteString(stdin, "go\n"); err != nil {
		t.Fatal(err)
	}

	counts := map[string]int{}
	l := notify.OpenLedger(p)
	for i := 0; i < 100; i++ {
		id := strconv.Itoa(i)
		isNew, err := l.MarkIfNew(id)
		if err != nil {
			t.Errorf("parent MarkIfNew(%s): %v", id, err)
		}
		if isNew {
			counts[id]++
		}
		time.Sleep(pause) // leave unlocked windows so both processes really contend
	}
	var ids []string
	select {
	case got, ok := <-childNew:
		if !ok {
			t.Fatal("helper exited without done")
		}
		ids = got
	case <-time.After(30 * time.Second):
		t.Fatal("helper did not finish")
	}
	for _, id := range ids {
		counts[id]++
	}
	for i := 0; i < 100; i++ {
		if c := counts[strconv.Itoa(i)]; c != 1 {
			t.Errorf("id %d new %d times overall", i, c)
		}
	}
	if len(counts) != 100 {
		t.Errorf("distinct new ids = %d", len(counts))
	}
	if f := readLedgerFile(t, p); len(f.IDs) != 100 {
		t.Errorf("file has %d ids", len(f.IDs))
	}
	t.Logf("parent new %d, child new %d", 100-len(ids), len(ids))
}

// TestHelperMarkLedger is the child process of TestLedgerTwoProcesses.
func TestHelperMarkLedger(t *testing.T) {
	if os.Getenv("MASIL_HELPER") != "1" {
		t.Skip("helper process only")
	}
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	l := notify.OpenLedger(p)
	os.Stdout.WriteString("ready\n")
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	for i := 99; i >= 0; i-- { // opposite order to the parent: they meet in the middle
		id := strconv.Itoa(i)
		isNew, err := l.MarkIfNew(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "child MarkIfNew(%s): %v\n", id, err)
		}
		if isNew {
			fmt.Fprintf(os.Stdout, "new %s\n", id)
		}
		time.Sleep(pause)
	}
	os.Stdout.WriteString("done\n")
}

func TestLedgerLockTimeoutShows(t *testing.T) {
	p := newPaths(t)
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	holder := flock.New(p.AlertsShownLock)
	if err := holder.Lock(); err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	start := time.Now()
	isNew, err := notify.OpenLedger(p).MarkIfNew("x")
	elapsed := time.Since(start)
	if !isNew || err == nil {
		t.Fatalf("MarkIfNew under held lock = %v, %v; want true + error", isNew, err)
	}
	if elapsed < 1800*time.Millisecond || elapsed > 4*time.Second {
		t.Errorf("waited %v, want ~2s", elapsed)
	}
}
