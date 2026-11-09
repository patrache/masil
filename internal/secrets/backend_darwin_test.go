package secrets

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
	// A find of the head (left-over pieces), then the add through stdin.
	if string(args) != "find-generic-password -s masil -a "+acct+" -w\n-i\n" || strings.Contains(string(args), marker) {
		t.Errorf("args = %q, want the head find and -i", args)
	}
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	want := `add-generic-password -U -s masil -a 'leet/masil.example/U1/cookie' -w 'masil:b64:` + base64.StdEncoding.EncodeToString([]byte(value)) + "'\n"
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
	// Get; Delete reads the head first (a chunked value has pieces to delete).
	want := "find-generic-password -s masil -a leet/h/U1/c -w\nfind-generic-password -s masil -a leet/h/U1/c -w\ndelete-generic-password -s masil -a leet/h/U1/c\n"
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

// TestKeychainLineBreaks: a value with a line break, CR or NUL goes on one
// encoded line (its text never becomes a command); a name with one is refused
// without running security.
func TestKeychainLineBreaks(t *testing.T) {
	dir := fakeSecurity(t)
	for _, v := range []string{"a\nadd-generic-password -s x", "a\rb", "a\x00b"} {
		if err := defaultOS().Set(bg, Service, "a/b/c/d", v); err != nil {
			t.Errorf("Set(%q) = %v", v, err)
		}
		stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
		if strings.Count(string(stdin), "\n") != 1 || strings.ContainsAny(strings.TrimSuffix(string(stdin), "\n"), "\r\x00") || strings.Contains(string(stdin), "-s x") {
			t.Errorf("stdin for %q = %q, want one encoded line", v, stdin)
		}
	}
	_ = os.Remove(filepath.Join(dir, "args"))
	for _, a := range []string{"a/b/c/d\nx", "a/b/c/d\rx", "a/b/c/d\x00"} {
		if err := defaultOS().Set(bg, Service, a, "v"); err == nil {
			t.Errorf("Set(account %q) succeeded", a)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "args")); err == nil {
		t.Error("security ran for a name that cannot be quoted on one line")
	}
}

// ---- a keychain emulator (the test binary re-run as /usr/bin/security) ----

// fakeKCEnv names the emulator's store folder; when set, this binary is the
// emulator (see init).
const fakeKCEnv = "MASIL_FAKE_KEYCHAIN"

// fakeSecLineMax is where the real `security -i` cuts a line: probed with
// read-only find commands, a line of ~4096 bytes or more runs its tail as a
// separate command.
const fakeSecLineMax = 4095

func init() {
	if dir := os.Getenv(fakeKCEnv); dir != "" {
		os.Exit(fakeKeychainMain(dir, os.Args[1:], os.Stdin, os.Stdout))
	}
}

// fakeKeychainMain behaves like /usr/bin/security for add-, find- and
// delete-generic-password: -i reads commands from stdin and cuts lines at
// fakeSecLineMax (the tail runs as its own command, as the real tool does),
// find -w prints a value with a byte outside printable ASCII as hex (as the
// real tool does). Every command is logged to <dir>/log; a cut line logs
// "SPLIT".
func fakeKeychainMain(dir string, args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) != 1 || args[0] != "-i" {
		return fakeSecCommand(dir, args, stdout)
	}
	data, _ := io.ReadAll(stdin)
	status := 0
	for _, line := range strings.Split(string(data), "\n") {
		if len(line) > fakeSecLineMax {
			fakeKCLogf(dir, "SPLIT")
		}
		for line != "" {
			piece := line[:min(len(line), fakeSecLineMax)]
			line = line[len(piece):]
			if strings.TrimSpace(piece) != "" {
				status = fakeSecCommand(dir, secWords(piece), stdout)
			}
		}
	}
	return status
}

// secWords splits an interactive-mode line: blanks separate words, '...'
// quotes, a backslash escapes the next byte (also inside quotes).
func secWords(line string) []string {
	var out []string
	var w strings.Builder
	quoted, word := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line):
			i++
			w.WriteByte(line[i])
			word = true
		case c == '\'':
			quoted = !quoted
			word = true
		case (c == ' ' || c == '\t') && !quoted:
			if word {
				out = append(out, w.String())
				w.Reset()
				word = false
			}
		default:
			w.WriteByte(c)
			word = true
		}
	}
	if word {
		out = append(out, w.String())
	}
	return out
}

func fakeKCItem(dir, service, account string) string {
	sum := sha256.Sum256([]byte(service + "\x00" + account))
	return filepath.Join(dir, "items", hex.EncodeToString(sum[:]))
}

