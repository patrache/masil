package leet

import (
	"errors"
	"math"
	"strings"

	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// The result slot (A10.4): a run shows each example as 입력 / 내 출력 /
// 기대 under "✓ 예제 3/3 · 0 ms"; a submit shows "✅ Accepted · 상위 15% ·
// 183 ms" or "✗ Wrong Answer · 57/63 · 실패한 입력…" with that input.
// Compile and runtime errors fold to their first line (v unfolds; folded
// by default). Every LeetCode string is sanitized; status words stay as
// LeetCode wrote them.

// foldedLines is how many lines a folded error keeps.
const foldedLines = 1

// topPercent is the "상위 N%" of a runtime percentile (beats p% → top
// 100-p, rounded up, at least 1).
func topPercent(p float64) int {
	return max(1, min(100, int(math.Ceil(100-p-1e-9))))
}

// valueLine is one value of a result ("" fields split by line breaks).
func valueLine(s string) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return textwidth.Sanitize(strings.Join(parts, ", "))
}

// summaryLine is the result's first line ("" = nothing to show).
func (in *instance) summaryLine(ctx *ui.Context) string {
	lines := in.resultText(ctx)
	if len(lines) == 0 {
		return ""
	}
	return lines[0].text
}

// rline is one result line with its style.
type rline struct {
	text  string
	style func(th *theme.Theme) func(...string) string
	warn  bool // a notice (wait, block, timeout…): also the bottom row, never cut
}

func dimSt(th *theme.Theme) func(...string) string  { return th.S.Dim.Render }
func baseSt(th *theme.Theme) func(...string) string { return th.S.Base.Render }
func okSt(th *theme.Theme) func(...string) string   { return th.S.Success.Render }
func badSt(th *theme.Theme) func(...string) string  { return th.S.Error.Render }
func warnSt(th *theme.Theme) func(...string) string { return th.S.Warning.Render }

// resultText is the result as lines (unwrapped, unstyled text).
func (in *instance) resultText(ctx *ui.Context) []rline {
	t := ctx.T
	var out []rline
	if n := in.waitLeft(ctx); n > 0 {
		out = append(out, rline{t.Leetcode.WaitSeconds(n), warnSt, true})
	}
	j := in.judge
	if j == nil {
		return out
	}
	switch j.phase {
	case jSending, jChecking:
		s := t.Leetcode.Running()
		if j.kind == kindSubmit {
			s = t.Leetcode.Submitting()
		}
		return append(out, rline{s, dimSt, false})
	case jFailed:
		switch {
		case j.timedOut:
			return append(out, rline{t.Leetcode.CheckTimeout(), warnSt, true})
		case errorsIsBlocked(j.err):
			return append(out, rline{t.Leetcode.Blocked(), warnSt, true})
		case errorsIsRate(j.err):
			return append(out, rline{t.Leetcode.RateLimited(), warnSt, true})
		}
		return append(out, rline{t.Leetcode.JudgeFailed(), warnSt, true})
	}
	r := j.res
	status := textwidth.Sanitize(r.StatusMsg)
	if errText := firstNonBlank(r.CompileError, r.RuntimeError); errText != "" {
		out = append(out, rline{t.Leetcode.ErrorLine(status), badSt, false})
		lines := strings.Split(strings.TrimRight(errText, "\n"), "\n")
		folded := (ctx.Mode == layout.ModeCompact) != j.toggled // compact: folded until v
		if folded && len(lines) > foldedLines {
			for _, l := range lines[:foldedLines] {
				out = append(out, rline{textwidth.Sanitize(l), dimSt, false})
			}
			return append(out, rline{t.Leetcode.ExpandHint(), dimSt, false})
		}
		for _, l := range lines {
			out = append(out, rline{textwidth.Sanitize(l), dimSt, false})
		}
		if len(lines) > foldedLines {
			out = append(out, rline{t.Leetcode.CollapseHint(), dimSt, false})
		}
		return out
	}
	runtime := textwidth.Sanitize(r.StatusRuntime)
	if j.kind == kindRun {
		n := min(len(r.CodeAnswer), len(r.ExpectedAnswer))
		ok := 0
		for i := range n {
			if r.CodeAnswer[i] == r.ExpectedAnswer[i] {
				ok++
			}
		}
		mark, st := "✓", okSt
		if ok != n || n == 0 {
			mark, st = "✗", badSt
		}
		out = append(out, rline{t.Leetcode.RunSummary(mark, ok, n, runtime), st, false})
		for i := range n {
			input := ""
			if j.q != nil && i < len(j.q.ExampleTestcases) {
				input = valueLine(j.q.ExampleTestcases[i])
			}
			st := baseSt
			if r.CodeAnswer[i] != r.ExpectedAnswer[i] {
				st = badSt
			}
			out = append(out,
				rline{t.Leetcode.ResultInput() + "  " + input, dimSt, false},
				rline{t.Leetcode.ResultOutput() + "  " + valueLine(r.CodeAnswer[i]), st, false},
				rline{t.Leetcode.ResultExpected() + "  " + valueLine(r.ExpectedAnswer[i]), baseSt, false})
		}
		return out
	}
	if r.StatusMsg == "Accepted" {
		if r.RuntimePercentile != nil {
			out = append(out, rline{t.Leetcode.AcceptedLine(status, topPercent(*r.RuntimePercentile), runtime), okSt, false})
		} else {
			out = append(out, rline{t.Leetcode.AcceptedLineShort(status, runtime), okSt, false})
		}
		if in.accepted != nil {
			out = append(out, rline{t.Leetcode.ShareHint(), dimSt, false})
		}
		return out
	}
	return append(out,
		rline{t.Leetcode.SubmitFailed(status, r.TotalCorrect, r.TotalTestcases), badSt, false},
		rline{t.Leetcode.ResultInput() + "  " + valueLine(r.LastTestcase), dimSt, false},
		rline{t.Leetcode.ResultOutput() + "  " + valueLine(r.CodeOutput), badSt, false},
		rline{t.Leetcode.ResultExpected() + "  " + valueLine(r.ExpectedOutput), baseSt, false})
}

// resultBlock is the result drawn at width w: notices wrapped whole (or
// left out: the bottom row has them), the rest cut to w.
func (in *instance) resultBlock(ctx *ui.Context, th *theme.Theme, w int, skipNotices bool) []string {
	var out []string
	for _, l := range in.resultText(ctx) {
		switch {
		case l.warn && skipNotices:
		case l.warn:
			for _, x := range textwidth.Wrap(textwidth.Sanitize(l.text), w) {
				out = append(out, l.style(th)(x))
			}
		default:
			out = append(out, l.style(th)(textwidth.Truncate(textwidth.Sanitize(l.text), w, "…")))
		}
	}
	return out
}

// foot is the bottom rows: the judge's notices, wrapped whole, else the
// key hints.
func (in *instance) foot(ctx *ui.Context, th *theme.Theme, hint string, w int) []string {
	var out []string
	for _, l := range in.resultText(ctx) {
		if l.warn {
			for _, x := range textwidth.Wrap(textwidth.Sanitize(l.text), w) {
				out = append(out, l.style(th)(x))
			}
		}
	}
	if len(out) == 0 {
		out = []string{in.hints(ctx, th, hint, w)}
	}
	return out
}

func firstNonBlank(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func errorsIsBlocked(err error) bool { return errors.Is(err, leetcode.ErrBlocked) }
func errorsIsRate(err error) bool    { return errors.Is(err, leetcode.ErrRateLimited) }
