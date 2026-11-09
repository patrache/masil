package fsroot

import (
	"path"
	"strings"
)

// deniedNames are glob patterns (path.Match syntax) matched against every
// lowercased slash-separated element of a path.
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
	for _, d := range deniedDirs {
		if strings.EqualFold(el, d) {
			return true
		}
	}
	low := strings.ToLower(el)
	for _, p := range deniedNames {
		if ok, _ := path.Match(p, low); ok {
			return true
		}
	}
	return false
}
