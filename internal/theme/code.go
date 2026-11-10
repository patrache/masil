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
	st, _ := t.codeToken(t.S.Base.Background(t.P.CodeBG), tt)
	return st
}

// EditorTokenStyle is a token's style in the code editors (v0.1.3-1):
// CodeStyle's colors without a background, and names, text and other
// unclassified tokens in FG (a buffer is mostly names).
func (t *Theme) EditorTokenStyle(tt chroma.TokenType) lipgloss.Style {
	st, known := t.codeToken(lipgloss.NewStyle(), tt)
	if !known {
		return lipgloss.NewStyle().Foreground(t.P.FG)
	}
	return st
}

// codeToken styles tt on st; known is false for the "other" fallback.
func (t *Theme) codeToken(st lipgloss.Style, tt chroma.TokenType) (lipgloss.Style, bool) {
	switch {
	case tt.InCategory(chroma.Keyword):
		return st.Foreground(t.P.Acc).Bold(true), true
	case tt.InSubCategory(chroma.LiteralString):
		return st.Foreground(t.P.Acc3), true
	case tt.InSubCategory(chroma.LiteralNumber):
		return st.Foreground(t.P.Acc2), true
	case tt.InCategory(chroma.Comment):
		return st.Foreground(t.P.Dim).Italic(true), true
	case tt.InSubCategory(chroma.NameFunction):
		return st.Foreground(t.P.Acc4), true
	case tt.InSubCategory(chroma.NameBuiltin):
		return st.Foreground(t.P.Acc2), true
	case tt.InCategory(chroma.Operator), tt.InCategory(chroma.Punctuation):
		return st.Foreground(t.P.FG), true
	case tt == chroma.GenericDeleted:
		return st.Foreground(t.P.Acc2), true
	case tt == chroma.GenericInserted:
		return st.Foreground(t.P.Acc3), true
	default:
		return st.Foreground(t.P.Acc3), false
	}
}
