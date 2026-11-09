package secrets

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ebitengine/purego"
)

// keychainITEnv turns on TestKeychainIntegration, which writes to the login
// keychain; plain `go test ./...` never touches it.
const keychainITEnv = "MASIL_KEYCHAIN_IT"

func TestKeychainIsDefault(t *testing.T) {
	b := defaultOS()
	if b == nil || b.Name() != BackendKeychain {
		t.Fatalf("defaultOS = %v", b)
	}
}

// TestSecFrameworksLoad: Security and CoreFoundation open in process (no cgo)
// and every constant resolves.
func TestSecFrameworksLoad(t *testing.T) {
	l, err := loadSec()
	if err != nil {
		t.Fatal(err)
	}
	for n := range kcNames {
		if l.consts[n] == 0 {
			t.Errorf("%s did not resolve", kcSymbols[n].name)
		}
	}
	if l.keyCallBacks == 0 || l.valueCallBacks == 0 {
		t.Error("the dictionary callbacks did not resolve")
	}
}

// TestSecDict: a pair list becomes a CFDictionary with every pair (a value
// with NUL bytes included) that the caller alone owns; a name that is not
// UTF-8 is refused before any SecItem call, and not as ErrUnavailable.
func TestSecDict(t *testing.T) {
	l, err := loadSec()
	if err != nil {
		t.Fatal(err)
	}
	var count func(d uintptr) int
	purego.RegisterLibFunc(&count, l.cf, "CFDictionaryGetCount")
	var retain func(d uintptr) int
	purego.RegisterLibFunc(&retain, l.cf, "CFGetRetainCount")

	pairs := kcAddAttrs("masil", "a/b/c/d", []byte("v\x00\n'\""))
	d, err := l.dict(pairs)
	if err != nil {
		t.Fatal(err)
	}
	if n := count(d); n != len(pairs) {
		t.Errorf("CFDictionaryGetCount = %d, want %d", n, len(pairs))
	}
	if n := retain(d); n != 1 {
		t.Errorf("CFGetRetainCount = %d, want 1 (only the caller's reference)", n)
	}
	l.CFRelease(d)

	if _, err := l.dict(kcItem("masil", "a/b/\xff")); err == nil || errors.Is(err, ErrUnavailable) {
		t.Errorf("dict(non-UTF-8 name) = %v, want an error that is not ErrUnavailable", err)
	}
}

// TestKeychainIntegration round-trips real items through the login keychain.
// It runs only with MASIL_KEYCHAIN_IT=1, under a service name of its own, and
// deletes what it wrote:
//
//	MASIL_KEYCHAIN_IT=1 go test -count=1 -run TestKeychainIntegration -v ./internal/secrets
//
// With MASIL_KEYCHAIN_IT_CLI=1 it also asks /usr/bin/security for the value:
// the item must not hand it over silently (the keychain shows its access
// prompt to the tool instead; the test gives up after a few seconds).
func TestKeychainIntegration(t *testing.T) {
	if os.Getenv(keychainITEnv) != "1" {
		t.Skip("set " + keychainITEnv + "=1 to write to the login keychain")
	}
	service := "masil-it-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	acct := "it/masil.example/U1/value"
	kc := defaultOS()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		_ = kc.Delete(context.Background(), service, acct)
	})

	if _, err := kc.Get(ctx, service, acct); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before Set = %v, want ErrNotFound", err)
	}
	big := strings.Repeat("eyJhbGciOiJIUzI1NiJ9.", 64<<10/21+1)[:64<<10]
	for _, v := range []string{"first", "a\nb\r'\"\\\x00z 토큰", big, "last"} {
		if err := kc.Set(ctx, service, acct, v); err != nil {
			t.Fatalf("Set(%d bytes) = %v", len(v), err)
		}
		got, err := kc.Get(ctx, service, acct)
		if err != nil || got != v {
			t.Fatalf("Get = %d bytes, %v; want %d bytes", len(got), err, len(v))
		}
	}

	if os.Getenv(keychainITEnv+"_CLI") == "1" {
		cli, stop := context.WithTimeout(ctx, 5*time.Second)
		out, err := exec.CommandContext(cli, "/usr/bin/security", "find-generic-password", "-s", service, "-a", acct, "-w").Output()
		stop()
		if err == nil && strings.Contains(string(out), "last") {
			t.Error("/usr/bin/security read the value without a prompt")
		} else {
			t.Logf("/usr/bin/security did not get the value (%v)", err)
		}
	}

	if err := kc.Delete(ctx, service, acct); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if _, err := kc.Get(ctx, service, acct); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := kc.Delete(ctx, service, acct); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete again = %v, want ErrNotFound", err)
	}
}
