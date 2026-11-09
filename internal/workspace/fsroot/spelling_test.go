package fsroot

import (
	"os"
	"path/filepath"
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
