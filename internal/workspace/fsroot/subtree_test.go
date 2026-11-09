package fsroot

import "testing"

func TestCheckSubtree(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, MasilIgnoreFile, "deep/x/y/hidden.txt\n")
	writeFile(t, proj, "deep/x/y/hidden.txt", "h")
	writeFile(t, proj, "deep/x/ok.txt", "o")
	writeFile(t, proj, "nested/.gitignore", "local\n")
	writeFile(t, proj, "clean/a/b.txt", "b")
	writeFile(t, proj, "other/z/.env", "S")
	symlink(t, proj, "../other", "clean/l")
	r := openRoot(t, proj)
	for rel, want := range map[string]Reason{
		"deep":   ReasonIgnored,
		"deep/x": ReasonIgnored,
		"nested": ReasonIgnored, // a rule file is hidden from peers
		"other":  ReasonDenied,
	} {
		if reason, ok := reasonOf(r.CheckSubtree(rel)); !ok || reason != want {
			t.Errorf("CheckSubtree(%q) reason = %v, want %v", rel, reason, want)
		}
	}
	// A link inside moves alone: its target's contents are not walked.
	for _, rel := range []string{"clean", "clean/a", "deep/x/ok.txt", "missing", "clean/l"} {
		if err := r.CheckSubtree(rel); err != nil {
			t.Errorf("CheckSubtree(%q) = %v, want nil", rel, err)
		}
	}
}
