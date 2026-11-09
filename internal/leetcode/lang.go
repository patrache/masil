package leetcode

// Lang is one LeetCode language: its slug (the GraphQL / judge name), the
// name shown on screen (not translated) and the solution file extension.
type Lang struct{ Slug, Name, Ext string }

// DefaultLang is the language a first run picks.
const DefaultLang = "golang"

// Languages is the language picker order (A10.5).
var Languages = []Lang{
	{"golang", "Go", ".go"},
	{"python3", "Python3", ".py"},
	{"python", "Python", ".py"},
	{"cpp", "C++", ".cpp"},
	{"java", "Java", ".java"},
	{"c", "C", ".c"},
	{"csharp", "C#", ".cs"},
	{"javascript", "JavaScript", ".js"},
	{"typescript", "TypeScript", ".ts"},
	{"php", "PHP", ".php"},
	{"swift", "Swift", ".swift"},
	{"kotlin", "Kotlin", ".kt"},
	{"dart", "Dart", ".dart"},
	{"ruby", "Ruby", ".rb"},
	{"scala", "Scala", ".scala"},
	{"rust", "Rust", ".rs"},
	{"racket", "Racket", ".rkt"},
	{"erlang", "Erlang", ".erl"},
	{"elixir", "Elixir", ".ex"},
}

// LangBySlug finds a language by its LeetCode slug.
func LangBySlug(slug string) (Lang, bool) {
	for _, l := range Languages {
		if l.Slug == slug {
			return l, true
		}
	}
	return Lang{}, false
}
