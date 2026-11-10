package fsroot

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// aliasOpens reports whether the file system opens dir/name through the
// different spelling alias (case-insensitive or folding file systems).
func aliasOpens(dir, alias string) bool {
	_, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(alias)))
	return err == nil
}

func TestCheckSpellingAliasDenied(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, "secrets.json", "TOKEN")
	writeFile(t, proj, ".git-credentials", "TOKEN")
	writeFile(t, proj, "keys/id_rsa", "TOKEN")
	r := openRoot(t, proj)
	tried := 0
	// U+017F LATIN SMALL LETTER LONG S folds to 's' on APFS/NTFS but not in
	// strings.ToLower.
	for _, alias := range []string{"SECRETS.JSON", "ſecrets.json", ".git-credentialſ", "keys/id_rſa"} {
		if !aliasOpens(proj, alias) {
			continue
		}
		tried++
		_, err := r.Check(alias, true)
		if reason, ok := reasonOf(err); !ok || reason != ReasonDenied {
			t.Errorf("Check(%q) err = %v, want Denied", alias, err)
		}
		if _, _, err := r.ReadText(alias); err == nil {
			// Owner reads are allowed, but the real name must be what was checked.
			if _, err := r.Check(alias, false); err == nil {
				t.Errorf("Check(%q, owner) passed for a denied file", alias)
			}
		}
	}
	if tried == 0 {
		t.Skip("case-sensitive file system: no alias opens")
	}
}

func TestCheckSpellingAliasIgnored(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, MasilIgnoreFile, "private/\ndocs/secret.txt\n")
	writeFile(t, proj, GitIgnoreFile, "build/\n")
	writeFile(t, proj, "private/x.txt", "hidden")
	writeFile(t, proj, "build/out", "hidden")
	writeFile(t, proj, "docs/secret.txt", "hidden")
	writeFile(t, proj, "docs/open.txt", "open")
	r := openRoot(t, proj)
	if !aliasOpens(proj, "PRIVATE/x.txt") {
		t.Skip("case-sensitive file system")
	}
	for _, alias := range []string{"PRIVATE/x.txt", "Private/x.txt", "BUILD/out", "docs/SECRET.TXT", ".GITIGNORE", ".MasilIgnore"} {
		_, err := r.Check(alias, true)
		if reason, ok := reasonOf(err); !ok || reason != ReasonIgnored {
			t.Errorf("Check(%q) err = %v, want Ignored", alias, err)
		}
	}
	entries, _, err := r.List("DOCS", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name == "secret.txt" {
			t.Errorf("List(DOCS) shows ignored secret.txt to a peer")
		}
	}
	// Exact spellings keep working.
	if _, err := r.Check("docs/open.txt", true); err != nil {
		t.Errorf("Check(docs/open.txt) = %v", err)
	}
	if text, _, err := r.ReadText("DOCS/open.txt"); err != nil || text != "open" {
		t.Errorf("ReadText(DOCS/open.txt) = %q, %v", text, err)
	}
}

// Names that do not exist yet have no stored name to compare, so the deny
// list itself must fold the way APFS and NTFS do: U+017F "ſ" is "s", U+212A
// KELVIN SIGN is "k". Otherwise a participant creates "ſecrets.json" and the
// owner's tools later write "secrets.json" into that same entry (v0.1.3
// security (h) follow-up).
func TestDeniedFoldsUnicodeCase(t *testing.T) {
	for _, rel := range []string{
		"ſecrets.json", "credentialſ.json", "id_rſa", ".git-credentialſ", "x.Key",
		"x.Key", "a/.Kube/../.ſſh/config", ".ſsh/config", "ſub/.ENV",
	} {
		if !Denied(rel) {
			t.Errorf("Denied(%q) = false, want true", rel)
		}
	}
	for _, rel := range []string{"ſecret.go", "Key.go", "straße.md"} {
		if Denied(rel) {
			t.Errorf("Denied(%q) = true, want false", rel)
		}
	}
}

func TestCreateFoldedDeniedName(t *testing.T) {
	proj := t.TempDir()
	r := openRoot(t, proj)
	for _, rel := range []string{"ſecrets.json", "id_rſa", "x.Key", "x.Key"} {
		if _, err := r.Check(rel, true); !isReason(err, ReasonDenied) {
			t.Errorf("Check(%q, peer) err = %v, want Denied", rel, err)
		}
		if err := r.Create(rel); !isReason(err, ReasonDenied) {
			t.Errorf("Create(%q) err = %v, want Denied", rel, err)
		}
	}
}

func isReason(err error, want Reason) bool {
	reason, ok := reasonOf(err)
	return ok && reason == want
}

// RuleFiles names the rule files above the path and above its link target.
func TestRuleFiles(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, proj, "a/b/c.txt", "x")
	writeFile(t, proj, "d/e.txt", "y")
	r := openRoot(t, proj)
	if err := os.Symlink("../d/e.txt", filepath.Join(proj, "a", "l.txt")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	got := r.RuleFiles("a/l.txt")
	for _, want := range []string{".gitignore", ".masilignore", "a/.gitignore", "a/.masilignore", "d/.masilignore", ".git/info/exclude"} {
		if !slices.Contains(got, want) {
			t.Errorf("RuleFiles = %v, lacks %s", got, want)
		}
	}
	if slices.Contains(got, "a/b/.gitignore") {
		t.Errorf("RuleFiles = %v, has a folder not above the path", got)
	}
}
