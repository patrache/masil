package theme

import "charm.land/lipgloss/v2"

// Hex palettes for the themes (CA 5.1 table, mockup CSS variables).
var (
	charmPalette = Palette{
		BG: lipgloss.Color("#1a1b26"), FG: lipgloss.Color("#e4e2f5"), Dim: lipgloss.Color("#7a7896"),
		Line: lipgloss.Color("#3b3a55"), Acc: lipgloss.Color("#b794f6"), Acc2: lipgloss.Color("#ff6fae"),
		Acc3: lipgloss.Color("#7ee0c3"), Acc4: lipgloss.Color("#7aa2f7"), Chip: lipgloss.Color("#2a2940"),
		SelBG: lipgloss.Color("#2d2650"), CodeBG: lipgloss.Color("#221f35"),
	}
	midnightPalette = Palette{
		BG: lipgloss.Color("#0f1419"), FG: lipgloss.Color("#c9d1d9"), Dim: lipgloss.Color("#5c6773"),
		Line: lipgloss.Color("#1f2a35"), Acc: lipgloss.Color("#59c2ff"), Acc2: lipgloss.Color("#ffb454"),
		Acc3: lipgloss.Color("#aad94c"), Acc4: lipgloss.Color("#d2a6ff"), Chip: lipgloss.Color("#151d26"),
		SelBG: lipgloss.Color("#13283a"), CodeBG: lipgloss.Color("#121a22"),
	}
	creamPalette = Palette{
		BG: lipgloss.Color("#fdf6ec"), FG: lipgloss.Color("#3a3226"), Dim: lipgloss.Color("#9a8b74"),
		Line: lipgloss.Color("#e2d3bb"), Acc: lipgloss.Color("#e0607e"), Acc2: lipgloss.Color("#5b8def"),
		Acc3: lipgloss.Color("#2f9e6e"), Acc4: lipgloss.Color("#8e6bbf"), Chip: lipgloss.Color("#f3e6d2"),
		SelBG: lipgloss.Color("#fbe3e8"), CodeBG: lipgloss.Color("#f5ead8"),
	}
	amberPalette = Palette{
		BG: lipgloss.Color("#1d2021"), FG: lipgloss.Color("#ebdbb2"), Dim: lipgloss.Color("#928374"),
		Line: lipgloss.Color("#504945"), Acc: lipgloss.Color("#fabd2f"), Acc2: lipgloss.Color("#fb4934"),
		Acc3: lipgloss.Color("#b8bb26"), Acc4: lipgloss.Color("#83a598"), Chip: lipgloss.Color("#32302f"),
		SelBG: lipgloss.Color("#3c3836"), CodeBG: lipgloss.Color("#282828"),
	}
	// stealthPalette (v0.1.3-5) looks like a plain Claude Code dark session from
	// across the room: no own background (Theme.Clear), light-grey text, grey
	// rules and a soft white accent like a border — nothing colored stands out.
	// The other accents are only faint tints so names stay distinguishable.
	// BG stays a real color for text on accents, images and fades.
	stealthPalette = Palette{
		BG: lipgloss.Color("#1a1a1a"), FG: lipgloss.Color("#d4d4d4"), Dim: lipgloss.Color("#808080"),
		Line: lipgloss.Color("#3a3a3a"), Acc: lipgloss.Color("#c8c8c8"), Acc2: lipgloss.Color("#b89a9a"),
		Acc3: lipgloss.Color("#9aae9a"), Acc4: lipgloss.Color("#a2a6bf"), Chip: lipgloss.Color("#262626"),
		SelBG: lipgloss.Color("#2a2a2a"), CodeBG: lipgloss.Color("#212121"),
		Edge: lipgloss.Color("#707070"), // focused borders: plain grey, no accent
	}
)
