package fsroot

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// deniedNames are glob patterns (path.Match syntax) matched against every
// case-folded (foldName) slash-separated element of a path.
var deniedNames = []string{
	".env", ".env.*",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.kdbx",
	"id_rsa*", "id_ed25519*",
	".netrc", ".npmrc", ".pypirc", ".git-credentials",
	"kubeconfig",
	"credentials*.json", "secrets.*",
}

// deniedDirs are folder names whose whole subtree (and the folder itself) is
// denied.
var deniedDirs = []string{".ssh", ".aws", ".gnupg", ".git"}

// Denied reports whether a slash path names a secret: any element matching
// the deny list, or anything inside .ssh/.aws/.gnupg/.git. Matching ignores
// case because the default macOS and Windows file systems do (".ENV" opens
// ".env" there).
func Denied(rel string) bool {
	for _, el := range strings.Split(rel, "/") {
		if el == "" || el == "." || el == ".." {
			continue
		}
		if deniedElement(el) {
			return true
		}
	}
	return false
}

func deniedElement(el string) bool {
	low := foldName(el)
	for _, d := range deniedDirs {
		if low == d {
			return true
		}
	}
	for _, p := range deniedNames {
		if ok, _ := path.Match(p, low); ok {
			return true
		}
	}
	return false
}

// foldName lowercases el the way case-insensitive file systems compare
// names: every rune whose simple case-fold orbit holds an ASCII letter
// becomes that letter in lower case (U+017F "ſ" -> "s", U+212A KELVIN SIGN
// -> "k"), the rest unicode.ToLower. strings.ToLower leaves "ſ" alone, yet
// APFS and NTFS store "ſecrets.json" as the same entry as "secrets.json", so
// a name that does not exist yet (no stored name to compare) could be
// created under a spelling the deny list misses (v0.1.3 security (h)).
func foldName(el string) string {
	return strings.Map(func(r rune) rune {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f < utf8.RuneSelf {
				return unicode.ToLower(f)
			}
		}
		return unicode.ToLower(r)
	}, el)
}
