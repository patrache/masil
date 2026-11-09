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

// v0.1.3 security (o): Prepare always ends the options with the mode Check
// reported, so a --permission-mode token that claude reads as another
// option's value cannot leave claude in a settings-file default mode.
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
		at := slices.Index(out, "--")
		if at < 0 {
			at = len(out)
		}
		opts := out[:at]
		if len(opts) < 4 || opts[len(opts)-4] != "--permission-mode" || opts[len(opts)-3] != tc.mode || opts[len(opts)-2] != "--settings" {
			t.Errorf("Prepare(%q) = %q, want ... --permission-mode %s --settings <file>", tc.argv, out, tc.mode)
		}
		if v := Check(out); !v.Allowed || v.Mode != tc.mode {
			t.Errorf("Check(%q) = %+v, want allowed %s", out, v, tc.mode)
		}
		cleanup()
	}
}
