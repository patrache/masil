// Package markup parses and renders the body markdown subset (api.md 2.1, CA 4.8).
package markup

// Doc is a parsed body.
type Doc struct{ Blocks []Block }

// BlockKind is Paragraph | Code.
type BlockKind int

// Block kinds.
const (
	Paragraph BlockKind = iota
	Code
)

// Block is a paragraph or a fenced code block.
type Block struct {
	Kind   BlockKind // Paragraph | Code
	Inline []Inline  // Paragraph
	Lang   string    // Code
	Code   string    // Code
}

// InlineKind is Text | Bold | Italic | InlineCode | Link | Mention | Here | Hashtag.
//
// CA 4.8 names the inline code-span kind `Code`, which collides with the block
// kind `Code` in the same package; the inline one is named InlineCode.
type InlineKind int

// Inline kinds.
const (
	Text InlineKind = iota
	Bold
	Italic
	InlineCode
	Link
	Mention
	Here
	Hashtag
)

// Inline is one inline span.
type Inline struct {
	Kind   InlineKind // Text | Bold | Italic | InlineCode | Link | Mention | Here | Hashtag
	Text   string
	URL    string // Link (also bare URLs)
	Handle string // Mention (lowercase, no @)
}

// ParseOptions: Hashtags is true for posts/stories, false for messages.
type ParseOptions struct{ Hashtags bool }

// Parse parses the api.md 2.1 grammar; no other markdown.
func Parse(body string, opts ParseOptions) Doc {
	// TODO(core): Task 17
	return Doc{}
}
