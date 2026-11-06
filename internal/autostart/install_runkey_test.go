package autostart

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// The Run-key backend is OS-independent code over runKeyStore (only install_windows.go wires it
// to the real registry), so these tests run on every OS.

// mapRunKey is a fake runKeyStore; missing values report fs.ErrNotExist like registry.ErrNotExist.
type mapRunKey map[string]string

func (m mapRunKey) Get(name string) (string, error) {
	v, ok := m[name]
	if !ok {
		return "", fs.ErrNotExist
	}
	return v, nil
}

func (m mapRunKey) Set(name, value string) error { m[name] = value; return nil }

func (m mapRunKey) Delete(name string) error {
	if _, ok := m[name]; !ok {
		return fs.ErrNotExist
	}
	delete(m, name)
	return nil
}

// brokenRunKey fails every call with a non-"not exist" error.
type brokenRunKey struct{}

var errAccess = errors.New("access is denied")

func (brokenRunKey) Get(string) (string, error) { return "", errAccess }
func (brokenRunKey) Set(string, string) error   { return errAccess }
func (brokenRunKey) Delete(string) error        { return errAccess }

func TestInstallWindows(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	keys := mapRunKey{}
	exe := writeExe(t)
	if err := runKeyInstall(keys, exe); err != nil {
		t.Fatal(err)
	}
	if got, want := keys["masil-daemon"], `"`+exe+`" daemon run`; got != want {
		t.Fatalf("Run value = %q, want %q", got, want)
	}
	if err := runKeyInstall(keys, exe); err != nil || len(keys) != 1 {
		t.Fatalf("reinstall must be idempotent: %v, %v", err, keys)
	}
	if err := runKeyInstall(brokenRunKey{}, exe); err == nil || !errors.Is(err, errAccess) {
		t.Fatalf("registry error must surface: %v", err)
	}
}

func TestInstallWindowsRejectsConfigDir(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", t.TempDir())
	keys := mapRunKey{}
	err := runKeyInstall(keys, writeExe(t))
	if err == nil || !strings.Contains(err.Error(), "Windows 자동 시작은 MASIL_CONFIG_DIR 를 지원하지 않습니다") {
		t.Fatalf("err = %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("value written: %v", keys)
	}
}

func TestQueryWindows(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	const detail = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\masil-daemon`
	keys := mapRunKey{}
	st, err := runKeyQuery(keys)
	if err != nil || st.Installed || st.Running || st.Detail != detail {
		t.Fatalf("not installed: %+v, %v", st, err)
	}
	exe := writeExe(t)
	if err := runKeyInstall(keys, exe); err != nil {
		t.Fatal(err)
	}
	st, err = runKeyQuery(keys)
	if err != nil || !st.Installed || st.Running || st.Detail != detail {
		t.Fatalf("installed: %+v, %v", st, err)
	}
	os.Remove(exe)
	st, err = runKeyQuery(keys)
	if err != nil || !st.Installed || st.Detail != detail+" · 실행 파일 없음: "+exe {
		t.Fatalf("missing exe: %+v, %v", st, err)
	}
	if _, err := runKeyQuery(brokenRunKey{}); !errors.Is(err, errAccess) {
		t.Fatalf("registry error must surface: %v", err)
	}
}

func TestUninstallWindows(t *testing.T) {
	t.Setenv("MASIL_CONFIG_DIR", "")
	keys := mapRunKey{"other-app": `"C:\x.exe"`}
	if err := runKeyInstall(keys, writeExe(t)); err != nil {
		t.Fatal(err)
	}
	if err := runKeyUninstall(keys); err != nil {
		t.Fatal(err)
	}
	if _, ok := keys["masil-daemon"]; ok || keys["other-app"] == "" {
		t.Fatalf("keys after uninstall: %v", keys)
	}
	if err := runKeyUninstall(keys); err != nil {
		t.Fatal("second uninstall must be a no-op:", err)
	}
	if err := runKeyUninstall(brokenRunKey{}); !errors.Is(err, errAccess) {
		t.Fatalf("registry error must surface: %v", err)
	}
}
