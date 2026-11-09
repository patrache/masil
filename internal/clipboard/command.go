package clipboard

import (
	"strings"
	"unicode/utf16"
)

// The builder below is pure and compiled on every OS, so the tests check each
// platform's command from any machine.

const (
	darwinPbcopy = "/usr/bin/pbcopy" // absolute: $PATH cannot swap it
	// wslClip is Windows' clip.exe as WSL mounts it by default (else
	// clip.exe is looked up on $PATH).
	wslClip = "/mnt/c/Windows/System32/clip.exe"
	// wslInterop exists under WSL (with WSL_DISTRO_NAME, the detection).
	wslInterop = "/proc/sys/fs/binfmt_misc/WSLInterop"
)

// utf8Locale is forced on pbcopy: it decodes stdin by the locale, and a C /
// POSIX / unset locale (launchd, cron, a minimal shell) turns Hangul into
// mojibake.
var utf8Locale = []string{"LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8"}

// command is one program run: name, constant argv, environment (nil = this
// process's) and the bytes for its stdin.
type command struct {
	name  string
	args  []string
	env   []string
	input []byte
}

// host is what commandFor reads from the machine (System fills it with the
// real calls, tests with fakes).
type host struct {
	goos    string
	getenv  func(string) string
	environ func() []string
	look    func(name string) (string, error) // $PATH lookup
	exists  func(path string) bool
	sysDir  func() string // Windows system directory ("" = unknown)
}

// remote reports an SSH session: a local program would fill the clipboard of
// the machine masil runs on, not the one the user sits at.
func remote(h host) bool {
	for _, k := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if h.getenv(k) != "" {
			return true
		}
	}
	return false
}

// commandFor is the clipboard program for h with text on stdin:
//   - SSH: ErrRemote (OSC 52 only);
//   - darwin: /usr/bin/pbcopy with a UTF-8 locale;
//   - windows: clip.exe from the system directory, UTF-16LE with a BOM;
//   - other unixes: wl-copy (Wayland), else xclip / xsel (X11), else WSL's
//     clip.exe (/mnt/c, else on $PATH); ErrUnavailable when none applies.
func commandFor(h host, text string) (command, error) {
	if remote(h) {
		return command{}, ErrRemote
	}
	switch h.goos {
	case "darwin":
		return command{name: darwinPbcopy, env: localeEnv(h.environ()), input: []byte(text)}, nil
	case "windows":
		dir := h.sysDir()
		if dir == "" {
			return command{}, ErrUnavailable
		}
		return command{name: strings.TrimRight(dir, `\`) + `\clip.exe`, input: windowsText(text)}, nil
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "illumos":
		return unixCommand(h, text)
	}
	return command{}, ErrUnavailable
}

func unixCommand(h host, text string) (command, error) {
	if h.getenv("WAYLAND_DISPLAY") != "" {
		if p, err := h.look("wl-copy"); err == nil {
			return command{name: p, input: []byte(text)}, nil
		}
	}
	if h.getenv("DISPLAY") != "" {
		if p, err := h.look("xclip"); err == nil {
			return command{name: p, args: []string{"-selection", "clipboard", "-in"}, input: []byte(text)}, nil
		}
		if p, err := h.look("xsel"); err == nil {
			return command{name: p, args: []string{"--clipboard", "--input"}, input: []byte(text)}, nil
		}
	}
	if h.goos == "linux" && (h.getenv("WSL_DISTRO_NAME") != "" || h.exists(wslInterop)) {
		if h.exists(wslClip) {
			return command{name: wslClip, input: windowsText(text)}, nil
		}
		// another automount root or system drive: WSL appends the Windows
		// PATH by default
		if p, err := h.look("clip.exe"); err == nil {
			return command{name: p, input: windowsText(text)}, nil
		}
	}
	return command{}, ErrUnavailable
}

// localeEnv is env without its locale variables plus utf8Locale.
func localeEnv(env []string) []string {
	out := make([]string, 0, len(env)+len(utf8Locale))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == "LANG" || k == "LC_ALL" || k == "LC_CTYPE" {
			continue
		}
		out = append(out, kv)
	}
	return append(out, utf8Locale...)
}

// windowsText is text as clip.exe reads it unambiguously: CRLF line ends,
// UTF-16LE with a byte order mark (without one it decodes by the console code
// page).
func windowsText(text string) []byte {
	return encodeUTF16LE(strings.ReplaceAll(text, "\n", "\r\n"))
}

// encodeUTF16LE is FF FE followed by s in UTF-16 little endian.
func encodeUTF16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2+2*len(units))
	b = append(b, 0xFF, 0xFE)
	for _, u := range units {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}
