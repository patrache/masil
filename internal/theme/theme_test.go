package theme

import (
	"hash/fnv"
	"image/color"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// paletteTable is CA 5.1: token order BG, FG, Dim, Line, Acc, Acc2, Acc3, Acc4, Chip, SelBG, CodeBG.
var paletteTable = map[Name][11]string{
	Charm:    {"#1a1b26", "#e4e2f5", "#7a7896", "#3b3a55", "#b794f6", "#ff6fae", "#7ee0c3", "#7aa2f7", "#2a2940", "#2d2650", "#221f35"},
	Midnight: {"#0f1419", "#c9d1d9", "#5c6773", "#1f2a35", "#59c2ff", "#ffb454", "#aad94c", "#d2a6ff", "#151d26", "#13283a", "#121a22"},
	Cream:    {"#fdf6ec", "#3a3226", "#9a8b74", "#e2d3bb", "#e0607e", "#5b8def", "#2f9e6e", "#8e6bbf", "#f3e6d2", "#fbe3e8", "#f5ead8"},
	Amber:    {"#1d2021", "#ebdbb2", "#928374", "#504945", "#fabd2f", "#fb4934", "#b8bb26", "#83a598", "#32302f", "#3c3836", "#282828"},
}

var tokenNames = [11]string{"BG", "FG", "Dim", "Line", "Acc", "Acc2", "Acc3", "Acc4", "Chip", "SelBG", "CodeBG"}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func tokens(p Palette) [11]color.Color {
	return [11]color.Color{p.BG, p.FG, p.Dim, p.Line, p.Acc, p.Acc2, p.Acc3, p.Acc4, p.Chip, p.SelBG, p.CodeBG}
}

func TestPalettesExact(t *testing.T) {
	for name, hexes := range paletteTable {
		th := Get(name)
		if th == nil || th.Name != name {
			t.Fatalf("Get(%q) = %v", name, th)
		}
		got := tokens(th.P)
		for i, hex := range hexes {
			if !sameColor(got[i], lipgloss.Color(hex)) {
				t.Errorf("%s.%s = %v, want %s", name, tokenNames[i], got[i], hex)
			}
		}
	}
}

func TestMeta(t *testing.T) {
	all := All()
	wantNames := []Name{Charm, Midnight, Cream, Amber}
	wantLetters := []string{"A", "B", "C", "D"}
	wantLabels := []string{"Charm 보라", "미드나잇 블루", "크림 파스텔", "레트로 앰버"}
	wantDark := []bool{true, true, false, true}
	wantBorder := []lipgloss.Border{lipgloss.RoundedBorder(), lipgloss.NormalBorder(), lipgloss.RoundedBorder(), lipgloss.DoubleBorder()}
	if len(all) != 4 {
		t.Fatalf("All() len = %d, want 4", len(all))
	}
	for i, th := range all {
		if th.Name != wantNames[i] || th.Letter != wantLetters[i] || Label(i18n.KoT(), th.Name) != wantLabels[i] || th.Dark != wantDark[i] {
			t.Errorf("All()[%d] = %s/%s/%s/%v, want %s/%s/%s/%v", i, th.Name, th.Letter, Label(i18n.KoT(), th.Name), th.Dark,
				wantNames[i], wantLetters[i], wantLabels[i], wantDark[i])
		}
		if th.Border != wantBorder[i] {
			t.Errorf("%s border = %+v, want %+v", th.Name, th.Border, wantBorder[i])
		}
		if Get(th.Name) != th {
			t.Errorf("Get(%s) differs from All()[%d]", th.Name, i)
		}
	}
	if Default != Amber {
		t.Errorf("Default = %q, want amber", Default)
	}
	if Get(Default).Name != Amber {
		t.Errorf("Get(Default).Name = %q", Get(Default).Name)
	}
	if g := Get("neon"); g == nil || g.Name != Default {
		t.Errorf("Get(unknown) = %v, want default theme", g)
	}
	// All returns a fresh slice: mutating it must not affect later calls.
	all[0] = nil
	if All()[0] == nil {
		t.Error("All() shares its backing slice")
	}
}

func TestParse(t *testing.T) {
	ok := map[string]Name{
		"AMBER": Amber, "d": Amber, "Cream": Cream, "b": Midnight, "charm": Charm,
		"A": Charm, "midnight": Midnight, "c": Cream, " amber ": Amber,
	}
	for in, want := range ok {
		got, valid := Parse(in)
		if !valid || got != want {
			t.Errorf("Parse(%q) = %q,%v want %q,true", in, got, valid, want)
		}
	}
	for _, in := range []string{"neon", "", "e", "amberx"} {
		if got, valid := Parse(in); valid {
			t.Errorf("Parse(%q) = %q,true want not ok", in, got)
		}
	}
}

func TestUserColor(t *testing.T) {
	for _, th := range All() {
		if !sameColor(th.UserColor("u_self", true), th.P.Acc) {
			t.Errorf("%s self color != Acc", th.Name)
		}
		slots := []color.Color{th.P.Acc2, th.P.Acc3, th.P.Acc4}
		seen := map[int]bool{}
		for _, id := range []string{"01JAAAAAAAAAAAAAAAAAAAAAAA", "alice", "bob", "carol", "dave", "erin", "민지", ""} {
			h := fnv.New32a()
			_, _ = h.Write([]byte(id))
			idx := int(h.Sum32() % 3)
			seen[idx] = true
			got := th.UserColor(id, false)
			if !sameColor(got, slots[idx]) {
				t.Errorf("%s UserColor(%q) = %v, want slot %d", th.Name, id, got, idx)
			}
			if !sameColor(got, th.UserColor(id, false)) {
				t.Errorf("%s UserColor(%q) not deterministic", th.Name, id)
			}
		}
		if len(seen) < 2 {
			t.Errorf("test ids hit only %d slot(s)", len(seen))
		}
	}
}

func TestStoryBG(t *testing.T) {
	for _, th := range All() {
		cases := map[string]color.Color{
			"accent": th.P.Acc, "accent2": th.P.Acc2, "accent3": th.P.Acc3, "accent4": th.P.Acc4,
			"plain": th.P.Chip, "unknown": th.P.Chip, "": th.P.Chip,
		}
		for slot, want := range cases {
			if got := th.StoryBG(slot); !sameColor(got, want) {
				t.Errorf("%s StoryBG(%q) = %v, want %v", th.Name, slot, got, want)
			}
		}
	}
}

func TestCodeStyle(t *testing.T) {
	type want struct {
		fg           func(p Palette) color.Color
		bold, italic bool
	}
	fg := func(f func(p Palette) color.Color) want { return want{fg: f} }
	acc := func(p Palette) color.Color { return p.Acc }
	acc2 := func(p Palette) color.Color { return p.Acc2 }
	acc3 := func(p Palette) color.Color { return p.Acc3 }
	acc4 := func(p Palette) color.Color { return p.Acc4 }
	dim := func(p Palette) color.Color { return p.Dim }
	fgc := func(p Palette) color.Color { return p.FG }
	cases := map[chroma.TokenType]want{
		chroma.Keyword:              {fg: acc, bold: true},
		chroma.KeywordDeclaration:   {fg: acc, bold: true},
		chroma.KeywordType:          {fg: acc, bold: true},
		chroma.LiteralString:        fg(acc3),
		chroma.LiteralStringDouble:  fg(acc3),
		chroma.LiteralNumber:        fg(acc2),
		chroma.LiteralNumberInteger: fg(acc2),
		chroma.Comment:              {fg: dim, italic: true},
		chroma.CommentSingle:        {fg: dim, italic: true},
		chroma.NameFunction:         fg(acc4),
		chroma.NameBuiltin:          fg(acc2),
		chroma.Operator:             fg(fgc),
		chroma.Punctuation:          fg(fgc),
		chroma.GenericDeleted:       fg(acc2),
		chroma.GenericInserted:      fg(acc3),
		chroma.Text:                 fg(acc3),
		chroma.Name:                 fg(acc3),
		chroma.NameVariable:         fg(acc3),
	}
	for _, th := range All() {
		for tt, w := range cases {
			st := th.CodeStyle(tt)
			if !sameColor(st.GetForeground(), w.fg(th.P)) {
				t.Errorf("%s CodeStyle(%s) fg = %v, want %v", th.Name, tt, st.GetForeground(), w.fg(th.P))
			}
			if st.GetBold() != w.bold || st.GetItalic() != w.italic {
				t.Errorf("%s CodeStyle(%s) bold/italic = %v/%v, want %v/%v", th.Name, tt, st.GetBold(), st.GetItalic(), w.bold, w.italic)
			}
			if !sameColor(st.GetBackground(), th.P.CodeBG) {
				t.Errorf("%s CodeStyle(%s) bg = %v, want CodeBG", th.Name, tt, st.GetBackground())
			}
		}
	}
}

// styleFields returns the Styles fields in declaration order.
func styleFields(s Styles) []struct {
	Name  string
	Style lipgloss.Style
} {
	v := reflect.ValueOf(s)
	out := make([]struct {
		Name  string
		Style lipgloss.Style
	}, 0, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		out = append(out, struct {
			Name  string
			Style lipgloss.Style
		}{v.Type().Field(i).Name, v.Field(i).Interface().(lipgloss.Style)})
	}
	return out
}

func TestStylesInheritBase(t *testing.T) {
	bgOwner := map[string]func(p Palette) color.Color{
		"Selected": func(p Palette) color.Color { return p.SelBG }, "Cursorline": func(p Palette) color.Color { return p.SelBG },
		"NewHighlight": func(p Palette) color.Color { return p.SelBG }, "Mention": func(p Palette) color.Color { return p.SelBG },
		"MentionMe": func(p Palette) color.Color { return p.SelBG },
		"Badge":     func(p Palette) color.Color { return p.Acc2 }, "BadgeMention": func(p Palette) color.Color { return p.Acc2 },
		"Pill": func(p Palette) color.Color { return p.Acc }, "TabActive": func(p Palette) color.Color { return p.Acc },
		"InlineCode": func(p Palette) color.Color { return p.CodeBG }, "CodeBlock": func(p Palette) color.Color { return p.CodeBG },
		"CodeBar": func(p Palette) color.Color { return p.CodeBG },
		// Chip-backed per plan Task 16 Step 3 (status bar, reactions).
		"StatusBar": func(p Palette) color.Color { return p.Chip }, "StatusHint": func(p Palette) color.Color { return p.Chip },
		"Reaction": func(p Palette) color.Color { return p.Chip }, "ReactionMine": func(p Palette) color.Color { return p.Chip },
		// drag selection: reverse video (fg as bg), unlike any Cursorline
		"TextSelect": func(p Palette) color.Color { return p.FG },
	}
	borderFG := map[string]func(p Palette) color.Color{
		"PaneFocused":   func(p Palette) color.Color { return p.Acc },
		"PaneBlurred":   func(p Palette) color.Color { return p.Line },
		"Input":         func(p Palette) color.Color { return p.Line },
		"InputFocused":  func(p Palette) color.Color { return p.Acc },
		"OverlayBorder": func(p Palette) color.Color { return p.Acc },
	}
	for _, th := range All() {
		fields := styleFields(th.S)
		if len(fields) != 42 {
			t.Fatalf("Styles has %d fields, test expects 42", len(fields))
		}
		for _, f := range fields {
			want := th.P.BG
			if o, ok := bgOwner[f.Name]; ok {
				want = o(th.P)
			}
			if !sameColor(f.Style.GetBackground(), want) {
				t.Errorf("%s S.%s bg = %v, want %v", th.Name, f.Name, f.Style.GetBackground(), want)
			}
			if f.Style.GetForeground() == nil {
				t.Errorf("%s S.%s has no foreground", th.Name, f.Name)
			}
			if bf, ok := borderFG[f.Name]; ok {
				if f.Style.GetBorderStyle() != th.Border {
					t.Errorf("%s S.%s border = %+v, want theme border", th.Name, f.Name, f.Style.GetBorderStyle())
				}
				if !sameColor(f.Style.GetBorderTopForeground(), bf(th.P)) || !sameColor(f.Style.GetBorderLeftForeground(), bf(th.P)) {
					t.Errorf("%s S.%s border fg = %v", th.Name, f.Name, f.Style.GetBorderTopForeground())
				}
				if !sameColor(f.Style.GetBorderTopBackground(), th.P.BG) || !sameColor(f.Style.GetBorderBottomBackground(), th.P.BG) {
					t.Errorf("%s S.%s border bg = %v, want BG", th.Name, f.Name, f.Style.GetBorderTopBackground())
				}
			} else if f.Style.GetBorderStyle() != (lipgloss.Border{}) {
				t.Errorf("%s S.%s unexpectedly has a border", th.Name, f.Name)
			}
		}
		s := th.S
		checks := []struct {
			name string
			got  color.Color
			want color.Color
		}{
			{"Base.fg", s.Base.GetForeground(), th.P.FG},
			{"Dim.fg", s.Dim.GetForeground(), th.P.Dim},
			{"Title.fg", s.Title.GetForeground(), th.P.Acc},
			{"Subtitle.fg", s.Subtitle.GetForeground(), th.P.Dim},
			{"Selected.fg", s.Selected.GetForeground(), th.P.Acc},
			{"Cursorline.fg", s.Cursorline.GetForeground(), th.P.Acc},
			{"Badge.fg", s.Badge.GetForeground(), th.P.BG},
			{"BadgeMention.fg", s.BadgeMention.GetForeground(), th.P.BG},
			{"Pill.fg", s.Pill.GetForeground(), th.P.BG},
			{"TabActive.fg", s.TabActive.GetForeground(), th.P.BG},
			{"Tab.fg", s.Tab.GetForeground(), th.P.Dim},
			{"StatusBar.fg", s.StatusBar.GetForeground(), th.P.FG},
			{"StatusHint.fg", s.StatusHint.GetForeground(), th.P.Dim},
			{"Mention.fg", s.Mention.GetForeground(), th.P.Acc},
			{"MentionMe.fg", s.MentionMe.GetForeground(), th.P.Acc},
			{"Link.fg", s.Link.GetForeground(), th.P.Acc4},
			{"Hashtag.fg", s.Hashtag.GetForeground(), th.P.Acc3},
			{"InlineCode.fg", s.InlineCode.GetForeground(), th.P.Acc3},
			{"CodeBar.fg", s.CodeBar.GetForeground(), th.P.Acc},
			{"Reaction.fg", s.Reaction.GetForeground(), th.P.FG},
			{"ReactionMine.fg", s.ReactionMine.GetForeground(), th.P.Acc},
			{"Timestamp.fg", s.Timestamp.GetForeground(), th.P.Dim},
			{"System.fg", s.System.GetForeground(), th.P.Dim},
			{"Deleted.fg", s.Deleted.GetForeground(), th.P.Dim},
			{"Placeholder.fg", s.Placeholder.GetForeground(), th.P.Dim},
			{"Online.fg", s.Online.GetForeground(), th.P.Acc3},
			{"Away.fg", s.Away.GetForeground(), th.P.Acc},
			{"DND.fg", s.DND.GetForeground(), th.P.Acc2},
			{"Offline.fg", s.Offline.GetForeground(), th.P.Dim},
			{"Error.fg", s.Error.GetForeground(), th.P.Acc2},
			{"Warning.fg", s.Warning.GetForeground(), th.P.Acc},
			{"Success.fg", s.Success.GetForeground(), th.P.Acc3},
			{"Overlay.fg", s.Overlay.GetForeground(), th.P.FG},
		}
		for _, c := range checks {
			if !sameColor(c.got, c.want) {
				t.Errorf("%s %s = %v, want %v", th.Name, c.name, c.got, c.want)
			}
		}
		if !s.Bold.GetBold() || !s.Title.GetBold() || !s.Pill.GetBold() || !s.TabActive.GetBold() || !s.MentionMe.GetBold() {
			t.Errorf("%s: Bold/Title/Pill/TabActive/MentionMe must be bold", th.Name)
		}
		if s.Mention.GetBold() {
			t.Errorf("%s: Mention must not be bold", th.Name)
		}
		if !s.Deleted.GetItalic() {
			t.Errorf("%s: Deleted must be italic", th.Name)
		}
		if !s.Link.GetUnderline() {
			t.Errorf("%s: Link must be underlined", th.Name)
		}
	}
}

// swatchLine renders one style sample exactly 80 cells wide with no unstyled cells.
func swatchLine(th *Theme, name string, st lipgloss.Style) string {
	const w = 80
	text := name + ": sample 가나 👍"
	var line string
	if b := st.GetBorderStyle(); b != (lipgloss.Border{}) {
		edge := lipgloss.NewStyle().Foreground(st.GetBorderTopForeground()).Background(st.GetBorderTopBackground())
		line = edge.Render(b.TopLeft+b.Top+b.Top+b.TopRight) + th.S.Base.Render(" ") + st.UnsetBorderStyle().Render(text)
	} else {
		line = st.Render(text)
	}
	if gap := w - textwidth.Width(line); gap > 0 {
		line += th.S.Base.Render(strings.Repeat(" ", gap))
	}
	return line
}

func TestSwatchGolden(t *testing.T) {
	for _, th := range All() {
		t.Run(string(th.Name), func(t *testing.T) {
			var lines []string
			for _, f := range styleFields(th.S) {
				lines = append(lines, swatchLine(th, f.Name, f.Style))
			}
			fitted := textwidth.Fit(lines, 80, len(lines))
			for i, l := range fitted {
				if w := textwidth.Width(l); w != 80 {
					t.Fatalf("line %d width %d, want 80", i, w)
				}
			}
			golden.RequireEqual(t, strings.Join(fitted, "\n")+"\n")
		})
	}
}

// TestTextSelectIsReverse: the drag-selection style is the theme reversed, so
// it reads in every theme and never looks like the cursor line (SelBG).
func TestTextSelectIsReverse(t *testing.T) {
	for _, th := range All() {
		st := th.S.TextSelect
		if !sameColor(st.GetForeground(), th.P.BG) || !sameColor(st.GetBackground(), th.P.FG) {
			t.Errorf("%s TextSelect fg/bg = %v/%v, want BG/FG", th.Name, st.GetForeground(), st.GetBackground())
		}
		if sameColor(st.GetBackground(), th.P.SelBG) {
			t.Errorf("%s TextSelect looks like the cursor line", th.Name)
		}
	}
}

// The code editors' token colors (v0.1.3-1): CodeStyle's foregrounds with
// no background (the editor is on the pane's own), and plain names and
// text in FG rather than Acc3.
func TestEditorTokenStyle(t *testing.T) {
	for _, th := range All() {
		for _, tt := range []chroma.TokenType{chroma.Keyword, chroma.LiteralString, chroma.LiteralNumber,
			chroma.Comment, chroma.NameFunction, chroma.NameBuiltin, chroma.Operator} {
			st, code := th.EditorTokenStyle(tt), th.CodeStyle(tt)
			if st.GetForeground() != code.GetForeground() || st.GetBold() != code.GetBold() || st.GetItalic() != code.GetItalic() {
				t.Errorf("%s %s: editor style differs from CodeStyle", th.Name, tt)
			}
			if _, none := st.GetBackground().(lipgloss.NoColor); !none {
				t.Errorf("%s %s: editor style has a background %v", th.Name, tt, st.GetBackground())
			}
		}
		for _, tt := range []chroma.TokenType{chroma.Name, chroma.Text, chroma.NameOther} {
			if fg := th.EditorTokenStyle(tt).GetForeground(); fg != th.P.FG {
				t.Errorf("%s %s fg = %v, want FG", th.Name, tt, fg)
			}
		}
	}
}
