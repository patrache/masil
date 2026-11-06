package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Pending fix 2 (supply chain): a tag such as actions/checkout@v7 can be moved
// to another commit by whoever controls the action's repository, so every
// `uses:` in .github/ must name a full 40-hex commit SHA followed by the
// release it was resolved from as `# vX.Y.Z` (resolved with
// `gh api repos/<owner>/<repo>/git/ref/tags/<tag>`, never from memory). This
// covers ci.yml, server-ci.yml and the daemon-release release.yml (goreleaser).
func TestWorkflowActionsPinnedToSHA(t *testing.T) {
	root := filepath.Join("..", "..", ".github")
	var files []string
	for _, pat := range []string{
		filepath.Join(root, "workflows", "*.yml"),
		filepath.Join(root, "workflows", "*.yaml"),
	} {
		m, err := filepath.Glob(pat)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	// Composite actions under .github/actions can call other actions too.
	_ = filepath.WalkDir(filepath.Join(root, "actions"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (d.Name() == "action.yml" || d.Name() == "action.yaml") {
			files = append(files, p)
		}
		return nil
	})
	if len(files) == 0 {
		t.Fatal("no workflow files found under .github/workflows")
	}
	contents := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		contents[filepath.ToSlash(f)] = string(b)
	}
	n, errs := checkActionPins(contents)
	for _, e := range errs {
		t.Error(e)
	}
	if n == 0 {
		t.Fatal("found no `uses:` lines; the check would pass vacuously")
	}
}

// The checker itself must reject every mutable form, or the test above proves
// nothing.
func TestCheckActionPinsRejectsMutableRefs(t *testing.T) {
	const sha = "3d3c42e5aac5ba805825da76410c181273ba90b1"
	bad := map[string]string{
		"major tag":          "      - uses: actions/checkout@v7\n",
		"exact tag":          "      - uses: actions/checkout@v7.0.1\n",
		"branch":             "      - uses: actions/checkout@main\n",
		"short sha":          "      - uses: actions/checkout@3d3c42e # v7.0.1\n",
		"upper-case sha":     "      - uses: actions/checkout@" + strings.ToUpper(sha) + " # v7.0.1\n",
		"no version comment": "      - uses: actions/checkout@" + sha + "\n",
		"major-only comment": "      - uses: actions/checkout@" + sha + " # v7\n",
		"no ref":             "      - uses: actions/checkout\n",
		"quoted tag":         "      - uses: \"actions/checkout@v7\"\n",
		"key not first":      "      - name: x\n        uses: actions/checkout@v7\n",
		"flow mapping":       "      - { uses: actions/checkout@v7 }\n",
		"docker tag":         "      - uses: docker://alpine:3.20\n",
		"crlf tag":           "      - uses: actions/checkout@v7\r\n",
		"same version, two shas": "      - uses: actions/checkout@" + sha + " # v7.0.1\n" +
			"      - uses: actions/checkout@" + strings.Repeat("a", 40) + " # v7.0.1\n",
		"same sha, two versions": "      - uses: actions/checkout@" + sha + " # v7.0.1\n" +
			"      - uses: actions/checkout@" + sha + " # v7.0.2\n",
		// Every spelling below is the same `uses` key to a YAML parser (and to
		// GitHub); a line-oriented `uses:` match saw none of them.
		"space before colon":     "      - uses : actions/checkout@v7\n",
		"double-quoted key":      "      - \"uses\": actions/checkout@v7\n",
		"double-quoted flow key": "      - {\"uses\": actions/checkout@v7}\n",
		"single-quoted key":      "      - 'uses': actions/checkout@v7\n",
		"complex key":            "      - ? uses\n        : actions/checkout@v7\n",
		"escaped key":            "      - \"u\\x73es\": actions/checkout@v7\n",
		"tagged key":             "      - !!str uses: actions/checkout@v7\n",
		"aliased key":            "      - &k uses: actions/checkout@" + sha + " # v7.0.1\n      - *k : actions/checkout@v7\n",
		"aliased value":          "x: &ref actions/checkout@v7\nsteps:\n  - uses: *ref\n",
		"folded value":           "      - uses: >-\n          actions/checkout@v7\n",
		"second document":        "a: 1\n---\nsteps:\n  - uses: actions/checkout@v7\n",
		"capitalised key":        "      - Uses: actions/checkout@v7\n",
		"reusable workflow tag":  "jobs:\n  call:\n    uses: octo/repo/.github/workflows/ci.yml@main\n",
		"unparseable yaml":       "steps:\n  - uses: actions/checkout@" + sha + " # v7.0.1\n bad: [\n",
	}
	for name, wf := range bad {
		if _, errs := checkActionPins(map[string]string{"wf.yml": wf}); len(errs) == 0 {
			t.Errorf("%s: accepted %q", name, wf)
		}
	}
	good := "steps:\n" +
		"      - uses: actions/checkout@" + sha + " # v7.0.1\n" +
		"      - uses: actions/checkout@" + sha + " # v7.0.1\r\n" +
		"        with:\n" +
		"          fetch-depth: 0\n" +
		"      - name: local\n" +
		"        uses: ./.github/actions/setup\n" +
		"      - uses: docker://alpine@sha256:" + strings.Repeat("0", 64) + "\n" +
		"      # - uses: actions/checkout@v7   (a comment is not a step)\n"
	n, errs := checkActionPins(map[string]string{"wf.yml": good})
	if len(errs) != 0 {
		t.Fatalf("pinned workflow rejected: %v", errs)
	}
	if n != 4 {
		t.Fatalf("counted %d uses lines, want 4", n)
	}
}

