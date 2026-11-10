package main

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/patrache/masil/internal/autostart"
	"github.com/patrache/masil/internal/daemon"
	"github.com/patrache/masil/internal/i18n"
)

func hasHangul(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// cliCase is one `masil …` run and what it prints.
type cliCase struct {
	args           []string
	code           int
	stdout, stderr string // exact text ("" = must be empty) unless prefix is set
	prefix         bool   // stdout is only a prefix
}

// The Korean CLI text is the 0.1.2 text, byte for byte (the catalog move
// changes no sentence).
func TestCLIOutputsKOUnchanged(t *testing.T) {
	t.Setenv("MASIL_LANG", "")
	const (
		uLogin   = "사용법: masil login [--handle <아이디> --password-stdin] — 로그인해요\n"
		uJoin    = "사용법: masil join <초대 코드> — 초대받은 방에 들어가요\n"
		uVersion = "사용법: masil version — 버전을 보여 줘요\n"
		uConfig  = "사용법: masil config path — 설정 폴더 위치를 보여 줘요\n"
		uDaemon  = "사용법: masil daemon run|install|uninstall|status\n"
	)
	help := "masil — 터미널에서 마실 가요\n\n명령:\n" +
		"  masil [tui] — 터미널 마실을 열어요\n" +
		"  masil login [--handle <아이디> --password-stdin] — 로그인해요\n" +
		"  masil signup — 가입해요\n" +
		"  masil logout — 이 기기에서 로그아웃해요\n" +
		"  masil join <초대 코드> — 초대받은 방에 들어가요\n" +
		"  masil version — 버전을 보여 줘요\n" +
		"  masil config path — 설정 폴더 위치를 보여 줘요\n" +
		"  masil help — 도움말\n" +
		"  masil daemon run|install|uninstall|status — 알림 데몬 (실행 · 자동 시작 등록/해제 · 상태)\n"
	cases := []cliCase{
		{args: []string{"help"}, stdout: help, prefix: true},
		{args: []string{"nope"}, code: 2, stderr: "알 수 없는 명령이에요: nope\n`masil help` 로 명령 목록을 볼 수 있어요\n"},
		{args: []string{"join"}, code: 2, stderr: "초대 코드 하나를 적어 주세요\n" + uJoin},
		{args: []string{"version", "x", "y"}, code: 2, stderr: "필요 없는 인자가 있어요: x y\n" + uVersion},
		{args: []string{"config"}, code: 2, stderr: uConfig},
		{args: []string{"login", "--bogus"}, code: 2, stderr: "인자가 잘못됐어요: flag provided but not defined: -bogus\n" + uLogin},
		{args: []string{"login", "-h"}, stdout: uLogin},
		{args: []string{"login", "x"}, code: 2, stderr: "필요 없는 인자가 있어요: x\n" + uLogin},
		{args: []string{"login", "--handle", "sam"}, code: 2, stderr: "--handle 과 --password-stdin 은 함께 써요\n" + uLogin},
		{args: []string{"login", "--handle", "@", "--password-stdin"}, code: 2, stderr: "아이디를 적어 주세요\n" + uLogin},
		{args: []string{"login", "--handle", "sam", "--password-stdin"}, code: 2, stderr: "stdin 으로 비밀번호를 넣어 주세요\n" + uLogin},
		{args: []string{"logout"}, stdout: "로그인돼 있지 않아요\n"},
		{args: []string{"daemon"}, code: 2, stderr: uDaemon},
		{args: []string{"daemon", "--help"}, stdout: uDaemon},
		{args: []string{"daemon", "run", "-h"}, stdout: "사용법: masil daemon run [--foreground] [--verbose]\n"},
		{args: []string{"daemon", "uninstall"}, stdout: "자동 시작을 해제했습니다.\n"},
	}
	stubDaemonFns(t)
	daemonGOOS = "darwin"
	for _, c := range cases {
		e := newEnv(t, "https://masil.test")
		checkCLI(t, e, c)
	}

	// install: the registered location, then the not-logged-in note.
	e := newEnv(t, "https://masil.test")
	checkCLI(t, e, cliCase{args: []string{"daemon", "install"},
		stdout: "자동 시작을 등록했습니다: launchd /fake/com.patrache.masil.daemon.plist\n" +
			"아직 로그인하지 않았습니다 — 'masil login' 뒤부터 알림이 옵니다.\n", prefix: true})
}

func checkCLI(t *testing.T, e *testEnv, c cliCase) {
	t.Helper()
	code := e.run(c.args...)
	out, errOut := e.out.String(), e.err.String()
	okOut := out == c.stdout || (c.prefix && strings.HasPrefix(out, c.stdout))
	if code != c.code || !okOut || errOut != c.stderr {
		t.Errorf("masil %s: exit %d (want %d)\nstdout %q\nwant   %q\nstderr %q\nwant   %q",
			strings.Join(c.args, " "), code, c.code, out, c.stdout, errOut, c.stderr)
	}
}

// MASIL_LANG=en (main's localize) turns every non-admin CLI text English.
func TestCLIEnglishNoHangul(t *testing.T) {
	stubDaemonFns(t)
	runs := [][]string{
		{"help"}, {"-h"}, {"nope"}, {"join"}, {"version", "x"}, {"config"},
		{"login", "--bogus"}, {"login", "-h"}, {"login", "x"}, {"login", "--handle", "sam"},
		{"login", "--handle", "@", "--password-stdin"}, {"login", "--handle", "sam", "--password-stdin"},
		{"logout"}, {"tui", "x"}, {"signup", "x"}, {"workspace"},
		{"daemon"}, {"daemon", "-h"}, {"daemon", "bogus"}, {"daemon", "run", "-h"}, {"daemon", "status", "x"},
		{"daemon", "install"}, {"daemon", "uninstall"}, {"daemon", "status"},
	}
	for _, args := range runs {
		t.Setenv("MASIL_LANG", "en")
		e := newEnv(t, "https://masil.test")
		localize(e.Env)
		if e.T == nil || e.Lang.Effective != i18n.En || e.Lang.Source != i18n.SourceEnv {
			t.Fatalf("localize: %+v", e.Lang)
		}
		e.run(args...)
		out := e.out.String() + e.err.String()
		if out == "" || hasHangul(out) {
			t.Errorf("masil %s (en):\n%s", strings.Join(args, " "), out)
		}
	}
	e := newEnv(t, "https://masil.test")
	t.Setenv("MASIL_LANG", "en")
	localize(e.Env)
	if h := helpText(e.T); !strings.Contains(h, "masil daemon run|install|uninstall|status — ") || !strings.Contains(h, "masil join <invite code>") {
		t.Fatalf("help (en):\n%s", h)
	}

	// Every branch of `masil daemon status`, on a made-up report.
	en := i18n.For(i18n.En)
	now := time.Date(2026, 11, 5, 2, 5, 0, 0, time.UTC)
	st := &daemon.State{Version: 1, LastSeq: 7, SavedAt: now.Add(-time.Minute)}
	ok := daemon.Report{Running: true, LoggedIn: true, Handle: "sam", CredServer: "http://a", ConfigServer: "http://a", DesktopEnabled: true, State: st}
	bad := daemon.Report{CredServer: "http://b", ConfigServer: "http://a", ServerMismatch: true, ShellServer: "http://c", Problems: []string{"level=WARN msg=x"}}
	for i, c := range []struct {
		r     daemon.Report
		as    autostart.Status
		asErr error
	}{
		{ok, autostart.Status{Installed: true, Detail: "launchd /x.plist"}, nil},
		{ok, autostart.Status{Installed: true, Detail: "launchd /x.plist", ExeMissing: "/old/masil"}, nil},
		{bad, autostart.Status{}, nil},
		{bad, autostart.Status{}, autostart.ErrUnsupported},
		{bad, autostart.Status{}, errors.New("launchctl: exit 1")},
	} {
		var b strings.Builder
		renderStatus(&b, en, c.r, c.as, c.asErr, now, time.UTC)
		if hasHangul(b.String()) {
			t.Errorf("status %d (en):\n%s", i, b.String())
		}
	}
}

// The status label column is the widest label + 2 in every language (ko: 11).
func TestStatusLabelWidth(t *testing.T) {
	if w := statusLabelWidth(i18n.KoT()); w != 11 {
		t.Fatalf("ko label width = %d, want 11", w)
	}
	if w := statusLabelWidth(i18n.For(i18n.En)); w != len("Autostart")+2 {
		t.Fatalf("en label width = %d", w)
	}
}
