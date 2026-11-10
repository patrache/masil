package lsp

import (
	"errors"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// URIFromPath makes a file:// URI from an absolute path (C:\a\b on Windows
// becomes file:///C:/a/b). Spaces, Hangul and other bytes are percent-encoded.
func URIFromPath(path string) string {
	p := filepath.ToSlash(filepath.Clean(path))
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // a Windows drive path
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// PathFromURI is the inverse of URIFromPath.
func PathFromURI(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" {
		return "", errors.New("lsp: not a file URI")
	}
	p := u.Path
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}
