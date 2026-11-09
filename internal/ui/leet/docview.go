package leet

import (
	"strconv"
	"strings"

	"github.com/patrache/masil/internal/leetcode/htmldoc"
	"github.com/patrache/masil/internal/markup"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
)

// docView draws a problem body ([]htmldoc.Block) as styled lines through
// markup.Render: a paragraph per block, list items with "• " / "N. " and
// Depth*2 cells of indent, <pre> as a code block, an image as a link line
// (images are not drawn). The lines are kept until the width, theme or
// image text changes.
type docView struct {
	blocks []htmldoc.Block

	width int
	th    *theme.Theme
	image string
	lines []string
}

func newDocView(blocks []htmldoc.Block) *docView { return &docView{blocks: blocks} }

// Lines is the body at width w (image = the image link text).
func (d *docView) Lines(th *theme.Theme, w int, image string) []string {
	if d == nil || w <= 0 {
		return nil
	}
	if d.lines != nil && d.width == w && d.th == th && d.image == image {
		return d.lines
	}
	d.width, d.th, d.image = w, th, image
	var out []string
	for i, b := range d.blocks {
		if i > 0 && !(isItem(b) && isItem(d.blocks[i-1])) {
			out = append(out, "")
		}
		out = append(out, blockLines(b, th, w, image)...)
	}
	if out == nil {
		out = []string{}
	}
	d.lines = out
	return out
}

func isItem(b htmldoc.Block) bool { return b.Kind == htmldoc.Bullet || b.Kind == htmldoc.Numbered }

// blockLines is one block's lines, each at most w cells.
func blockLines(b htmldoc.Block, th *theme.Theme, w int, image string) []string {
	indent := min(max(b.Depth, 0)*2, max(0, w-4))
	inner := w - indent
	var lines []string
	switch b.Kind {
	case htmldoc.Pre:
		lines = markup.Render(markup.Doc{Blocks: []markup.Block{{Kind: markup.Code, Code: b.Text}}}, th, markup.RenderOptions{Width: inner})
	case htmldoc.Image:
		alt := strings.TrimSpace(textwidth.Sanitize(b.Alt))
		text := image
		if alt != "" {
			text += " " + alt
		}
		lines = []string{th.S.Dim.Render(textwidth.Truncate(text, inner, "…"))}
	default:
		ins := make([]markup.Inline, 0, len(b.Inline)+1)
		switch b.Kind {
		case htmldoc.Bullet:
			ins = append(ins, markup.Inline{Kind: markup.Text, Text: "• "})
		case htmldoc.Numbered:
			ins = append(ins, markup.Inline{Kind: markup.Text, Text: strconv.Itoa(b.N) + ". "})
		}
		for _, in := range b.Inline {
			ins = append(ins, markup.Inline{Kind: markup.InlineKind(in.Kind), Text: in.Text})
		}
		lines = markup.Render(markup.Doc{Blocks: []markup.Block{{Kind: markup.Paragraph, Inline: ins}}}, th, markup.RenderOptions{Width: inner})
	}
	if indent > 0 {
		pad := th.S.Base.Render(strings.Repeat(" ", indent))
		for i := range lines {
			lines[i] = pad + lines[i]
		}
	}
	return lines
}
