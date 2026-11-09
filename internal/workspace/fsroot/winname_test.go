package fsroot

import "testing"

// v0.1.3 security (g): on Windows ".env.", ".env " and ".env::$DATA" name
// the same file as ".env" (and an 8.3 short name aliases a long one), so a
// peer could create or reach a denied file under a spelling the deny list
// never matches. Those name forms are refused there before any lookup.
func TestCheckRejectsWindowsAliasForms(t *testing.T) {
	old := windowsNames
	windowsNames = true
	t.Cleanup(func() { windowsNames = old })
	proj := t.TempDir()
	writeFile(t, proj, "ok.txt", "x")
	r := openRoot(t, proj)
	for _, rel := range []string{
		".env.", ".env ", ".env::$DATA", "ok.txt:stream", "sub./x", "sub /x",
		"SECRET~1.JSO", "dir/CREDEN~2", "a/.env..",
	} {
		_, err := r.Check(rel, true)
		if reason, ok := reasonOf(err); !ok || reason != ReasonInvalid {
			t.Errorf("Check(%q) err = %v, want Invalid", rel, err)
		}
		if err := r.Create(rel); err == nil {
			t.Errorf("Create(%q) succeeded", rel)
		}
	}
	// Ordinary names (dots inside, a lone tilde, "~" without a digit) pass.
	for _, rel := range []string{"ok.txt", "a.b.c", "notes~", "x~y.txt", "./ok.txt", "dir/../ok.txt"} {
		if _, err := r.Check(rel, true); err != nil {
			t.Errorf("Check(%q) = %v, want ok", rel, err)
		}
	}
}

// Elsewhere these are ordinary names (macOS/Linux store them literally).
func TestCheckKeepsUnixNamesOffWindows(t *testing.T) {
	old := windowsNames
	windowsNames = false
	t.Cleanup(func() { windowsNames = old })
	r := openRoot(t, t.TempDir())
	for _, rel := range []string{"notes.", "file~1.txt", "notes:x"} {
		if _, err := r.Check(rel, true); err != nil {
			t.Errorf("Check(%q) = %v, want ok", rel, err)
		}
	}
}
