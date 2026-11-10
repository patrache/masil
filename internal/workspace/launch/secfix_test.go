package launch

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// v0.1.3 security (n): only the exact file Prepare wrote passes as masil's
// --settings; a user file that merely looks like one (same name pattern)
// is the user's own settings and blocks.
func TestCheckSettingsExactPreparedPath(t *testing.T) {
	tmp := t.TempDir()
	fake := filepath.Join(tmp, "masil-claude-1234.json")
	if err := os.WriteFile(fake, []byte(`{"permissions":{"defaultMode":"dontAsk"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"claude", "--settings", fake},
		{"claude", "--settings=" + fake},
		{"claude", "--settings", "masil-claude-x.json"},
	} {
		if v := Check(argv); v.Allowed || v.Reason != ReasonUserSettings {
			t.Errorf("Check(%q) = %+v, want blocked with ReasonUserSettings", argv, v)
		}
		out, cleanup, err := Prepare(argv, "", tmp)
		if err != nil {
			t.Fatal(err)
		}
		if v := Check(out); v.Allowed || v.Reason != ReasonUserSettings {
			t.Errorf("Check(Prepare(%q)) = %+v, want blocked with ReasonUserSettings", argv, v)
		}
		cleanup()
	}
	out, cleanup, err := Prepare([]string{"claude"}, "", tmp)
	if err != nil {
		t.Fatal(err)
	}
	if v := Check(out); !v.Allowed {
		t.Errorf("Check(%q) = %+v, want allowed", out, v)
	}
	cleanup()
	if v := Check(out); v.Allowed {
		t.Errorf("Check(%q) after cleanup = %+v, want blocked (file no longer masil's)", out, v)
	}
}

// v0.1.3 security (o): Prepare always pins the mode Check reported (right
// after the program name), so a --permission-mode token that claude reads as
// another option's value cannot leave claude in a settings-file default mode.
func TestPrepareAlwaysPinsCheckedMode(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		mode string
	}{
		{[]string{"claude"}, "manual"},
		{[]string{"claude", "--permission-mode=plan"}, "plan"},
		{[]string{"claude", "--add-dir", "--permission-mode", "acceptEdits", "-c"}, "acceptEdits"},
		{[]string{"claude", "--permission-mode", "plan", "--", "hi"}, "plan"},
	} {
		out, cleanup, err := Prepare(tc.argv, "", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if len(out) < 5 || out[1] != "--permission-mode" || out[2] != tc.mode || out[3] != "--settings" {
			t.Errorf("Prepare(%q) = %q, want claude --permission-mode %s --settings <file> ...", tc.argv, out, tc.mode)
		}
		if v := Check(out); !v.Allowed || v.Mode != tc.mode {
			t.Errorf("Check(%q) = %+v, want allowed %s", out, v, tc.mode)
		}
		cleanup()
	}
}

// v0.1.3 security (n)(o) follow-up: claude's option parser can take a "--"
// as the value of the option before it, leaving later options live. Check
// therefore validates option-like tokens after "--" too.
func TestCheckOptionsAfterDoubleDash(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want Reason
	}{
		{[]string{"claude", "--append-system-prompt", "--", "--permission-mode", "dontAsk"}, ReasonModeNotAllowed},
		{[]string{"claude", "--append-system-prompt", "--", "--permission-mode=bypassPermissions"}, ReasonModeNotAllowed},
		{[]string{"claude", "--append-system-prompt", "--", "--settings", "/home/u/my.json"}, ReasonUserSettings},
		{[]string{"claude", "--", "--settings={\"permissions\":{}}"}, ReasonUserSettings},
		{[]string{"claude", "--", "--dangerously-skip-permissions"}, ReasonBypassFlag},
		// Two different modes: which one claude keeps depends on how it
		// parses the options in between, so Check cannot name it.
		{[]string{"claude", "--permission-mode", "acceptEdits", "--add-dir", "--permission-mode", "plan"}, ReasonModeNotAllowed},
	} {
		if v := Check(tc.argv); v.Allowed || v.Reason != tc.want {
			t.Errorf("Check(%q) = %+v, want blocked with %v", tc.argv, v, tc.want)
		}
	}
	// A prompt after "--" that only mentions the words stays allowed.
	for _, argv := range [][]string{
		{"claude", "--", "be dangerously fast with --permission-mode"},
		{"claude", "--permission-mode", "plan", "--permission-mode=plan", "--", "hi"},
	} {
		if v := Check(argv); !v.Allowed {
			t.Errorf("Check(%q) = %+v, want allowed", argv, v)
		}
	}
}

// Prepare puts the pinned mode and masil's --settings right after the
// program name, where no option of the user's can take them as its value.
func TestPreparePinsBeforeUserOptions(t *testing.T) {
	for _, argv := range [][]string{
		{"claude", "--append-system-prompt", "--", "hi"},
		{"claude", "--append-system-prompt"},
		{"claude", "--permission-mode", "plan"},
	} {
		out, cleanup, err := Prepare(argv, "", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		mode := Check(argv).Mode
		if len(out) != len(argv)+4 || out[0] != argv[0] || out[1] != "--permission-mode" || out[2] != mode ||
			out[3] != "--settings" || !slices.Equal(out[5:], argv[1:]) {
			t.Errorf("Prepare(%q) = %q, want claude --permission-mode %s --settings <file> <args>", argv, out, mode)
		}
		if v := Check(out); !v.Allowed || v.Mode != mode {
			t.Errorf("Check(%q) = %+v, want allowed %s", out, v, mode)
		}
		cleanup()
	}
}
