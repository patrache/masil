package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileFallback(t *testing.T) {
	fos := newFakeOS(false)
	p := testPaths(t)
	s := Open(Options{OS: fos, Paths: p})
	s.SetFallback(FallbackFile)
	b, err := s.Put(bg, key("U1", "cookie"), New("file-value"))
	if err != nil || b != BackendFile {
		t.Fatalf("Put = %v, %v", b, err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(p.SecretsDir)
		if err != nil || st.Mode().Perm() != 0o700 {
			t.Errorf("folder mode = %v, %v", st.Mode().Perm(), err)
		}
		st, err = os.Stat(filepath.Join(p.SecretsDir, "tools.json"))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("tools.json mode = %v, %v", st.Mode().Perm(), err)
		}
	}
	gi, err := os.ReadFile(filepath.Join(p.SecretsDir, ".gitignore"))
	if err != nil || string(gi) != "*\n" {
		t.Errorf(".gitignore = %q, %v", gi, err)
	}
	idx, err := os.ReadFile(filepath.Join(p.SecretsDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(idx), "file-value") || !strings.Contains(string(idx), "leet/masil.example/U1/cookie") {
		t.Errorf("index.json = %s", idx)
	}

	s2 := Open(Options{OS: fos, Paths: p})
	v, b, err := s2.Get(bg, key("U1", "cookie"))
	if err != nil || v.Reveal() != "file-value" || b != BackendFile {
		t.Fatalf("Get after restart = %q, %v, %v", v.Reveal(), b, err)
	}
	if s2.Where(key("U1", "cookie")) != BackendFile {
		t.Errorf("Where = %q", s2.Where(key("U1", "cookie")))
	}
	if err := s2.Delete(bg, key("U1", "cookie")); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(p.SecretsDir, "tools.json"))
	if strings.Contains(string(data), "file-value") {
		t.Errorf("tools.json still holds the value: %s", data)
	}
}

func TestFileWidePermsNarrowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits")
	}
	p := testPaths(t)
	if err := os.MkdirAll(p.SecretsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.SecretsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := Open(Options{OS: newFakeOS(false), Paths: p})
	s.SetFallback(FallbackFile)
	if _, err := s.Put(bg, key("U1", "c"), New("v")); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(p.SecretsDir)
	if st.Mode().Perm() != 0o700 {
		t.Errorf("folder mode = %v", st.Mode().Perm())
	}
}

func TestFileBrokenReadsEmpty(t *testing.T) {
	p := testPaths(t)
	if err := os.MkdirAll(p.SecretsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"tools.json", "index.json"} {
		if err := os.WriteFile(filepath.Join(p.SecretsDir, f), []byte("{broken"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := Open(Options{OS: newFakeOS(false), Paths: p})
	s.SetFallback(FallbackFile)
	if _, _, err := s.Get(bg, key("U1", "c")); err == nil {
		t.Fatal("Get on a broken file succeeded")
	}
	if _, err := s.Put(bg, key("U1", "c"), New("v")); err != nil {
		t.Fatal(err)
	}
	v, _, err := Open(Options{OS: newFakeOS(false), Paths: p}).Get(bg, key("U1", "c"))
	if err != nil || v.Reveal() != "v" {
		t.Fatalf("Get = %v", err)
	}
}

func TestFileWithoutFolder(t *testing.T) {
	s := Open(Options{OS: newFakeOS(false)}) // no paths (bare tests)
	s.SetFallback(FallbackFile)
	if _, err := s.Put(bg, key("U1", "c"), New("v")); err == nil {
		t.Fatal("file fallback without a secrets folder succeeded")
	}
	s.SetFallback(FallbackSession)
	if b, err := s.Put(bg, key("U1", "c"), New("v")); err != nil || b != BackendMemory {
		t.Fatalf("session Put = %v, %v", b, err)
	}
}
