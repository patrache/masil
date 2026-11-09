package secrets

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeSecurity installs a /usr/bin/security stand-in that records its args and
// standard input in dir, sleeps FAKE_SEC_SLEEP seconds, prints FAKE_SEC_OUT and
// exits FAKE_SEC_EXIT.
func fakeSecurity(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "security")
	body := `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_SEC_DIR/args"
if [ "$1" = "-i" ]; then cat > "$FAKE_SEC_DIR/stdin"; fi
sleep "${FAKE_SEC_SLEEP:-0}"
if [ -n "$FAKE_SEC_OUT" ]; then printf '%s\n' "$FAKE_SEC_OUT"; fi
exit "${FAKE_SEC_EXIT:-0}"
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_SEC_DIR", dir)
	t.Setenv("FAKE_SEC_SLEEP", "0")
	t.Setenv("FAKE_SEC_OUT", "")
	t.Setenv("FAKE_SEC_EXIT", "0")
	prev := securityPath
	securityPath = script
	t.Cleanup(func() { securityPath = prev })
	return dir
}

func TestSecurityUsesStdin(t *testing.T) {
	dir := fakeSecurity(t)
	t.Setenv("FAKE_SEC_SLEEP", "1")
	marker := "zq81-" + strconv.Itoa(os.Getpid())
	value := `it's "x" \ ` + marker
	acct := "leet/masil.example/U1/cookie"

	done := make(chan error, 1)
	go func() { done <- defaultOS().Set(bg, Service, acct, value) }()

	sawFake := false
	deadline := time.Now().Add(900 * time.Millisecond)
	for time.Now().Before(deadline) {
		out, err := exec.Command("ps", "-ww", "-A", "-o", "args").Output()
		if err != nil {
			t.Fatalf("ps: %v", err)
		}
		if strings.Contains(string(out), marker) {
			t.Fatalf("the value is visible in ps:\n%s", out)
		}
		if strings.Contains(string(out), filepath.Join(dir, "security")) {
			sawFake = true
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := <-done; err != nil {
		t.Fatalf("Set = %v", err)
	}
	if !sawFake {
		t.Fatal("ps never showed the fake security process (the check proved nothing)")
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if strings.TrimSpace(string(args)) != "-i" || strings.Contains(string(args), marker) {
		t.Errorf("args = %q, want only -i", args)
	}
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	want := `add-generic-password -U -s masil -a 'leet/masil.example/U1/cookie' -w 'it\'s "x" \\ ` + marker + "'\n"
	if string(stdin) != want {
		t.Errorf("stdin = %q\nwant    %q", stdin, want)
	}
}

func TestPutKeychainDeniedNeedsConsent(t *testing.T) {
	for _, code := range []string{"51", "45", "128", "36"} {
		t.Run(code, func(t *testing.T) {
			fakeSecurity(t)
			t.Setenv("FAKE_SEC_EXIT", code)
			err := defaultOS().Set(bg, Service, "leet/h/U1/c", "v")
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("Set = %v, want ErrUnavailable", err)
			}
			s := Open(Options{Paths: testPaths(t)}) // the default (keychain) backend
			if _, err := s.Put(bg, key("U1", "c"), New("v")); !errors.Is(err, ErrNeedConsent) {
				t.Fatalf("Put = %v, want ErrNeedConsent", err)
			}
			if s.Fallback() != FallbackUnset {
				t.Fatal("fallback chosen without consent")
			}
		})
	}
}

func TestKeychainGetDelete(t *testing.T) {
	dir := fakeSecurity(t)
	t.Setenv("FAKE_SEC_OUT", "the value")
	b := defaultOS()
	if b.Name() != BackendKeychain {
		t.Fatalf("Name = %q", b.Name())
	}
	v, err := b.Get(bg, Service, "leet/h/U1/c")
	if err != nil || v != "the value" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	t.Setenv("FAKE_SEC_OUT", "")
	if err := b.Delete(bg, Service, "leet/h/U1/c"); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	want := "find-generic-password -s masil -a leet/h/U1/c -w\ndelete-generic-password -s masil -a leet/h/U1/c\n"
	if string(args) != want {
		t.Errorf("args = %q, want %q", args, want)
	}

	t.Setenv("FAKE_SEC_EXIT", "44") // errSecItemNotFound
	if _, err := b.Get(bg, Service, "leet/h/U1/c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
	if err := b.Delete(bg, Service, "leet/h/U1/c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete = %v, want ErrNotFound", err)
	}
	t.Setenv("FAKE_SEC_EXIT", "51")
	if _, err := b.Get(bg, Service, "leet/h/U1/c"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Get = %v, want ErrUnavailable", err)
	}
}

func TestKeychainMissingTool(t *testing.T) {
	prev := securityPath
	securityPath = filepath.Join(t.TempDir(), "nope")
	t.Cleanup(func() { securityPath = prev })
	if err := defaultOS().Set(bg, Service, "a/b/c/d", "v"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Set = %v, want ErrUnavailable", err)
	}
}

func TestKeychainRejectsLineBreaks(t *testing.T) {
	dir := fakeSecurity(t)
	for _, v := range []string{"a\nadd-generic-password -s x", "a\rb", "a\x00b"} {
		if err := defaultOS().Set(bg, Service, "a/b/c/d", v); err == nil {
			t.Errorf("Set(%q) succeeded", v)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "args")); err == nil {
		t.Error("security ran for a value that cannot be quoted on one line")
	}
}
