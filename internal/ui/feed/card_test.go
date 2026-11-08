package feed

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// halfblockStub stands in for imgrender: rows lines of cols upper half blocks.
func halfblockStub(_ api.Media, cols, rows int) []string {
	out := make([]string, rows)
	for i := range out {
		out[i] = strings.Repeat("▀", cols)
	}
	return out
}

// cardItems is the golden card set: hana (image), dev_kim (#golang), a repost,
// a quote, a poll and a deleted post.
func cardItems() []api.TimelineItem {
	return []api.TimelineItem{
		itemOf(postBy("hana")), itemOf(postBy("dev_kim")), repostItem(),
		itemOf(quotePost()), itemOf(pollPost()), itemOf(deletedPost()),
	}
}

func render(ctx *ui.Context, it api.TimelineItem, o cardOpts) cardView {
	p := it.Post
	return renderCard(ctx, it, &p, o)
}

func assertWidths(t *testing.T, label string, lines []string, w int, exact bool) {
	t.Helper()
	for i, l := range lines {
		sw, wc := ansi.StringWidth(l), ansi.StringWidthWc(l)
		if sw != wc {
			t.Errorf("%s line %d: StringWidth %d != StringWidthWc %d: %q", label, i, sw, wc, ansi.Strip(l))
		}
		if exact && sw != w || !exact && sw > w {
			t.Errorf("%s line %d: width %d, want %s%d: %q", label, i, sw, map[bool]string{true: "", false: "<= "}[exact], w, ansi.Strip(l))
		}
	}
}

func TestCardGolden(t *testing.T) {
	ctx := uitest.New(t).Context()
	cases := []struct {
		name string
		w    int
		d    ui.Density
	}{
		{"command_feed", 54, ui.DensityFull}, {"standard", 92, ui.DensityFull},
		{"compact", 118, ui.DensityLine}, {"widget", 78, ui.DensityBubble}, {"tiny", 50, ui.DensityMinimal}, {"narrow", 24, ui.DensityFull},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var all []string
			for i, it := range cardItems() {
				v := render(ctx, it, cardOpts{Width: c.w, Density: c.d, Selected: i == 0, PollSel: -1, Image: halfblockStub})
				if len(v.Lines) == 0 {
					t.Fatalf("item %d (%s): no lines", i, it.ID)
				}
				assertWidths(t, fmt.Sprintf("item %d", i), v.Lines, c.w, true)
				all = append(all, v.Lines...)
			}
			golden.RequireEqual(t, ansi.Strip(strings.Join(all, "\n")))
		})
	}
}

func cardText(v cardView) string { return ansi.Strip(strings.Join(v.Lines, "\n")) }

func TestCardStatsLine(t *testing.T) {
	ctx := uitest.New(t).Context()
	for _, d := range []ui.Density{ui.DensityFull, ui.DensityBubble} {
		hana := cardText(render(ctx, itemOf(postBy("hana")), cardOpts{Width: 54, Density: d, PollSel: -1}))
		if !strings.Contains(hana, "♥ 12  ↻ 3  💬 4") {
			t.Errorf("density %d: hana card lacks stats line:\n%s", d, hana)
		}
		dev := cardText(render(ctx, itemOf(postBy("dev_kim")), cardOpts{Width: 54, Density: d, PollSel: -1}))
		if !strings.Contains(dev, "♥ 5  💬 2") || strings.Contains(dev, "↻") {
			t.Errorf("density %d: dev_kim card stats wrong (want ♥ 5  💬 2, no ↻):\n%s", d, dev)
		}
	}
	// A quoted post shows its quote count; my own repost keeps ↻ even at zero.
	p := postBy("dev_kim")
	p.QuoteCount = 2
	p.Viewer.Reposted = true
	got := cardText(render(ctx, itemOf(p), cardOpts{Width: 54, Density: ui.DensityFull, PollSel: -1}))
	if !strings.Contains(got, "♥ 5  ↻ 0  💬 2  ❝ 2") {
		t.Errorf("quote count / own repost stats wrong:\n%s", got)
	}
}

