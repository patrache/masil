package theme

import (
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
)

// CodeStyle maps a chroma token type to a theme style: Keyword=Acc bold, String=Acc3,
// Number=Acc2, Comment=Dim italic, Name.Function=Acc4, Name.Builtin=Acc2,
// Operator/Punctuation=FG, Generic.Deleted=Acc2, Generic.Inserted=Acc3, other=Acc3.
func (t *Theme) CodeStyle(tt chroma.TokenType) lipgloss.Style {
	// TODO(core): Task 16
	return lipgloss.Style{}
}
