package lazygit

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeRunner answers Output by "name arg arg…" keys.
type fakeRunner struct {
	out   map[string]string
	fail  map[string]error
	calls []string
}

func (f *fakeRunner) Output(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, key)
	if err, ok := f.fail[key]; ok {
		return nil, err
	}
	if out, ok := f.out[key]; ok {
		return []byte(out), nil
	}
	return nil, errors.New("exit status 128")
}

func fakeEnv(r *fakeRunner, paths map[string]string) Env {
	return Env{
		LookPath: func(name string) (string, error) {
			if p, ok := paths[name]; ok {
				return p, nil
			}
			return "", exec.ErrNotFound
		},
		Getenv: func(string) string { return "" },
		Run:    r,
		GOOS:   "darwin",
	}
}

func TestLocateConfigPathMissing(t *testing.T) {
	env := fakeEnv(&fakeRunner{}, map[string]string{"lazygit": "/usr/local/bin/lazygit"})
	if _, err := Locate(env, "/opt/none/lazygit"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("missing cfg path: err = %v, want ErrNotInstalled", err)
	}
	// The configured path wins over PATH.
	env = fakeEnv(&fakeRunner{}, map[string]string{"lazygit": "/usr/local/bin/lazygit", "/opt/lg/lazygit": "/opt/lg/lazygit"})
	if got, err := Locate(env, "/opt/lg/lazygit"); err != nil || got != "/opt/lg/lazygit" {
		t.Fatalf("cfg path: %q, %v", got, err)
	}
	if got, err := Locate(env, ""); err != nil || got != "/usr/local/bin/lazygit" {
		t.Fatalf("PATH: %q, %v", got, err)
	}
	env = fakeEnv(&fakeRunner{}, nil)
	if _, err := Locate(env, ""); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("not on PATH: err = %v", err)
	}
}

func TestVersionParse(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		"/bin/lg --version": "commit=abc, build date=2026-11-07T00:00:00Z, build source=homebrew, version=0.66.3, os=darwin, arch=arm64, git version=2.47.1\n",
	}}
	got, err := Version(context.Background(), fakeEnv(r, nil), "/bin/lg")
	if err != nil || got != "0.66.3" {
		t.Fatalf("Version = %q, %v; want 0.66.3", got, err)
	}
	r.out["/bin/lg --version"] = "commit=abc, build date=…, version=0.66.3, os=darwin"
	if got, _ := Version(context.Background(), fakeEnv(r, nil), "/bin/lg"); got != "0.66.3" {
		t.Fatalf("plan sample: %q", got)
	}
	for _, bad := range []string{"", "lazygit", "version=\x1b[31m1.0", "git version=2.47.1"} {
		r.out["/bin/lg --version"] = bad
		if got, err := Version(context.Background(), fakeEnv(r, nil), "/bin/lg"); err == nil {
			t.Errorf("Version(%q) = %q, want error", bad, got)
		}
	}
	r.fail = map[string]error{"/bin/lg --version": errors.New("boom")}
	if _, err := Version(context.Background(), fakeEnv(r, nil), "/bin/lg"); err == nil {
		t.Error("run failure: want error")
	}
}

func TestInstallHintPerOS(t *testing.T) {
	cases := map[string][]string{
		"darwin":  {"brew install lazygit"},
		"windows": {"winget install -e --id=JesseDuffield.lazygit", "scoop install lazygit"},
		"linux":   {"https://github.com/jesseduffield/lazygit#installation"},
		"freebsd": {"https://github.com/jesseduffield/lazygit#installation"},
	}
	for goos, want := range cases {
		if got := InstallHint(goos); !slices.Equal(got, want) {
			t.Errorf("InstallHint(%q) = %q, want %q", goos, got, want)
		}
	}
	if TestedVersion != "0.66.3" {
		t.Errorf("TestedVersion = %q", TestedVersion)
	}
}

// The context bounds the real runner even when a child keeps its output
// open (a wrapper script that leaves a process behind).
func TestSystemRunTimeoutBoundsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := SystemEnv().Run.Output(ctx, "", "sh", "-c", "sleep 5; true")
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Output returned after %v, want about the timeout", d)
	}
	if err == nil {
		t.Fatal("no error after the timeout")
	}
}
