package app

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
)

// The decoy screen (v0.1.3-5, owner): ctrl+g swaps the whole window for what
// looks like a coding-agent session at work in a terminal, and ctrl+g again
// brings masil back exactly as it was. While it shows, every other key, mouse
// event and paste is swallowed (ctrl+c still quits) and toasts stay hidden, so
// nothing of the chat leaks onto the screen. It draws in the terminal's own
// background with fixed plain colors, whatever the theme.

// bossTick is the decoy's animation step.
const bossTick = 200 * time.Millisecond

// bossStepTicks is how many ticks pass before the next transcript line shows.
const bossStepTicks = 12

// bossKeep caps the transcript entries rendered (older ones scrolled away).
const bossKeep = 80

// bossTickMsg advances the decoy of generation gen.
type bossTickMsg struct{ gen int }

// bossScreen is the decoy's state: ticks since it opened.
type bossScreen struct {
	gen   int
	ticks int
}

var (
	bossFG     = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))
	bossDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
	bossBullet = lipgloss.NewStyle().Foreground(lipgloss.Color("#4eba65"))
	bossBox    = lipgloss.NewStyle().Foreground(lipgloss.Color("#5f5f5f"))
	bossSpin   = lipgloss.NewStyle().Foreground(lipgloss.Color("#d77757"))
)

// bossPrompt is the request the session appears to be working on.
const bossPrompt = "refactor the session cache so expired entries are evicted lazily, and keep the tests green"

// bossScript is the transcript, one entry per step; it loops after the last.
// kind: "say" (⏺ prose), "tool" (⏺ Tool(arg)), "out" (⎿ result).
var bossScript = []struct{ kind, text string }{
	{"say", "I'll start by looking at how the session cache is structured."},
	{"tool", "Read(internal/cache/session.go)"},
	{"out", "Read 214 lines"},
	{"tool", "Search(pattern: \"evict|expire\", path: \"internal\")"},
	{"out", "Found 6 files"},
	{"tool", "Read(internal/cache/session_test.go)"},
	{"out", "Read 388 lines"},
	{"say", "Expired entries are swept by a ticker today. I'll drop the sweeper and check expiry on read instead, then make Len() skip stale entries."},
	{"tool", "Update(internal/cache/session.go)"},
	{"out", "Updated internal/cache/session.go with 31 additions and 44 removals"},
	{"tool", "Update(internal/cache/session_test.go)"},
	{"out", "Updated internal/cache/session_test.go with 22 additions and 9 removals"},
	{"tool", "Bash(go test ./internal/cache/...)"},
	{"out", "FAIL: TestLenSkipsExpired (0.00s) — got 3, want 2"},
	{"say", "Len() still counts the entry that expires exactly at the boundary. The comparison should be inclusive."},
	{"tool", "Update(internal/cache/session.go)"},
	{"out", "Updated internal/cache/session.go with 1 addition and 1 removal"},
	{"tool", "Bash(go test ./internal/cache/... -race)"},
	{"out", "ok  	internal/cache	1.204s"},
	{"tool", "Bash(go vet ./... && go test ./...)"},
	{"out", "ok  	(48 packages)"},
	{"say", "All green. Next I'll look at the callers that relied on the sweeper's metrics."},
	{"tool", "Search(pattern: \"cache.Sweep\", path: \".\")"},
	{"out", "Found 3 files"},
	{"tool", "Read(internal/server/metrics.go)"},
	{"out", "Read 156 lines"},
}

// bossVerbs rotate on the spinner line like a working agent's status.
var bossVerbs = []string{"Thinking", "Reading", "Pondering", "Refactoring", "Considering", "Working", "Checking"}

// bossSpinFrames is the spinner glyph cycle.
var bossSpinFrames = []string{"·", "✢", "✳", "✶", "✻", "✽", "✻", "✶", "✳", "✢"}

// toggleBoss opens the decoy (and starts its ticks) or closes it.
func (m *Model) toggleBoss() tea.Cmd {
	if m.boss != nil {
		m.boss = nil
		return nil
	}
	m.bossGen++
	m.boss = &bossScreen{gen: m.bossGen}
	return ui.After(m.ctx, bossTick, bossTickMsg{gen: m.bossGen})
}

// onBossTick advances an open decoy and re-arms its tick.
func (m *Model) onBossTick(msg bossTickMsg) tea.Cmd {
	if m.boss == nil || msg.gen != m.boss.gen {
		return nil
	}
	m.boss.ticks++
	return ui.After(m.ctx, bossTick, bossTickMsg{gen: msg.gen})
}

// lines renders the decoy in exactly w×h cells.
func (b *bossScreen) lines(w, h int) []string {
	inner := max(w-4, 1)
	var body []string
	add := func(s string) { body = append(body, s) }
	wrap := func(text string, width int) []string { return wrapWords(text, max(width, 1)) }

	for i, l := range wrap(bossPrompt, w-2) {
		if i == 0 {
			add(bossDim.Render("> " + l))
		} else {
			add(bossDim.Render("  " + l))
		}
	}
	add("")
	shown := 1 + b.ticks/bossStepTicks
	for i := max(shown-bossKeep, 0); i < shown; i++ { // only the tail can be on screen
		e := bossScript[i%len(bossScript)]
		switch e.kind {
		case "say":
			for j, l := range wrap(e.text, w-2) {
				if j == 0 {
					add(bossFG.Render("⏺ " + l))
				} else {
					add(bossFG.Render("  " + l))
				}
			}
			add("")
		case "tool":
			add(bossBullet.Render("⏺ ") + bossFG.Bold(true).Render(e.text))
		case "out":
			add(bossDim.Render("  ⎿  " + e.text))
			add("")
		}
	}

	secs := b.ticks * int(bossTick/time.Millisecond) / 1000
	tokens := 1200 + b.ticks*37
	verb := bossVerbs[(b.ticks/30)%len(bossVerbs)]
	spin := bossSpinFrames[b.ticks%len(bossSpinFrames)]
	status := bossSpin.Render(spin+" "+verb+"…") + bossDim.Render(fmt.Sprintf(" (%ds · ↑ %.1fk tokens · esc to interrupt)", secs, float64(tokens)/1000))
	foot := []string{
		status,
		"",
		bossBox.Render("╭" + strings.Repeat("─", inner+2) + "╮"),
		bossBox.Render("│ ") + bossFG.Render(">") + strings.Repeat(" ", max(inner-1, 0)) + bossBox.Render(" │"),
		bossBox.Render("╰" + strings.Repeat("─", inner+2) + "╯"),
		bossDim.Render("  ? for shortcuts"),
	}
	room := max(h-len(foot), 0)
	if len(body) > room { // the newest lines stay, like a scrolling terminal
		body = body[len(body)-room:]
	}
	out := make([]string, 0, h)
	out = append(out, body...)
	for len(out) < room {
		out = append(out, "")
	}
	out = append(out, foot...)
	if len(out) > h {
		out = out[len(out)-h:]
	}
	return textwidth.Fit(out, w, h)
}

// wrapWords breaks text into lines of at most width cells at spaces.
func wrapWords(text string, width int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = word
		case textwidth.Width(cur)+1+textwidth.Width(word) <= width:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	for i, l := range lines {
		lines[i] = textwidth.Truncate(l, width, "")
	}
	return lines
}
