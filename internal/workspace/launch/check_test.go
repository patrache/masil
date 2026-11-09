package launch

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCheckClaude(t *testing.T) {
	ok := map[string]string{
		"claude":                                  "manual",
		"claude --permission-mode plan":           "plan",
		"claude --permission-mode=manual":         "manual",
		"/usr/local/bin/claude -c":                "manual",
		"claude -p fix-the-dangerously-named-bug": "manual",
	}
	for cmd, mode := range ok {
		v := Check(strings.Fields(cmd))
		if !v.Allowed || v.Reason != ReasonOK || v.Mode != mode {
			t.Errorf("Check(%q) = %+v, want allowed mode %q", cmd, v, mode)
		}
		if len(v.Warnings) != 0 {
			t.Errorf("Check(%q) warnings = %v", cmd, v.Warnings)
		}
	}
	bad := map[string]Reason{
		"claude --dangerously-skip-permissions":                             ReasonBypassFlag,
		"claude --allow-dangerously-skip-permissions":                       ReasonBypassFlag,
		"claude --some-new-dangerously-flag=1":                              ReasonBypassFlag,
		"claude --permission-mode bypassPermissions":                        ReasonModeNotAllowed,
		"claude --permission-mode=auto":                                     ReasonModeNotAllowed,
		"claude --permission-mode dontAsk":                                  ReasonModeNotAllowed,
		"claude --permission-mode future-mode":                              ReasonModeNotAllowed,
		"claude --permission-mode":                                          ReasonModeNotAllowed,
		"claude --permission-mode plan --permission-mode bypassPermissions": ReasonModeNotAllowed,
		"claude.exe --dangerously-skip-permissions":                         ReasonBypassFlag,
	}
	for cmd, want := range bad {
		v := Check(strings.Fields(cmd))
		if v.Allowed || v.Reason != want {
			t.Errorf("Check(%q) = %+v, want blocked with %v", cmd, v, want)
		}
	}
	if v := Check([]string{"claude", "--permission-mode", "acceptEdits"}); !v.Allowed || v.Mode != "acceptEdits" || !slices.Contains(v.Warnings, WarnAcceptEdits) {
		t.Errorf("acceptEdits = %+v", v)
	}
	if v := Check([]string{"zsh"}); !v.Allowed || !slices.Contains(v.Warnings, WarnShell) {
		t.Errorf("zsh = %+v", v)
	}
	if v := Check([]string{"/bin/bash", "-l"}); !v.Allowed || !slices.Contains(v.Warnings, WarnShell) {
		t.Errorf("bash = %+v", v)
	}
	if v := Check([]string{"python3", "x.py"}); !v.Allowed || !slices.Contains(v.Warnings, WarnUnknownProgram) {
		t.Errorf("python3 = %+v", v)
	}
	if v := Check(nil); v.Allowed {
		t.Errorf("empty argv allowed: %+v", v)
	}
}

func TestPrograms(t *testing.T) {
	have := map[string]bool{"claude": true}
	look := func(name string) (string, error) {
		if have[name] {
			return "/opt/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	ps := Programs("/bin/zsh", look)
	if len(ps) != 3 || ps[0].Name != "zsh" || ps[0].Argv[0] != "/bin/zsh" || ps[1].Name != "claude" || !ps[2].Custom() {
		t.Errorf("Programs = %+v", ps)
	}
	have["nvim"] = true
	ps = Programs("", look)
	if len(ps) != 4 || ps[0].Name != "sh" || ps[2].Name != "nvim" || !ps[3].Custom() || ps[0].Custom() {
		t.Errorf("Programs(no shell) = %+v", ps)
	}
}

// A user --settings (file or inline JSON) can set permissions.defaultMode to
// a mode outside the allowlist, and two --settings are not known to combine,
// so it blocks; masil's own temp file from Prepare does not.
func TestCheckUserSettings(t *testing.T) {
	for _, argv := range [][]string{
		{"claude", "--settings", `{"permissions":{"defaultMode":"dontAsk"}}`},
		{"claude", `--settings={"permissions":{"defaultMode":"auto"}}`},
		{"claude", "--settings", "my.json", "--permission-mode", "plan"},
		{"claude", "--settings"},
	} {
		if v := Check(argv); v.Allowed || v.Reason != ReasonUserSettings {
			t.Errorf("Check(%q) = %+v, want blocked with ReasonUserSettings", argv, v)
		}
	}
	out, cleanup, err := Prepare([]string{"claude", "-c"}, "http://x", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if v := Check(out); !v.Allowed || v.Mode != "manual" {
		t.Errorf("Check(Prepare output %q) = %+v, want allowed manual", out, v)
	}
}

// Prepare pins the checked mode on the command line so no settings source
// (user, project, local) can start claude in another default mode.
func TestPreparePinsMode(t *testing.T) {
	out, cleanup, err := Prepare([]string{"claude", "-c"}, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if i := slices.Index(out, "--permission-mode"); i < 0 || i+1 >= len(out) || out[i+1] != "manual" {
		t.Errorf("out = %v, want --permission-mode manual", out)
	}
	out2, cleanup2, err := Prepare([]string{"claude", "--permission-mode=plan"}, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	if slices.Contains(out2, "--permission-mode") {
		t.Errorf("out = %v, the user's own mode flag is kept as is", out2)
	}
}