func TestCardHashtagHit(t *testing.T) {
	ctx := uitest.New(t).Context()
	for _, d := range []ui.Density{ui.DensityFull, ui.DensityBubble, ui.DensityLine} {
		v := render(ctx, itemOf(postBy("dev_kim")), cardOpts{Width: 92, Density: d, PollSel: -1})
		found := false
		for _, h := range v.Hits {
			if h.Kind != hitHashtag || h.Value != "golang" {
				continue
			}
			found = true
			if got := ansi.Strip(ansi.Cut(v.Lines[h.Line], h.X0, h.X1)); got != "#golang" {
				t.Errorf("density %d: hashtag hit %+v covers %q, want #golang", d, h, got)
			}
		}
		if !found {
			t.Errorf("density %d: no hashtag hit for golang in %+v", d, v.Hits)
		}
	}
}

func TestCardPollHits(t *testing.T) {
	ctx := uitest.New(t).Context()
	v := render(ctx, itemOf(pollPost()), cardOpts{Width: 54, Density: ui.DensityFull, PollSel: 1})
	var vals []string
	for _, h := range v.Hits {
		if h.Kind != hitPollOption {
			continue
		}
		vals = append(vals, h.Value)
		line := ansi.Strip(v.Lines[h.Line])
		want := map[string]string{"0": "국밥", "1": "돈까스"}[h.Value]
		if !strings.Contains(line, want) {
			t.Errorf("poll hit %+v is on line %q, want the %s option", h, line, want)
		}
	}
	if strings.Join(vals, ",") != "0,1" {
		t.Fatalf("poll option hits = %v, want [0 1]", vals)
	}
}

func TestCardAuthorAndQuoteHits(t *testing.T) {
	ctx := uitest.New(t).Context()
	q := quotePost()
	v := render(ctx, itemOf(q), cardOpts{Width: 54, Density: ui.DensityFull, PollSel: -1})
	var author, quote bool
	for _, h := range v.Hits {
		switch {
		case h.Kind == hitAuthor && h.Value == q.Author.ID:
			author = strings.Contains(ansi.Strip(ansi.Cut(v.Lines[h.Line], h.X0, h.X1)), "@minji")
		case h.Kind == hitQuote && h.Value == postBy("hana").ID:
			quote = true
		}
	}
	if !author || !quote {
		t.Fatalf("hits author=%v quote=%v: %+v", author, quote, v.Hits)
	}
	if text := cardText(v); !strings.Contains(text, "┃") || !strings.Contains(text, "@hana") {
		t.Fatalf("quote box missing:\n%s", text)
	}
}

func TestCardImagePlaceholder(t *testing.T) {
	ctx := uitest.New(t).Context()
	for _, img := range []func(api.Media, int, int) []string{nil, func(api.Media, int, int) []string { return nil }} {
		v := render(ctx, itemOf(postBy("hana")), cardOpts{Width: 54, Density: ui.DensityFull, PollSel: -1, Image: img})
		if !strings.Contains(cardText(v), "이미지 불러오는 중") {
			t.Fatalf("no placeholder:\n%s", cardText(v))
		}
		images := 0
		for _, h := range v.Hits {
			if h.Kind == hitImage && h.Value == "0" {
				images++
			}
		}
		_, rows := imageBox(postBy("hana").Media[0], 52, ui.DensityFull)
		if images != rows {
			t.Fatalf("image hits = %d, want one per image row (%d)", images, rows)
		}
	}
}

