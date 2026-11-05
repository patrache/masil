package widgets

import (
	"strconv"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
)

// chipGap separates reaction chips on one line.
const chipGap = "  "

// ReactionChips renders reaction chips wrapped to width.
//
// Chip = sanitized emoji + " " + count (Reaction, or ReactionMine when Me), chips on a
// line are separated by two Base blanks, lines are packed greedily and never wider than
// width (a chip wider than width is truncated). Lines are not padded.
func ReactionChips(t *theme.Theme, rs []api.ReactionSummary, width int) []string {
	if width <= 0 || len(rs) == 0 {
		return nil
	}
	var (
		lines []string
		cur   string
		curW  int
	)
	gap := t.S.Base.Render(chipGap)
	gapW := textwidth.Width(chipGap)
	for _, r := range rs {
		text := textwidth.Sanitize(r.Emoji) + " " + strconv.Itoa(r.Count)
		text = textwidth.Truncate(text, width, "…")
		tw := textwidth.Width(text)
		st := t.S.Reaction
		if r.Me {
			st = t.S.ReactionMine
		}
		chip := st.Render(text)
		switch {
		case curW == 0:
			cur, curW = chip, tw
		case curW+gapW+tw <= width:
			cur += gap + chip
			curW += gapW + tw
		default:
			lines = append(lines, cur)
			cur, curW = chip, tw
		}
	}
	if curW > 0 {
		lines = append(lines, cur)
	}
	return lines
}
