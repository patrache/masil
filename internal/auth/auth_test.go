package auth_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/ids"
)

func paths(t *testing.T) config.Paths {
	t.Helper()
	t.Setenv("MASIL_CONFIG_DIR", filepath.Join(t.TempDir(), "masil"))
	t.Setenv("MASIL_CACHE_DIR", t.TempDir())
	p, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCredentialsRoundTrip(t *testing.T) {
	p := paths(t)
	in := &auth.Credentials{
		Version:   1,
		Server:    "http://x",
		Token:     "msl_abc",
		SessionID: "01JA7AAAAAAAAAAAAAAAAAAAAA",
		UserID:    "01JA1AAAAAAAAAAAAAAAAAAAAA",
		Handle:    "minji",
		SavedAt:   time.Date(2026, 11, 5, 2, 3, 4, 567000000, time.UTC),
	}
	if err := auth.Save(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := auth.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != in.Version || out.Server != in.Server || out.Token != in.Token ||
		out.SessionID != in.SessionID || out.UserID != in.UserID || out.Handle != in.Handle ||
		!out.SavedAt.Equal(in.SavedAt) {
		t.Fatalf("round trip mismatch: got %+v", *out)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(p.Credentials)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
		}
	}
	matches, _ := filepath.Glob(filepath.Join(p.Dir, ".credentials.json.tmp-*"))
	if len(matches) != 0 {
		t.Fatalf("temp files left: %v", matches)
	}
}

func TestLoadMissingOrEmptyToken(t *testing.T) {
	p := paths(t)
	if _, err := auth.Load(p); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("missing: err = %v", err)
	}
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Credentials, []byte(`{"version":1,"token":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := auth.Load(p); !errors.Is(err, auth.ErrNoCredentials) || c != nil {
		t.Fatalf("empty token: c=%v err=%v", c, err)
	}
}

func TestLoadGarbageCredentials(t *testing.T) {
	p := paths(t)
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Credentials, []byte("{{{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := auth.Load(p)
	if !errors.Is(err, auth.ErrNoCredentials) || c != nil {
		t.Fatalf("garbage: c=%v err=%v", c, err)
	}
	if _, err := os.Stat(p.Credentials); err != nil {
		t.Fatalf("garbage file must not be deleted: %v", err)
	}
}

func TestLoadFixesPerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on windows")
	}
	p := paths(t)
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Credentials, []byte(`{"version":1,"token":"msl_x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.Credentials, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Load(p); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p.Credentials)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}
}

func TestClearAbsentIsNil(t *testing.T) {
	p := paths(t)
	if err := auth.Clear(p); err != nil {
		t.Fatalf("clear absent: %v", err)
	}
	if err := auth.Save(p, &auth.Credentials{Version: 1, Token: "msl_x"}); err != nil {
		t.Fatal(err)
	}
	if err := auth.Clear(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Credentials); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still present: %v", err)
	}
	if _, err := auth.Load(p); !errors.Is(err, auth.ErrNoCredentials) {
		t.Fatalf("after clear: %v", err)
	}
}

func TestMatchesIgnoresTrailingSlash(t *testing.T) {
	c := &auth.Credentials{Server: "https://a/"}
	if !c.Matches("https://a") || !c.Matches("https://a/") {
		t.Fatal("trailing slash on stored server")
	}
	c = &auth.Credentials{Server: "https://a"}
	if !c.Matches("https://a/") || !c.Matches("https://a") {
		t.Fatal("trailing slash on argument")
	}
	if c.Matches("https://b") || c.Matches("https://b/") {
		t.Fatal("matched a different server")
	}
	if (&auth.Credentials{}).Matches("https://a") {
		t.Fatal("empty server matched")
	}
}

func TestRedactedNeverContainsToken(t *testing.T) {
	c := &auth.Credentials{Token: "msl_Q2xhdWRlIGlzIGEgZ29vZCBib3kgYW5kIHdyaXRlcyBnb29k"}
	if c.Redacted() != "msl_…(redacted)" || strings.Contains(c.Redacted(), c.Token[4:]) {
		t.Fatal(c.Redacted())
	}
	if (&auth.Credentials{}).Redacted() != "msl_…(redacted)" {
		t.Fatal("empty token")
	}
	var nilc *auth.Credentials
	if nilc.Redacted() != "msl_…(redacted)" {
		t.Fatal("nil credentials")
	}
}

func TestDeviceIDStable(t *testing.T) {
	p := paths(t)
	id, err := auth.DeviceID(p)
	if err != nil {
		t.Fatal(err)
	}
	if !ids.Valid(id) {
		t.Fatalf("invalid id %q", id)
	}
	b, err := os.ReadFile(p.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != id+"\n" {
		t.Fatalf("file = %q, want %q", b, id+"\n")
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(p.DeviceID)
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v", st.Mode().Perm())
		}
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(p.DeviceID, old, old); err != nil {
		t.Fatal(err)
	}
	id2, err := auth.DeviceID(p)
	if err != nil {
		t.Fatal(err)
	}
	if id2 != id {
		t.Fatalf("second call %q != %q", id2, id)
	}
	st, err := os.Stat(p.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(old) {
		t.Fatalf("device_id rewritten: mtime %v", st.ModTime())
	}
}

func TestDeviceIDReplacesGarbage(t *testing.T) {
	p := paths(t)
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.DeviceID, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := auth.DeviceID(p)
	if err != nil {
		t.Fatal(err)
	}
	if !ids.Valid(id) {
		t.Fatalf("invalid id %q", id)
	}
	b, _ := os.ReadFile(p.DeviceID)
	if string(b) != id+"\n" {
		t.Fatalf("file = %q", b)
	}
}

func TestDeviceIDAcceptsTrailingWhitespace(t *testing.T) {
	p := paths(t)
	if err := config.EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	want := ids.New()
	if err := os.WriteFile(p.DeviceID, []byte("  "+want+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := auth.DeviceID(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
