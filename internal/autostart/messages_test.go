package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unicode"

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

// Every autostart error keeps its 0.1.2 Korean sentence (err.Error() and
// Message in ko), and Message in en has no Korean in it.
func TestAutostartMessages(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone", "masil")
	_, statErr := os.Stat(missing)
	temp := "/var/folders/x/T/go-build1234/b001/exe/masil"
	t.Setenv("MASIL_CONFIG_DIR", dir)
	runKeyErr := runKeyInstall(mapRunKey{}, writeExe(t))

	cases := []struct {
		name string
		err  error
		ko   string
	}{
		{"not_absolute", checkExe("bin/masil"), "자동 시작에 등록할 실행 파일 경로가 절대 경로가 아닙니다: bin/masil"},
		{"missing", checkExe(missing), "자동 시작에 등록할 실행 파일을 찾지 못했습니다: " + missing + ": " + statErr.Error()},
		{"not_regular", checkExe(dir), "자동 시작에 등록할 실행 파일이 일반 파일이 아닙니다: " + dir},
		{"temporary", stableErr(temp), "임시 빌드 실행 파일(" + temp + ")은 자동 시작에 등록할 수 없습니다 — 'go run' 대신 설치한 masil 로 다시 실행하세요"},
		{"runkey_config_dir", runKeyErr, "Windows 자동 시작은 MASIL_CONFIG_DIR 를 지원하지 않습니다"},
		{"no_systemd", ErrNoSystemd, "systemd 사용자 세션을 찾지 못했습니다 — 'masil daemon run' 을 셸 시작 파일에 넣어 실행하세요"},
		{"no_systemd_cause", noSystemd(errors.New("exit status 1")), "systemd 사용자 세션을 찾지 못했습니다 — 'masil daemon run' 을 셸 시작 파일에 넣어 실행하세요 (exit status 1)"},
	}
	en := i18n.For(i18n.En)
	for _, c := range cases {
		if c.err == nil {
			t.Fatalf("%s: no error", c.name)
		}
		if got := c.err.Error(); got != c.ko {
			t.Errorf("%s: Error() = %q, want %q", c.name, got, c.ko)
		}
		if got := Message(i18n.KoT(), c.err); got != c.ko {
			t.Errorf("%s: Message(ko) = %q, want %q", c.name, got, c.ko)
		}
		if got := Message(nil, c.err); got != c.ko {
			t.Errorf("%s: Message(nil) = %q, want ko", c.name, got)
		}
		if got := Message(en, c.err); hasHangul(got) || got == "" {
			t.Errorf("%s: Message(en) = %q, want English", c.name, got)
		}
	}

	var ee *ExeError
	if !errors.As(cases[1].err, &ee) || ee.Kind != ExeMissing || ee.Exe != missing || !errors.Is(cases[1].err, os.ErrNotExist) {
		t.Fatalf("missing: %#v, want *ExeError{ExeMissing} wrapping the stat error", cases[1].err)
	}
	if !errors.Is(runKeyErr, ErrRunKeyConfigDir) || !errors.Is(cases[6].err, ErrNoSystemd) {
		t.Fatal("sentinels must match with errors.Is")
	}
	// An error autostart does not know passes through as it is.
	if got := Message(en, errors.New("autostart: set x: denied")); got != "autostart: set x: denied" {
		t.Fatalf("other error: %q", got)
	}
}

// Query reports a vanished executable in Status.ExeMissing; Detail is only the
// registration's location (the CLI renders the note in its language).
func TestExeMissingField(t *testing.T) {
	st := Status{Detail: "launchd /x.plist"}
	withExeCheck(&st, "/nope/masil")
	if st.Detail != "launchd /x.plist" || st.ExeMissing != "/nope/masil" {
		t.Fatalf("missing exe: %+v", st)
	}
	exe := writeExe(t)
	st = Status{Detail: "d"}
	withExeCheck(&st, exe)
	if st.ExeMissing != "" || st.ExeUnknown {
		t.Fatalf("present exe: %+v", st)
	}
	// A registration naming no executable (broken plist/unit/value): gone,
	// with no path to show (0.1.2 printed "… · 실행 파일 없음: " — review).
	st = Status{Detail: "d"}
	withExeCheck(&st, "")
	if st.ExeMissing != "" || !st.ExeUnknown {
		t.Fatalf("unnamed exe: %+v, want ExeMissing \"\" and ExeUnknown", st)
	}
}

func stableErr(exe string) error {
	_, err := StableExecutable(exe)
	return err
}
