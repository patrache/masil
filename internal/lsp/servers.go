package lsp

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Server is a language server command for one language.
type Server struct {
	// Lang is the LSP languageId of the documents it serves ("go", "python" …).
	Lang string
	// Name is the program's base name ("gopls"), for logs.
	Name string
	// Command is argv; Command[0] is the resolved program path.
	Command []string
	// Check, when set, is an argv that must succeed before the server is
	// started (v0.1.3-1: "rust-analyzer --version" fails for rustup's proxy
	// when the component is not installed); a failure means no server.
	Check []string
}

// serverTable lists, per languageId, the commands to try in order (first one
// on PATH wins).
var serverTable = map[string][][]string{
	"go":              {{"gopls"}},
	"python":          {{"pyright-langserver", "--stdio"}, {"basedpyright-langserver", "--stdio"}, {"pylsp"}},
	"c":               {{"clangd"}},
	"cpp":             {{"clangd"}},
	"rust":            {{"rust-analyzer"}},
	"typescript":      {{"typescript-language-server", "--stdio"}},
	"typescriptreact": {{"typescript-language-server", "--stdio"}},
	"javascript":      {{"typescript-language-server", "--stdio"}},
	"javascriptreact": {{"typescript-language-server", "--stdio"}},
	"java":            {{"jdtls"}},
	"csharp":          {{"csharp-ls"}},
	"php":             {{"intelephense", "--stdio"}},
	"swift":           {{"sourcekit-lsp"}},
	"kotlin":          {{"kotlin-language-server"}},
	"ruby":            {{"solargraph", "stdio"}},
	"scala":           {{"metals"}},
}

// serverChecks are the check arguments (Server.Check) by program name: a
// proxy that is on PATH but cannot run the server fails them.
var serverChecks = map[string][]string{
	"rust-analyzer": {"--version"},
}

// extLang maps a lower-cased file extension to its languageId.
var extLang = map[string]string{
	".go": "go",
	".py": "python", ".pyi": "python",
	".c": "c", ".h": "c",
	".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".c++": "cpp", ".hh": "cpp", ".hpp": "cpp", ".hxx": "cpp",
	".rs": "rust",
	".ts": "typescript", ".mts": "typescript", ".cts": "typescript", ".tsx": "typescriptreact",
	".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "javascriptreact",
	".java":  "java",
	".cs":    "csharp",
	".php":   "php",
	".swift": "swift",
	".kt":    "kotlin", ".kts": "kotlin",
	".rb":    "ruby",
	".scala": "scala", ".sc": "scala",
}

// leetLang maps a LeetCode language slug (internal/leetcode.Languages) to a
// languageId; slugs without a server here (dart, racket, erlang, elixir) are absent.
var leetLang = map[string]string{
	"golang": "go", "python3": "python", "python": "python", "cpp": "cpp", "c": "c",
	"java": "java", "csharp": "csharp", "javascript": "javascript", "typescript": "typescript",
	"php": "php", "swift": "swift", "kotlin": "kotlin", "ruby": "ruby", "scala": "scala", "rust": "rust",
}

// LangForPath is the languageId for a file name by its extension, "" when
// no server here knows it.
func LangForPath(path string) string {
	return extLang[strings.ToLower(filepath.Ext(path))]
}

// LangForLeetCode is the languageId for a LeetCode language slug ("golang" →
// "go"), "" when no server here knows it.
func LangForLeetCode(slug string) string { return leetLang[slug] }

// lookPath is exec.LookPath (a fake in tests).
var lookPath = exec.LookPath

var (
	overrideMu sync.RWMutex
	overrides  map[string][]string
)

// SetOverrides replaces the configured commands ([lsp.<lang>] command = [...]
// in config.toml): for a languageId with a non-empty argv, that command is
// tried before the built-in table. A nil map clears them. An override for
// "typescript" / "javascript" also covers the …react variant.
func SetOverrides(m map[string][]string) {
	cp := make(map[string][]string, len(m))
	for k, v := range m {
		if len(v) > 0 && strings.TrimSpace(v[0]) != "" {
			cp[strings.ToLower(strings.TrimSpace(k))] = append([]string(nil), v...)
		}
	}
	overrideMu.Lock()
	overrides = cp
	overrideMu.Unlock()
}

func overrideFor(lang string) []string {
	overrideMu.RLock()
	defer overrideMu.RUnlock()
	if v, ok := overrides[lang]; ok {
		return v
	}
	if base, ok := strings.CutSuffix(lang, "react"); ok {
		return overrides[base]
	}
	return nil
}

// Find picks the server for languageId lang: the configured override when
// its program is on PATH, else the first built-in command on PATH. false
// means there is none; the caller keeps word completion.
func Find(lang string) (Server, bool) {
	lang = strings.ToLower(lang)
	var cands [][]string
	if o := overrideFor(lang); o != nil {
		cands = append(cands, o)
	}
	cands = append(cands, serverTable[lang]...)
	for _, argv := range cands {
		p, err := lookPath(argv[0])
		if err != nil || p == "" {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		cmd := append([]string{p}, argv[1:]...)
		name := strings.TrimSuffix(filepath.Base(argv[0]), filepath.Ext(argv[0]))
		srv := Server{Lang: lang, Name: name, Command: cmd}
		if args, ok := serverChecks[name]; ok {
			srv.Check = append([]string{p}, args...)
		}
		return srv, true
	}
	return Server{}, false
}
