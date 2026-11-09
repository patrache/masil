package apitool

import (
	"errors"
	"strconv"
	"strings"
)

// ErrBadPath is a JSON path outside the supported subset.
var ErrBadPath = errors.New("apitool: unsupported json path")

// pathStep is one step: a key, an index, or every element ([*]).
type pathStep struct {
	key   string
	index int
	kind  byte // 'k' key, 'i' index, '*' wildcard
}

// parsePath parses the subset: $ then .name, ['name'], ["name"], [n], [*].
func parsePath(path string) ([]pathStep, error) {
	if !strings.HasPrefix(path, "$") {
		return nil, ErrBadPath
	}
	var steps []pathStep
	rest := path[1:]
	for rest != "" {
		switch rest[0] {
		case '.':
			rest = rest[1:]
			n := 0
			for n < len(rest) && rest[n] != '.' && rest[n] != '[' {
				n++
			}
			name := rest[:n]
			if name == "" || name == "*" || strings.ContainsAny(name, "]'\" ") {
				return nil, ErrBadPath
			}
			steps = append(steps, pathStep{key: name, kind: 'k'})
			rest = rest[n:]
		case '[':
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				return nil, ErrBadPath
			}
			in := rest[1:end]
			if len(in) >= 2 && (in[0] == '\'' || in[0] == '"') {
				q := in[0]
				// the closing quote must be right before ']' (a name may hold ']')
				cq := strings.IndexByte(rest[2:], q)
				if cq < 0 || len(rest) < cq+4 || rest[2+cq+1] != ']' {
					return nil, ErrBadPath
				}
				steps = append(steps, pathStep{key: rest[2 : 2+cq], kind: 'k'})
				rest = rest[2+cq+2:]
				continue
			}
			switch {
			case in == "*":
				steps = append(steps, pathStep{kind: '*'})
			default:
				n, err := strconv.Atoi(in)
				if err != nil || n < 0 || strings.HasPrefix(in, "+") {
					return nil, ErrBadPath
				}
				steps = append(steps, pathStep{index: n, kind: 'i'})
			}
			rest = rest[end+1:]
		default:
			return nil, ErrBadPath
		}
	}
	return steps, nil
}

// pathHasWildcard reports whether path can yield several values.
func pathHasWildcard(path string) bool {
	steps, err := parsePath(path)
	if err != nil {
		return false
	}
	for _, s := range steps {
		if s.kind == '*' {
			return true
		}
	}
	return false
}

// EvalPath evaluates path on a decoded JSON document (map[string]any, []any,
// json.Number …). Grammar: $, .name, ['name'], [n], [*]; anything else is
// ErrBadPath. A path that matches nothing is an empty result.
func EvalPath(doc any, path string) ([]any, error) {
	steps, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	cur := []any{doc}
	for _, s := range steps {
		var next []any
		for _, v := range cur {
			switch s.kind {
			case 'k':
				if m, ok := v.(map[string]any); ok {
					if c, ok := m[s.key]; ok {
						next = append(next, c)
					}
				}
			case 'i':
				if a, ok := v.([]any); ok && s.index < len(a) {
					next = append(next, a[s.index])
				}
			case '*':
				switch t := v.(type) {
				case []any:
					next = append(next, t...)
				case map[string]any:
					for _, k := range sortedKeys(t) {
						next = append(next, t[k])
					}
				}
			}
		}
		cur = next
	}
	if cur == nil {
		cur = []any{}
	}
	return cur, nil
}