func TestCardDeletedAndUnavailableQuote(t *testing.T) {
	ctx := uitest.New(t).Context()
	if got := cardText(render(ctx, itemOf(deletedPost()), cardOpts{Width: 54, Density: ui.DensityFull, PollSel: -1})); !strings.Contains(got, "삭제된 글이에요") {
		t.Fatalf("deleted card:\n%s", got)
	}
	for reason, want := range map[string]string{"deleted": "삭제된 글이에요", "not_visible": "볼 수 없는 글이에요"} {
		p := quotePost()
		p.Quote = &api.QuotedPost{Available: false, ID: sid(99), Reason: reason}
		if got := cardText(render(ctx, itemOf(p), cardOpts{Width: 54, Density: ui.DensityFull, PollSel: -1})); !strings.Contains(got, "┃ "+want) {
			t.Errorf("quote %s: want %q:\n%s", reason, want, got)
		}
	}
}

func TestImageBox(t *testing.T) {
	m := postBy("hana").Media[0] // 64x48
	cases := []struct {
		name       string
		m          api.Media
		contentW   int
		d          ui.Density
		cols, rows int
	}{
		{"full", m, 52, ui.DensityFull, 48, 12},
		{"full narrow", m, 22, ui.DensityFull, 22, 8},
		{"bubble", m, 76, ui.DensityBubble, 30, 11},
		{"no size is 4:3", api.Media{}, 40, ui.DensityFull, 40, 12},
		{"tall clamps", api.Media{Width: ptr(1), Height: ptr(1000)}, 20, ui.DensityFull, 20, 12},
		{"wide clamps", api.Media{Width: ptr(1000), Height: ptr(1)}, 20, ui.DensityFull, 20, 3},
		{"line", m, 100, ui.DensityLine, 0, 0},
		{"minimal", m, 100, ui.DensityMinimal, 0, 0},
		{"no room", m, 0, ui.DensityFull, 0, 0},
	}
	for _, c := range cases {
		if cols, rows := imageBox(c.m, c.contentW, c.d); cols != c.cols || rows != c.rows {
			t.Errorf("%s: imageBox = %d,%d want %d,%d", c.name, cols, rows, c.cols, c.rows)
		}
	}
}

func TestHiddenByBlock(t *testing.T) {
	ctx := uitest.New(t).Context()
	hana, dev := userOf("hana"), userOf("dev_kim")
	if hiddenByBlock(ctx, repostItem()) {
		t.Fatal("nothing blocked yet")
	}
	ctx.Store.Blocks[dev.ID] = fixtureNow
	if !hiddenByBlock(ctx, repostItem()) {
		t.Fatal("repost by a blocked user must hide")
	}
	if hiddenByBlock(ctx, itemOf(postBy("hana"))) {
		t.Fatal("hana's own post is not blocked")
	}
	ctx.Store.Blocks[hana.ID] = fixtureNow
	if !hiddenByBlock(ctx, itemOf(postBy("hana"))) {
		t.Fatal("post by a blocked author must hide")
	}
}

func TestCardHostileWidths(t *testing.T) {
	ctx := uitest.New(t, uitest.WithSeed(func(s *store.Store) { fixtures.Seed(s); fixtures.Hostile(s) })).Context()
	densities := []ui.Density{ui.DensityFull, ui.DensityLine, ui.DensityBubble, ui.DensityMinimal}
	items := append(fixtures.HostileTimeline(), cardItems()...)
	for i, it := range items {
		for w := 1; w <= 80; w++ {
			for _, d := range densities {
				for _, img := range []func(api.Media, int, int) []string{nil, halfblockStub} {
					v := render(ctx, it, cardOpts{Width: w, Density: d, Selected: w%2 == 0, PollSel: w % 3, Image: img})
					label := fmt.Sprintf("item %d w=%d d=%d", i, w, d)
					assertWidths(t, label, v.Lines, w, true)
					for _, h := range v.Hits {
						if h.Line < 0 || h.Line >= len(v.Lines) || h.X0 < 0 || h.X1 > w || h.X0 >= h.X1 {
							t.Errorf("%s: hit out of bounds %+v (%d lines)", label, h, len(v.Lines))
						}
					}
				}
			}
		}
	}
}
