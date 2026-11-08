package widgets

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
)

// PollView renders bars "국밥 ████████░░ 4", chat + feed.
//
// Line 0: question · "N명 참여" (+ " (마감)" when closed). Then one line per option:
// "✓ " for my votes (else two blanks), the label padded to the longest label (capped at
// 40% of width), a bar of █ (acc) / ░ (line) cells and the vote count right-aligned.
// Filled cells = round(bar * votes / max(voters, top votes, 1)). The option at slice
// position selected gets the Selected background. Every line is at most width cells.
func PollView(t *theme.Theme, p api.Poll, width int, selected int) []string {
	if width <= 0 {
		return nil
	}
	lines := []string{pollHeader(t, p, width)}

	labels := make([]string, len(p.Options))
	maxLabel, maxVotes, countW := 0, 0, 1
	for i, o := range p.Options {
		labels[i] = textwidth.Sanitize(o.Text)
		maxLabel = max(maxLabel, textwidth.Width(labels[i]))
		maxVotes = max(maxVotes, o.VoteCount)
		countW = max(countW, len(strconv.Itoa(max(0, o.VoteCount))))
	}
	labelW := min(maxLabel, width*40/100)
	denom := max(p.VoterCount, maxVotes, 1)
	const markW = 2 // "✓ " or "  "
	barW := width - markW - labelW - 1 - 1 - countW
	if barW < 1 {
		barW = 0
	}

	for i, o := range p.Options {
		sel := i == selected
		on := func(s lipgloss.Style) lipgloss.Style {
			if sel {
				return s.Background(t.P.SelBG)
			}
			return s
		}
		label := on(t.S.Base)
		if sel {
			label = t.S.Selected
		}
		mark := label.Render("  ")
		if slices.Contains(p.MyVotes, o.Idx) {
			mark = on(t.S.Success).Render("✓ ")
		}
		var b strings.Builder
		b.WriteString(mark)
		lw := min(labelW, max(0, width-markW))
		b.WriteString(label.Render(textwidth.PadRight(textwidth.Truncate(labels[i], lw, "…"), lw)))
		if barW > 0 {
			votes := min(max(0, o.VoteCount), denom)
			full := (barW*votes*2 + denom) / (2 * denom)
			b.WriteString(label.Render(" "))
			b.WriteString(on(t.S.Base.Foreground(t.P.Acc)).Render(strings.Repeat("█", full)))
			b.WriteString(on(t.S.Base.Foreground(t.P.Line)).Render(strings.Repeat("░", barW-full)))
		}
		b.WriteString(label.Render(" "))
		b.WriteString(on(t.S.Dim).Render(textwidth.PadLeft(strconv.Itoa(max(0, o.VoteCount)), countW)))
		lines = append(lines, textwidth.Truncate(b.String(), width, ""))
	}
	return lines
}

// pollHeader is "question · N명 참여[ (마감)]", the question truncated first.
func pollHeader(t *theme.Theme, p api.Poll, width int) string {
	suffix := " · " + strconv.Itoa(max(0, p.VoterCount)) + "명 참여"
	if p.Closed {
		suffix += " (마감)"
	}
	q := textwidth.Sanitize(p.Question)
	qw := width - textwidth.Width(suffix)
	if qw < 2 {
		return textwidth.Truncate(t.S.Bold.Render(q)+t.S.Dim.Render(suffix), width, "…")
	}
	return t.S.Bold.Render(textwidth.Truncate(q, qw, "…")) + t.S.Dim.Render(suffix)
}
