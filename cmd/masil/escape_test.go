package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/daemon"
)

// hostileEnvelope is an error body whose code, message and fields try to write
// escape sequences (screen clear, OSC 52 clipboard, OSC 0 title, C1 CSI) and
// bidi controls, invisible format characters and line separators that make
// the line read differently ("\u202e"+"txt.exe" reads as "exe.txt",
// pending-fixes 1).
var hostileEnvelope = map[string]any{"error": map[string]any{
	"code":    "x\x1b[2J\x1b]52;c;ZXZpbA==\x07\u202etxt.exe\u2067역\u2069\u200b\ufeff\u00ad\u2028",
	"message": "\x1b]0;PWNED\x07\u009b31m\u202e\u2029",
	"details": map[string]any{"fields": map[string]string{"\x1b[2J\u202e\u2066": "\x1b]52;c;ZXZpbA==\x07\u200e\u200f\u061c\u2060\U000e0041"}},
}}

// assertNoEscapes fails if out holds ESC, BEL, a C1 control, a format
// character (Cf: bidi, zero-width, BOM, soft hyphen, tags) or U+2028/U+2029.
func assertNoEscapes(t *testing.T, what, out string) {
	t.Helper()
	for _, r := range out {
		if r != '\n' && (r < 0x20 || r >= 0x7f && r <= 0x9f || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)) {
			t.Fatalf("%s carries control or format rune %U: %q", what, r, out)
		}
	}
}

// TestCLIServerErrorsSanitized (pending-fixes 0-b): server-supplied error text
// printed by login/logout never reaches stderr with escape sequences.
func TestCLIServerErrorsSanitized(t *testing.T) {
	for _, status := range []int{400, 500} {
		srv := apitest.New(t)
		srv.JSON("POST /v1/auth/login", status, hostileEnvelope)
		srv.JSON("POST /v1/auth/logout", status, hostileEnvelope)
		e := newEnv(t, srv.URL)
		e.Stdin = strings.NewReader("pw\n")
		if code := e.run("login", "--handle", "sam", "--password-stdin"); code != 1 {
			t.Fatalf("login => %d", code)
		}
		assertNoEscapes(t, "login stderr", e.err.String())
		if e.err.Len() == 0 {
			t.Fatal("login: no error shown")
		}

		e.err.Reset()
		saveCreds(t, e.Paths, srv.URL)
		if code := e.run("logout"); code != 0 {
			t.Fatalf("logout => %d", code)
		}
		assertNoEscapes(t, "logout stderr", e.err.String())
		if !strings.Contains(e.err.String(), "서버에 로그아웃을 알리지 못했어요") {
			t.Fatalf("logout stderr = %q", e.err.String())
		}
	}
}

// TestDaemonRunErrorSanitized: the daemon's fatal error is printed as text.
func TestDaemonRunErrorSanitized(t *testing.T) {
	stubDaemonFns(t)
	daemonRunFn = func(context.Context, daemon.Options) error {
		return errors.New("boom \x1b]52;c;ZXZpbA==\x07\x1b[2J\u009b31m\u202e\u200b\u2028")
	}
	env, _, stderr := daemonTestEnv(t)
	if err := daemonCmd(t.Context(), env, []string{"run", "--foreground"}); err == nil {
		t.Fatal("want error")
	}
	assertNoEscapes(t, "daemon stderr", stderr.String())
	if !strings.Contains(stderr.String(), "데몬이 오류로 멈췄습니다: boom") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