var (
	pinnedRef  = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:/[^@\s]+)?)@([0-9a-f]{40})$`)
	versionTag = regexp.MustCompile(`^#\s*(v\d+\.\d+\.\d+)$`)
	dockerUses = regexp.MustCompile(`^docker://[^@\s]+@sha256:[0-9a-f]{64}$`)
)

// checkActionPins checks every `uses` key in the given files (path -> content)
// and returns how many it saw plus one error per violation.
//
// The files are read with a YAML parser, not line by line: `uses : x`,
// `"uses": x`, `'uses': x`, `? uses` / `: x`, `"u\x73es": x` and `{"uses": x}`
// are all the same key to GitHub, and a text match on `uses:` misses them.
// Keys match case-insensitively and an alias as the value is refused, so the
// check errs towards failing. A file that does not parse is an error too.
func checkActionPins(files map[string]string) (int, []error) {
	var errs []error
	n := 0
	versionSHA := map[string]string{} // action@version -> sha
	shaVersion := map[string]string{} // action@sha -> version
	check := func(path string, key, val *yaml.Node) {
		n++
		at := fmt.Sprintf("%s:%d", path, val.Line)
		if val.Kind != yaml.ScalarNode {
			errs = append(errs, fmt.Errorf("%s: `uses` value must be a plain string, not an alias, mapping or sequence", at))
			return
		}
		v := strings.TrimSpace(val.Value)
		switch {
		case strings.HasPrefix(v, "./"):
			return // local action in this repository: same commit as the workflow
		case strings.HasPrefix(v, "docker://"):
			if !dockerUses.MatchString(v) {
				errs = append(errs, fmt.Errorf("%s: docker action must be pinned by @sha256 digest: %q", at, v))
			}
			return
		}
		comment := strings.TrimSpace(val.LineComment)
		if comment == "" {
			comment = strings.TrimSpace(key.LineComment)
		}
		p, c := pinnedRef.FindStringSubmatch(v), versionTag.FindStringSubmatch(comment)
		if p == nil || c == nil {
			errs = append(errs, fmt.Errorf("%s: action must be pinned as owner/repo@<40-hex commit sha> # vX.Y.Z: %q %q", at, v, comment))
			return
		}
		action, sha, version := p[1], p[2], c[1]
		if prev, ok := versionSHA[action+"@"+version]; ok && prev != sha {
			errs = append(errs, fmt.Errorf("%s: %s %s pinned to %s here but %s elsewhere", at, action, version, sha, prev))
		}
		versionSHA[action+"@"+version] = sha
		if prev, ok := shaVersion[action+"@"+sha]; ok && prev != version {
			errs = append(errs, fmt.Errorf("%s: %s@%s labelled %s here but %s elsewhere", at, action, sha, version, prev))
		}
		shaVersion[action+"@"+sha] = version
	}
	var walk func(path string, nd *yaml.Node)
	walk = func(path string, nd *yaml.Node) {
		// Aliases are not followed: the node they point to is walked where
		// its anchor is defined.
		if nd.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(nd.Content); i += 2 {
				k := nd.Content[i]
				if k.Kind == yaml.AliasNode && k.Alias != nil {
					k = k.Alias
				}
				if k.Kind == yaml.ScalarNode && strings.EqualFold(k.Value, "uses") {
					check(path, k, nd.Content[i+1])
				}
			}
		}
		for _, c := range nd.Content {
			walk(path, c)
		}
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		dec := yaml.NewDecoder(strings.NewReader(files[path]))
		for {
			var doc yaml.Node
			err := dec.Decode(&doc)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: not valid YAML, so its `uses` cannot be checked: %v", path, err))
				break
			}
			walk(path, &doc)
		}
	}
	return n, errs
}
