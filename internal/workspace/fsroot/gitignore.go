package fsroot

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitTimeout bounds one git check-ignore call.
const gitTimeout = 2 * time.Second

// GitRunner runs `git -C dir check-ignore -z --stdin` and returns stdout and
// the exit code (err only when git could not run at all).
type GitRunner = func(ctx context.Context, dir string, stdin []byte) ([]byte, int, error)

func runGit(ctx context.Context, dir string, stdin []byte) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "check-ignore", "-z", "--stdin")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out, ee.ExitCode(), nil
	}
	if err != nil {
		return nil, -1, err
	}
	return out, 0, nil
}

// readerFunc reads a slash path inside the folder.
type readerFunc func(rel string) ([]byte, error)

// ruleIgnorer applies ignore files with one name (".gitignore" or
// ".masilignore") found at the root and in every parent folder of a path.
type ruleIgnorer struct {
	name string
	read readerFunc
}

func (ri ruleIgnorer) Ignored(rels []string, dirs []bool) ([]bool, error) {
	cache := map[string]*Matcher{}
	matcher := func(base string) *Matcher {
		if m, ok := cache[base]; ok {
			return m
		}
		p := ri.name
		if base != "" {
			p = base + "/" + ri.name
		}
		var m *Matcher
		if data, err := ri.read(p); err == nil {
			m = ParseIgnore(base, strings.Split(string(data), "\n"))
		}
		cache[base] = m
		return m
	}
	out := make([]bool, len(rels))
	for i, rel := range rels {
		els := split(rel)
		for n := 1; n <= len(els) && !out[i]; n++ {
			p := strings.Join(els[:n], "/")
			isDir := n < len(els) || dirs[i]
			ignored := false
			for j := 0; j < n; j++ {
				m := matcher(strings.Join(els[:j], "/"))
				if m == nil {
					continue
				}
				if ign, dec := m.Match(p, isDir); dec {
					ignored = ign
				}
			}
			out[i] = ignored
		}
	}
	return out, nil
}

// anyIgnorer ORs several ignorers.
type anyIgnorer []Ignorer

func (a anyIgnorer) Ignored(rels []string, dirs []bool) ([]bool, error) {
	out := make([]bool, len(rels))
	for _, ig := range a {
		res, err := ig.Ignored(rels, dirs)
		if err != nil {
			return nil, err
		}
		for i, v := range res {
			out[i] = out[i] || v
		}
	}
	return out, nil
}

// gitIgnorer asks git (exact .gitignore, info/exclude and core.excludesFile
// semantics) and falls back to the built-in parser when git fails.
//
// git exits 128 for a whole batch when any one path is invalid for it ("is
// beyond a symbolic link", a path inside a submodule). That must not drop
// the rules from outside the folder for the other paths (v0.1.3 security
// (af) follow-up), so on exit 128 git is first asked with no paths: if that
// works the repository is fine and the batch is split in halves until each
// part is answered by git or is one path git refuses, which alone gets the
// built-in parser. At most 2n+1 git calls for n paths, all within one
// gitTimeout; a repository git refuses outright costs 2 calls.
type gitIgnorer struct {
	dir      string
	run      GitRunner
	fallback Ignorer
}

func (g gitIgnorer) Ignored(rels []string, dirs []bool) ([]bool, error) {
	if len(rels) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	out, code, err := g.ask(ctx, rels)
	if err == nil && (code == 0 || code == 1) {
		return out, nil
	}
	if err == nil && code == 128 && len(rels) > 1 {
		if _, pc, perr := g.ask(ctx, nil); perr == nil && (pc == 0 || pc == 1) {
			out = make([]bool, len(rels))
			h := len(rels) / 2
			g.split(ctx, rels[:h], dirs[:h], out[:h])
			g.split(ctx, rels[h:], dirs[h:], out[h:])
			return out, nil
		}
	}
	return g.fallback.Ignored(rels, dirs)
}

// split writes into out the answers for rels: git's when it answers, the
// halves again on exit 128, the built-in parser for a single refused path
// or when git fails otherwise (time-out).
func (g gitIgnorer) split(ctx context.Context, rels []string, dirs []bool, out []bool) {
	res, code, err := g.ask(ctx, rels)
	if err == nil && (code == 0 || code == 1) {
		copy(out, res)
		return
	}
	if err == nil && code == 128 && len(rels) > 1 {
		h := len(rels) / 2
		g.split(ctx, rels[:h], dirs[:h], out[:h])
		g.split(ctx, rels[h:], dirs[h:], out[h:])
		return
	}
	fb, ferr := g.fallback.Ignored(rels, dirs)
	for i := range out {
		out[i] = ferr != nil || fb[i] // a failing fallback hides (fail closed)
	}
}

// ask runs one git check-ignore for rels. code 0 or 1 with a nil err is an
// answer (out[i] = rels[i] ignored); anything else is a failure.
func (g gitIgnorer) ask(ctx context.Context, rels []string) ([]bool, int, error) {
	// Paths go without a trailing slash: git looks at the file system to see
	// whether an existing path is a folder, and "x/" would wrongly match "x/**".
	var in bytes.Buffer
	for _, rel := range rels {
		in.WriteString(rel)
		in.WriteByte(0)
	}
	stdout, code, err := g.run(ctx, g.dir, in.Bytes())
	out := make([]bool, len(rels))
	if err != nil || code != 0 {
		return out, code, err
	}
	hit := map[string]bool{}
	for _, p := range bytes.Split(stdout, []byte{0}) {
		if len(p) > 0 {
			hit[string(p)] = true
		}
	}
	for i, rel := range rels {
		out[i] = hit[rel]
	}
	return out, code, nil
}

func rootReader(dir string) readerFunc {
	return func(rel string) ([]byte, error) {
		r, err := os.OpenRoot(dir)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return r.ReadFile(filepath.FromSlash(rel))
	}
}

// NewGitIgnorer uses `git -C dir check-ignore -z --stdin` (2 s limit,
// GIT_OPTIONAL_LOCKS=0; exit 1 = nothing ignored; exit 128 or no git = the
// built-in parser, only for the refused paths when git answers the others).
// run nil = the real git. .masilignore files are applied on top.
func NewGitIgnorer(dir string, run GitRunner) Ignorer {
	if run == nil {
		run = runGit
	}
	read := rootReader(dir)
	return anyIgnorer{
		gitIgnorer{dir: dir, run: run, fallback: ruleIgnorer{name: GitIgnoreFile, read: read}},
		ruleIgnorer{name: MasilIgnoreFile, read: read},
	}
}

// NewFileIgnorer parses root and nested .gitignore files itself (no git), plus
// .masilignore files.
func NewFileIgnorer(r *Root) Ignorer {
	read := func(rel string) ([]byte, error) { return r.root.ReadFile(filepath.FromSlash(rel)) }
	return anyIgnorer{ruleIgnorer{name: GitIgnoreFile, read: read}, ruleIgnorer{name: MasilIgnoreFile, read: read}}
}
