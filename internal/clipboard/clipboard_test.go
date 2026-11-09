package clipboard

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// fakeHost is a host with the given env, tools on $PATH and existing files.
func fakeHost(goos string, env map[string]string, tools []string, files ...string) host {
	return host{
		goos:    goos,
		getenv:  func(k string) string { return env[k] },
		environ: func() []string { return []string{"HOME=/h", "LANG=C", "LC_ALL=POSIX", "LC_CTYPE=C", "PATH=/bin"} },
		look: func(name string) (string, error) {
			if slices.Contains(tools, name) {
				return "/usr/bin/" + name, nil
			}
			return "", fs.ErrNotExist
		},
		exists: func(p string) bool { return slices.Contains(files, p) },
		sysDir: func() string { return `C:\Windows\system32` },
	}
}

// TestCommandFor is the per-OS table: which program, which constant argv, what
// environment and what goes to stdin.
func TestCommandFor(t *testing.T) {
	const text = "안녕\n하세요 👍"
	utf16 := encodeUTF16LE("안녕\r\n하세요 👍")
	tests := []struct {
		name    string
		h       host
		want    command
		wantErr error
	}{
		{"darwin pbcopy, UTF-8 locale forced", fakeHost("darwin", nil, nil),
			command{name: "/usr/bin/pbcopy", env: []string{"HOME=/h", "PATH=/bin", "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8"}, input: []byte(text)}, nil},
		{"darwin over ssh", fakeHost("darwin", map[string]string{"SSH_CONNECTION": "1.2.3.4 5 6.7.8.9 22"}, nil), command{}, ErrRemote},
		{"linux over ssh (SSH_TTY)", fakeHost("linux", map[string]string{"SSH_TTY": "/dev/pts/1", "DISPLAY": ":0"}, []string{"xclip"}), command{}, ErrRemote},
		{"linux over ssh (SSH_CLIENT)", fakeHost("linux", map[string]string{"SSH_CLIENT": "1.2.3.4 5 22", "WAYLAND_DISPLAY": "wayland-0"}, []string{"wl-copy"}), command{}, ErrRemote},
		{"wayland wl-copy", fakeHost("linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, []string{"wl-copy", "xclip"}),
			command{name: "/usr/bin/wl-copy", input: []byte(text)}, nil},
		{"wayland without wl-copy falls back to X", fakeHost("linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, []string{"xclip"}),
			command{name: "/usr/bin/xclip", args: []string{"-selection", "clipboard", "-in"}, input: []byte(text)}, nil},
		{"x11 xclip", fakeHost("linux", map[string]string{"DISPLAY": ":0"}, []string{"xclip", "xsel"}),
			command{name: "/usr/bin/xclip", args: []string{"-selection", "clipboard", "-in"}, input: []byte(text)}, nil},
		{"x11 xsel", fakeHost("freebsd", map[string]string{"DISPLAY": ":0"}, []string{"xsel"}),
			command{name: "/usr/bin/xsel", args: []string{"--clipboard", "--input"}, input: []byte(text)}, nil},
		{"x11 without a tool", fakeHost("linux", map[string]string{"DISPLAY": ":0"}, nil), command{}, ErrUnavailable},
		{"no display", fakeHost("linux", nil, []string{"xclip", "wl-copy"}), command{}, ErrUnavailable},
		{"wsl clip.exe", fakeHost("linux", map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}, nil, wslClip),
			command{name: wslClip, input: utf16}, nil},
		{"wsl clip.exe on PATH (custom automount root)", fakeHost("linux", map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}, []string{"clip.exe"}),
			command{name: "/usr/bin/clip.exe", input: utf16}, nil},
		{"wsl prefers the mounted clip.exe", fakeHost("linux", map[string]string{"WSL_DISTRO_NAME": "Ubuntu"}, []string{"clip.exe"}, wslClip),
			command{name: wslClip, input: utf16}, nil},
		{"not wsl: clip.exe on PATH is ignored", fakeHost("linux", nil, []string{"clip.exe"}), command{}, ErrUnavailable},
		{"wsl prefers a display tool", fakeHost("linux", map[string]string{"WSL_DISTRO_NAME": "Ubuntu", "WAYLAND_DISPLAY": "wayland-0"}, []string{"wl-copy"}, wslClip),
			command{name: "/usr/bin/wl-copy", input: []byte(text)}, nil},
		{"windows clip.exe from the system dir", fakeHost("windows", nil, nil),
			command{name: `C:\Windows\system32\clip.exe`, input: utf16}, nil},
		{"windows over ssh", fakeHost("windows", map[string]string{"SSH_CLIENT": "x"}, nil), command{}, ErrRemote},
		{"windows without a system dir", func() host { h := fakeHost("windows", nil, nil); h.sysDir = func() string { return "" }; return h }(), command{}, ErrUnavailable},
		{"plan9", fakeHost("plan9", nil, nil), command{}, ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := commandFor(tt.h, text)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.name != tt.want.name || !slices.Equal(got.args, tt.want.args) || !slices.Equal(got.env, tt.want.env) || string(got.input) != string(tt.want.input) {
				t.Fatalf("got %q %q env=%q input=%q\nwant %q %q env=%q input=%q",
					got.name, got.args, got.env, got.input, tt.want.name, tt.want.args, tt.want.env, tt.want.input)
			}
			if strings.Contains(strings.Join(got.args, " "), "안녕") {
				t.Fatal("the text reached argv")
			}
		})
	}
}

