package theme

import (
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
)

// CodeStyle maps a chroma token type to a theme style: Keyword=Acc bold, String=Acc3,
// Number=Acc2, Comment=Dim italic, Name.Function=Acc4, Name.Builtin=Acc2,
// Operator/Punctuation=FG, Generic.Deleted=Acc2, Generic.Inserted=Acc3, other=Acc3.
// The background is CodeBG so highlighted spans sit inside inline code and code blocks.
func (t *Theme) CodeStyle(tt chroma.TokenType) lipgloss.Style {
	st := t.S.Base.Background(t.P.CodeBG)
	switch {
	case tt.InCategory(chroma.Keyword):
		return st.Foreground(t.P.Acc).Bold(true)
	case tt.InSubCategory(chroma.LiteralString):
		return st.Foreground(t.P.Acc3)
	case tt.InSubCategory(chroma.LiteralNumber):
		return st.Foreground(t.P.Acc2)
	case tt.InCategory(chroma.Comment):
		return st.Foreground(t.P.Dim).Italic(true)
	case tt.InSubCategory(chroma.NameFunction):
		return st.Foreground(t.P.Acc4)
	case tt.InSubCategory(chroma.NameBuiltin):
		return st.Foreground(t.P.Acc2)
	case tt.InCategory(chroma.Operator), tt.InCategory(chroma.Punctuation):
		return st.Foreground(t.P.FG)
	case tt == chroma.GenericDeleted:
		return st.Foreground(t.P.Acc2)
	case tt == chroma.GenericInserted:
		return st.Foreground(t.P.Acc3)
	default:
		return st.Foreground(t.P.Acc3)
	}
}
