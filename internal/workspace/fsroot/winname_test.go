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
		"a/.env..",
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

// "~" and a digit is an ordinary long name on Windows too (Copy~1.txt,
// v1~2/). Existing 8.3 aliases are mapped to their stored name by diskName,
// and a name that does not exist cannot be an alias, so neither the owner
// nor a participant is refused it (v0.1.3 security (g) follow-up).
func TestWindowsTildeDigitNamesUsable(t *testing.T) {
	old := windowsNames
	windowsNames = true
	t.Cleanup(func() { windowsNames = old })
	proj := t.TempDir()
	writeFile(t, proj, "Copy~1.txt", "x")
	writeFile(t, proj, "v1~2/a.txt", "y")
	r := openRoot(t, proj)
	for _, forPeer := range []bool{false, true} {
		for _, rel := range []string{"Copy~1.txt", "v1~2", "v1~2/a.txt", "NEW~3.txt"} {
			if _, err := r.Check(rel, forPeer); err != nil {
				t.Errorf("Check(%q, %v) = %v, want ok", rel, forPeer, err)
			}
		}
	}
	if _, _, err := r.List("v1~2", false); err != nil {
		t.Errorf("List(v1~2) = %v", err)
	}
	if _, _, err := r.ReadText("Copy~1.txt"); err != nil {
		t.Errorf("ReadText(Copy~1.txt) = %v", err)
	}
	if err := r.Rename("Copy~1.txt", "Copy~2.txt"); err != nil {
		t.Errorf("Rename = %v", err)
	}
	if err := r.Remove("Copy~2.txt"); err != nil {
		t.Errorf("Remove = %v", err)
	}
}