func TestEncodeUTF16LE(t *testing.T) {
	got := encodeUTF16LE("가😀")
	want := []byte{0xFF, 0xFE, 0x00, 0xAC, 0x3D, 0xD8, 0x00, 0xDE}
	if !slices.Equal(got, want) {
		t.Fatalf("got % x, want % x", got, want)
	}
}

// TestSystemWriterRuns: Write builds the command for the host and runs it with
// a deadline; a failing run is an error, a remote session runs nothing.
func TestSystemWriterRuns(t *testing.T) {
	var ran []command
	var deadline bool
	run := func(ctx context.Context, c command) error {
		_, deadline = ctx.Deadline()
		ran = append(ran, c)
		return nil
	}
	w := &systemWriter{h: fakeHost("darwin", nil, nil), run: run}
	if err := w.Write("한글"); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0].name != "/usr/bin/pbcopy" || string(ran[0].input) != "한글" || !deadline {
		t.Fatalf("ran %+v deadline=%v", ran, deadline)
	}

	ran = nil
	w = &systemWriter{h: fakeHost("darwin", map[string]string{"SSH_TTY": "/dev/ttys001"}, nil), run: run}
	if err := w.Write("x"); !errors.Is(err, ErrRemote) || len(ran) != 0 {
		t.Fatalf("ssh: err=%v ran=%d", err, len(ran))
	}

	boom := errors.New("exit status 1")
	w = &systemWriter{h: fakeHost("linux", map[string]string{"DISPLAY": ":0"}, []string{"xclip"}), run: func(context.Context, command) error { return boom }}
	if err := w.Write("x"); !errors.Is(err, boom) || errors.Is(err, ErrRemote) {
		t.Fatalf("failed run: err=%v", err)
	}
}

// TestSystemWriterClamps: at most MaxBytes reach the program, cut on a rune
// boundary.
func TestSystemWriterClamps(t *testing.T) {
	var got []byte
	w := &systemWriter{h: fakeHost("darwin", nil, nil), run: func(_ context.Context, c command) error { got = c.input; return nil }}
	if err := w.Write(strings.Repeat("가", MaxBytes)); err != nil {
		t.Fatal(err)
	}
	if len(got) > MaxBytes || !utf8.Valid(got) || len(got) < MaxBytes-3 {
		t.Fatalf("len %d valid=%v", len(got), utf8.Valid(got))
	}
}

func TestClamp(t *testing.T) {
	for _, tt := range []struct {
		s    string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 2, "ab"},
		{"가나", 4, "가"},
		{"가나", 3, "가"},
		{"가나", 2, ""},
	} {
		if got := Clamp(tt.s, tt.n); got != tt.want {
			t.Errorf("Clamp(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

func TestRecorder(t *testing.T) {
	r := &Recorder{}
	if err := r.Write("a"); err != nil {
		t.Fatal(err)
	}
	r.Fail(ErrUnavailable)
	if err := r.Write("b"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if got := r.Texts(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("texts %q", got)
	}
}

// TestOnlyExecRunExecs pins the shape the tests above rely on: os/exec is
// imported by run.go only, and only execRun (and lookPath, which resolves a
// tool name) use it — so every program the package starts is a command built
// by commandFor.
func TestOnlyExecRunExecs(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v %v", files, err)
	}
	fset := token.NewFileSet()
	saw := false
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range file.Imports {
			if p := strings.Trim(im.Path.Value, `"`); (p == "os/exec" || p == "syscall") && f != "run.go" {
				t.Errorf("%s imports %s (only run.go may)", f, p)
			}
		}
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fd.Name.Name == "execRun" {
				saw = true
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "exec" && fd.Name.Name != "execRun" && (fd.Name.Name != "lookPath" || sel.Sel.Name != "LookPath") {
						t.Errorf("%s: %s uses exec.%s outside execRun", f, fd.Name.Name, sel.Sel.Name)
					}
				}
				return true
			})
		}
	}
	if !saw {
		t.Error("execRun not found")
	}
}