func fakeKCLogf(dir, f string, a ...any) {
	fh, err := os.OpenFile(filepath.Join(dir, "log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer fh.Close()
	fmt.Fprintf(fh, f+"\n", a...)
}

func fakeSecCommand(dir string, w []string, stdout io.Writer) int {
	if len(w) == 0 {
		return 0
	}
	verb := w[0]
	opt := map[string]string{}
	takes := map[string]bool{"-s": true, "-a": true}
	if verb == "add-generic-password" {
		takes["-w"] = true
	}
	for i := 1; i < len(w); i++ {
		if takes[w[i]] && i+1 < len(w) {
			opt[w[i]] = w[i+1]
			i++
		} else {
			opt[w[i]] = ""
		}
	}
	fakeKCLogf(dir, "%s %s", verb, opt["-a"])
	item := fakeKCItem(dir, opt["-s"], opt["-a"])
	switch verb {
	case "add-generic-password":
		if err := os.MkdirAll(filepath.Join(dir, "items"), 0o700); err != nil {
			return 1
		}
		if err := os.WriteFile(item, []byte(opt["-w"]), 0o600); err != nil {
			return 1
		}
		return 0
	case "find-generic-password":
		b, err := os.ReadFile(item)
		if err != nil {
			return secExitNotFound
		}
		printable := !strings.ContainsFunc(string(b), func(r rune) bool { return r < ' ' || r > '~' })
		if printable {
			fmt.Fprintf(stdout, "%s\n", b)
		} else {
			fmt.Fprintf(stdout, "%x\n", b)
		}
		return 0
	case "delete-generic-password":
		if err := os.Remove(item); err != nil {
			return secExitNotFound
		}
		return 0
	default:
		fakeKCLogf(dir, "UNKNOWN")
		return 1
	}
}

// fakeKeychain points securityPath at the emulator and returns its folder.
func fakeKeychain(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv(fakeKCEnv, dir)
	prev := securityPath
	securityPath = exe
	t.Cleanup(func() { securityPath = prev })
	return dir
}

func fakeKCLog(t *testing.T, dir string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(dir, "log"))
	return string(b)
}

func fakeKCCount(t *testing.T, dir string) int {
	t.Helper()
	es, _ := os.ReadDir(filepath.Join(dir, "items"))
	return len(es)
}

// TestKeychainLongValue: a value far past the interactive-mode line limit
// (a long JWT) is stored whole, no line is cut (a cut runs the value's tail as
// a security command), and overwriting or deleting it leaves no pieces.
func TestKeychainLongValue(t *testing.T) {
	dir := fakeKeychain(t)
	kc := defaultOS()
	acct := "api/masil.example:8443/U1/env/prod/TOKEN"
	long := strings.Repeat("eyJhbGciOiJIUzI1NiJ9.", 600) + " list-keychains -d user "
	if err := kc.Set(bg, Service, acct, long); err != nil {
		t.Fatalf("Set(long) = %v", err)
	}
	if log := fakeKCLog(t, dir); strings.Contains(log, "SPLIT") || strings.Contains(log, "UNKNOWN") {
		t.Fatalf("a security line was cut:\n%s", log)
	}
	got, err := kc.Get(bg, Service, acct)
	if err != nil || got != long {
		t.Fatalf("Get = %d bytes, %v; want %d bytes", len(got), err, len(long))
	}
	if err := kc.Set(bg, Service, acct, "short"); err != nil {
		t.Fatal(err)
	}
	if got, err := kc.Get(bg, Service, acct); err != nil || got != "short" {
		t.Fatalf("Get after overwrite = %q, %v", got, err)
	}
	if n := fakeKCCount(t, dir); n != 1 {
		t.Errorf("%d keychain items after a short overwrite, want 1 (old pieces left)", n)
	}
	if err := kc.Set(bg, Service, acct, long); err != nil {
		t.Fatal(err)
	}
	if err := kc.Delete(bg, Service, acct); err != nil {
		t.Fatal(err)
	}
	if n := fakeKCCount(t, dir); n != 0 {
		t.Errorf("%d keychain items after Delete, want 0", n)
	}
	if _, err := kc.Get(bg, Service, acct); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
}

// TestKeychainTooLarge: a value past every piece is refused without running
// security, and not as ErrUnavailable (the keychain works; the store must not
// mark it down).
func TestKeychainTooLarge(t *testing.T) {
	dir := fakeKeychain(t)
	huge := strings.Repeat("x", 1<<20)
	err := defaultOS().Set(bg, Service, "api/h/U1/big", huge)
	if err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("Set(1 MiB) = %v, want a too-large error that is not ErrUnavailable", err)
	}
	if strings.Contains(fakeKCLog(t, dir), "add-generic-password") {
		t.Error("security stored something for a value that cannot fit")
	}
	s := Open(Options{Paths: testPaths(t)})
	if _, err := s.Put(bg, key("U1", "big"), New(huge)); err == nil || errors.Is(err, ErrNeedConsent) {
		t.Fatalf("Put(1 MiB) = %v, want the too-large error", err)
	}
	if b, err := s.Put(bg, key("U1", "small"), New("v")); err != nil || b != BackendKeychain {
		t.Fatalf("Put(small) after a too-large value = %q, %v; want the keychain", b, err)
	}
}

// TestKeychainNonASCII: security -w prints a value with non-ASCII bytes as
// hex; the value must still come back as written.
func TestKeychainNonASCII(t *testing.T) {
	fakeKeychain(t)
	kc := defaultOS()
	for _, v := range []string{"토큰abc", "a\nb", "tab\there", `it's "x" \ y`} {
		if err := kc.Set(bg, Service, "api/h/U1/v", v); err != nil {
			t.Fatalf("Set(%q) = %v", v, err)
		}
		if got, err := kc.Get(bg, Service, "api/h/U1/v"); err != nil || got != v {
			t.Errorf("Get = %q, %v; want %q", got, err, v)
		}
	}
}

// TestKeychainRawValue: an item holding a plain value (not written by this
// encoding) reads back as is.
func TestKeychainRawValue(t *testing.T) {
	dir := fakeKeychain(t)
	if err := os.MkdirAll(filepath.Join(dir, "items"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fakeKCItem(dir, Service, "leet/h/U1/c"), []byte("plain-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := defaultOS().Get(bg, Service, "leet/h/U1/c"); err != nil || got != "plain-value" {
		t.Fatalf("Get = %q, %v", got, err)
	}
}
